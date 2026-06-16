# Upstream Provenance — `crypto/vrf/ecvrf/`

Spec reference: WorldLand Rockies v.1.0 spec §8.7.

## Source

- **Upstream**: [`github.com/ProtonMail/go-ecvrf`](https://github.com/ProtonMail/go-ecvrf)
- **Upstream commit**: `dce739d6b120640fb237f6ca18538fd1cefa91c9`
- **Upstream license**: MIT (see `LICENSE` in upstream tree)
- **Cipher suite**: ECVRF-EDWARDS25519-SHA512-TAI (RFC 9381 §5,
  `suite_string = 0x03`).

## Why ProtonMail/go-ecvrf

The originally specified upstream (Algorand `go-algorand/crypto/vrf/`) is
a CGo wrapper around libsodium's `vrf_ietfdraft03`, which implements
ECVRF-EDWARDS25519-SHA512-Elligator2 (`suite_string = 0x04`). That suite
is **incompatible** with the TAI variant mandated by spec §3 — the two
schemes produce different `ProofToHash` outputs for the same `(sk, alpha)`.

ProtonMail's library is a small, pure-Go (no CGo) implementation of
exactly the right TAI suite, validated against the published RFC test
vectors. WorldLand's VCT use of the VRF supplies a fully public `alpha =
H_{h-1} || h`, so the Try-And-Increment timing-side-channel concern
flagged in RFC 9381 §7.5 does not apply (there is no secret in the
hashed material).

The spec was amended on `feature/vrf-library`'s parent (commit
`fc2b948` on `docs/rockies-v1.0-spec`) to record this decision in §8.7.

## Modifications from upstream

The following changes were applied during the fork:

1. **Package documentation** updated to reference RFC 9381 (the published
   IETF document) instead of `draft-irtf-cfrg-vrf-10`. The on-the-wire
   format is identical.
2. **Typed errors** (`crypto/vrf/ecvrf/errors.go`) per spec §8.4. Each
   error message carries the `ecvrf:` prefix for greppability.
3. **`Verify()` API** changed from `(verified bool, beta []byte, err error)`
   to `(beta []byte, err error)`. A semantically invalid (but
   structurally well-formed) proof now returns `ErrInvalidVRFProof`
   rather than `(false, nil, nil)`. This eliminates a footgun where
   callers could ignore the bool and treat a forged proof as valid.
4. **`Public()`** changed from `(*PublicKey, error)` to `*PublicKey`. It
   cannot fail (the public key was derived from a valid scalar at key
   construction time).
5. **`NewPublicKey()`** now eagerly decodes the curve point and rejects
   malformed encodings at construction. Upstream deferred the check to
   the first `Verify()` call.
6. **`ProofToHash()`** exposed as a top-level function (not just an
   internal helper). The VRF precompile (spec §4.5) needs this entry
   point because it returns beta but does the verification through the
   library's `Verify`.
7. **Cofactor multiplication in `proofToHash`** moved into the
   helper itself (rather than the caller), simplifying the call sites.
8. **Constants exposed**: `ProofSize`, `PublicKeySize`, `BetaSize`,
   `SeedSize`, `SuiteID` — needed by the precompile, header schema, and
   registry layers.
9. **Comments** rewritten to cite RFC 9381 sections rather than the
   draft. Algorithm steps are unchanged.

The *cryptographic algorithm* (every line of curve arithmetic and every
hash input) is unchanged from upstream and from the RFC; modifications
are restricted to API hygiene and documentation. Validation: the full
RFC 9381 §B.3 test corpus passes byte-for-byte (see `ecvrf_test.go`).

## Re-baseline procedure

Should ProtonMail publish a security fix for the upstream, the diff
must be re-applied here. Steps:

1. Fetch the new upstream commit; record the hash in this file.
2. Diff `upstream/ecvrf/ecvrf.go` against this package's `ecvrf.go`.
3. Apply algorithmic changes only; preserve the modifications listed
   above (typed errors, two-state Verify return, etc.).
4. Re-run `go test ./crypto/vrf/...` and confirm the RFC 9381 §B.3
   vectors still pass.
5. Update this file's `Upstream commit` line and submit a PR
   referencing the upstream advisory.
