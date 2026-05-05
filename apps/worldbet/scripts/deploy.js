// Deploy PriceOracle + WorldBet, register WL/USD, BTC/USD, ETH/USD.
// Env:
//   ORACLE_SIGNERS  comma-separated signer addresses (defaults to deployer)
//   ORACLE_THRESHOLD  M of N (default 2)
//   DEPLOYER_KEY    private key (set via hardhat network accounts)

const fs = require("fs");
const path = require("path");
const { ethers, network } = require("hardhat");

const ASSETS = ["WL/USD", "BTC/USD", "ETH/USD"];

async function main() {
  const [deployer] = await ethers.getSigners();
  console.log(`Network: ${network.name} (chainId ${network.config.chainId})`);
  console.log(`Deployer: ${deployer.address}`);

  const signersCsv = process.env.ORACLE_SIGNERS || deployer.address;
  const signers = signersCsv.split(",").map((s) => s.trim()).filter(Boolean);
  const threshold = parseInt(process.env.ORACLE_THRESHOLD || "2", 10);

  if (signers.length === 1 && threshold > 1) {
    console.log(`Note: ORACLE_SIGNERS has 1 signer; lowering threshold to 1 for dev.`);
  }
  const effThreshold = Math.min(threshold, signers.length);
  console.log(`Oracle: ${effThreshold}-of-${signers.length}`);

  const Oracle = await ethers.getContractFactory("PriceOracle");
  const oracle = await Oracle.deploy(deployer.address, signers, effThreshold);
  await oracle.waitForDeployment();
  const oracleAddr = await oracle.getAddress();
  console.log(`PriceOracle deployed: ${oracleAddr}`);

  const WorldBet = await ethers.getContractFactory("WorldBet");
  const wb = await WorldBet.deploy(oracleAddr, deployer.address);
  await wb.waitForDeployment();
  const wbAddr = await wb.getAddress();
  console.log(`WorldBet deployed:    ${wbAddr}`);

  const assetMap = {};
  for (const name of ASSETS) {
    const key = ethers.id(name); // keccak256
    const tx = await wb.registerAsset(key);
    await tx.wait();
    assetMap[name] = key;
    console.log(`  registered ${name.padEnd(8)} ${key}`);
  }

  // Round-boundary alignment notice for the oracle bot.
  const now = Math.floor(Date.now() / 1000);
  const ROUND = 3600;
  const nextHour = Math.ceil(now / ROUND) * ROUND;
  const firstLockableRound = Math.floor(now / ROUND);
  console.log("");
  console.log(`Current UTC hour:        ${new Date(Math.floor(now / ROUND) * ROUND * 1000).toISOString()}`);
  console.log(`Next UTC hour boundary:  ${new Date(nextHour * 1000).toISOString()} (in ${nextHour - now}s)`);
  console.log(`Oracle bot must post for hourId=${nextHour / ROUND} before that boundary`);
  console.log(`(this becomes the lockPrice for round ${firstLockableRound}).`);

  const out = {
    network: network.name,
    chainId: Number(network.config.chainId),
    deployer: deployer.address,
    oracle: oracleAddr,
    worldbet: wbAddr,
    signers,
    threshold: effThreshold,
    assets: assetMap,
    deployedAt: new Date().toISOString(),
  };
  const outPath = path.join(__dirname, "..", "deployments.json");
  fs.writeFileSync(outPath, JSON.stringify(out, null, 2));
  console.log(`\nWrote ${outPath}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
