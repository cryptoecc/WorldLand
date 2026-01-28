package kaiju

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/cryptoecc/WorldLand/common"
	"github.com/cryptoecc/WorldLand/consensus"
	"github.com/cryptoecc/WorldLand/core/types"
	"github.com/cryptoecc/WorldLand/crypto"
	"github.com/cryptoecc/WorldLand/log"
	"github.com/cryptoecc/WorldLand/metrics"
	"github.com/cryptoecc/WorldLand/rpc"
	"golang.org/x/crypto/sha3"
)

type ECC struct {
	config Config

	// Mining related fields
	rand     *rand.Rand    // Properly seeded random source for nonces
	threads  int           // Number of threads to mine on if mining
	update   chan struct{} // Notification channel to update mining parameters
	hashrate metrics.Meter // Meter tracking the average hashrate
	remote   *remoteSealer

	// Remote sealer related fields
	workCh       chan *sealTask   // Notification channel to push new work and relative result channel to remote sealer
	fetchWorkCh  chan *sealWork   // Channel used for remote sealer to fetch mining work
	submitWorkCh chan *mineResult // Channel used for remote sealer to submit their mining result
	fetchRateCh  chan chan uint64 // Channel used to gather submitted hash rate for local or remote sealer.
	submitRateCh chan *hashrate   // Channel used for remote sealer to submit their mining hashrate

	shared    *ECC          // Shared PoW verifier to avoid cache regeneration
	fakeFail  uint64        // Block number which fails PoW check even in fake mode
	fakeDelay time.Duration // Time delay to sleep for before returning from verify

	// VRF key pair for proof generation and verification
	vrfPublicKey  []byte         // ED25519 public key (32 bytes)
	vrfPrivateKey []byte         // ED25519 private key (64 bytes)
	vrfCoinbase   common.Address // Coinbase address used to derive current VRF keys

	lock      sync.Mutex // Ensures thread safety for the in-memory caches and mining fields
	closeOnce sync.Once  // Ensures exit channel will not be closed twice.
}

type Mode uint

const (
	// Existing epoch for DAG seed generation (DO NOT CHANGE)
	epochLength = 30000 // Blocks per epoch for DAG seed hash - used by seedHash()

	// Sortition epoch configuration (for VRF-based mining eligibility)
	SortitionEpochLength  = 100 // Blocks per sortition epoch (every 100 blocks)
	SortitionSeedLookback = 10  // Blocks before sortition epoch boundary for seed (fork resistance)

	ModeNormal Mode = iota
	ModeShared
	ModeTest
	ModeFake
	ModeFullFake
)

// SortitionEpoch returns the sortition epoch number for a given block number.
// This is used for VRF-based mining eligibility, NOT for DAG generation.
func SortitionEpoch(blockNumber uint64) uint64 {
	return blockNumber / SortitionEpochLength
}

// SortitionEpochStartBlock returns the first block number of the given sortition epoch.
func SortitionEpochStartBlock(epoch uint64) uint64 {
	return epoch * SortitionEpochLength
}

// IsSortitionEpochStart checks if the given block number is at the start of a sortition epoch.
func IsSortitionEpochStart(blockNumber uint64) bool {
	return blockNumber%SortitionEpochLength == 0
}

// GetSortitionSeedBlockNumber returns the block number to use as seed for sortition.
// Uses a block SortitionSeedLookback blocks before the sortition epoch boundary to resist fork attacks.
func GetSortitionSeedBlockNumber(blockNumber uint64) uint64 {
	epoch := SortitionEpoch(blockNumber)

	if epoch == 0 {
		return 0 // Genesis block for epoch 0
	}

	// Seed block = sortition epoch start - SortitionSeedLookback
	epochStart := SortitionEpochStartBlock(epoch)
	if epochStart > SortitionSeedLookback {
		return epochStart - SortitionSeedLookback
	}
	return 0 // Fallback to genesis if not enough blocks
}

