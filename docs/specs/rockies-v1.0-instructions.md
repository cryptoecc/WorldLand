# WorldLand Rockies Upgrade v.1.0 — Implementation Specification

**Author**: Prof. Heung-No Lee, INFONET Lab, GIST
**Date**: May 2026
**Target client**: WorldLand reference client (`github.com/cryptoecc/WorldLand`)
**Status**: Draft for Claude Code execution

---

## 0. About this Document

This is a work specification for implementing **Rockies Upgrade v.1.0** of the WorldLand blockchain. The protocol is grounded in two technical papers:

- *The Coin-Toss Protocol* (`docs/specs/worldland_vct.pdf`) — consensus
- *The Tail-Certificate Protocol* (`docs/specs/worldland_tailcert.pdf`) — economic layer (NOT in v.1.0 scope)

**Where this document and the papers conflict, this document takes precedence for v.1.0.** The papers describe the full multi-version vision; this document defines the v.1.0 subset only.

---

## 1. Project Context

- **Repository**: `github.com/cryptoecc/WorldLand`
- **Base**: go-ethereum fork, ChainID 103 (Seoul mainnet), EVM **London** (preserved; do NOT upgrade in this scope)
- **Branch strategy**:
  - Feature branch: `feature/rockies-v1.0`
  - One PR per module (§4 below)
  - Merge order: VRF lib → registry contract → VRF precompile → header schema → consensus engine → epoch handler → fork-choice tiebreaker
  - All PRs require `make test` pass + integration test pass + spec-section reference in PR description
- **Existing components preserved**: ECCPoW LDPC syndrome puzzle (`consensus/eccpow/`), EVM London opcodes, mining loop infrastructure, RPC interfaces.

---

## 2. Scope (Explicit IN / OUT)

### IN scope for v.1.0

- Stake-bound DID registry (Solidity contract, genesis predeploy)
- VRF key registration and on-chain binding
- Verifiable Coin Toss (VCT) function evaluation
- VRF-gated ECCPoW consensus engine
- Block header schema extension
- Block validation pipeline updates
- Epoch-based dynamic *p* adjustment
- Fork-choice rule with VRF-output tiebreaker
- VRF verification precompile
- JSON-RPC endpoints for VCT state inspection

### OUT of scope (deferred to v.1.1+)

- TPM 2.0 endorsement-key attestation (v.1.1)
- zk-SNARK privacy wrapping for DID (v.1.1)
- Tail Certificate primitive (v.1.2)
- Slot/Machine DID hierarchy (v.1.2)
- Three-part reward decomposition (v.1.2)
- Service revenue redirection (v.1.2)
- Dynamic security pricing (v.1.2)
- GPU attestation, AI workload marketplace (v.1.2)
- EVM upgrade (Cancun migration, separate track)

### Acknowledged security limitation

v.1.0 operates without hardware-bound DID. Per Theorem 2 of *The Coin-Toss Protocol*, security under stake-only DID collapses to **stake-proportional PoS combined with ECCPoW's ASIC resistance**. This is accepted; v.1.1 will introduce TPM-anchored DID to restore "one CPU, one vote".

---

## 3. Confirmed Parameters

```
Block time           : 10 s (average, existing)
Target T             : 1000 expected heads per block
s_0 (stake per DID)  : 10^5 WLC
Epoch length         : 10,000 blocks (~27.8 hours)
VRF scheme           : ECVRF-EDWARDS25519-SHA512-TAI (RFC 9381, scheme_id = 0x01)
DID definition       : DID_i = keccak256(pk_VRF_i)
Bootstrap p          : p_0 = 1.0 until first epoch transition
p_min                : 1e-4 (floor to prevent pathological zero)
N_min (activation)   : 50 active DIDs required at hardfork block
Max auto-defer       : 5 epochs (~6 days) before governance handoff
UNSTAKE_DELAY        : 50,000 blocks (~5.8 days)
Hardfork name        : RockiesV1
EVM version          : London (unchanged)
```

---

## 4. Core Specifications

### 4.1 Block Header Extension

Add four fields to `core/types/Header`:

```go
type Header struct {
    // ... existing fields preserved ...
    VRFScheme   uint8     // crypto-agility identifier; 0x01 = ECVRF-Ed25519
    VRFProof    []byte    // π_i, 80 bytes (RFC 9381 ECVRF proof: gamma‖c‖s)
    MinerVRFKey []byte    // pk_i, 32 bytes
    MinerDID    [32]byte  // keccak256(pk_VRF_i)
}
```

