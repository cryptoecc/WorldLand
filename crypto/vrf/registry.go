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

// Package vrf is the WorldLand VRF crypto-agility hub. The block header's
// VRFScheme byte (spec §4.1, §8.3) selects which underlying VRF cipher
// suite is used to verify a proof. All scheme dispatch is concentrated in
// this file: future hardforks adding a new VRF (e.g. a post-quantum scheme)
// only need to register it here.
package vrf

import (
	"errors"
	"fmt"

	"github.com/cryptoecc/WorldLand/crypto/vrf/ecvrf"
)

// SchemeID is the WorldLand crypto-agility byte stored in
// types.Header.VRFScheme. It is independent of the underlying RFC suite
// byte (which is, e.g., 0x03 for ECVRF-EDWARDS25519-SHA512-TAI inside
// RFC 9381's hash domain separation).
type SchemeID uint8

const (
	// SchemeECVRFEdwards25519SHA512TAI is the v.1.0 default scheme,
	// implementing RFC 9381 ECVRF-EDWARDS25519-SHA512-TAI.
	SchemeECVRFEdwards25519SHA512TAI SchemeID = 0x01

	// 0x02..0xFF are reserved for future schemes (e.g. PQ-VRF). Each
	// future scheme MUST add a case to schemeRegistry below in a single,
	// reviewable change.
)

// ErrUnknownScheme is returned for any SchemeID that has not been
// registered. Callers should treat this as a hard validation failure.
var ErrUnknownScheme = errors.New("vrf: unknown scheme id")

// Verifier is the minimal interface every registered VRF scheme must
// implement. ProofSize is the canonical proof length for that scheme;
// PublicKeySize is the canonical public-key length. Higher layers use
// these to slice the precompile input (spec §4.5) and to validate header
// field lengths (spec §4.1).
type Verifier interface {
	// Name returns a human-readable identifier (used in logs/RPC).
	Name() string
	// ProofSize is the byte length of a valid proof.
	ProofSize() int
	// PublicKeySize is the byte length of a valid public key.
	PublicKeySize() int
	// Verify checks (pk, alpha, proof) and returns the VRF output beta
	// on success, or a typed error on failure.
	Verify(pk, alpha, proof []byte) (beta []byte, err error)
	// ProofToHash extracts beta from proof without verifying. Callers
	// MUST run Verify when authenticity matters.
	ProofToHash(proof []byte) (beta []byte, err error)
}

// LookupScheme returns the Verifier registered for id, or
// (nil, ErrUnknownScheme).
func LookupScheme(id SchemeID) (Verifier, error) {
	if v, ok := schemeRegistry[id]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("%w: 0x%02x", ErrUnknownScheme, byte(id))
}

// schemeRegistry is populated via init() below. To add a new scheme:
//   1. Implement the Verifier interface in a sibling sub-package.
//   2. Reserve a new SchemeID constant above.
//   3. Add a single registration line to init().
var schemeRegistry = map[SchemeID]Verifier{}

func register(id SchemeID, v Verifier) {
	if _, ok := schemeRegistry[id]; ok {
		panic(fmt.Sprintf("vrf: duplicate registration for scheme 0x%02x", byte(id)))
	}
	schemeRegistry[id] = v
}

func init() {
	register(SchemeECVRFEdwards25519SHA512TAI, ecvrfTAIVerifier{})
}

// ecvrfTAIVerifier adapts the ecvrf package to the Verifier interface.
type ecvrfTAIVerifier struct{}

func (ecvrfTAIVerifier) Name() string      { return "ECVRF-EDWARDS25519-SHA512-TAI" }
func (ecvrfTAIVerifier) ProofSize() int    { return ecvrf.ProofSize }
func (ecvrfTAIVerifier) PublicKeySize() int { return ecvrf.PublicKeySize }

func (ecvrfTAIVerifier) Verify(pk, alpha, proof []byte) ([]byte, error) {
	publicKey, err := ecvrf.NewPublicKey(pk)
	if err != nil {
		return nil, err
	}
	return publicKey.Verify(alpha, proof)
}

func (ecvrfTAIVerifier) ProofToHash(proof []byte) ([]byte, error) {
	return ecvrf.ProofToHash(proof)
}
