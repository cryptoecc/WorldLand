#!/usr/bin/env node
/* eslint-disable no-console */
// WorldBet keeper: drives the permissionless lockRound / settleRound
// transitions so users don't have to.
//
// Both calls are open to anyone — the keeper just needs a funded gas
// account. Lock fails until the oracle bot has posted the hourly price;
// the keeper retries on the next tick. After the 30-min grace,
// lockRound / settleRound auto-refund instead of erroring.
//
// Env:
//   RPC_URL          required, JSON-RPC
//   WORLDBET_ADDR    required, deployed WorldBet address
//   KEEPER_KEY       required, 0x... funded gas key (no privileges needed)
//   ASSETS           default "WL/USD,BTC/USD,ETH/USD"
//   POLL_MS          default 30000
//   LOOKBACK_ROUNDS  default 4 (rounds scanned around current)
//   VERBOSE          set to "0" to silence per-round status lines

const { ethers } = require("ethers");

const RPC      = process.env.RPC_URL;
const WB_ADDR  = process.env.WORLDBET_ADDR;
const KEY      = process.env.KEEPER_KEY;
const ASSETS   = (process.env.ASSETS || "WL/USD,BTC/USD,ETH/USD").split(",").map((s) => s.trim());
const POLL_MS  = parseInt(process.env.POLL_MS || "30000", 10);
const LOOKBACK = parseInt(process.env.LOOKBACK_ROUNDS || "4", 10);
const VERBOSE  = process.env.VERBOSE !== "0";

const ABI = [
  "function currentRoundId() view returns (uint64)",
  "function roundView(bytes32 asset, uint64 id, address user) view returns (tuple(uint128 upPool, uint128 downPool, uint64 lockTime, uint64 closeTime, uint128 lockPrice, uint128 closePrice, uint8 status) r, tuple(uint128 upAmount, uint128 downAmount, bool claimed) b)",
  "function lockRound(bytes32 asset, uint64 id)",
  "function settleRound(bytes32 asset, uint64 id)",
  "function oracle() view returns (address)",
];
const ORACLE_ABI = [
  "function priceAt(bytes32 asset, uint64 hourId) view returns (uint128 price, uint64 timestamp, bool posted)",
];

const BENIGN = /oracle pending|lock first|too early|not open|settled|no round/i;

const STATUS_NAMES = ["OPEN", "LOCKED", "UP-WINS", "DOWN-WINS", "REFUND"];

function ts() { return new Date().toISOString(); }
function fmt(s) { return s >= 0 ? `+${s}s` : `${s}s`; }

async function processOne(wb, oracle, label, key, id, now) {
  let r;
  try {
    const out = await wb.roundView(key, id, ethers.ZeroAddress);
    r = out[0];
  } catch (e) {
    console.error(`[${label}] #${id} roundView ERR: ${e.shortMessage || e.message}`);
    return;
  }

  const lockTime  = Number(r.lockTime);
  const closeTime = Number(r.closeTime);
  const status    = Number(r.status);
  const statusName = STATUS_NAMES[status] || `status${status}`;

  // Round was never opened (no bets placed) -> idle.
  if (lockTime === 0) {
    if (VERBOSE) console.log(`  [${label}] #${id} idle (no bets)`);
    return;
  }

  if (VERBOSE) {
    const lockHour = Math.floor(lockTime / 3600);
    const closeHour = Math.floor(closeTime / 3600);
    let oracleNote = "";
    try {
      const lockPosted = (await oracle.priceAt(key, lockHour)).posted;
      const closePosted = (await oracle.priceAt(key, closeHour)).posted;
      oracleNote = ` oracle[lock=${lockPosted ? "Y" : "n"} close=${closePosted ? "Y" : "n"}]`;
    } catch {}
    console.log(
      `  [${label}] #${id} ${statusName}` +
      ` lockTime=${fmt(lockTime - now)} closeTime=${fmt(closeTime - now)}` +
      ` pools=${ethers.formatEther(r.upPool)}/${ethers.formatEther(r.downPool)}` +
      oracleNote
    );
  }

  if (status === 0 && now >= lockTime) {
    try {
      const tx = await wb.lockRound(key, id);
      const rcpt = await tx.wait();
      console.log(`[${label}] LOCK   #${id} tx=${rcpt.hash} block=${rcpt.blockNumber}`);
    } catch (e) {
      const msg = e.shortMessage || e.message || String(e);
      if (BENIGN.test(msg)) {
        if (VERBOSE) console.log(`  [${label}] #${id} lock skipped: ${msg}`);
      } else {
        console.error(`[${label}] LOCK #${id} ERR: ${msg}`);
      }
    }
  }

  if (status < 2 && now >= closeTime) {
    try {
      const tx = await wb.settleRound(key, id);
      const rcpt = await tx.wait();
      console.log(`[${label}] SETTLE #${id} tx=${rcpt.hash} block=${rcpt.blockNumber}`);
    } catch (e) {
      const msg = e.shortMessage || e.message || String(e);
      if (BENIGN.test(msg)) {
        if (VERBOSE) console.log(`  [${label}] #${id} settle skipped: ${msg}`);
      } else {
        console.error(`[${label}] SETTLE #${id} ERR: ${msg}`);
      }
    }
  }
}

async function tick(wb, oracle) {
  const now = Math.floor(Date.now() / 1000);
  let cur;
  try {
    cur = Number(await wb.currentRoundId());
  } catch (e) {
    console.error(`[tick ${ts()}] currentRoundId ERR: ${e.shortMessage || e.message}`);
    return;
  }
  if (VERBOSE) console.log(`[tick ${ts()}] now=${now} currentRoundId=${cur}`);

  for (const label of ASSETS) {
    const key = ethers.id(label);
    for (let off = LOOKBACK; off >= 0; off--) {
      const id = cur - off;
      if (id < 0) continue;
      await processOne(wb, oracle, label, key, id, now);
    }
  }
}

async function main() {
  if (!RPC || !WB_ADDR || !KEY) {
    console.error("env required: RPC_URL, WORLDBET_ADDR, KEEPER_KEY");
    process.exit(1);
  }
  const provider = new ethers.JsonRpcProvider(RPC);
  const signer = new ethers.Wallet(KEY.startsWith("0x") ? KEY : "0x" + KEY, provider);
  const wb = new ethers.Contract(WB_ADDR, ABI, signer);
  const oracleAddr = await wb.oracle();
  const oracle = new ethers.Contract(oracleAddr, ORACLE_ABI, provider);

  const net = await provider.getNetwork();
  console.log(
    `Keeper ${signer.address} chainId=${net.chainId} pollMs=${POLL_MS}` +
    ` lookback=${LOOKBACK} verbose=${VERBOSE}` +
    ` assets=${ASSETS.join(",")}`
  );
  console.log(`WorldBet=${WB_ADDR} oracle=${oracleAddr}`);

  const run = async () => {
    try { await tick(wb, oracle); }
    catch (e) { console.error("tick:", e.message || e); }
  };

  await run();
  setInterval(run, POLL_MS);
}

main().catch((e) => { console.error(e); process.exit(1); });
