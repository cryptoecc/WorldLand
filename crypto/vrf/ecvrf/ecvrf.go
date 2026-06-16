// Copyright 2026 The WorldLand / go-ethereum Authors
// Portions Copyright (c) 2021 ProtonMail, MIT-licensed (see UPSTREAM.md).
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

// Package ecvrf implements ECVRF-EDWARDS25519-SHA512-TAI as specified by
// RFC 9381 §5 (suite_string = 0x03), the cipher suite mandated by WorldLand
// Rockies v.1.0 spec §3.
//
// The implementation is derived from ProtonMail/go-ecvrf (MIT). See
// UPSTREAM.md for the exact upstream commit and a summary of WorldLand
// modifications.
package ecvrf

import (
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"io"

	"filippo.io/edwards25519"
)

// Suite/protocol constants. Sizes match RFC 9381 §5 for ED25519-SHA512-TAI.
const (
	// SeedSize is the RFC 8032 seed length (32 bytes).
	SeedSize = 32
	// PublicKeySize is the canonical Ed25519 point encoding (32 bytes).
	PublicKeySize = 32
	// PrivateKeySize is the RFC 8032 private-key encoding: seed || public.
	PrivateKeySize = SeedSize + PublicKeySize
	// IntermediateSize is the truncated challenge length c (16 bytes,
	// per RFC 9381 §5.5 cLen for ED25519-SHA512-TAI).
	IntermediateSize = 16
	// ProofSize is gamma || c || s = 32 + 16 + 32 = 80 bytes.
	ProofSize = PublicKeySize + IntermediateSize + SeedSize
	// BetaSize is the SHA-512 ProofToHash output (64 bytes).
	BetaSize = 64

	// SuiteID is the RFC 9381 suite_string for ECVRF-EDWARDS25519-SHA512-TAI.
	// Note: this is the *RFC* suite byte, not the WorldLand crypto-agility
	// scheme byte (which is 0x01; see crypto/vrf/registry.go and spec §8.3).
	SuiteID = 0x03
)

// Domain-separation tags from RFC 9381 §5.4.
const (
	dstHashToCurve = 0x01
	dstHashPoints  = 0x02
	dstProofToHash = 0x03
)

// PrivateKey is a VRF private key. It bundles the RFC 8032 seed, derived
// public point, and the precomputed secret scalar so Prove() does not redo
// the SHA-512 + clamp on every call.
type PrivateKey struct {
	seed []byte // 32 bytes (RFC 8032 SK)
	pk   []byte // 32 bytes (canonical encoding of A = x*B)
	x    *edwards25519.Scalar
}

// PublicKey is a VRF public key (a canonical Ed25519 point encoding).
type PublicKey struct {
	pk    []byte
	point *edwards25519.Point
}

// GenerateKey samples a fresh keypair. If rnd is nil, crypto/rand is used.
// Calling with a 32-byte deterministic reader reproduces an RFC 8032 seed.
func GenerateKey(rnd io.Reader) (*PrivateKey, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	seed := make([]byte, SeedSize)
	if _, err := io.ReadFull(rnd, seed); err != nil {
		return nil, err
	}
	return privateKeyFromSeed(seed)
}

// NewPrivateKey deserialises a 64-byte (seed || pk) RFC 8032 private key.
// The supplied pk is checked against the value derived from the seed.
func NewPrivateKey(skBytes []byte) (*PrivateKey, error) {
	if len(skBytes) != PrivateKeySize {
		return nil, ErrInvalidPrivateKey
	}
	sk, err := privateKeyFromSeed(skBytes[:SeedSize])
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(sk.pk, skBytes[SeedSize:]) != 1 {
		return nil, ErrInvalidPrivateKey
	}
	return sk, nil
}

func privateKeyFromSeed(seed []byte) (*PrivateKey, error) {
	// RFC 8032 §5.1.5: x = clamp(SHA512(seed)[:32]).
	h := sha512.Sum512(seed)
	x, err := edwards25519.NewScalar().SetBytesWithClamping(h[:32])
	if err != nil {
		return nil, ErrInvalidPrivateKey
	}
	A := (&edwards25519.Point{}).ScalarBaseMult(x)
	cp := make([]byte, SeedSize)
	copy(cp, seed)
	return &PrivateKey{seed: cp, pk: A.Bytes(), x: x}, nil
}

// Public returns the matching public key.
func (sk *PrivateKey) Public() *PublicKey {
	pkCopy := make([]byte, PublicKeySize)
	copy(pkCopy, sk.pk)
	point, _ := (&edwards25519.Point{}).SetBytes(sk.pk) // safe: derived from valid scalar
	return &PublicKey{pk: pkCopy, point: point}
}

