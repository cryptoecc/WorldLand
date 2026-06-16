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

package ecvrf

import "errors"

// Typed errors required by spec §8.4. Higher layers (consensus engine,
// precompile) wrap these with block-height / DID context as needed.
var (
	ErrInvalidPrivateKey  = errors.New("ecvrf: invalid private key")
	ErrInvalidPublicKey   = errors.New("ecvrf: invalid public key")
	ErrInvalidProofLength = errors.New("ecvrf: invalid proof length")
	ErrInvalidProofPoint  = errors.New("ecvrf: invalid proof curve point")
	ErrInvalidProofScalar = errors.New("ecvrf: invalid proof scalar")
	ErrInvalidVRFProof    = errors.New("ecvrf: VRF proof failed verification")
	ErrHashToCurveFailed  = errors.New("ecvrf: hash-to-curve exhausted counter")
)