**Note on VRF output storage**: The proof `π_i` already contains the curve point γ, from which β = `ProofToHash(γ)` (32 bytes, RFC 9381 §5.2) is deterministically derived during verification. We therefore do NOT store β as a separate header field; this saves 32 bytes per header (~1 GB over 10 years at 10 s block time) at the cost of one extra hash per validation, which is negligible.

**RLP encoding**: append the four new fields after existing serialization. Pre-RockiesV1 blocks omit them entirely (gate decoding via `IsRockiesV1(num)` chain config check).

**Backward compatibility**: nodes syncing pre-RockiesV1 history MUST decode legacy header format identically to current production client.

### 4.2 VCT Function

For miner *i* at block height *h*:

$$
\text{VCT}_i(H_{h-1}, h) \;=\; \mathbb{1}\!\left[\,\text{uint256}(\beta_i) \,<\, p_h \cdot 2^{256}\,\right]
$$

where:
- $(\gamma_i, c_i, s_i) = \text{ECVRF\_Prove}(sk_i,\ \alpha_i)$ with input $\alpha_i = H_{h-1}\,\|\,\text{uint64\_be}(h)$
- $\pi_i = \gamma_i \,\|\, c_i \,\|\, s_i$ (the 80-byte proof stored in the header)
- $\beta_i = \text{ECVRF\_ProofToHash}(\gamma_i)$ (32-byte output, RFC 9381 §5.2)
- $p_h$ is the active epoch's parameter, encoded as a uint256 fixed-point fraction of $2^{256}$

Heads ($\text{VCT}_i = 1$) grants the miner the right to attempt ECCPoW for block *h*. Tails forces skip.

**Determinism**: $\beta_i$ is uniquely determined by $(sk_i, \alpha_i)$ under ECVRF Full Uniqueness (RFC 9381 §3.1). One previous block + one DID + one private key ⇒ exactly one $\beta_i$. Grinding by varying $h$ or $H_{h-1}$ is impossible because both are fixed once parent finalizes.

### 4.3 Epoch Adjustment

At block heights $h \equiv 0 \pmod{10000}$ (epoch boundary):

$$
p_{e+1} \;=\; \max\!\left(p_{\min},\ \min\!\left(1,\ \frac{T}{N_{\text{active},e}}\right)\right)
$$

where $N_{\text{active},e}$ is the count of distinct active DIDs at the close of epoch *e* (queried from the stake registry contract). $T = 1000$, $p_{\min} = 10^{-4}$.

The new $p_{e+1}$ takes effect at block $h+1$ and is committed to chain state for deterministic verification by all nodes.

### 4.4 Stake/DID Registry Contract

**Predeploy address**: `0x0000000000000000000000000000000000001000` (system contract slot, predeployed at genesis or via state transition at T−90 days, see §6.2).

**Storage layout** (Solidity 0.8.x, EVM London compatible):

```solidity
contract StakeRegistry {
    uint256 public constant S_0 = 100_000 ether;       // 10^5 WLC
    uint256 public constant UNSTAKE_DELAY = 50_000;    // 5 epochs

    struct StakeRecord {
        address operator;
        bytes32 did;
        bytes pkVRF;                  // 32 bytes
        uint256 stakeAmount;
        uint256 registeredBlock;
        uint256 unstakeRequestBlock;  // 0 if active
        bool active;
    }

    mapping(bytes32 => StakeRecord) public records;
    mapping(address => bytes32[]) public operatorDIDs;
    uint256 public activeCount;

    event Registered(bytes32 indexed did, address indexed operator,
                     bytes pkVRF, uint256 blockNumber);
    event UnstakeRequested(bytes32 indexed did, uint256 blockNumber);
    event Withdrawn(bytes32 indexed did, address indexed operator, uint256 amount);
}
```

**External functions**:

| Function | Behavior |
|---|---|
| `register(bytes pkVRF, bytes proofOfPossession) payable` | `msg.value` MUST equal `S_0`. Verify `pkVRF` is well-formed Ed25519 point. Verify `proofOfPossession` is a valid signature by `sk_VRF` over `keccak256(msg.sender ‖ "WL_REG_v1")`. Compute `did = keccak256(pkVRF)`. Reject if already registered. Insert record, increment `activeCount`. |
| `requestUnstake(bytes32 did)` | Caller must be `record.operator`. Mark `unstakeRequestBlock = block.number`. DID transitions to inactive at next epoch boundary; decrement `activeCount` then. |
| `withdraw(bytes32 did)` | After `unstakeRequestBlock + UNSTAKE_DELAY` blocks, transfer `S_0` back to operator and delete record. |
| `isActive(bytes32 did) view returns (bool)` | Used by consensus engine in header validation. |
| `getRecord(bytes32 did) view returns (StakeRecord)` | Used to fetch `pkVRF` for VRF verification. |
| `getActiveCount() view returns (uint256)` | Used by epoch handler. |

