// WorldLand Seoul mainnet + Gwangju testnet definitions for viem/wagmi.
import { defineChain } from "viem";

export const seoul = defineChain({
  id: 103,
  name: "WorldLand Seoul",
  nativeCurrency: { name: "WorldLand", symbol: "WL", decimals: 18 },
  rpcUrls: {
    default: { http: [process.env.NEXT_PUBLIC_SEOUL_RPC || "https://seoul.worldland.foundation"] },
  },
  blockExplorers: {
    default: { name: "Seoul Scan", url: process.env.NEXT_PUBLIC_SEOUL_EXPLORER || "https://scan.worldland.foundation" },
  },
});

export const gwangju = defineChain({
  id: 10395,
  name: "WorldLand Gwangju",
  nativeCurrency: { name: "WorldLand", symbol: "WL", decimals: 18 },
  rpcUrls: {
    default: { http: [process.env.NEXT_PUBLIC_GWANGJU_RPC || "https://gwangju.worldland.foundation"] },
  },
});

export const ASSETS = [
  { label: "WL/USD",  key: "WL/USD"  },
  { label: "BTC/USD", key: "BTC/USD" },
  { label: "ETH/USD", key: "ETH/USD" },
];

export const WORLDBET_ADDRESS = process.env.NEXT_PUBLIC_WORLDBET_ADDRESS || "";
