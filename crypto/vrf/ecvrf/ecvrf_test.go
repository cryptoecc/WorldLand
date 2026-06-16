// Copyright 2026 The WorldLand / go-ethereum Authors
// This file is part of the WorldLand library.
//
// The WorldLand library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The WorldLand library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.

// RFC 9381 Appendix B.3 test vectors for ECVRF-EDWARDS25519-SHA512-TAI.
//
// Vectors transcribed verbatim from RFC 9381 (August 2023). They are
// identical to draft-irtf-cfrg-vrf-10 Appendix A.3 Examples 7, 8, 9 and to
// the ProtonMail/go-ecvrf upstream test corpus (commit
// dce739d6b120640fb237f6ca18538fd1cefa91c9), which is the upstream we forked
// for this package per spec §8.7.

package ecvrf

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"filippo.io/edwards25519"
)

// rfcVector groups the published RFC 9381 §B.3 fields plus the intermediate
// values used to exercise the internal helpers.
type rfcVector struct {
	name string
	// Externally visible fields.
	sk    string // 32-byte ed25519 seed (RFC 8032 SK)
	pk    string // 32-byte ed25519 public key
	alpha string // VRF input
	pi    string // 80-byte VRF proof (gamma || c || s)
	beta  string // 64-byte VRF output (ProofToHash(gamma))
	// Intermediate values for white-box tests.
	x string // secret scalar x = clamp(SHA512(sk)[:32]) (RFC 8032 §5.1.5)
	h string // hashToCurve(pk, alpha) encoded
	k string // 64-byte nonce SHA-512 output prior to scalar reduction
	u string // U = k*B
	v string // V = k*H
}

// rfc9381B3 is the full RFC 9381 §B.3 test corpus for
// ECVRF-EDWARDS25519-SHA512-TAI (3 vectors).
var rfc9381B3 = []rfcVector{
	{
		name:  "RFC9381_B3_Example1",
		sk:    "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
		pk:    "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
		alpha: "",
		pi: "8657106690b5526245a92b003bb079ccd1a92130477671f6fc01ad16f26f723f" +
			"5e8bd1839b414219e8626d393787a192241fc442e6569e96c462f62b8079b9ed" +
			"83ff2ee21c90c7c398802fdeebea4001",
		beta: "90cf1df3b703cce59e2a35b925d411164068269d7b2d29f3301c03dd757876ff" +
			"66b71dda49d2de59d03450451af026798e8f81cd2e333de5cdf4f3e140fdd8ae",
		x: "307c83864f2833cb427a2ef1c00a013cfdff2768d980c0a3a520f006904de94f",
		h: "91bbed02a99461df1ad4c6564a5f5d829d0b90cfc7903e7a5797bd658abf3318",
		k: "7100f3d9eadb6dc4743b029736ff283f5be494128df128df2817106f345b8594" +
			"b6d6da2d6fb0b4c0257eb337675d96eab49cf39e66cc2c9547c2bf8b2a6afae4",
		u: "aef27c725be964c6a9bf4c45ca8e35df258c1878b838f37d9975523f09034071",
		v: "5016572f71466c646c119443455d6cb9b952f07d060ec8286d678615d55f954f",
	},
	{
		name:  "RFC9381_B3_Example2",
		sk:    "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb",
		pk:    "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c",
		alpha: "72",
		pi: "f3141cd382dc42909d19ec5110469e4feae18300e94f304590abdced48aed593" +
			"f7eaf3eb2f1a968cba3f6e23b386aeeaab7b1ea44a256e811892e13eeae7c9f6" +
			"ea8992557453eac11c4d5476b1f35a08",
		beta: "eb4440665d3891d668e7e0fcaf587f1b4bd7fbfe99d0eb2211ccec90496310eb" +
			"5e33821bc613efb94db5e5b54c70a848a0bef4553a41befc57663b56373a5031",
		x: "68bd9ed75882d52815a97585caf4790a7f6c6b3b7f821c5e259a24b02e502e51",
		h: "5b659fc3d4e9263fd9a4ed1d022d75eaacc20df5e09f9ea937502396598dc551",
		k: "42589bbf0c485c3c91c1621bb4bfe04aed7be76ee48f9b00793b2342acb9c167" +
			"cab856f9f9d4febc311330c20b0a8afd3743d05433e8be8d32522ecdc16cc5ce",
		u: "1dcb0a4821a2c48bf53548228b7f170962988f6d12f5439f31987ef41f034ab3",
		v: "fd03c0bf498c752161bae4719105a074630a2aa5f200ff7b3995f7bfb1513423",
	},
	{
		name:  "RFC9381_B3_Example3",
		sk:    "c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7",
		pk:    "fc51cd8e6218a1a38da47ed00230f0580816ed13ba3303ac5deb911548908025",
		alpha: "af82",
		pi: "9bc0f79119cc5604bf02d23b4caede71393cedfbb191434dd016d30177ccbf80" +
			"e29dc513c01c3a980e0e545bcd848222d08a6c3e3665ff5a4cab13a643bef812" +
			"e284c6b2ee063a2cb4f456794723ad0a",
		beta: "645427e5d00c62a23fb703732fa5d892940935942101e456ecca7bb217c61c45" +
			"2118fec1219202a0edcf038bb6373241578be7217ba85a2687f7a0310b2df19f",
		x: "909a8b755ed902849023a55b15c23d11ba4d7f4ec5c2f51b1325a181991ea95c",
		h: "bf4339376f5542811de615e3313d2b36f6f53c0acfebb482159711201192576a",
		k: "38b868c335ccda94a088428cbf3ec8bc7955bfaffe1f3bd2aa2c59fc31a0febc" +
			"59d0e1af3715773ce11b3bbdd7aba8e3505d4b9de6f7e4a96e67e0d6bb6d6c3a",
		u: "2bae73e15a64042fcebf062abe7e432b2eca6744f3e8265bc38e009cd577ecd5",
		v: "88cba1cb0d4f9b649d9a86026b69de076724a93a65c349c988954f0961c5d506",
	},
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decoding %q: %v", s, err)
	}
	return b
}

