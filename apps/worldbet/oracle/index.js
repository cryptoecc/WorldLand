#!/usr/bin/env node
/* eslint-disable no-console */
// WorldBet oracle bot.
//
// Per UTC-hour boundary, sample WL / BTC / ETH against USDT from KuCoin,
// Gate, MEXC, HTX, take the median, sign via EIP-712, and submit to the
// PriceOracle. Threshold M-of-N is enforced on-chain.
//
// Modes:
//   1. Combined (dev / single-host): set SIGNER_KEYS to >= threshold keys
//      and the bot signs with all of them and posts directly.
//   2. Distributed (production): each host runs the bot with one key,
//      posts its signature to PEER_SIG_URLS, and the leader submits
//      once it has >= threshold signatures. Implemented as an HTTP
//      sidecar on PEER_PORT (default 8787); see /sig and /collect.
//
// Env:
//   RPC_URL          required, WorldLand JSON-RPC
//   ORACLE_ADDR      required, deployed PriceOracle address
//   SIGNER_KEYS      comma-separated 0x... keys (combined mode if >=2)
//   ASSETS           default "WL/USD,BTC/USD,ETH/USD"
//   PEER_SIG_URLS    optional, comma-separated peer /sig URLs
//   PEER_PORT        optional, sidecar port (default 8787)
//   POLL_MS          default 60000

const { ethers } = require("ethers");
const http = require("http");
const https = require("https");
const { URL } = require("url");

const RPC = process.env.RPC_URL;
const ORACLE_ADDR = process.env.ORACLE_ADDR;
const SIGNER_KEYS = (process.env.SIGNER_KEYS || "").split(",").map((s) => s.trim()).filter(Boolean);
const ASSETS = (process.env.ASSETS || "WL/USD,BTC/USD,ETH/USD").split(",").map((s) => s.trim());
const PEER_SIG_URLS = (process.env.PEER_SIG_URLS || "").split(",").map((s) => s.trim()).filter(Boolean);
const PEER_PORT = parseInt(process.env.PEER_PORT || "8787", 10);
const POLL_MS = parseInt(process.env.POLL_MS || "60000", 10);

const ORACLE_ABI = [
  "function postPrice(bytes32 asset, uint64 hourId, uint128 price, uint64 timestamp, bytes[] signatures)",
  "function priceAt(bytes32 asset, uint64 hourId) view returns (uint128 price, uint64 timestamp, bool posted)",
  "function threshold() view returns (uint8)",
  "function isSigner(address) view returns (bool)",
];

const TYPES = {
  Price: [
    { name: "asset", type: "bytes32" },
    { name: "hourId", type: "uint64" },
    { name: "price", type: "uint128" },
    { name: "timestamp", type: "uint64" },
  ],
};

const SYM = {
  "WL/USD":  { kucoin: "WL-USDT",  gate: "WL_USDT",  mexc: "WLUSDT",  htx: "wlusdt" },
  "BTC/USD": { kucoin: "BTC-USDT", gate: "BTC_USDT", mexc: "BTCUSDT", htx: "btcusdt" },
  "ETH/USD": { kucoin: "ETH-USDT", gate: "ETH_USDT", mexc: "ETHUSDT", htx: "ethusdt" },
};

// ---- HTTP utility (minimal, no deps) ----

function fetchJSON(url, timeoutMs = 10000) {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const lib = u.protocol === "http:" ? http : https;
    const req = lib.get(url, { timeout: timeoutMs }, (res) => {
      let data = "";
      res.on("data", (c) => (data += c));
      res.on("end", () => {
        try { resolve(JSON.parse(data)); }
        catch (e) { reject(new Error(`bad json from ${url}: ${e.message}`)); }
      });
    });
    req.on("error", reject);
    req.on("timeout", () => { req.destroy(new Error(`timeout ${url}`)); });
  });
}

// ---- CEX adapters ----

async function fromKuCoin(asset) {
  const r = await fetchJSON(`https://api.kucoin.com/api/v1/market/orderbook/level1?symbol=${SYM[asset].kucoin}`);
  if (!r || !r.data || !r.data.price) throw new Error("kucoin empty");
  return Number(r.data.price);
}
async function fromGate(asset) {
  const r = await fetchJSON(`https://api.gateio.ws/api/v4/spot/tickers?currency_pair=${SYM[asset].gate}`);
  if (!Array.isArray(r) || !r[0] || !r[0].last) throw new Error("gate empty");
  return Number(r[0].last);
}
async function fromMexc(asset) {
  const r = await fetchJSON(`https://api.mexc.com/api/v3/ticker/price?symbol=${SYM[asset].mexc}`);
  if (!r || !r.price) throw new Error("mexc empty");
  return Number(r.price);
}
async function fromHtx(asset) {
  const r = await fetchJSON(`https://api.huobi.pro/market/detail/merged?symbol=${SYM[asset].htx}`);
  if (!r || !r.tick || !r.tick.close) throw new Error("htx empty");
  return Number(r.tick.close);
}

async function median(asset) {
  const out = await Promise.allSettled([fromKuCoin(asset), fromGate(asset), fromMexc(asset), fromHtx(asset)]);
  const samples = out
    .map((s, i) => ({ src: ["kucoin", "gate", "mexc", "htx"][i], v: s.status === "fulfilled" ? s.value : null, err: s.reason }))
    .filter((x) => Number.isFinite(x.v) && x.v > 0);
  if (samples.length < 2) {
    throw new Error(`${asset}: only ${samples.length} sources OK`);
  }
  const nums = samples.map((s) => s.v).sort((a, b) => a - b);
  const mid = nums.length % 2 === 0
    ? (nums[nums.length / 2 - 1] + nums[nums.length / 2]) / 2
    : nums[(nums.length - 1) / 2];
  return { median: mid, samples };
}

