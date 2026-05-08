# WorldBet

An hourly pari-mutuel prediction market on the WorldLand ecosystem.

WorldLand was built around ECCPoW — error-correction-code proof-of-work — and after the Rockies upgrade the energy and hardware cost of mining a single WLC is, by most operators' accounting, well above the current market price of the listed WL token. WorldBet is one community attempt to give holders a more interesting on-chain use of WL than just sitting on it: a transparent, no-house, hourly market on whether WL/USD, BTC/USD, or ETH/USD will move up or down. It is meant to be played casually, not as an investment vehicle.

The design is intentionally pari-mutuel — every payout is funded by other players' bets, the contract holds zero liquidity risk, and the 3% protocol fee is split between a community prize pool, a referrer rebate, and a permissionless burn. These are tunable parameters of an open-source contract; there is no house edge on top of the fee.

> **Disclaimer.** WorldBet is software. Outcomes are pari-mutuel and determined by other participants' bets and a public oracle. Nothing in this repository is financial advice, an offer to sell, or a solicitation. Authors and contributors disclaim liability for any decision made on the basis of using this code. Where you live, prediction markets and on-chain wagering may be regulated or prohibited; check before participating.

> **Where to deploy.** Primary target is **BNB Smart Chain (BSC, chainId 56)** because the WL BEP-20 token (`0x8aaB31fbc69C92fa53f600910Cf0f215531F8239`) is listed on KuCoin / Gate / MEXC / HTX and pairs on PancakeSwap there. WorldLand Seoul mainnet (chainId 103) is also wired and is the natural deployment target once a bridged WL exists there. Until then, treat Seoul as a forward-compatible target rather than the launch chain.

## Mechanics

- **Markets**: WL/USD, BTC/USD, ETH/USD
- **Rounds**: 1 hour each, rolling — every UTC hour a new round opens for betting; the prior hour's bets go *live* once the lock price posts; the hour before that *settles* on the close price.
- **Stake**: WL (ERC-20 / BEP-20). Users approve the contract once, then bet by amount.
- **Payout**: pari-mutuel — winners get back their stake plus their pro-rata slice of the loser pool. Contract has zero LP risk.
- **Fees (3% total)**:
  - 1.0% → weekly leaderboard prize pool (`prizePool`, owner-distributed off events)
  - 0.3% → sticky referrer rebate (claimable, falls back to burn if no referrer)
  - 1.7% → `pendingBurn`, anyone can call `burn()` which transfers to `0x...dEaD` (WL has no public `burn()`; transfer to the dead address removes circulating supply, totalSupply unchanged)
- **Oracle**: 2-of-3 EIP-712 multisig. Bots fetch median spot from KuCoin / Gate / MEXC / HTX at every UTC hour, sign, and post on-chain.
- **Refund cases** (round status 4): one-sided pool, tie (lock == close), or oracle fails to post within a 30-minute grace window.
- **Anti-whale**: `setMaxBetPerRound(asset, cap)` — opt-in per-asset cap on a single user's bet in one round.

## Layout

```
worldbet/
├── contracts/
│   ├── PriceOracle.sol     M-of-N EIP-712 signed price feed
│   ├── WorldBet.sol        multi-asset pari-mutuel rounds + fee split + burn
│   └── MockWL.sol          testnet-only stand-in for WL (open faucet)
├── scripts/
│   ├── deploy.js           deploy Oracle + WorldBet, register WL/BTC/ETH
│   ├── deploy-mock-wl.js   testnet-only: deploy MockWL + seed deployer
│   ├── mint-tWL.js         testnet-only: faucet helper
│   ├── check-cex.js        verify WL/BTC/ETH listings before deploy
│   └── inspect-rounds.js   one-shot round-state diagnostic
├── oracle/index.js         CEX sampler + median + EIP-712 sign + post
├── keeper/index.js         permissionless lockRound / settleRound driver
├── test/                   Foundry fuzz + stateful invariant tests
├── frontend/               Next.js + wagmi/viem (cards, burn counter, ref link)
├── hardhat.config.js       bsc (56) + bscTestnet (97) + seoul (103) + gwangju (10395)
├── foundry.toml            Solidity 0.8.24, fuzz=1000 / invariant=100×50
└── README.md
```

## Install & deploy (BSC testnet — chainId 97)

Run this **first** before mainnet to dry-run the full pipeline. WL is not listed on testnet, so we deploy a `MockWL` (open faucet, `tWL` symbol) and point WorldBet at it.