// GetSortitionSeedHashWithBatch returns the hash to use as VRF input for sortition,
// checking the current batch of headers being verified first before querying the chain.
func (ecc *ECC) GetSortitionSeedHashWithBatch(chain consensus.ChainHeaderReader, blockNumber uint64, batchHeaders []*types.Header) common.Hash {
	seedBlockNum := GetSortitionSeedBlockNumber(blockNumber)

	// First, check if the seed block is in the current batch being verified
	// This handles the case where we're verifying blocks in parallel and the seed
	// block hasn't been imported into the chain yet but is in the same batch
	if batchHeaders != nil {
		for _, h := range batchHeaders {
			if h.Number.Uint64() == seedBlockNum {
				log.Debug("✅ Seed block found in current batch",
					"forBlock", blockNumber,
					"seedBlock", seedBlockNum,
					"seedHash", h.Hash().Hex()[:16]+"...")
				return h.Hash()
			}
		}
	}

	// Seed block not in batch, fall back to chain lookup
	return ecc.GetSortitionSeedHash(chain, blockNumber)
}

// GetSortitionSeedHash returns the hash to use as VRF input for sortition.
// This retrieves the block hash from SortitionSeedLookback blocks before the sortition epoch boundary.
func (ecc *ECC) GetSortitionSeedHash(chain consensus.ChainHeaderReader, blockNumber uint64) common.Hash {
	seedBlockNum := GetSortitionSeedBlockNumber(blockNumber)

	currentHead := chain.CurrentHeader()
	log.Info("🔍 Getting sortition seed",
		"forBlock", blockNumber,
		"needSeedBlock", seedBlockNum,
		"currentHead", currentHead.Number.Uint64(),
		"currentHash", currentHead.Hash().Hex()[:16]+"...")

	header := chain.GetHeaderByNumber(seedBlockNum)
	if header == nil {
		log.Warn("❌ SEED BLOCK MISSING",
			"forBlock", blockNumber,
			"needSeedBlock", seedBlockNum,
			"currentHead", currentHead.Number.Uint64())
		// Return empty hash - seed block must be available for verification
		return common.Hash{}
	}

	log.Info("✅ Seed block found",
		"seedBlock", seedBlockNum,
		"seedHash", header.Hash().Hex()[:16]+"...")
	return header.Hash()
}

// IsEligibleForSortitionEpoch checks if this miner is eligible to mine in the sortition epoch
// containing the given block number, using VRF-based sortition.
func (ecc *ECC) IsEligibleForSortitionEpoch(chain consensus.ChainHeaderReader, blockNumber uint64) (bool, []byte, error) {
	ecc.lock.Lock()
	defer ecc.lock.Unlock()

	// Check if VRF keys are configured
	if len(ecc.vrfPublicKey) == 0 || len(ecc.vrfPrivateKey) == 0 {
		return false, nil, errors.New("VRF keys not configured")
	}

	// Get the seed hash for this sortition epoch
	seedHash := ecc.GetSortitionSeedHash(chain, blockNumber)
	if seedHash == (common.Hash{}) {
		return false, nil, errors.New("failed to get sortition seed hash")
	}

	sortitionEpoch := SortitionEpoch(blockNumber)
	seedBlockNum := GetSortitionSeedBlockNumber(blockNumber)

	// Generate VRF proof using the seed hash
	proof, hash, err := Prove(ecc.vrfPublicKey, ecc.vrfPrivateKey, seedHash.Bytes())
	if err != nil {
		return false, nil, fmt.Errorf("failed to generate VRF proof: %w", err)
	}

	// Check if the proof passes sortition
	eligible := CheckSortition(proof)

	log.Info("🎲 Sortition epoch check",
		"sortitionEpoch", sortitionEpoch,
		"blockNumber", blockNumber,
		"seedBlock", seedBlockNum,
		"seedHash", seedHash.Hex()[:16]+"...",
		"vrfHash", hex.EncodeToString(hash)[:16]+"...",
		"eligible", eligible)

	return eligible, proof, nil
}

