// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package kaiju

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/cryptoecc/WorldLand/common"
	"github.com/cryptoecc/WorldLand/common/math"
	"github.com/cryptoecc/WorldLand/core/types"
)

type diffTest struct {
	ParentTimestamp    uint64
	ParentDifficulty   *big.Int
	CurrentTimestamp   uint64
	CurrentBlocknumber *big.Int
	CurrentDifficulty  *big.Int
}

func (d *diffTest) UnmarshalJSON(b []byte) (err error) {
	var ext struct {
		ParentTimestamp    string
		ParentDifficulty   string
		CurrentTimestamp   string
		CurrentBlocknumber string
		CurrentDifficulty  string
	}
	if err := json.Unmarshal(b, &ext); err != nil {
		return err
	}

	d.ParentTimestamp = math.MustParseUint64(ext.ParentTimestamp)
	d.ParentDifficulty = math.MustParseBig256(ext.ParentDifficulty)
	d.CurrentTimestamp = math.MustParseUint64(ext.CurrentTimestamp)
	d.CurrentBlocknumber = math.MustParseBig256(ext.CurrentBlocknumber)
	d.CurrentDifficulty = math.MustParseBig256(ext.CurrentDifficulty)

	return nil
}

func TestDecodingVerification(t *testing.T) {
	for i := 0; i < 8; i++ {
		ecc := ECC{}
		header := new(types.Header)
		header.Difficulty = ProbToDifficulty(Table[0].miningProb)
		hash := ecc.SealHash(header).Bytes()

		_, hashVector, outputWord, LDPCNonce, digest := RunOptimizedConcurrencyLDPC(header, hash)

		headerForTest := types.CopyHeader(header)
		headerForTest.MixDigest = common.BytesToHash(digest)
		headerForTest.Nonce = types.EncodeNonce(LDPCNonce)
		hashForTest := ecc.SealHash(headerForTest).Bytes()

		flag, hashVectorOfVerification, outputWordOfVerification, digestForValidation := VerifyOptimizedDecoding(headerForTest, hashForTest)

		encodedDigestForValidation := common.BytesToHash(digestForValidation)

		//fmt.Printf("%+v\n", header)
		//fmt.Printf("Hash : %v\n", hash)
		//fmt.Println()

		//fmt.Printf("%+v\n", headerForTest)
		//fmt.Printf("headerForTest : %v\n", headerForTest)
		//fmt.Println()

		// * means padding for compare easily
		if flag && bytes.Equal(headerForTest.MixDigest[:], encodedDigestForValidation[:]) {
			fmt.Printf("Hash vector ** ************ : %v\n", hashVector)
			fmt.Printf("Hash vector of verification : %v\n", hashVectorOfVerification)

			fmt.Printf("Outputword ** ************ : %v\n", outputWord)
			fmt.Printf("Outputword of verification : %v\n", outputWordOfVerification)

			fmt.Printf("LDPC Nonce : %v\n", LDPCNonce)
			fmt.Printf("Digest : %v\n", headerForTest.MixDigest[:])
			/*
				t.Logf("Hash vector : %v\n", hashVector)
				t.Logf("Outputword : %v\n", outputWord)
				t.Logf("LDPC Nonce : %v\n", LDPCNonce)
				t.Logf("Digest : %v\n", header.MixDigest[:])
			*/
		} else {
			fmt.Printf("Hash vector ** ************ : %v\n", hashVector)
			fmt.Printf("Hash vector of verification : %v\n", hashVectorOfVerification)

			fmt.Printf("Outputword ** ************ : %v\n", outputWord)
			fmt.Printf("Outputword of verification : %v\n", outputWordOfVerification)

			fmt.Printf("flag : %v\n", flag)
			fmt.Printf("Digest compare result : %v\n", bytes.Equal(headerForTest.MixDigest[:], encodedDigestForValidation[:]))
			fmt.Printf("Digest *** ********** : %v\n", headerForTest.MixDigest[:])
			fmt.Printf("Digest for validation : %v\n", encodedDigestForValidation)

			t.Errorf("Test Fail")
			/*
				t.Errorf("flag : %v\n", flag)
				t.Errorf("Digest compare result : %v", bytes.Equal(header.MixDigest[:], digestForValidation)
				t.Errorf("Digest : %v\n", digest)
				t.Errorf("Digest for validation : %v\n", digestForValidation)
			*/
		}
		//t.Logf("\n")
		fmt.Println()
	}
}