// invalidPointBytes returns 32 bytes that filippo.io/edwards25519 SetBytes
// rejects (no valid x coordinate). About half of random 32-byte strings
// are non-curve, so a deterministic seeded search terminates quickly.
func invalidPointBytes(t *testing.T) []byte {
	t.Helper()
	for seed := uint32(0); seed < 1024; seed++ {
		h := sha256.Sum256([]byte{byte(seed), byte(seed >> 8), byte(seed >> 16), byte(seed >> 24)})
		if _, err := (&edwards25519.Point{}).SetBytes(h[:]); err != nil {
			return append([]byte{}, h[:]...)
		}
	}
	t.Fatal("no invalid Ed25519 point encoding found in 1024 SHA-256 candidates")
	return nil
}

// TestRFC9381_B3_Prove verifies the canonical Prove() output of every vector
// in RFC 9381 §B.3 byte-for-byte (proof and beta).
func TestRFC9381_B3_Prove(t *testing.T) {
	for _, v := range rfc9381B3 {
		v := v
		t.Run(v.name, func(t *testing.T) {
			fullSK := append(mustHex(t, v.sk), mustHex(t, v.pk)...)
			sk, err := NewPrivateKey(fullSK)
			if err != nil {
				t.Fatalf("NewPrivateKey: %v", err)
			}

			beta, proof, err := sk.Prove(mustHex(t, v.alpha))
			if err != nil {
				t.Fatalf("Prove: %v", err)
			}
			if got, want := hex.EncodeToString(proof), v.pi; got != want {
				t.Errorf("proof mismatch:\n got=%s\nwant=%s", got, want)
			}
			if got, want := hex.EncodeToString(beta), v.beta; got != want {
				t.Errorf("beta mismatch:\n got=%s\nwant=%s", got, want)
			}
			if len(proof) != ProofSize {
				t.Errorf("proof len = %d, want %d", len(proof), ProofSize)
			}
			if len(beta) != BetaSize {
				t.Errorf("beta len = %d, want %d", len(beta), BetaSize)
			}
		})
	}
}