// Config are the configuration parameters of the ethash.
type Config struct {
	PowMode Mode
	// When set, notifications sent by the remote sealer will
	// be block header JSON objects instead of work package arrays.
	NotifyFull bool
	Log        log.Logger `toml:"-"`
}

// hasher is a repetitive hasher allowing the same hash data structures to be
// reused between hash runs instead of requiring new ones to be created.
//var hasher func(dest []byte, data []byte)

var (
	two256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))

	// sharedECC is a full instance that can be shared between multiple users.
	sharedECC *ECC

	// algorithmRevision is the data structure version used for file naming.
	algorithmRevision = 2
)

func init() {
	sharedConfig := Config{
		PowMode: ModeNormal,
	}
	sharedECC = New(sharedConfig, nil, false)
}

type verifyParameters struct {
	n          uint64
	m          uint64
	wc         uint64
	wr         uint64
	seed       uint64
	outputWord []uint64
}

//const cross_err = 0.01

//type (
//	intMatrix   [][]int
//	floatMatrix [][]float64
//)

// RunOptimizedConcurrencyLDPC use goroutine for mining block
func RunOptimizedConcurrencyLDPC(header *types.Header, hash []byte) (bool, []int, []int, uint64, []byte) {
	//Need to set difficulty before running LDPC
	// Number of goroutines : 500, Number of attempts : 50000 Not bad

	var LDPCNonce uint64
	var hashVector []int
	var outputWord []int
	var digest []byte
	var flag bool

	//var wg sync.WaitGroup
	//var outerLoopSignal = make(chan struct{})
	//var innerLoopSignal = make(chan struct{})
	//var goRoutineSignal = make(chan struct{})

	parameters, _ := setParameters(header)
	H := generateH(parameters)
	colInRow, rowInCol := generateQ(parameters, H)

	for i := 0; i < 64; i++ {
		var goRoutineHashVector []int
		var goRoutineOutputWord []int
		goRoutineNonce := generateRandomNonce()
		seed := make([]byte, 40)
		copy(seed, hash)
		binary.LittleEndian.PutUint64(seed[32:], goRoutineNonce)
		seed = crypto.Keccak512(seed)
		//fmt.Printf("nonce: %v\n", seed)

		goRoutineHashVector = generateHv(parameters, seed)
		goRoutineHashVector, goRoutineOutputWord, _ = OptimizedDecoding(parameters, goRoutineHashVector, H, rowInCol, colInRow)

		flag, _ = MakeDecision(header, colInRow, goRoutineOutputWord)

		if flag {
			hashVector = goRoutineHashVector
			outputWord = goRoutineOutputWord
			LDPCNonce = goRoutineNonce
			digest = seed
			break
		}
	}
	return flag, hashVector, outputWord, LDPCNonce, digest
}

func RunOptimizedConcurrencyLDPC_Seoul(header *types.Header, hash []byte) (bool, []int, []int, uint64, []byte) {
	//Need to set difficulty before running LDPC
	// Number of goroutines : 500, Number of attempts : 50000 Not bad

	var LDPCNonce uint64
	var hashVector []int
	var outputWord []int
	var digest []byte
	var flag bool

	//var wg sync.WaitGroup
	//var outerLoopSignal = make(chan struct{})
	//var innerLoopSignal = make(chan struct{})
	//var goRoutineSignal = make(chan struct{})

	parameters, _ := setParameters_Seoul(header)
	H := generateH(parameters)
	colInRow, rowInCol := generateQ(parameters, H)

	for i := 0; i < 64; i++ {
		var goRoutineHashVector []int
		var goRoutineOutputWord []int
		goRoutineNonce := generateRandomNonce()
		seed := make([]byte, 40)
		copy(seed, hash)
		binary.LittleEndian.PutUint64(seed[32:], goRoutineNonce)
		seed = crypto.Keccak512(seed)
		//fmt.Printf("nonce: %v\n", seed)

		goRoutineHashVector = generateHv(parameters, seed)
		goRoutineHashVector, goRoutineOutputWord, _ = OptimizedDecodingSeoul(parameters, goRoutineHashVector, H, rowInCol, colInRow)

		flag, _ = MakeDecision_Seoul(header, colInRow, goRoutineOutputWord)

		if flag {
			hashVector = goRoutineHashVector
			outputWord = goRoutineOutputWord
			LDPCNonce = goRoutineNonce
			digest = seed
			break
		}
	}
	return flag, hashVector, outputWord, LDPCNonce, digest
}