// TestVRFProofWithoutProof tests that blocks without VRF proof are rejected
func TestVRFProofWithoutProof(t *testing.T) {
	ecc := &ECC{}
	header := &types.Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(1000),
		Time:       1234567890,
	}

	err := ecc.verifyVRFProof(header)
	if err == nil {
		t.Fatal("Expected error for header without VRF proof")
	}
	if err.Error() != "VRF proof is required but missing" {
		t.Errorf("Expected 'VRF proof is required but missing', got '%s'", err.Error())
	}
}

// TestVRFProofWithoutPublicKey tests that blocks without VRF public key are rejected
func TestVRFProofWithoutPublicKey(t *testing.T) {
	ecc := &ECC{}
	header := &types.Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(1000),
		Time:       1234567890,
		VRFProof:   []byte("dummy_proof"),
	}

	err := ecc.verifyVRFProof(header)
	if err == nil {
		t.Fatal("Expected error for VRF proof without public key")
	}
	if err.Error() != "VRF public key is required but missing" {
		t.Errorf("Expected 'VRF public key is required but missing', got '%s'", err.Error())
	}
}

// TestVRFProofInvalid tests that blocks with invalid VRF proofs are rejected
func TestVRFProofInvalid(t *testing.T) {
	ecc := &ECC{}
	pk, _ := KeyGen()

	header := &types.Header{
		Number:       big.NewInt(1),
		Difficulty:   big.NewInt(1000),
		Time:         1234567890,
		VRFProof:     []byte("invalid_proof_data"),
		VRFPublicKey: pk,
	}

	err := ecc.verifyVRFProof(header)
	if err == nil {
		t.Fatal("Expected error for invalid VRF proof")
	}
}

// TestVRFProofMismatchedKey tests that blocks with mismatched key and proof are rejected
func TestVRFProofMismatchedKey(t *testing.T) {
	ecc := &ECC{}
	pk1, sk1 := KeyGen()
	pk2, _ := KeyGen()

	message := []byte("test message")
	pi, _, err := Prove(pk1, sk1, message)
	if err != nil {
		t.Fatalf("Failed to generate VRF proof: %v", err)
	}

	header := &types.Header{
		Number:       big.NewInt(1),
		Difficulty:   big.NewInt(1000),
		Time:         1234567890,
		VRFProof:     pi,
		VRFPublicKey: pk2,
	}

	err = ecc.verifyVRFProof(header)
	if err == nil {
		t.Fatal("Expected error for mismatched key and proof")
	}
}

// TestVRFProofSortition tests that blocks are accepted or rejected based on sortition
func TestVRFProofSortition(t *testing.T) {
	ecc := &ECC{}
	numTests := 10
	passCount := 0

	for i := 0; i < numTests; i++ {
		pk, sk := KeyGen()
		message := ecc.SealHash(&types.Header{
			Number:     big.NewInt(int64(i + 1)),
			Difficulty: big.NewInt(1000),
			Time:       uint64(1234567890 + i),
		}).Bytes()

		pi, _, err := Prove(pk, sk, message)
		if err != nil {
			t.Fatalf("Failed to generate VRF proof: %v", err)
		}

		header := &types.Header{
			Number:       big.NewInt(int64(i + 1)),
			Difficulty:   big.NewInt(1000),
			Time:         uint64(1234567890 + i),
			VRFProof:     pi,
			VRFPublicKey: pk,
		}

		err = ecc.verifyVRFProof(header)
		passedSortition := CheckSortition(pi)

		if passedSortition {
			if err != nil {
				t.Errorf("Block %d: valid VRF proof passing sortition was rejected: %v", i, err)
			} else {
				passCount++
			}
		} else {
			if err == nil {
				t.Errorf("Block %d: VRF proof failing sortition was accepted", i)
			}
		}
	}
}