**Proof-of-possession (PoP)**: prevents rogue-key attacks and registration of keys controlled by other parties. Implementation must use the VRF precompile (§4.5) for verification, not a separate Ed25519 verifier (avoid code duplication).

### 4.5 VRF Verification Precompile

**Address**: `0x0000000000000000000000000000000000000014` (next available slot after London precompiles `0x01..0x09`; verify no conflict with existing WorldLand precompiles before final assignment).

**Input format** (concatenated bytes):

```
offset 0   : pkVRF        (32 bytes)
offset 32  : alpha_len    (4 bytes, big-endian uint32)
offset 36  : alpha        (alpha_len bytes, the VRF input message)
offset 36+alpha_len : proof  (80 bytes, RFC 9381 format)
```

**Output**: 32-byte `beta` (the proof-to-hash output, RFC 9381 §5.2) on success; revert on any failure.

**Gas cost**: 6000 (calibrated to ~Ed25519 verification; benchmark on reference hardware before final value).

### 4.6 Consensus Engine

Create `consensus/vct/` adapting `consensus/eccpow/`. Do not delete `eccpow` — `vct` wraps and extends it.

#### Sealing path (`Seal()`)

```
1. Wait for parent block H_{h-1} to finalize.
2. Read p_h from chain state (current epoch).
3. For each registered DID controlled by this node:
     a. Compute (γ_i, c_i, s_i) = ECVRF_Prove(sk_i, H_{h-1} ‖ uint64_be(h)).
     b. Compute β_i = ECVRF_ProofToHash(γ_i).
     c. If uint256(β_i) >= p_h · 2^256: skip (tails).
     d. Else (heads): begin ECCPoW search using existing LDPC machinery.
4. On valid ECCPoW solution:
     - Construct header with extended fields:
         VRFScheme = 0x01
         VRFProof = γ_i ‖ c_i ‖ s_i  (80 bytes)
         MinerVRFKey = pk_i           (32 bytes)
         MinerDID = keccak256(pk_i)   (32 bytes)
     - Broadcast block.
```

If a node controls multiple DIDs and more than one obtains heads, the node SHOULD attempt ECCPoW on the lowest-$\beta_i$ DID first (deterministic tiebreaker preference).

#### Verification path (`VerifyHeader()`)

```
1. Run all existing pre-RockiesV1 checks (parent linkage, timestamps, gas limits).
2. Read p_h from state for the epoch containing h.
3. Query StakeRegistry: assert isActive(MinerDID) at block H_{h-1}.
4. Assert getRecord(MinerDID).pkVRF == MinerVRFKey.
5. Assert keccak256(MinerVRFKey) == MinerDID.
6. Assert VRFScheme == 0x01.
7. Call VRF precompile with (MinerVRFKey, H_{h-1} ‖ uint64_be(h), VRFProof).
   - Precompile returns β = ECVRF_ProofToHash(γ) on success, reverts on invalid proof.
8. Assert uint256(β) < p_h · 2^256.
9. Verify ECCPoW solution against existing rules.
```

**Order rationale**: VRF check (step 7, ~ms) before ECCPoW verification (step 9, computationally heavier) — rejecting invalid blocks on the cheap check first saves CPU.

#### Fork choice (`SelectChain()`)

1. Heaviest cumulative ECCPoW work (existing rule, primary).
2. **New tiebreaker**: at the divergence block, prefer chain whose miner's $\text{uint256}(\beta_i)$ is lower, where $\beta_i$ is derived from the block's `VRFProof` field. Deterministic; cannot be ground.
3. Final tiebreaker: lower block hash (existing).

### 4.7 Epoch Handler

New module `consensus/vct/epoch.go`:

- Maintain a state-tree-backed counter of distinct DIDs that mined or submitted heads-bearing headers in the current epoch (heads-only counting; tails are not observable on-chain).
- At block heights divisible by 10,000:
  - Compute `N_active` = `StakeRegistry.getActiveCount()` (NOT the heads-count; we use registry count for `p` denominator since registry is the authoritative active set).
  - Compute `p_{e+1}` per §4.3.
  - Write to chain state at a fixed storage slot (e.g., `params/vct.go::EpochStateSlot`).
