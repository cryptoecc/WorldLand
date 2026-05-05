require("@nomicfoundation/hardhat-toolbox");

const SEOUL_RPC = process.env.SEOUL_RPC || "https://seoul.worldland.foundation";
const GWANGJU_RPC = process.env.GWANGJU_RPC || "https://gwangju.worldland.foundation";
const PK = process.env.DEPLOYER_KEY ? [process.env.DEPLOYER_KEY] : [];

module.exports = {
  solidity: {
    version: "0.8.24",
    settings: {
      optimizer: { enabled: true, runs: 200 },
      evmVersion: "london",
    },
  },
  networks: {
    seoul: { url: SEOUL_RPC, chainId: 103, accounts: PK },
    gwangju: { url: GWANGJU_RPC, chainId: 10395, accounts: PK },
  },
};
