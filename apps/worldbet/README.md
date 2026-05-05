# WorldBet

Hourly multi-asset pari-mutuel price-prediction market for **WorldLand Seoul mainnet (chainId 103)**.
Designed to drive direct WL buy pressure, deflation via burn, and viral growth via sticky referrals.

## Mechanics

- **Markets**: WL/USD, BTC/USD, ETH/USD
- **Rounds**: 1 hour each, rolling — every UTC hour a new round opens for betting; the prior hour's bets go *live* once the lock price posts; the hour before that *settles* on the close price.
- **Bets**: native WL, UP or DOWN
- **Payout**: pari-mutuel — winners get back their stake plus their pro-rata slice of the loser pool. Contract has zero LP risk.
- **Fees (3% total)**:
  - 1.0% → weekly leaderboard prize pool (`prizePool`, owner-distributed off events)
  - 0.3% → sticky referrer rebate (claimable, falls back to burn if no referrer)
  - 1.7% → `pendingBurn`, anyone can call `burn()` to send to `0x000000000000000000000000000000000000dEaD`
- **Oracle**: 2-of-3 EIP-712 multisig. Bots fetch median spot from KuCoin / Gate / MEXC / HTX at every UTC hour, sign, and post on-chain.
- **Refund cases** (round status 4): one-sided pool, tie (lock == close), or oracle fails to post within a 30-minute grace window.
- **Anti-whale**: `setMaxBetPerRound(asset, cap)` — opt-in per-asset cap on a single user's bet in one round.

## Layout

```
apps/worldbet/
├── contracts/
│   ├── PriceOracle.sol     M-of-N EIP-712 signed price feed
│   └── WorldBet.sol        multi-asset pari-mutuel rounds + fee split + burn
├── scripts/deploy.js       deploy Oracle + WorldBet, register WL/BTC/ETH
├── oracle/index.js         CEX sampler + median + EIP-712 sign + post
├── frontend/               Next.js + wagmi/viem (cards, burn counter, ref link)
├── hardhat.config.js       seoul (103) + gwangju (10395), London EVM, 0.8.24
└── README.md
```

## Install & deploy

```bash
cd apps/worldbet
npm install

# 3-signer setup (recommended for mainnet):
export DEPLOYER_KEY=0x...                    # funded WL on Seoul
export ORACLE_SIGNERS=0xAAA...,0xBBB...,0xCCC...
export ORACLE_THRESHOLD=2

npx hardhat compile
npx hardhat run scripts/deploy.js --network seoul   # or --network gwangju
```

`deployments.json` is written next to the package with the addresses, signer set, and registered asset keys.

## Run the oracle bot

Combined mode (single host, holds 2 of 3 keys — fine for testnet, **not recommended for mainnet**):

```bash
export RPC_URL=https://seoul.worldland.foundation
export ORACLE_ADDR=0x...                     # from deployments.json
export SIGNER_KEYS=0xkey1,0xkey2             # >= threshold
node oracle/index.js
```

Distributed mode (one key per host, peers exchange signatures over HTTP):

```bash
# host A
SIGNER_KEYS=0xkeyA SIDECAR=1 PEER_PORT=8787 \
PEER_SIG_URLS=http://hostB:8787,http://hostC:8787 \
node oracle/index.js

# host B / C — same shape, different keys
```

The leader (any host) submits to chain once `>=` threshold valid signatures are gathered.

## Frontend

```bash
cd frontend
cp .env.example .env.local        # set NEXT_PUBLIC_WORLDBET_ADDRESS
npm install
npm run dev
```

The page reads pool sizes, locks countdown, your bet, the burn counter, and your referral link (`?ref=0x...`). Bets and burn are user-triggered; no admin keys are loaded.

## Operator runbook (per UTC hour)

For round `N` opened at hour `T`:

| When           | Who   | Action                                                            |
|----------------|-------|-------------------------------------------------------------------|
| `T` … `T+1h`   | users | place UP/DOWN bets via UI                                         |
| around `T+1h`  | bot   | post oracle price for `hourId = T/3600 + 1` (= round N's lock)    |
| any            | any   | call `lockRound(asset, N)` once lock price is on chain            |
| around `T+2h`  | bot   | post oracle price for `hourId = T/3600 + 2` (= round N's close)   |
| any            | any   | call `settleRound(asset, N)`                                      |
| any            | user  | `claim(asset, N)` to collect winnings or refund                   |
| any            | any   | `burn()` flushes `pendingBurn` to `0x...dEaD`                     |

A keeper script (~30 lines on top of the oracle bot) can call `lockRound` / `settleRound` automatically.

## Viral launch checklist

- [ ] External audit ($5–10k) before mainnet — single contract, ~370 LoC
- [ ] 3 signer keys distributed to 3 separate hosts; rotate procedure documented
- [ ] Lock/settle keeper running alongside oracle bot
- [ ] Initial WL liquidity bot to bootstrap pools so the first hours are not one-sided
- [ ] Referral link landing page + Discord/Telegram bot to mint personalized links
- [ ] Twitter thread template with the burn counter snapshot
- [ ] Weekly leaderboard cron: read `BetPlaced` / `Claimed` events, rank by net WL, call `distributePrize(...)`
- [ ] `setMaxBetPerRound` configured per asset for the first week, raised after telemetry

## Decisions on record

- **Oracle**: 2-of-3 EIP-712 multisig with median CEX pricing (no Chainlink/Pyth on WorldLand inner-tree at present).
- **Markets**: WL, BTC, ETH launched simultaneously — diversifies user mindshare and prevents single-asset stagnation.
- **Pari-mutuel** (not order-book or AMM): zero protocol risk, payouts capped to the pool.
- **Burn destination**: `0x...dEaD`, permissionless trigger so anyone can flush for the social-proof event.

## Out of scope (follow-ups)

- Foundry fuzz tests on payout math, oracle threshold edge cases, refund paths.
- Auto-lock/settle keeper.
- Production signer-relay infra (HSM, signature sidecar hardening).
- Front-end deployment (Vercel) + analytics.