// ---- EIP-712 ----

async function buildDomain(provider, addr) {
  const net = await provider.getNetwork();
  return {
    name: "WorldBet PriceOracle",
    version: "1",
    chainId: Number(net.chainId),
    verifyingContract: addr,
  };
}

function priceTo1e8(m) {
  // 8-decimal fixed point.
  return ethers.parseUnits(m.toFixed(8), 8);
}

async function signWith(key, domain, value) {
  const w = new ethers.Wallet(key.startsWith("0x") ? key : "0x" + key);
  return w.signTypedData(domain, TYPES, value);
}

// ---- Sidecar (peer signature exchange) ----

const cache = new Map(); // `${asset}|${hourId}` -> { value, sigs: Set<sig> }

function cacheKey(asset, hourId) { return `${asset}|${hourId}`; }

function startSidecar(domain) {
  const server = http.createServer(async (req, res) => {
    if (req.method === "GET" && req.url.startsWith("/sig")) {
      const u = new URL(req.url, `http://localhost:${PEER_PORT}`);
      const asset = u.searchParams.get("asset");
      const hourId = u.searchParams.get("hourId");
      const ck = cacheKey(asset, hourId);
      const entry = cache.get(ck);
      if (!entry) { res.writeHead(404); res.end("no entry"); return; }
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ value: entry.value, sigs: [...entry.sigs] }));
    } else {
      res.writeHead(404); res.end();
    }
  });
  server.listen(PEER_PORT, () => console.log(`[sidecar] listening :${PEER_PORT}`));
}

async function pullPeerSigs(asset, hourId) {
  if (PEER_SIG_URLS.length === 0) return [];
  const urls = PEER_SIG_URLS.map((u) => `${u.replace(/\/$/, "")}/sig?asset=${encodeURIComponent(asset)}&hourId=${hourId}`);
  const results = await Promise.allSettled(urls.map((u) => fetchJSON(u, 5000)));
  const sigs = [];
  for (const r of results) {
    if (r.status === "fulfilled" && r.value && Array.isArray(r.value.sigs)) {
      for (const s of r.value.sigs) sigs.push(s);
    }
  }
  return sigs;
}

// ---- Per-asset post ----

async function postOne(asset, provider, oracle, domain, threshold) {
  const now = Math.floor(Date.now() / 1000);
  const hourId = Math.floor(now / 3600);
  const key = ethers.id(asset);

  const existing = await oracle.priceAt(key, hourId);
  if (existing.posted) {
    console.log(`[${asset}] hour ${hourId} already posted, skipping`);
    return;
  }

  const { median: m, samples } = await median(asset);
  const price = priceTo1e8(m);
  const value = { asset: key, hourId, price, timestamp: now };

  const ck = cacheKey(asset, hourId);
  const entry = cache.get(ck) || { value, sigs: new Set() };
  // refresh value (median may shift slightly between reads, keep first)
  if (!cache.has(ck)) cache.set(ck, entry);

  // Sign locally with all available keys.
  for (const k of SIGNER_KEYS) {
    const sig = await signWith(k, domain, entry.value);
    entry.sigs.add(sig);
  }

  // Pull peer sigs if configured.
  const peer = await pullPeerSigs(asset, hourId);
  for (const s of peer) entry.sigs.add(s);

  const sigList = [...entry.sigs];
  console.log(`[${asset}] hour=${hourId} median=${m.toFixed(6)} samples=${samples.map(s => `${s.src}:${s.v}`).join(",")} sigs=${sigList.length}/${threshold}`);

  if (sigList.length < threshold) {
    console.log(`[${asset}] waiting for peer signatures`);
    return;
  }

  const tx = await oracle.postPrice(key, hourId, entry.value.price, entry.value.timestamp, sigList);
  const r = await tx.wait();
  console.log(`[${asset}] posted tx=${r.hash} block=${r.blockNumber}`);
  cache.delete(ck);
}

// ---- main ----

async function main() {
  if (!RPC || !ORACLE_ADDR || SIGNER_KEYS.length === 0) {
    console.error("env required: RPC_URL, ORACLE_ADDR, SIGNER_KEYS=key1[,key2,...]");
    process.exit(1);
  }
  const provider = new ethers.JsonRpcProvider(RPC);
  const submitter = new ethers.Wallet(SIGNER_KEYS[0].startsWith("0x") ? SIGNER_KEYS[0] : "0x" + SIGNER_KEYS[0], provider);
  const oracle = new ethers.Contract(ORACLE_ADDR, ORACLE_ABI, submitter);
  const threshold = Number(await oracle.threshold());
  const domain = await buildDomain(provider, ORACLE_ADDR);
  console.log(`Oracle ${ORACLE_ADDR} threshold=${threshold} chainId=${domain.chainId} keys=${SIGNER_KEYS.length} peers=${PEER_SIG_URLS.length}`);

  if (PEER_SIG_URLS.length > 0 || process.env.SIDECAR === "1") startSidecar(domain);

  const tick = async () => {
    for (const a of ASSETS) {
      try { await postOne(a, provider, oracle, domain, threshold); }
      catch (e) { console.error(`[${a}] ${e.message}`); }
    }
  };

  await tick();
  setInterval(tick, POLL_MS);
}

main().catch((e) => { console.error(e); process.exit(1); });