- Apply transition: `p_h` for `h > epoch_boundary` uses the new value.

**Note on N_active source**: §4.3 uses registry's active count, not heads-derived count. This ensures determinism (registry is part of state, observable identically by all nodes) and is more conservative than heads-counting (which undercounts due to randomness of sampling).

### 4.8 JSON-RPC Endpoints

Add to `internal/ethapi/`:

```
vct_currentP() -> string (decimal, e.g., "0.001")
vct_currentEpoch() -> uint64
vct_activeCount() -> uint64
vct_nextEpochAt() -> uint64 (block height of next boundary)
vct_didInfo(did bytes32) -> { operator, pkVRF, stakeAmount, active, ... }
did_register(pkVRF, popSig) -> tx hash  (helper that constructs registration tx)
```

---

## 5. Testing Plan

### 5.1 Unit Tests

- **VRF library**: RFC 9381 Appendix B.3 test vectors (ECVRF-EDWARDS25519-SHA512-TAI). 100% coverage of `Prove()`, `Verify()`, error paths (invalid points, malformed proofs, wrong scheme byte).
- **Registry contract**: Foundry tests for register/unstake/withdraw/double-registration/wrong-stake-amount/PoP-failure/operator-mismatch.
- **VCT threshold**: deterministic given fixed (sk, H_{h-1}, h, p). 1e6 random samples confirm bias matches p within ±0.1%.
- **Header RLP**: round-trip for both legacy and post-RockiesV1 headers; cross-version decoder selects correct path via chain config.
- **Precompile gas**: benchmark VRF verification on reference hardware (4-core, 16GB), confirm <1ms per call at 6000 gas.

### 5.2 Property Tests

- **Sybil resistance**: operator with `k · S_0` WLC obtains exactly `k` DIDs.
- **VRF determinism**: identical (sk, input) yields identical (y, π) across runs.
- **Threshold bias**: 10^6 VRF outputs at `p = 0.1` → empirical heads rate in [0.099, 0.101].
- **Unforgeability**: random adversary with N=10^4 (sk, message) trials cannot produce valid (y', π') with y' below threshold for any sk it does not own (uses VRF EUF-CMA security).

### 5.3 Integration Tests

- **Local devnet**: 10 nodes, T=2, p=0.2, 1000 blocks.
  - Avg heads per block ≈ 2 (within ±0.3)
  - All blocks pass full validation
  - Fork-choice tiebreaker correct under simulated 2-node partition
- **Monte Carlo PoS-equivalence test** (Theorem 2 from VCT paper): 50,000 blocks at adversary stake fractions {0.25, 0.5, 1.0, 1.5}. Empirical winner ratio matches `s/(s+1)` within ±5×10⁻³.

### 5.4 Replay Test

Sync from genesis to block (RockiesV1Block − 1) on existing mainnet history. Resulting state root MUST equal current production client byte-for-byte.

### 5.5 Beta Network

Deploy to public beta network (separate from Seoul mainnet) for **minimum 3 months** prior to mainnet activation. Monitor:
- Fork rate (target <1%)
- Block-time variance (target σ < 3s for 10s mean)
- Epoch p stability (no oscillation > 2× between adjacent epochs after warmup)
- Memory/CPU footprint of VRF verification (target <5% overhead vs. pre-RockiesV1)

---

## 6. Hardfork Activation

### 6.1 Chain Config

In `params/config.go`:

```go
type ChainConfig struct {
    // ... existing fields ...
    RockiesV1Block *big.Int `json:"rockiesV1Block,omitempty"`
}

func (c *ChainConfig) IsRockiesV1(num *big.Int) bool {
    return isForked(c.RockiesV1Block, num)
}
```

### 6.2 Migration Timeline

| Milestone | Action |
|---|---|
| **T − 180 days** | Public announcement: hardfork block height, operator migration guide published. |
| **T − 90 days** | Stake registry contract deployed via state transition at this block height. Mining still uses pre-RockiesV1 ECCPoW. Operators register stakes and VRF keys during the 90-day grace window. |
| **T − 30 days** | Health check: if `activeCount < 50`, public warning + governance discussion. |
| **T = RockiesV1Block** | Activation logic runs (§6.3). If `activeCount ≥ 50`, VCT gating begins. Otherwise, auto-defer. |
| **T + 10,000 blocks** | First dynamic *p* adjustment: $p_0 = 1.0 \to p_1 = T/N_{\text{active}}$. |

