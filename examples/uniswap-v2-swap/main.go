// Package main demonstrates a multi-step Uniswap V2 swap using go-weiroll.
// This example chains multiple operations where the output of one step
// becomes the input to the next step.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

// Uniswap V2 Router02 ABI (subset of relevant methods)
const uniswapV2RouterABI = `[
	{
		"name": "swapExactTokensForTokens",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "amountIn", "type": "uint256"},
			{"name": "amountOutMin", "type": "uint256"},
			{"name": "path", "type": "address[]"},
			{"name": "to", "type": "address"},
			{"name": "deadline", "type": "uint256"}
		],
		"outputs": [
			{"name": "amounts", "type": "uint256[]"}
		]
	},
	{
		"name": "getAmountsOut",
		"type": "function",
		"stateMutability": "view",
		"inputs": [
			{"name": "amountIn", "type": "uint256"},
			{"name": "path", "type": "address[]"}
		],
		"outputs": [
			{"name": "amounts", "type": "uint256[]"}
		]
	}
]`

// ERC20 ABI (subset for approve and transfer)
const erc20ABI = `[
	{
		"name": "approve",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "spender", "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "bool"}
		]
	},
	{
		"name": "transfer",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "to", "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "bool"}
		]
	},
	{
		"name": "balanceOf",
		"type": "function",
		"stateMutability": "view",
		"inputs": [
			{"name": "account", "type": "address"}
		],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	}
]`

// Helper library ABI for extracting values from arrays
// In practice, you'd deploy a helper contract for array operations
const helperLibraryABI = `[
	{
		"name": "extractLastElement",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "amounts", "type": "uint256[]"}
		],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	}
]`

func main() {
	// Parse ABIs
	routerABI := weiroll.MustParseABI(uniswapV2RouterABI)
	tokenABI := weiroll.MustParseABI(erc20ABI)
	helperABI := weiroll.MustParseABI(helperLibraryABI)

	// Contract addresses (Ethereum mainnet examples)
	uniswapRouter := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D") // Uniswap V2 Router02
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")          // WETH
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")          // USDC
	dai := common.HexToAddress("0x6B175474E89094C44Da98b954EesedcDAE6Bf26")            // DAI
	helperLib := common.HexToAddress("0x1111111111111111111111111111111111111111")     // Helper library (deploy your own)
	recipient := common.HexToAddress("0x3333333333333333333333333333333333333333")     // Final recipient

	// Create contract wrappers
	router := weiroll.NewContract(uniswapRouter, routerABI)
	wethToken := weiroll.NewContract(weth, tokenABI)
	usdcToken := weiroll.NewContract(usdc, tokenABI)
	helper := weiroll.NewContract(helperLib, helperABI)

	// Create planner
	planner := weiroll.New()

	// Parameters
	amountIn := big.NewInt(1e18)             // 1 WETH
	minAmountOut := big.NewInt(0)            // Accept any amount (in practice, use proper slippage)
	deadline := big.NewInt(1735689600)       // Far future timestamp
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	fmt.Println("Building multi-step Uniswap V2 swap plan...")
	fmt.Println("===========================================")
	fmt.Println()

	// Step 1: Approve router to spend WETH
	fmt.Println("Step 1: Approve Uniswap Router to spend WETH")
	planner.Add(wethToken.MustInvoke("approve", uniswapRouter, maxUint256))

	// Step 2: Swap WETH -> USDC
	// The swap returns uint256[] amounts where the last element is the output amount
	fmt.Println("Step 2: Swap WETH -> USDC")
	path1 := []common.Address{weth, usdc}
	swapResult1 := planner.Add(router.MustInvoke(
		"swapExactTokensForTokens",
		amountIn,
		minAmountOut,
		path1,
		helperLib, // Send to helper first (for demo; in practice send to VM or self)
		deadline,
	))

	// Step 3: Extract the output amount from the swap result
	// swapExactTokensForTokens returns uint256[] where last element is final output
	fmt.Println("Step 3: Extract output amount from swap result")
	usdcAmount := planner.Add(helper.MustInvoke("extractLastElement", swapResult1))

	// Step 4: Approve router to spend the USDC we just received
	fmt.Println("Step 4: Approve Uniswap Router to spend USDC")
	planner.Add(usdcToken.MustInvoke("approve", uniswapRouter, maxUint256))

	// Step 5: Swap USDC -> DAI using the output from Step 2
	// This demonstrates chaining - usdcAmount comes from the previous swap
	fmt.Println("Step 5: Swap USDC -> DAI (using output from Step 2)")
	path2 := []common.Address{usdc, dai}
	planner.Add(router.MustInvoke(
		"swapExactTokensForTokens",
		usdcAmount, // <- This is the key: using return value from previous step!
		minAmountOut,
		path2,
		recipient,
		deadline,
	))

	fmt.Println()
	fmt.Println("Compiling plan...")

	// Compile the plan
	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Failed to compile plan: %v", err)
	}

	// Print results
	fmt.Println()
	fmt.Printf("Compiled Plan Summary:\n")
	fmt.Printf("  Total commands: %d\n", len(plan.Commands))
	fmt.Printf("  State slots used: %d\n", len(plan.State))
	fmt.Println()

	fmt.Println("Encoded Commands:")
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}
	fmt.Println()

	fmt.Println("Initial State Slots:")
	for i, state := range plan.State {
		if len(state) > 0 {
			// Truncate for display
			display := hex.EncodeToString(state)
			if len(display) > 64 {
				display = display[:64] + "..."
			}
			fmt.Printf("  [%d] 0x%s\n", i, display)
		} else {
			fmt.Printf("  [%d] (empty - return value placeholder)\n", i)
		}
	}

	// Get data ready for contract execution
	commands := plan.CommandsAsBytes32()
	state := plan.StateAsBytes()

	fmt.Println()
	fmt.Printf("Ready for weiroll VM execution:\n")
	fmt.Printf("  commands (bytes32[]): %d entries\n", len(commands))
	fmt.Printf("  state (bytes[]): %d entries\n", len(state))
	fmt.Println()
	fmt.Println("The key insight: Step 5 uses 'usdcAmount' which is the")
	fmt.Println("return value from Step 3, which extracted the output")
	fmt.Println("from the Step 2 swap. All executed atomically!")
}