// TestRFC9381_B3_Verify verifies that the canonical proofs from the RFC
// validate and recover the published beta.
func TestRFC9381_B3_Verify(t *testing.T) {
	for _, v := range rfc9381B3 {
		v := v
		t.Run(v.name, func(t *testing.T) {
			pk, err := NewPublicKey(mustHex(t, v.pk))
			if err != nil {
				t.Fatalf("NewPublicKey: %v", err)
			}
			beta, err := pk.Verify(mustHex(t, v.alpha), mustHex(t, v.pi))
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if got, want := hex.EncodeToString(beta), v.beta; got != want {
				t.Errorf("beta mismatch:\n got=%s\nwant=%s", got, want)
			}
		})
	}
}

// TestRFC9381_B3_GenerateKey verifies that seeding GenerateKey from each
// vector's RFC-8032 seed yields the correct (sk, pk, x) tuple.
func TestRFC9381_B3_GenerateKey(t *testing.T) {
	for _, v := range rfc9381B3 {
		v := v
		t.Run(v.name, func(t *testing.T) {
			sk, err := GenerateKey(bytes.NewReader(mustHex(t, v.sk)))
			if err != nil {
				t.Fatalf("GenerateKey: %v", err)
			}
			gotPK := sk.Public().Bytes()
			if !bytes.Equal(gotPK, mustHex(t, v.pk)) {
				t.Errorf("public key mismatch:\n got=%x\nwant=%s", gotPK, v.pk)
			}

			// Cross-check against stdlib ed25519 derivation.
			edKey := ed25519.NewKeyFromSeed(mustHex(t, v.sk))
			if !bytes.Equal(edKey[32:], gotPK) {
				t.Errorf("ed25519 stdlib pk mismatch:\n got=%x\nwant=%x", gotPK, edKey[32:])
			}
		})
	}
}

// TestRFC9381_B3_HashToCurveTAI verifies the hashToCurveTAI helper against
// the RFC's published intermediate H values.
func TestRFC9381_B3_HashToCurveTAI(t *testing.T) {
	for _, v := range rfc9381B3 {
		v := v
		t.Run(v.name, func(t *testing.T) {
			h, err := hashToCurveTAI(mustHex(t, v.pk), mustHex(t, v.alpha))
			if err != nil {
				t.Fatalf("hashToCurveTAI: %v", err)
			}
			if got, want := hex.EncodeToString(h.Bytes()), v.h; got != want {
				t.Errorf("H mismatch:\n got=%s\nwant=%s", got, want)
			}
		})
	}
}

// TestRFC9381_B3_NonceHash verifies the SHA512(SHA512(sk)[32:] || H) nonce
// derivation (RFC 9381 §5.4.2.2).
func TestRFC9381_B3_NonceHash(t *testing.T) {
	for _, v := range rfc9381B3 {
		v := v
		t.Run(v.name, func(t *testing.T) {
			got := generateNonceHash(mustHex(t, v.sk), mustHex(t, v.h))
			if want := mustHex(t, v.k); !bytes.Equal(got, want) {
				t.Errorf("k mismatch:\n got=%x\nwant=%x", got, want)
			}
		})
	}
}

// TestRFC9381_B3_ProofToHash verifies the standalone ProofToHash helper
// against published betas (the precompile path uses this).
func TestRFC9381_B3_ProofToHash(t *testing.T) {
	for _, v := range rfc9381B3 {
		v := v
		t.Run(v.name, func(t *testing.T) {
			beta, err := ProofToHash(mustHex(t, v.pi))
			if err != nil {
				t.Fatalf("ProofToHash: %v", err)
			}
			if got, want := hex.EncodeToString(beta), v.beta; got != want {
				t.Errorf("beta mismatch:\n got=%s\nwant=%s", got, want)
			}
		})
	}
}