```bash
npm install

# 0. Get testnet BNB for gas. Faucet: https://testnet.bnbchain.org/faucet-smart
#    (paste your deployer address; ~0.5 BNB is plenty for the whole flow.)

# 1. Set deployer + signers
export DEPLOYER_KEY=0x...                        # funded testnet BNB
export ORACLE_SIGNERS=0xAAA...,0xBBB...,0xCCC...
export ORACLE_THRESHOLD=2

# 2. Deploy MockWL (10M tWL minted to deployer for seed)
npx hardhat run scripts/deploy-mock-wl.js --network bscTestnet
# -> writes mock-wl.json with the address

# 3. Deploy oracle + WorldBet (auto-loads mock-wl.json on testnet)
npx hardhat run scripts/deploy.js --network bscTestnet

# 4. Faucet helper — mint tWL to a tester (or yourself)
RECIPIENT=0xTESTER AMOUNT=50000 \
  npx hardhat run scripts/mint-tWL.js --network bscTestnet

# 5. Run oracle bot + keeper
RPC_URL=https://data-seed-prebsc-1-s1.binance.org:8545 \
  ORACLE_ADDR=$(jq -r .oracle deployments.json) \
  SIGNER_KEYS=0xkey1,0xkey2 \
  node oracle/index.js &

RPC_URL=https://data-seed-prebsc-1-s1.binance.org:8545 \
  WORLDBET_ADDR=$(jq -r .worldbet deployments.json) \
  KEEPER_KEY=0x... \
  node keeper/index.js &

# 6. Frontend (set NEXT_PUBLIC_DEFAULT_CHAIN_ID=97 + addresses from deployments.json)
cd frontend && npm install && npm run dev

# 7. Verify on BSCScan testnet (optional but useful for debugging)
npx hardhat verify --network bscTestnet $(jq -r .oracle deployments.json) \
  $(jq -r .deployer deployments.json) "[$(jq -r '.signers | join(\",\")' deployments.json)]" 2
npx hardhat verify --network bscTestnet $(jq -r .worldbet deployments.json) \
  $(jq -r .oracle deployments.json) $(jq -r .wl deployments.json) $(jq -r .deployer deployments.json)
```

Run for 24h on testnet to verify oracle posts hourly, keeper drives lock/settle, payouts are correct, and the frontend works end-to-end. Then proceed to mainnet.

## Install & deploy (BSC mainnet — chainId 56)

```bash
npm install

# 0. Verify CEX listings. Each market needs >= 2 healthy venues for
#    the median to work. Fix symbols / drop markets if any is short.
npm run check:cex            # WL, BTC, ETH

# 1. Keys + addresses
export DEPLOYER_KEY=0x...                              # funded BNB on BSC
export ORACLE_SIGNERS=0xAAA...,0xBBB...,0xCCC...
export ORACLE_THRESHOLD=2
# WL_ADDRESS defaults to BSC mainnet 0x8aaB31fb... in scripts/deploy.js;
# override with WL_ADDRESS=0x... only for testnet/other chains.

# 2. Compile + deploy
npx hardhat compile
npx hardhat run scripts/deploy.js --network bsc            # mainnet
# or:
# npx hardhat run scripts/deploy.js --network bscTestnet   # WL_ADDRESS required
```

`deployments.json` is written next to the package with the WL address, oracle/worldbet addresses, signer set, and registered asset keys.

### Conservative initial caps (post-deploy, before announcing)

WL is real money. Cap individual bets tightly for the first 24 hours so a bad oracle post or undiscovered bug only burns bounded WL. The owner can loosen them after monitoring.

```bash
# from a hardhat console on the deployer key:
npx hardhat console --network bsc
> const wb = await ethers.getContractAt("WorldBet", "0x...");
> const cap = ethers.parseEther("100");  // 100 WL per user per round
> for (const k of ["WL/USD","BTC/USD","ETH/USD"]) await wb.setMaxBetPerRound(ethers.id(k), cap);
```

### Deployment safety reminders

- **No emergency pause** in v1. For a critical bug the owner can `setMaxBetPerRound(asset, 1)` to block new bets per asset; in-flight rounds still settle on whatever oracle posts. A real `pause()` + `emergencyRefund()` is the first follow-up if BSC TVL grows beyond the contained-loss threshold.
- **External audit** recommended before significant volume. Self-checked with 32 unit/fuzz tests + 5 stateful invariants × 5000 calls.
- **Oracle key distribution.** Combined mode (one host, all keys) is fine for low-stakes launch. Distribute keys across 3 hosts (peer-signature sidecar mode) before TVL grows.

## Run the oracle bot

Combined mode (single host, holds 2 of 3 keys):

```bash
export RPC_URL=https://bsc-dataseed.binance.org
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
cp .env.example .env.local
# Edit .env.local: set NEXT_PUBLIC_WORLDBET_ADDRESS to the deployed address.
# NEXT_PUBLIC_WL_TOKEN_ADDRESS defaults to BSC mainnet WL.
# NEXT_PUBLIC_DEFAULT_CHAIN_ID defaults to 56 (BSC).
npm install
npm run dev
```

UI shows the user's WL balance, asset cards with pool ratios + lock countdown, the burn counter, and a referral link panel. Betting requires an `approve` of WL to the contract first (one-time per allowance amount); the UI prompts when allowance < bet amount. Bets and burn are user-triggered; no admin keys are loaded.

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

