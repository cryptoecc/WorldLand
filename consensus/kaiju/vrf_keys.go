package kaiju

import (
	"crypto/ed25519"
	"crypto/sha512"

	"github.com/cryptoecc/WorldLand/common"
)

// DeriveVRFKeys derives deterministic ED25519 VRF keys from the coinbase address
// This allows the VRF keys to be derived from the miner's coinbase without additional key management
// The nodeKey parameter can be a node's ECDSA private key bytes for additional entropy
func DeriveVRFKeys(coinbase common.Address, nodeKey []byte) (publicKey, privateKey []byte, err error) {
	// Combine coinbase address and node's private key (if available) to create seed
	message := append([]byte("WorldLand VRF Key Derivation:"), coinbase.Bytes()...)
	if len(nodeKey) > 0 {
		message = append(message, nodeKey...)
	}

	// Hash to get seed
	seed := sha512.Sum512(message)

	// Generate ED25519 key from seed
	reader := &deterministicReader{seed: seed[:]}
	pub, priv, err := ed25519.GenerateKey(reader)
	if err != nil {
		return nil, nil, err
	}

	return pub, priv, nil
}

// deterministicReader is a deterministic random reader for key generation
type deterministicReader struct {
	seed  []byte
	index int
}

func (r *deterministicReader) Read(p []byte) (n int, err error) {
	for i := range p {
		if r.index >= len(r.seed) {
			// Re-hash the seed if we need more randomness
			hash := sha512.Sum512(r.seed)
			r.seed = hash[:]
			r.index = 0
		}
		p[i] = r.seed[r.index]
		r.index++
	}
	return len(p), nil
}