// TestRoundTrip exercises the keygen→prove→verify flow with random keys.
func TestRoundTrip(t *testing.T) {
	sk, err := GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pk := sk.Public()

	alpha := []byte("worldland-vct-alpha")
	beta1, proof, err := sk.Prove(alpha)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if len(proof) != ProofSize {
		t.Fatalf("proof len = %d", len(proof))
	}

	beta2, err := pk.Verify(alpha, proof)
	if err != nil {
		t.Fatalf("Verify rejected our own proof: %v", err)
	}
	if !bytes.Equal(beta1, beta2) {
		t.Fatalf("beta mismatch: prove=%x verify=%x", beta1, beta2)
	}
}

// TestVerifyRejectsForgery: every single-bit flip of a valid proof must
// cause Verify to return ErrInvalidVRFProof. This is the unforgeability
// property — we sweep all 80*8 = 640 bit positions of the proof.
func TestVerifyRejectsForgery(t *testing.T) {
	sk, err := GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pk := sk.Public()

	alpha := []byte("forgery-test")
	_, proof, err := sk.Prove(alpha)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	for i := 0; i < ProofSize; i++ {
		for j := uint(0); j < 8; j++ {
			tampered := make([]byte, ProofSize)
			copy(tampered, proof)
			tampered[i] ^= 1 << j

			beta, err := pk.Verify(alpha, tampered)
			if err == nil {
				t.Fatalf("forgery accepted at byte %d bit %d (beta=%x)", i, j, beta)
			}
		}
	}
}