The included `keeper/` does this automatically: it polls every 30s, calls `lockRound` once the oracle has posted, and `settleRound` once `closeTime` passes. Lock failures while the oracle is still pending are silently retried; after the 30-min grace, the contract auto-refunds.

```bash
export RPC_URL=https://seoul.worldland.foundation
export WORLDBET_ADDR=0x...
export KEEPER_KEY=0x...               # any funded WL key (no privileges needed)
node keeper/index.js
# or: npm run keeper
```

Run it on the same host as the oracle bot or independently — both are stateless and idempotent.

The keeper logs every tick at INFO level. A normal idle line looks like:

```
[tick 2026-05-07T06:00:01Z] now=1714990801 currentRoundId=493923
  [WL/USD] #493923 OPEN lockTime=+3599s closeTime=+7199s pools=0/0 oracle[lock=n close=n]
  [WL/USD] #493922 idle (no bets)
```

Set `VERBOSE=0` once you trust it to silence per-round status lines and keep only the LOCK / SETTLE / ERR events.

### One-shot round inspector

When the keeper appears silent and you want to know whether the chain is actually idle (most common) or something is stuck, run:

```bash
npm run inspect:bscTestnet
# or: npx hardhat run scripts/inspect-rounds.js --network bsc
```

It prints the state of the most recent rounds for every asset, whether the oracle has posted the lock and close prices, and what action (if any) should happen next.

## BSC launch checklist

- [ ] `npm run check:cex` returns >= 2 venues for WL/USD; otherwise drop the WL/USD market or fix `oracle/index.js` symbol map
- [ ] `forge test` green (32 unit/fuzz + 5 invariants)
- [ ] BSC testnet (chainId 97) dry-run end-to-end (deploy + bet + lock + settle + claim + burn)
- [ ] BSC mainnet deploy with `ORACLE_THRESHOLD=2`, 3 distinct signer addresses
- [ ] Set `setMaxBetPerRound` to a conservative cap (e.g. 100 WL) on every asset before announcing
- [ ] Oracle bot + keeper bot running; manually verify the first hourly post lands on chain
- [ ] Verify contracts on BSCScan (`npx hardhat verify --network bsc <addr> ...args`)
- [ ] Dashboard / explorer link to the contract pinned in the community channel
- [ ] First 24h: monitor for one-sided rounds, oracle misses, gas anomalies

## Hardening (before significant TVL)

- [ ] Add `pause()` + `emergencyRefund(asset, id)` admin functions; redo fuzz/invariants
- [ ] External audit ($5–10k) — single contract
- [ ] 3 signer keys on 3 separate hosts (not combined-mode); peer signature sidecar with value-convergence (oracle bot leader proposes the median, peers sign that exact value)
- [ ] Lock/settle keeper redundancy (>= 2 instances)
- [ ] Initial liquidity / market-making bot to seed the first hours of each asset's pool
- [ ] Referral link landing page + Discord/Telegram bot
- [ ] Twitter thread template with the burn counter snapshot
- [ ] Weekly leaderboard cron: read `BetPlaced` / `Claimed` events, rank by net WL, call `distributePrize(...)`
- [ ] Loosen `setMaxBetPerRound` after telemetry stabilizes

## Seoul (post-bridge target)

WorldBet is wired to deploy on WorldLand Seoul (chainId 103) once the WL → WLC bridge ships and a wrapped WL ERC-20 exists on Seoul. Steps then:

1. Deploy WL ERC-20 on Seoul (or use the bridge's wrapped form).
2. `WL_ADDRESS=0x... npx hardhat run scripts/deploy.js --network seoul`
3. Same operator + frontend stack; the dApp logic is unchanged.

The Seoul deployment is a forward-compatible mirror, not a substitute for the BSC launch.

## Decisions on record

- **Initial deployment**: BSC (chainId 56), where WL is BEP-20-listed. WorldLand Seoul mirror once a bridged WL ERC-20 exists there.
- **Oracle**: 2-of-3 EIP-712 multisig with median CEX pricing (no Chainlink/Pyth dependency).
- **Markets**: WL, BTC, ETH offered side by side so players have multiple options.
- **Pari-mutuel** (not order-book or AMM): zero protocol risk, payouts capped to the pool, no house edge beyond the disclosed 3% fee.
- **Burn destination**: `0x...dEaD` (WL has no public `burn()`; transfer to the dead address is the standard ERC-20 substitute), permissionless trigger so anyone can flush.
- **Stake currency**: WL ERC-20 (vanilla, no fee-on-transfer, 18 decimals); user approves once, then bets by amount.

## Out of scope (follow-ups)

- `pause()` + `emergencyRefund` admin functions (add before TVL grows).
- Distributed oracle value-convergence (currently combined mode; peer-sidecar mode needs a leader-proposes / peers-sign protocol so signatures align).
- Production signer-relay infra (HSM, signature sidecar hardening, key rotation).
- Front-end deployment (Vercel) + analytics + leaderboard cron.