// Bytes serialises the private key as seed || pk.
func (sk *PrivateKey) Bytes() []byte {
	out := make([]byte, PrivateKeySize)
	copy(out, sk.seed)
	copy(out[SeedSize:], sk.pk)
	return out
}

// Seed returns a copy of the RFC 8032 seed.
func (sk *PrivateKey) Seed() []byte {
	out := make([]byte, SeedSize)
	copy(out, sk.seed)
	return out
}

// NewPublicKey deserialises and validates a 32-byte Ed25519 public key.
// Unlike the upstream, we eagerly decode the point so malformed keys are
// rejected at construction (rather than when first verified).
func NewPublicKey(pkBytes []byte) (*PublicKey, error) {
	if len(pkBytes) != PublicKeySize {
		return nil, ErrInvalidPublicKey
	}
	point, err := (&edwards25519.Point{}).SetBytes(pkBytes)
	if err != nil {
		return nil, ErrInvalidPublicKey
	}
	cp := make([]byte, PublicKeySize)
	copy(cp, pkBytes)
	return &PublicKey{pk: cp, point: point}, nil
}

// Bytes returns a copy of the canonical public-key encoding.
func (pk *PublicKey) Bytes() []byte {
	out := make([]byte, PublicKeySize)
	copy(out, pk.pk)
	return out
}

// Prove computes (beta, pi) = ECVRF_prove(sk, alpha) per RFC 9381 §5.1.
// Determinism follows from sk and alpha: both are protocol-fixed in the
// VCT use (alpha = H_{h-1} || uint64_be(h); see spec §4.2).
func (sk *PrivateKey) Prove(alpha []byte) (beta, proof []byte, err error) {
	H, err := hashToCurveTAI(sk.pk, alpha)
	if err != nil {
		return nil, nil, err
	}

	// gamma = x * H
	gamma := (&edwards25519.Point{}).ScalarMult(sk.x, H)

	// k = ECVRF_nonce_generation_RFC8032(sk, H)  (§5.4.2.2)
	kHash := generateNonceHash(sk.seed, H.Bytes())
	k, err := edwards25519.NewScalar().SetUniformBytes(kHash)
	if err != nil {
		return nil, nil, err
	}

	// c = ECVRF_hash_points(H, gamma, k*B, k*H)  truncated to 16 bytes.
	c := hashPoints(
		H,
		gamma,
		(&edwards25519.Point{}).ScalarBaseMult(k),
		(&edwards25519.Point{}).ScalarMult(k, H),
	)
	cScal, err := cToScalar(c)
	if err != nil {
		return nil, nil, err
	}

	// s = (k + c*x) mod q
	s := edwards25519.NewScalar().Add(k, edwards25519.NewScalar().Multiply(cScal, sk.x))

	proof = make([]byte, ProofSize)
	copy(proof, gamma.Bytes())
	copy(proof[PublicKeySize:], c)
	copy(proof[PublicKeySize+IntermediateSize:], s.Bytes())

	return proofToHashFromGamma(gamma), proof, nil
}

// Verify implements RFC 9381 §5.3. It returns the recovered beta on
// success, or one of the typed errors (ErrInvalidProofLength / Point /
// Scalar / VRFProof) on failure.
//
// We map the upstream's three-state (verified, beta, err) return into a
// two-state (beta, err) form: a structurally well-formed but semantically
// invalid proof now returns ErrInvalidVRFProof. Callers can use errors.Is
// to distinguish bad-input from bad-signature without inspecting bools.
func (pk *PublicKey) Verify(alpha, proof []byte) ([]byte, error) {
	if len(proof) != ProofSize {
		return nil, ErrInvalidProofLength
	}
	gamma, err := (&edwards25519.Point{}).SetBytes(proof[:PublicKeySize])
	if err != nil {
		return nil, ErrInvalidProofPoint
	}
	c, err := cToScalar(proof[PublicKeySize : PublicKeySize+IntermediateSize])
	if err != nil {
		// 16-byte zero-padded value is always canonical; this branch is
		// defensive, hence we map to ErrInvalidProofScalar for consistency.
		return nil, ErrInvalidProofScalar
	}
	s, err := edwards25519.NewScalar().SetCanonicalBytes(proof[PublicKeySize+IntermediateSize:])
	if err != nil {
		return nil, ErrInvalidProofScalar
	}

	H, err := hashToCurveTAI(pk.pk, alpha)
	if err != nil {
		return nil, err
	}

	// U = s*B - c*Y
	U := (&edwards25519.Point{}).Subtract(
		(&edwards25519.Point{}).ScalarBaseMult(s),
		(&edwards25519.Point{}).ScalarMult(c, pk.point),
	)
	// V = s*H - c*Gamma
	V := (&edwards25519.Point{}).Subtract(
		(&edwards25519.Point{}).ScalarMult(s, H),
		(&edwards25519.Point{}).ScalarMult(c, gamma),
	)

	cPrime := hashPoints(H, gamma, U, V)
	if subtle.ConstantTimeCompare(cPrime, proof[PublicKeySize:PublicKeySize+IntermediateSize]) != 1 {
		return nil, ErrInvalidVRFProof
	}
	return proofToHashFromGamma(gamma), nil
}

