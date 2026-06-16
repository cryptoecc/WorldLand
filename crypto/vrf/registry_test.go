// Copyright 2026 The WorldLand / go-ethereum Authors
// This file is part of the WorldLand library.
//
// The WorldLand library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package vrf

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/cryptoecc/WorldLand/crypto/vrf/ecvrf"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// TestLookupScheme_Default confirms SchemeID 0x01 dispatches to the RFC 9381
// TAI implementation and that constants stay aligned with the ecvrf
// package (spec §8.3).
func TestLookupScheme_Default(t *testing.T) {
	v, err := LookupScheme(SchemeECVRFEdwards25519SHA512TAI)
	if err != nil {
		t.Fatalf("LookupScheme: %v", err)
	}
	if got, want := v.Name(), "ECVRF-EDWARDS25519-SHA512-TAI"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if v.ProofSize() != ecvrf.ProofSize {
		t.Errorf("ProofSize() = %d, want %d", v.ProofSize(), ecvrf.ProofSize)
	}
	if v.PublicKeySize() != ecvrf.PublicKeySize {
		t.Errorf("PublicKeySize() = %d, want %d", v.PublicKeySize(), ecvrf.PublicKeySize)
	}
}

// TestLookupScheme_Unknown checks every unregistered scheme byte returns
// the typed ErrUnknownScheme.
func TestLookupScheme_Unknown(t *testing.T) {
	for id := 0; id < 256; id++ {
		if SchemeID(id) == SchemeECVRFEdwards25519SHA512TAI {
			continue
		}
		_, err := LookupScheme(SchemeID(id))
		if !errors.Is(err, ErrUnknownScheme) {
			t.Errorf("scheme 0x%02x err = %v, want ErrUnknownScheme", id, err)
		}
	}
}

// TestRegistryVerify_RFC9381Vector1 dispatches an RFC 9381 §B.3 Example 1
// verification through the registry to confirm the wiring is correct
// end-to-end (header byte → scheme → verify → beta).
func TestRegistryVerify_RFC9381Vector1(t *testing.T) {
	pk := mustHex(t, "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
	pi := mustHex(t,
		"8657106690b5526245a92b003bb079ccd1a92130477671f6fc01ad16f26f723f"+
			"5e8bd1839b414219e8626d393787a192241fc442e6569e96c462f62b8079b9ed"+
			"83ff2ee21c90c7c398802fdeebea4001")
	wantBeta := mustHex(t,
		"90cf1df3b703cce59e2a35b925d411164068269d7b2d29f3301c03dd757876ff"+
			"66b71dda49d2de59d03450451af026798e8f81cd2e333de5cdf4f3e140fdd8ae")

	v, err := LookupScheme(SchemeECVRFEdwards25519SHA512TAI)
	if err != nil {
		t.Fatalf("LookupScheme: %v", err)
	}
	beta, err := v.Verify(pk, []byte{}, pi)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !bytes.Equal(beta, wantBeta) {
		t.Fatalf("beta mismatch:\n got=%x\nwant=%x", beta, wantBeta)
	}

	// ProofToHash standalone path agrees.
	beta2, err := v.ProofToHash(pi)
	if err != nil {
		t.Fatalf("ProofToHash: %v", err)
	}
	if !bytes.Equal(beta2, wantBeta) {
		t.Fatalf("ProofToHash beta mismatch")
	}
}

// TestRegistryVerify_BadInputs confirms the registry forwards typed
// errors from the underlying suite.
func TestRegistryVerify_BadInputs(t *testing.T) {
	v, _ := LookupScheme(SchemeECVRFEdwards25519SHA512TAI)

	// Wrong public-key length.
	if _, err := v.Verify(make([]byte, 16), []byte{}, make([]byte, ecvrf.ProofSize)); !errors.Is(err, ecvrf.ErrInvalidPublicKey) {
		t.Errorf("short pk err = %v, want ErrInvalidPublicKey", err)
	}
	// Wrong proof length.
	pk := mustHex(t, "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
	if _, err := v.Verify(pk, []byte{}, make([]byte, 79)); !errors.Is(err, ecvrf.ErrInvalidProofLength) {
		t.Errorf("short proof err = %v, want ErrInvalidProofLength", err)
	}
}