// MakeDecision check outputWord is valid or not using colInRow
func MakeDecision(header *types.Header, colInRow [][]int, outputWord []int) (bool, int) {
	parameters, difficultyLevel := setParameters(header)
	for i := 0; i < parameters.m; i++ {
		sum := 0
		for j := 0; j < parameters.wr; j++ {
			//	fmt.Printf("i : %d, j : %d, m : %d, wr : %d \n", i, j, m, wr)
			sum = sum + outputWord[colInRow[j][i]]
		}
		if sum%2 == 1 {
			return false, -1
		}
	}

	var numOfOnes int
	for _, val := range outputWord {
		numOfOnes += val
	}

	if numOfOnes >= Table[difficultyLevel].decisionFrom &&
		numOfOnes <= Table[difficultyLevel].decisionTo &&
		numOfOnes%Table[difficultyLevel].decisionStep == 0 {
		//fmt.Printf("hamming weight: %v\n", numOfOnes)
		return true, numOfOnes
	}

	return false, numOfOnes
}

// MakeDecision check outputWord is valid or not using colInRow
func MakeDecision_Seoul(header *types.Header, colInRow [][]int, outputWord []int) (bool, int) {
	parameters, _ := setParameters_Seoul(header)
	for i := 0; i < parameters.m; i++ {
		sum := 0
		for j := 0; j < parameters.wr; j++ {
			//	fmt.Printf("i : %d, j : %d, m : %d, wr : %d \n", i, j, m, wr)
			sum = sum + outputWord[colInRow[j][i]]
		}
		if sum%2 == 1 {
			return false, -1
		}
	}

	var numOfOnes int
	for _, val := range outputWord {
		numOfOnes += val
	}

	if numOfOnes >= parameters.n/4 &&
		numOfOnes <= parameters.n/4*3 {
		//fmt.Printf("hamming weight: %v\n", numOfOnes)
		return true, numOfOnes
	}

	return false, numOfOnes
}

//func isRegular(nSize, wCol, wRow int) bool {
//	res := float64(nSize*wCol) / float64(wRow)
//	m := math.Round(res)
//
//	if int(m)*wRow == nSize*wCol {
//		return true
//	}
//
//	return false
//}

//func SetDifficulty(nSize, wCol, wRow int) bool {
//	if isRegular(nSize, wCol, wRow) {
//		n = nSize
//		wc = wCol
//		wr = wRow
//		m = int(n * wc / wr)
//		return true
//	}
//	return false
//}

//func newIntMatrix(rows, cols int) intMatrix {
//	m := intMatrix(make([][]int, rows))
//	for i := range m {
//		m[i] = make([]int, cols)
//	}
//	return m
//}
//
//func newFloatMatrix(rows, cols int) floatMatrix {
//	m := floatMatrix(make([][]float64, rows))
//	for i := range m {
//		m[i] = make([]float64, cols)
//	}
//	return m
//}

// New creates a full sized ethash PoW scheme and starts a background thread for
// remote mining, also optionally notifying a batch of remote services of new work
// packages.

func New(config Config, notify []string, noverify bool) *ECC {
	if config.Log == nil {
		config.Log = log.Root()
	}
	ecc := &ECC{
		config:       config,
		update:       make(chan struct{}),
		hashrate:     metrics.NewMeterForced(),
		workCh:       make(chan *sealTask),
		fetchWorkCh:  make(chan *sealWork),
		submitWorkCh: make(chan *mineResult),
		fetchRateCh:  make(chan chan uint64),
		submitRateCh: make(chan *hashrate),
	}
	if config.PowMode == ModeShared {
		ecc.shared = sharedECC
	}
	ecc.remote = startRemoteSealer(ecc, notify, noverify)
	return ecc
}

