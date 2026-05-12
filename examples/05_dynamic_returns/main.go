// Case 5: Dynamic-length returns (`bytes`, `string`, `T[]`).
//
// Example: UniswapV2Router.swapExactTokensForTokens returns
// `uint256[] amounts` — one element per hop in the path. To use the
// final output amount downstream, you need a single-element extractor.
//
// go-weiroll automatically sets the dynamic-slot flag (0x80) when the
// effective return type is dynamic. So piping `swapResult` (uint256[])
// into a function that accepts `uint256[]` Just Works — no .RawReturn,
// no .As needed.
//
// Pitfalls:
//
//   - Piping a dynamic *ReturnValue into a static-typed argument is
//     rejected at Invoke time (TypeMismatchError). Not silent.
//
//   - Dynamic returns are heavier in state. Exposing a full uint256[]
//     when only one element is consumed downstream wastes a slot.
//     If your consumer always wants the last hop, expose the scalar,
//     not the array.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"time"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

const uniV2RouterABI = `[
	{
		"name": "swapExactTokensForTokens",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "amountIn",     "type": "uint256"},
			{"name": "amountOutMin", "type": "uint256"},
			{"name": "path",         "type": "address[]"},
			{"name": "to",           "type": "address"},
			{"name": "deadline",     "type": "uint256"}
		],
		"outputs": [{"name": "amounts", "type": "uint256[]"}]
	}
]`

// MathLib.extractLastElement is a real Solidity helper deployed in our
// integration tests (integration/contracts/MathLib.sol). Single command,
// returns the last element of a uint256[].
const mathLibABI = `[
	{
		"name": "extractLastElement",
		"type": "function",
		"stateMutability": "pure",
		"inputs":  [{"name": "amounts", "type": "uint256[]"}],
		"outputs": [{"name": "",        "type": "uint256"}]
	}
]`

const erc20ABI = `[
	{
		"name": "transfer",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "to",     "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	}
]`

func main() {
	router := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	mathAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD02") // your deployment
	recipient := common.HexToAddress("0xCAFECAFECAFECAFECAFECAFECAFECAFECAFECAFE")

	uniV2 := weiroll.NewContract(router, weiroll.MustParseABI(uniV2RouterABI))
	math := weiroll.NewContract(mathAddr, weiroll.MustParseABI(mathLibABI))
	usdcToken := weiroll.NewContract(usdc, weiroll.MustParseABI(erc20ABI))

	planner := weiroll.New()

	// Step 1: swap 1 WETH -> USDC via the router. Returns uint256[]
	// (one entry per hop; for a 2-token path this is [amountIn, amountOut]).
	path := []common.Address{weth, usdc}
	amountIn := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18))
	deadline := big.NewInt(time.Now().Unix() + 3600)

	swapResult := planner.Add(uniV2.MustInvoke(
		"swapExactTokensForTokens",
		amountIn,
		big.NewInt(0), // accept any (test only — set a real slippage bound in production)
		path,
		recipient, // router sends tokens directly to recipient
		deadline,
	))
	fmt.Printf("swapResult.Type() = %s (dynamic=%v) — pipes anywhere a uint256[] is expected\n",
		swapResult.Type().String(), swapResult.IsDynamic())

	// Step 2: extract the last element. extractLastElement takes
	// uint256[] and returns uint256 — types align cleanly.
	usdcOut := planner.Add(math.MustInvoke("extractLastElement", swapResult))
	fmt.Printf("usdcOut.Type()    = %s\n", usdcOut.Type().String())

	// Step 3: do something with usdcOut. Here, transfer it back to a
	// hot wallet. (Note: tokens are at `recipient` after the swap, not
	// at the VM, so this transfer would need a different setup in a
	// real recipe — see Case 6 for the self-balance pattern.)
	planner.Add(usdcToken.MustInvoke("transfer", recipient, usdcOut))

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("\nCommands: %d\n", len(plan.Commands))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	// Verify the dynamic-flag plumbing.
	_, _, _, prodSlot, _, _ := weiroll.DecodeCommand(plan.Commands[0])
	_, _, consArgs, _, _, _ := weiroll.DecodeCommand(plan.Commands[1])
	fmt.Printf("\nswap return slot:        0x%02x (dynamic=%v)\n",
		prodSlot, prodSlot&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("extractor 1st arg slot:  0x%02x (dynamic=%v)\n",
		consArgs[0], consArgs[0]&weiroll.DynamicSlotFlag != 0)
}