### 6.3 Activation with Auto-Defer

At block `RockiesV1Block`, the consensus engine evaluates:

```
if StakeRegistry.getActiveCount() >= N_MIN:
    activate_VCT_gating()
else:
    defer_count += 1
    if defer_count > MAX_DEFER:                  // 5 attempts ~ 6 days
        emit ActivationStalled(block_number)
        // Block production continues under pre-RockiesV1 rules.
        // Governance multisig may submit either:
        //   (a) ForceActivate transaction (regardless of count), or
        //   (b) ExtendDefer transaction (additional N epochs)
        wait_for_governance_action()
    else:
        next_check_block = block_number + EPOCH_LENGTH  // retry in 10,000 blocks
        emit ActivationDeferred(block_number, defer_count, next_check_block)
```

Constants:
- `N_MIN = 50` (matches §3)
- `MAX_DEFER = 5`
- `EPOCH_LENGTH = 10000`

The auto-defer mechanism is encoded in the consensus engine itself (not a separate contract), so all nodes deterministically agree on activation status. Governance override is a special transaction type recognized by the consensus engine, signed by the WorldLand governance multisig (address TBD; suggested 3-of-5 with INFONET researchers and core engineering team).

### 6.4 Operator Action Required

Existing miners must, during the T−90 to T window:

1. Generate Ed25519 VRF key pair (`sk`, `pk`).
2. Sign PoP message `keccak256(operator_address ‖ "WL_REG_v1")` with `sk`.
3. Submit `register(pk, popSig)` transaction with exactly 100,000 WLC.
4. Configure WorldLand client with the new `sk` (config file path: `~/.worldland/vct_key.json`).
5. Restart client.

Documented step-by-step in `docs/operator/migration-guide.md` (§7 below).

### 6.5 Key Recovery

If an operator loses their VRF private key, recovery proceeds through the standard unstake flow (operator EOA signature is sufficient; VRF key not required):

1. `requestUnstake(did)` — signed by operator EOA
2. Wait `UNSTAKE_DELAY = 50,000` blocks (~5.8 days)
3. `withdraw(did)` — releases stake
4. Generate new VRF key, `register(newPk, newPoP)` — yields a new DID

Total recovery time: ~5.8 days. **No separate `replaceKey` primitive in v.1.0.** Faster recovery via direct key replacement may be considered in v.1.1 based on operator feedback.

---

## 7. Documentation Deliverables

| File | Audience | Content |
|---|---|---|
| `docs/specs/rockies-v1.0.md` | Researchers, auditors | Refined version of this document |
| `docs/operator/staking-guide.md` | Node operators | How to stake, register VRF key, verify registration |
| `docs/operator/migration-guide.md` | Node operators | Step-by-step pre-/post-T procedure |
| `docs/api/vct-rpc.md` | dApp developers | RPC endpoint reference |
| `docs/security/v1.0-threat-model.md` | Auditors | Documented limitations (PoS-equivalence per Theorem 2), v.1.1 mitigations |
| `README.md` (update) | All | Status badge, version, brief overview |

---

## 8. Working Style for Claude Code

### 8.1 TDD Discipline

For each module, write tests against the spec equations BEFORE implementation. The mathematical formulas in §4 are the source of truth. Test names should reference spec sections (e.g., `TestVCTThreshold_Spec_4_2`).

### 8.2 Module Isolation

One PR per module, no cross-cutting changes. Specifically:

- PR 1: VRF library (`crypto/vrf/ecvrf/`) — pure crypto, no consensus dependency
- PR 2: Stake registry contract (`contracts/stake_registry/`) — pure Solidity, deployable standalone
- PR 3: VRF precompile (`core/vm/contracts.go` addition) — wraps PR 1
- PR 4: Header schema extension (`core/types/block.go`) — RLP round-trip tested in isolation
- PR 5: Consensus engine (`consensus/vct/`) — depends on PRs 1–4
- PR 6: Epoch handler (`consensus/vct/epoch.go`) — depends on PR 5
- PR 7: Fork-choice tiebreaker (`core/forkchoice.go`) — depends on PR 4
- PR 8: JSON-RPC endpoints (`internal/ethapi/api_vct.go`) — depends on PRs 5, 6
- PR 9: Chain config + activation (`params/config.go`) — depends on all

### 8.3 Crypto-Agility Hook