func NewTester(notify []string, noverify bool) *ECC {
	ecc := &ECC{
		config:       Config{PowMode: ModeTest},
		update:       make(chan struct{}),
		hashrate:     metrics.NewMeterForced(),
		workCh:       make(chan *sealTask),
		fetchWorkCh:  make(chan *sealWork),
		submitWorkCh: make(chan *mineResult),
		fetchRateCh:  make(chan chan uint64),
		submitRateCh: make(chan *hashrate),
	}
	ecc.remote = startRemoteSealer(ecc, notify, noverify)
	return ecc
}

// NewFaker creates a ethash consensus engine with a fake PoW scheme that accepts
// all blocks' seal as valid, though they still have to conform to the Ethereum
// consensus rules.
func NewFaker() *ECC {
	return &ECC{
		config: Config{
			PowMode: ModeFake,
			Log:     log.Root(),
		},
	}
}

// NewFakeFailer creates a ethash consensus engine with a fake PoW scheme that
// accepts all blocks as valid apart from the single one specified, though they
// still have to conform to the Ethereum consensus rules.
func NewFakeFailer(fail uint64) *ECC {
	return &ECC{
		config: Config{
			PowMode: ModeFake,
			Log:     log.Root(),
		},
		fakeFail: fail,
	}
}

// NewFakeDelayer creates a ethash consensus engine with a fake PoW scheme that
// accepts all blocks as valid, but delays verifications by some time, though
// they still have to conform to the Ethereum consensus rules.
func NewFakeDelayer(delay time.Duration) *ECC {
	return &ECC{
		config: Config{
			PowMode: ModeFake,
			Log:     log.Root(),
		},
		fakeDelay: delay,
	}
}

// NewFullFaker creates an ethash consensus engine with a full fake scheme that
// accepts all blocks as valid, without checking any consensus rules whatsoever.
func NewFullFaker() *ECC {
	return &ECC{
		config: Config{
			PowMode: ModeFullFake,
			Log:     log.Root(),
		},
	}
}

// NewShared creates a full sized ethash PoW shared between all requesters running
// in the same process.
//func NewShared() *ECC {
//	return &ECC{shared: sharedECC}
//}

// Close closes the exit channel to notify all backend threads exiting.
func (ecc *ECC) Close() error {
	return ecc.StopRemoteSealer()
}

// StopRemoteSealer stops the remote sealer
func (ecc *ECC) StopRemoteSealer() error {
	ecc.closeOnce.Do(func() {
		// Short circuit if the exit channel is not allocated.
		if ecc.remote == nil {
			return
		}
		close(ecc.remote.requestExit)
		<-ecc.remote.exitCh
	})
	return nil
}

// Threads returns the number of mining threads currently enabled. This doesn't
// necessarily mean that mining is running!
func (ecc *ECC) Threads() int {
	ecc.lock.Lock()
	defer ecc.lock.Unlock()

	return ecc.threads
}

// SetThreads updates the number of mining threads currently enabled. Calling
// this method does not start mining, only sets the thread count. If zero is
// specified, the miner will use all cores of the machine. Setting a thread
// count below zero is allowed and will cause the miner to idle, without any
// work being done.
func (ecc *ECC) SetThreads(threads int) {
	ecc.lock.Lock()
	defer ecc.lock.Unlock()

	// If we're running a shared PoW, set the thread count on that instead
	if ecc.shared != nil {
		ecc.shared.SetThreads(threads)
		return
	}
	// Update the threads and ping any running seal to pull in any changes
	ecc.threads = threads
	select {
	case ecc.update <- struct{}{}:
	default:
	}
}

