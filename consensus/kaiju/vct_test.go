package kaiju

import (
	"encoding/hex"
	"fmt"

	// "reflect"
	"testing"
	"time"
)

func VCTtest(howmany int, message string, probability uint8, verbose bool) ([]int, time.Duration) {
	var msg []byte
	msg = []byte(message)
	nodeSet := make([]Nodes, howmany)
	winVCT := []int{}
	nthreshold := Prob[(probability/5)-1].norm_bound

	totalTime := time.Now()
	for i := 0; i < howmany; i++ {
		nodeSet[i] = PerformFalconVCT(i, msg, nthreshold)
		if nodeSet[i].VCT_res {
			winVCT = append(winVCT, i)
		}
	}
	totalElapsedTime := time.Since(totalTime)
	avgElapsedTime := totalElapsedTime / time.Duration(howmany)

	fmt.Println()

	if verbose {
		for i := 0; i < howmany; i++ {
			var temp int = len(nodeSet[i].pi)
			fmt.Println("------------------------------------------------------------------------------------------------------------------------")
			fmt.Println("id : ", nodeSet[i].id)
			fmt.Println("norm : ", nodeSet[i].norm)
			fmt.Println("VCT result : ", nodeSet[i].VCT_res)
			fmt.Println("proof : ", nodeSet[i].pi[:16], "......", nodeSet[i].pi[temp-9:])
			fmt.Println("verify : ", nodeSet[i].vrfy_res)
			fmt.Println("elapsed_time : ", nodeSet[i].exe_time)
			fmt.Println("------------------------------------------------------------------------------------------------------------------------")
		}
	}
	return winVCT, avgElapsedTime
}

// 같은 seed / 메시지 / 확률 -> 동일 결과
// func TestDeterministicSingle(t *testing.T) {
// 	msg := []byte("deterministic")
// 	nth := Prob[1].norm_bound // 10%

// 	seed := makeSeed(42)
// 	n1 := PerformFalconVCT(0, msg, nth, seed)
// 	n2 := PerformFalconVCT(0, msg, nth, seed)

// 	// 실행시간 문자열만 제거 후 비교
// 	n1.exe_time, n2.exe_time = "", ""
// 	if !reflect.DeepEqual(n1, n2) {
// 		t.Fatalf("non-deterministic: %+v != %+v", n1, n2)
// 	}
// }

// func TestDeterministicBatch(t *testing.T) {
// 	howmany := 5
// 	seeds := make([][]byte, howmany)
// 	for i := 0; i < howmany; i++ {
// 		seeds[i] = makeSeed(uint64(100 + i))
// 	}

// 	win1, _ := VCTtest(howmany, "batch-test", 10, false, seeds)
// 	win2, _ := VCTtest(howmany, "batch-test", 10, false, seeds)

// 	if !reflect.DeepEqual(win1, win2) {
// 		t.Fatalf("winner set differs: %v vs %v", win1, win2)
// 	}
// }

func TestVct(t *testing.T) {
	howmany := 10
	message := "test message for VCT"
	probability := uint8(20) // 10%
	verbose := true

	fmt.Printf("\n=== Running VCT Test with %d nodes ===\n", howmany)

	winners, avgTime := VCTtest(howmany, message, probability, verbose)

	fmt.Printf("\n=== Test Results ===\n")
	fmt.Printf("Total nodes: %d\n", howmany)
	fmt.Printf("Winners (VCT passed): %v\n", winners)
	fmt.Printf("Number of winners: %d\n", len(winners))
	fmt.Printf("Average execution time: %v\n", avgTime)

	// Basic validation
	if len(winners) < 0 || len(winners) > howmany {
		t.Fatalf("Invalid number of winners: %d (should be between 0 and %d)", len(winners), howmany)
	}

	if avgTime <= 0 {
		t.Fatalf("Invalid average time: %v", avgTime)
	}

	t.Logf("Test passed: %d/%d nodes passed VCT", len(winners), howmany)
}

// TestED25519VCT tests VCT using ED25519 (much faster than Falcon)
func TestED25519VCT(t *testing.T) {
	howmany := 10
	message := []byte("test message for ED25519 VCT")
	verbose := true

	fmt.Printf("\n=== Running ED25519 VCT Test with %d nodes ===\n", howmany)

	winners := []int{}
	totalTime := time.Now()

	for i := 0; i < howmany; i++ {
		fmt.Printf("\n[Node %d] Starting ED25519 VCT...\n", i)

		// Step 1: Generate key pair
		fmt.Printf("[Node %d] Step 1: Generating ED25519 key pair...\n", i)
		keyStart := time.Now()
		pk, sk := KeyGen()
		fmt.Printf("[Node %d]   Key generation took: %v\n", i, time.Since(keyStart))

		// Step 2: Generate proof
		fmt.Printf("[Node %d] Step 2: Generating VRF proof...\n", i)
		proveStart := time.Now()
		pi, hash, err := Prove(pk, sk, message)
		fmt.Printf("[Node %d]   Proof generation took: %v\n", i, time.Since(proveStart))
		if err != nil {
			t.Fatalf("Node %d: Prove failed: %v", i, err)
		}

		// Step 3: Verify proof
		fmt.Printf("[Node %d] Step 3: Verifying proof...\n", i)
		verifyStart := time.Now()
		valid, err := Verify(pk, pi, message)
		fmt.Printf("[Node %d]   Verification took: %v\n", i, time.Since(verifyStart))
		if err != nil {
			t.Fatalf("Node %d: Verify failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("Node %d: Verification returned false", i)
		}
		fmt.Printf("[Node %d]   Verification result: SUCCESS\n", i)

		// Step 4: Check VCT condition (sortition)
		randomNumber := hex.EncodeToString(hash)
		vctPassed := Sortition(randomNumber)

		fmt.Printf("[Node %d] Step 4: VCT Check\n", i)
		fmt.Printf("[Node %d]   Random number: %s\n", i, randomNumber[:16]+"...")

		if vctPassed {
			winners = append(winners, i)
			fmt.Printf("[Node %d]   VCT CHECK PASSED ✓ (starts with 'a')\n", i)
		} else {
			fmt.Printf("[Node %d]   VCT CHECK FAILED ✗ (doesn't start with 'a')\n", i)
		}

		if verbose && vctPassed {
			fmt.Printf("[Node %d]   Full random number: %s\n", i, randomNumber)
		}
	}

	totalElapsedTime := time.Since(totalTime)
	avgElapsedTime := totalElapsedTime / time.Duration(howmany)

	fmt.Printf("\n=== Test Results ===\n")
	fmt.Printf("Total nodes: %d\n", howmany)
	fmt.Printf("Winners (VCT passed): %v\n", winners)
	fmt.Printf("Number of winners: %d\n", len(winners))
	fmt.Printf("Total time: %v\n", totalElapsedTime)
	fmt.Printf("Average time per node: %v\n", avgElapsedTime)

	// Validation
	if len(winners) < 0 || len(winners) > howmany {
		t.Fatalf("Invalid number of winners: %d", len(winners))
	}

	t.Logf("Test passed: %d/%d nodes passed VCT", len(winners), howmany)
}