The `VRFScheme` byte in the header is the migration vector. Document that `0x01 = ECVRF-EDWARDS25519-SHA512-TAI`, reserve `0x02..0xFF` for future schemes (PQ-VRF). All scheme dispatch logic in a single file (`crypto/vrf/registry.go`) so future hardforks add a case branch only.

### 8.4 Error Handling

No silent panics. All VRF/registry errors must be typed (`ErrInvalidVRFProof`, `ErrDIDNotActive`, `ErrThresholdExceeded`, etc.) and logged with block height + DID context.

### 8.5 Performance Budget

Full block validation (header + transactions + VRF) must complete in **<500 ms** on reference hardware (4-core, 16 GB). VRF verification alone <1 ms.

### 8.6 Code Review

Each PR description must reference the spec section(s) it implements. Reviewers verify correspondence to the math in §4. Reviewers: senior INFONET researchers; final approval by Prof. Lee.

### 8.7 Reference Implementations

For VRF, **fork from a mature existing implementation rather than write from scratch**. Recommended sources, in order of preference:

1. ProtonMail `github.com/ProtonMail/go-ecvrf` — pure-Go ECVRF-EDWARDS25519-SHA512-TAI implementation matching §3's chosen suite (suite_string = 0x03). MIT-licensed. (Algorand's `go-algorand/crypto/vrf/` was previously listed here but was rejected because it wraps libsodium's draft-03 ELL2 variant (suite_string = 0x04), which is incompatible with the TAI suite mandated by §3.)
2. Filecoin `go-filecoin` VRF utilities (cross-validation reference).
3. ChainLink VRF (Solidity-side reference for cross-validation).

Document the exact upstream commit hash forked from in `crypto/vrf/ecvrf/UPSTREAM.md`.

---

## 9. Resolved Design Decisions

The following items were resolved during the specification phase. They are recorded here for traceability and to inform reviewers of the rationale.

| # | Question | Decision | Rationale |
|---|---|---|---|
| 1 | Activation floor at T = RockiesV1Block | $N_{\min} = 50$, auto-defer up to 5 epochs (~6 days), then governance multisig override (§6.3) | Statistical safety: $50 \times p_0 = 50 \gg 1$ guarantees at least one heads per block at activation. Auto-defer + governance is hybrid of code-as-safety-net and human backstop. |
| 2 | VRF key recovery primitive | None in v.1.0. Use standard unstake → re-register (~5.8 days) (§6.5) | Adding `replaceKey()` is unnecessary since DID = $H(pk_{\text{VRF}})$ is intentionally immutable. The standard unstake path requires only operator EOA signature, not the lost VRF key. v.1.1 may add fast-path replacement based on operator feedback. |
| 3 | Block reward decomposition | NOT in v.1.0; defer to v.1.2 | v.1.0 keeps existing 100%-to-winner reward. Reward decomposition (winner/participation/availability) requires Tail Certificate infrastructure, which is also v.1.2 scope. |
| 4 | Header storage of $\beta$ vs only $\pi$ | Store only $\pi$ (gamma + c + s, 80 bytes). Derive $\beta$ at verification (§4.1) | Saves 32 bytes/header (~1 GB over 10 years). $\beta$ is deterministically derivable from $\gamma \subset \pi$ via RFC 9381 §5.2 ProofToHash. Verification cost (one extra SHA-512) is negligible (~µs). Light clients gain no benefit from stored $\beta$ since proof verification is required regardless. |
| 5 | Stake registry deployment vector | State transition at T−90 days (§6.2) | WorldLand mainnet is already live; genesis predeploy is not feasible. State-transition deploy at T−90 gives operators 90 days to stake/register before VCT gating activates. |

---

## References

- Lee, H.-N., *The Coin-Toss Protocol*, manuscript, May 2026. (`docs/specs/worldland_vct.pdf`)
- Lee, H.-N., *The Tail-Certificate Protocol*, manuscript, May 2026. (`docs/specs/worldland_tailcert.pdf`)
- Goldberg, S., et al., *RFC 9381: Verifiable Random Functions (VRFs)*, IETF, August 2023.
- Park, H., Kim, S., Lee, H.-N., *Time-Varying LDPC Code-Based Proof-of-Work for Cryptocurrency Mining*, Symmetry 12(6), 2020.
- Buterin, V., et al., *EIP-1559: Fee market change for ETH 1.0 chain*, 2019. (For reference re: London EVM features preserved.)

---

**End of specification.**