// SetVRFKeys sets the VRF key pair used for proof generation
// The publicKey should be 32 bytes (ED25519 public key)
// The privateKey should be 64 bytes (ED25519 private key)
func (ecc *ECC) SetVRFKeys(publicKey, privateKey []byte, coinbase common.Address) {
	ecc.lock.Lock()
	defer ecc.lock.Unlock()

	ecc.vrfPublicKey = make([]byte, len(publicKey))
	copy(ecc.vrfPublicKey, publicKey)

	ecc.vrfPrivateKey = make([]byte, len(privateKey))
	copy(ecc.vrfPrivateKey, privateKey)

	ecc.vrfCoinbase = coinbase
}

// EnsureVRFKeys checks if VRF keys match the given coinbase, and re-derives them if not.
// This should be called before mining to ensure keys are in sync with current coinbase.
func (ecc *ECC) EnsureVRFKeys(coinbase common.Address) error {
	ecc.lock.Lock()
	defer ecc.lock.Unlock()

	// Check if keys already match this coinbase
	if ecc.vrfCoinbase == coinbase && len(ecc.vrfPublicKey) > 0 && len(ecc.vrfPrivateKey) > 0 {
		return nil // Already up to date
	}

	// Need to derive new keys
	pubKey, privKey, err := DeriveVRFKeys(coinbase, nil)
	if err != nil {
		return fmt.Errorf("failed to derive VRF keys: %w", err)
	}

	ecc.vrfPublicKey = make([]byte, len(pubKey))
	copy(ecc.vrfPublicKey, pubKey)

	ecc.vrfPrivateKey = make([]byte, len(privKey))
	copy(ecc.vrfPrivateKey, privKey)

	ecc.vrfCoinbase = coinbase

	log.Info("🔑 VRF keys re-derived for coinbase change", "coinbase", coinbase)
	return nil
}

// Hashrate implements PoW, returning the measured rate of the search invocations
// per second over the last minute.
// Note the returned hashrate includes local hashrate, but also includes the total
// hashrate of all remote miner.
func (ecc *ECC) Hashrate() float64 {
	// Short circuit if we are run the ecc in normal/test mode.

	var res = make(chan uint64, 1)

	select {
	case ecc.remote.fetchRateCh <- res:
	case <-ecc.remote.exitCh:
		// Return local hashrate only if ecc is stopped.
		return ecc.hashrate.Rate1()
	}

	// Gather total submitted hash rate of remote sealers.
	return ecc.hashrate.Rate1() + float64(<-res)
}

// APIs implements consensus.Engine, returning the user facing RPC APIs.
func (ecc *ECC) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	// In order to ensure backward compatibility, we exposes ecc RPC APIs
	// to both eth and ecc namespaces.
	return []rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   &API{ecc},
			Public:    true,
		},
		{
			Namespace: "ecc",
			Version:   "1.0",
			Service:   &API{ecc},
			Public:    true,
		},
	}
}

// hasher is a repetitive hasher allowing the same hash data structures to be
// reused between hash runs instead of requiring new ones to be created.
type hasher func(dest []byte, data []byte)

// makeHasher creates a repetitive hasher, allowing the same hash data structures to
// be reused between hash runs instead of requiring new ones to be created. The returned
// function is not thread safe!
func makeHasher(h hash.Hash) hasher {
	// sha3.state supports Read to get the sum, use it to avoid the overhead of Sum.
	// Read alters the state but we reset the hash before every operation.
	type readerHash interface {
		hash.Hash
		Read([]byte) (int, error)
	}
	rh, ok := h.(readerHash)
	if !ok {
		panic("can't find Read method on hash")
	}
	outputLen := rh.Size()
	return func(dest []byte, data []byte) {
		rh.Reset()
		rh.Write(data)
		rh.Read(dest[:outputLen])
	}
}

// seedHash is the seed to use for generating a verification cache and the mining
// dataset.
func seedHash(block uint64) []byte {
	seed := make([]byte, 32)
	if block < epochLength {
		return seed
	}
	keccak256 := makeHasher(sha3.NewLegacyKeccak256())
	for i := 0; i < int(block/epochLength); i++ {
		keccak256(seed, seed)
	}
	return seed
}

// // SeedHash is the seed to use for generating a verification cache and the mining
// // dataset.
func SeedHash(block uint64) []byte {
	return seedHash(block)
}