// ProofToHash extracts the VRF output beta from a proof, without verifying
// it (RFC 9381 §5.2). Callers MUST run Verify separately when authenticity
// matters; this helper exists for the precompile (§4.5) which performs the
// verify step itself and only needs to surface beta on success.
//
// Even so, ProofToHash refuses obviously malformed inputs (wrong length,
// non-curve gamma) so misuse cannot crash callers.
func ProofToHash(proof []byte) ([]byte, error) {
	if len(proof) != ProofSize {
		return nil, ErrInvalidProofLength
	}
	gamma, err := (&edwards25519.Point{}).SetBytes(proof[:PublicKeySize])
	if err != nil {
		return nil, ErrInvalidProofPoint
	}
	return proofToHashFromGamma(gamma), nil
}

// --- internal helpers (RFC 9381 §5.4) ---

// hashToCurveTAI implements ECVRF_hash_to_curve_try_and_increment
// (RFC 9381 §5.4.1.1): try ctr = 0, 1, ... until SHA-512(suite || 0x01 ||
// pk || alpha || ctr || 0x00)[:32] decodes to a non-identity point, then
// multiply by the cofactor.
func hashToCurveTAI(pk, alpha []byte) (*edwards25519.Point, error) {
	for ctr := 0; ctr < 256; ctr++ {
		h := sha512.New()
		h.Write([]byte{SuiteID, dstHashToCurve})
		h.Write(pk)
		h.Write(alpha)
		h.Write([]byte{byte(ctr), 0x00})

		p, err := (&edwards25519.Point{}).SetBytes(h.Sum(nil)[:PublicKeySize])
		if err == nil && p.Equal(edwards25519.NewIdentityPoint()) == 0 {
			return (&edwards25519.Point{}).MultByCofactor(p), nil
		}
	}
	return nil, ErrHashToCurveFailed
}

// generateNonceHash computes the 64-byte SHA-512 nonce input per
// RFC 9381 §5.4.2.2 (ECVRF_nonce_generation_RFC8032). The 64-byte output
// is later reduced mod q via SetUniformBytes.
func generateNonceHash(seed, h []byte) []byte {
	skHash := sha512.New()
	skHash.Write(seed)
	out := skHash.Sum(nil)

	nonceHash := sha512.New()
	nonceHash.Write(out[SeedSize:]) // upper half of SHA512(sk_seed)
	nonceHash.Write(h)
	return nonceHash.Sum(nil)
}

// hashPoints implements ECVRF_hash_points (RFC 9381 §5.4.3): truncated
// SHA-512 challenge over (suite, 0x02, P_1, ..., P_n, 0x00).
func hashPoints(points ...*edwards25519.Point) []byte {
	h := sha512.New()
	h.Write([]byte{SuiteID, dstHashPoints})
	for _, p := range points {
		h.Write(p.Bytes())
	}
	h.Write([]byte{0x00})
	return h.Sum(nil)[:IntermediateSize]
}

// proofToHashFromGamma implements RFC 9381 §5.2 once gamma is decoded.
// beta = SHA-512(suite || 0x03 || cofactor*gamma || 0x00).
func proofToHashFromGamma(gamma *edwards25519.Point) []byte {
	gammaC := (&edwards25519.Point{}).MultByCofactor(gamma)
	h := sha512.New()
	h.Write([]byte{SuiteID, dstProofToHash})
	h.Write(gammaC.Bytes())
	h.Write([]byte{0x00})
	return h.Sum(nil)
}

// cToScalar zero-pads the 16-byte challenge into a canonical 32-byte scalar.
// 16 leading bytes are always < 2^252+... so the result is canonical.
func cToScalar(c []byte) (*edwards25519.Scalar, error) {
	buf := make([]byte, SeedSize)
	copy(buf, c)
	return edwards25519.NewScalar().SetCanonicalBytes(buf)
}