// TestVRFProofDeterministic tests that VRF verification is deterministic
func TestVRFProofDeterministic(t *testing.T) {
	ecc := &ECC{}
	pk, sk := KeyGen()
	message := []byte("deterministic test")
	pi, _, err := Prove(pk, sk, message)
	if err != nil {
		t.Fatalf("Failed to generate VRF proof: %v", err)
	}

	header := &types.Header{
		Number:       big.NewInt(100),
		Difficulty:   big.NewInt(1000),
		Time:         1234567890,
		VRFProof:     pi,
		VRFPublicKey: pk,
	}

	firstErr := ecc.verifyVRFProof(header)
	for i := 0; i < 20; i++ {
		err := ecc.verifyVRFProof(header)
		if (firstErr == nil) != (err == nil) {
			t.Fatalf("Verification not deterministic: first=%v, iteration %d=%v", firstErr, i, err)
		}
		if firstErr != nil && err != nil && firstErr.Error() != err.Error() {
			t.Fatalf("Error message changed: first=%v, iteration %d=%v", firstErr, i, err)
		}
	}
}

// TestVRFProofCorrectSubmission tests that blocks with valid VRF proofs passing sortition are accepted
func TestVRFProofCorrectSubmission(t *testing.T) {
	ecc := &ECC{}

	maxAttempts := 10
	accepted := false

	for attempt := 0; attempt < maxAttempts; attempt++ {
		pk, sk := KeyGen()
		message := ecc.SealHash(&types.Header{
			Number:     big.NewInt(int64(attempt + 1)),
			Difficulty: big.NewInt(1000),
			Time:       uint64(1234567890 + attempt),
		}).Bytes()

		pi, hash, err := Prove(pk, sk, message)
		if err != nil {
			t.Fatalf("Failed to generate VRF proof: %v", err)
		}

		// Check if this proof passes sortition
		if !CheckSortition(pi) {
			continue
		}

		// Found a proof that passes sortition, now verify it's accepted
		header := &types.Header{
			Number:       big.NewInt(int64(attempt + 1)),
			Difficulty:   big.NewInt(1000),
			Time:         uint64(1234567890 + attempt),
			VRFProof:     pi,
			VRFPublicKey: pk,
		}

		err = ecc.verifyVRFProof(header)
		if err != nil {
			t.Fatalf("Valid VRF proof passing sortition was rejected: %v", err)
		}

		// Verify the proof cryptographically
		valid, err := Verify(pk, pi, message)
		if err != nil {
			t.Fatalf("VRF verification failed: %v", err)
		}
		if !valid {
			t.Fatal("VRF proof verification returned false")
		}

		t.Logf("✓ Correct VRF proof accepted (attempt %d)", attempt+1)
		t.Logf("  VRF output: %x", hash)
		t.Logf("  Sortition: PASSED")
		accepted = true
		break
	}

	if !accepted {
		t.Fatalf("Failed to generate a proof passing sortition after %d attempts", maxAttempts)
	}
}

// TestVRFProofMultipleValid tests multiple valid VRF proofs are accepted when they pass sortition
func TestVRFProofMultipleValid(t *testing.T) {
	ecc := &ECC{}

	// Generate multiple valid proofs
	validCount := 0
	maxTests := 50
	targetValid := 3 // Try to get at least 3 valid proofs

	for i := 0; i < maxTests && validCount < targetValid; i++ {
		pk, sk := KeyGen()
		message := ecc.SealHash(&types.Header{
			Number:     big.NewInt(int64(i + 1)),
			Difficulty: big.NewInt(1000),
			Time:       uint64(1234567890 + i),
		}).Bytes()

		pi, hash, err := Prove(pk, sk, message)
		if err != nil {
			t.Fatalf("Failed to generate VRF proof: %v", err)
		}

		// Only test proofs that pass sortition
		if !CheckSortition(pi) {
			continue
		}

		header := &types.Header{
			Number:       big.NewInt(int64(i + 1)),
			Difficulty:   big.NewInt(1000),
			Time:         uint64(1234567890 + i),
			VRFProof:     pi,
			VRFPublicKey: pk,
		}

		err = ecc.verifyVRFProof(header)
		if err != nil {
			t.Errorf("Block %d: valid VRF proof was rejected: %v", i, err)
			continue
		}

		validCount++
		t.Logf("✓ Block %d: VRF proof accepted, output=%x", i, hash[:8])
	}

	if validCount == 0 {
		t.Fatalf("No valid proofs passed sortition in %d attempts", maxTests)
	}

	t.Logf("\n=== Summary: %d/%d blocks with valid VRF proofs were accepted ===", validCount, maxTests)
}