// TestVerifyRejectsWrongAlpha: changing alpha alone must invalidate the
// proof (RFC 9381 §3.1 unique-output property).
func TestVerifyRejectsWrongAlpha(t *testing.T) {
	sk, err := GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pk := sk.Public()

	_, proof, err := sk.Prove([]byte("alpha-A"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if _, err := pk.Verify([]byte("alpha-B"), proof); !errors.Is(err, ErrInvalidVRFProof) {
		t.Fatalf("wrong-alpha verify err = %v, want ErrInvalidVRFProof", err)
	}
}

// TestVerifyRejectsWrongPK: the proof must be bound to the claimed pk.
func TestVerifyRejectsWrongPK(t *testing.T) {
	sk1, _ := GenerateKey(nil)
	sk2, _ := GenerateKey(nil)
	pk2 := sk2.Public()

	_, proof, err := sk1.Prove([]byte("alpha"))
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if _, err := pk2.Verify([]byte("alpha"), proof); !errors.Is(err, ErrInvalidVRFProof) {
		t.Fatalf("wrong-pk verify err = %v, want ErrInvalidVRFProof", err)
	}
}

// TestProveDeterministic: identical inputs must yield identical outputs
// (RFC 9381 §3.1 full-uniqueness — the basis of VCT determinism per spec
// §4.2).
func TestProveDeterministic(t *testing.T) {
	sk, _ := GenerateKey(nil)
	alpha := []byte("deterministic-input")

	beta1, proof1, _ := sk.Prove(alpha)
	beta2, proof2, _ := sk.Prove(alpha)

	if !bytes.Equal(beta1, beta2) {
		t.Errorf("nondeterministic beta")
	}
	if !bytes.Equal(proof1, proof2) {
		t.Errorf("nondeterministic proof")
	}
}

// --- error-path coverage (spec §8.4: typed errors required) ---

func TestNewPrivateKey_BadLength(t *testing.T) {
	for _, n := range []int{0, 1, 31, 32, 63, 65, 128} {
		_, err := NewPrivateKey(make([]byte, n))
		if !errors.Is(err, ErrInvalidPrivateKey) {
			t.Errorf("len=%d err=%v, want ErrInvalidPrivateKey", n, err)
		}
	}
}

func TestNewPublicKey_BadLength(t *testing.T) {
	for _, n := range []int{0, 1, 31, 33, 64} {
		_, err := NewPublicKey(make([]byte, n))
		if !errors.Is(err, ErrInvalidPublicKey) {
			t.Errorf("len=%d err=%v, want ErrInvalidPublicKey", n, err)
		}
	}
}

func TestNewPublicKey_NotOnCurve(t *testing.T) {
	bad := invalidPointBytes(t)
	if _, err := NewPublicKey(bad); !errors.Is(err, ErrInvalidPublicKey) {
		t.Errorf("non-curve point err = %v, want ErrInvalidPublicKey", err)
	}
}

func TestVerify_BadProofLength(t *testing.T) {
	pk, _ := NewPublicKey(mustHex(t, rfc9381B3[0].pk))
	for _, n := range []int{0, 1, 79, 81, 160} {
		_, err := pk.Verify([]byte("alpha"), make([]byte, n))
		if !errors.Is(err, ErrInvalidProofLength) {
			t.Errorf("len=%d err=%v, want ErrInvalidProofLength", n, err)
		}
	}
}

func TestVerify_BadGammaPoint(t *testing.T) {
	pk, _ := NewPublicKey(mustHex(t, rfc9381B3[0].pk))
	bad := make([]byte, ProofSize)
	copy(bad[:32], invalidPointBytes(t))
	if _, err := pk.Verify([]byte("alpha"), bad); !errors.Is(err, ErrInvalidProofPoint) {
		t.Errorf("bad gamma err = %v, want ErrInvalidProofPoint", err)
	}
}

func TestVerify_NonCanonicalScalar(t *testing.T) {
	// s field is 32 bytes at proof[48:80]; setting it all-0xff exceeds the
	// group order and must be rejected as non-canonical (RFC 9381 §5.3).
	pk, _ := NewPublicKey(mustHex(t, rfc9381B3[0].pk))
	bad := mustHex(t, rfc9381B3[0].pi)
	for i := 48; i < 80; i++ {
		bad[i] = 0xff
	}
	if _, err := pk.Verify([]byte(""), bad); !errors.Is(err, ErrInvalidProofScalar) {
		t.Errorf("non-canonical s err = %v, want ErrInvalidProofScalar", err)
	}
}

func TestProofToHash_BadLength(t *testing.T) {
	for _, n := range []int{0, 79, 81} {
		if _, err := ProofToHash(make([]byte, n)); !errors.Is(err, ErrInvalidProofLength) {
			t.Errorf("len=%d err=%v, want ErrInvalidProofLength", n, err)
		}
	}
}

func TestProofToHash_BadGamma(t *testing.T) {
	bad := make([]byte, ProofSize)
	copy(bad[:32], invalidPointBytes(t))
	if _, err := ProofToHash(bad); !errors.Is(err, ErrInvalidProofPoint) {
		t.Errorf("bad gamma err = %v, want ErrInvalidProofPoint", err)
	}
}

// TestErrorMessages: the spec §8.4 mandates typed errors, but messages
// should also be greppable. Validate prefix.
func TestErrorMessages(t *testing.T) {
	errs := []error{
		ErrInvalidPrivateKey, ErrInvalidPublicKey, ErrInvalidProofLength,
		ErrInvalidProofPoint, ErrInvalidProofScalar, ErrInvalidVRFProof,
		ErrHashToCurveFailed,
	}
	for _, e := range errs {
		if e == nil {
			t.Errorf("nil error in error set")
			continue
		}
		if !strings.HasPrefix(e.Error(), "ecvrf: ") {
			t.Errorf("error %q missing 'ecvrf:' prefix", e.Error())
		}
	}
}

// BenchmarkProve / BenchmarkVerify are required by spec §5.1 to validate the
// <1ms target on reference hardware (4-core, 16 GB).
func BenchmarkProve(b *testing.B) {
	sk, err := GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	alpha := []byte("benchmark-alpha")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = sk.Prove(alpha)
	}
}

func BenchmarkVerify(b *testing.B) {
	sk, err := GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	pk := sk.Public()
	alpha := []byte("benchmark-alpha")
	_, proof, _ := sk.Prove(alpha)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pk.Verify(alpha, proof)
	}
}
