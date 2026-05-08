// One-shot diagnostic: print recent round states + oracle posting status
// for every asset. Use this when the keeper appears silent to figure out
// whether the chain is actually idle (most common) or something is stuck.
//
// Usage:
//   npx hardhat run scripts/inspect-rounds.js --network bscTestnet
//   LOOKBACK=8 npx hardhat run scripts/inspect-rounds.js --network bsc

const fs = require("fs");
const path = require("path");
const { ethers, network } = require("hardhat");

const ASSETS = ["WL/USD", "BTC/USD", "ETH/USD"];
const LOOKBACK = parseInt(process.env.LOOKBACK || "6", 10);
const STATUS_NAMES = ["OPEN", "LOCKED", "UP-WINS", "DOWN-WINS", "REFUND"];
const ORACLE_GRACE = 1800;

function fmt(s) { return s >= 0 ? `+${s}s` : `${s}s`; }

async function main() {
  const depPath = path.join(__dirname, "..", "deployments.json");
  if (!fs.existsSync(depPath)) {
    throw new Error("deployments.json not found. Run scripts/deploy.js first.");
  }
  const dep = JSON.parse(fs.readFileSync(depPath, "utf8"));
  if (dep.chainId !== Number(network.config.chainId)) {
    throw new Error(`deployments.json chainId=${dep.chainId} but --network gave ${network.config.chainId}`);
  }

  const wb = await ethers.getContractAt("WorldBet", dep.worldbet);
  const oracle = await ethers.getContractAt("PriceOracle", dep.oracle);

  const block = await ethers.provider.getBlock("latest");
  const now = Number(block.timestamp);
  const cur = Number(await wb.currentRoundId());

  console.log(`network=${network.name} chainId=${dep.chainId}`);
  console.log(`block=${block.number}  block.timestamp=${now} (${new Date(now * 1000).toISOString()})`);
  console.log(`currentRoundId=${cur}\n`);

  for (const label of ASSETS) {
    const key = ethers.id(label);
    console.log(`=== ${label} ===`);
    for (let off = LOOKBACK; off >= 0; off--) {
      const id = cur - off;
      if (id < 0) continue;
      const out = await wb.roundView(key, id, ethers.ZeroAddress);
      const r = out[0];
      const lockTime = Number(r.lockTime);
      const closeTime = Number(r.closeTime);
      const status = Number(r.status);
      const statusName = STATUS_NAMES[status] || `status${status}`;
      const upPool = ethers.formatEther(r.upPool);
      const downPool = ethers.formatEther(r.downPool);

      if (lockTime === 0) {
        console.log(`  #${id}  ${statusName.padEnd(9)}  (idle, no bets)`);
        continue;
      }

      const lockHour = Math.floor(lockTime / 3600);
      const closeHour = Math.floor(closeTime / 3600);
      const lockOracle = await oracle.priceAt(key, lockHour);
      const closeOracle = await oracle.priceAt(key, closeHour);

      console.log(
        `  #${id}  ${statusName.padEnd(9)}` +
        `  lockTime ${fmt(lockTime - now)}  closeTime ${fmt(closeTime - now)}` +
        `  pools=${upPool}/${downPool}`
      );
      console.log(
        `        oracle: lock@${lockHour} ${lockOracle.posted ? `OK px=${lockOracle.price}` : "PENDING"}` +
        `, close@${closeHour} ${closeOracle.posted ? `OK px=${closeOracle.price}` : "PENDING"}`
      );

      // Diagnose what should happen.
      let action = "";
      if (status === 0 && now < lockTime) {
        action = "open for bets";
      } else if (status === 0 && now >= lockTime && lockOracle.posted) {
        action = "*** SHOULD LOCK NOW ***";
      } else if (status === 0 && now >= lockTime && !lockOracle.posted) {
        const grace = lockTime + ORACLE_GRACE;
        action = now >= grace
          ? "*** SHOULD LOCK -> AUTO REFUND (grace expired) ***"
          : `waiting for lock oracle (grace ends in ${grace - now}s)`;
      } else if (status === 1 && now < closeTime) {
        action = "live, awaiting close";
      } else if (status === 1 && now >= closeTime && closeOracle.posted) {
        action = "*** SHOULD SETTLE NOW ***";
      } else if (status === 1 && now >= closeTime && !closeOracle.posted) {
        const grace = closeTime + ORACLE_GRACE;
        action = now >= grace
          ? "*** SHOULD SETTLE -> AUTO REFUND (grace expired) ***"
          : `waiting for close oracle (grace ends in ${grace - now}s)`;
      } else if (status >= 2) {
        action = "finalized (claims open)";
      }
      console.log(`        -> ${action}`);
    }
    console.log();
  }

  console.log("Hint: a round only appears if at least one bet was placed in its hour.");
  console.log("      Expand window with LOOKBACK=N if you want to scan further back.");
}

main().catch((e) => { console.error(e); process.exit(1); });
