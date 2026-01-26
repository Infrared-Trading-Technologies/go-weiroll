// Package main demonstrates WETH wrap/unwrap operations using go-weiroll.
// This example shows how weiroll handles functions with no return values,
// which is common for functions like WETH's deposit() and withdraw().
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

// WETH ABI - Wrapped Ether contract
// Note: deposit() and withdraw() have NO return values
const wethABI = `[
	{
		"name": "deposit",
		"type": "function",
		"stateMutability": "payable",
		"inputs": [],
		"outputs": []
	},
	{
		"name": "withdraw",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "wad", "type": "uint256"}
		],
		"outputs": []
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
	},
	{
		"name": "transfer",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "dst", "type": "address"},
			{"name": "wad", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "bool"}
		]
	},
	{
		"name": "approve",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "guy", "type": "address"},
			{"name": "wad", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "bool"}
		]
	}
]`

func main() {
	fmt.Println("=== WETH Wrap/Unwrap Example ===")
	fmt.Println()

	// Parse ABI
	parsedWethABI := weiroll.MustParseABI(wethABI)

	// Contract addresses
	// Mainnet WETH: 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	vmAddr := common.HexToAddress("0x1111111111111111111111111111111111111111") // Example VM address
	recipientAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")

	// Create WETH contract wrapper
	weth := weiroll.NewContract(wethAddr, parsedWethABI)

	// =========================================
	// Example 1: Simple ETH -> WETH wrap
	// =========================================
	fmt.Println("--- Example 1: Simple ETH -> WETH Wrap ---")

	planner1 := weiroll.New()
	wrapAmount := big.NewInt(1e18) // 1 ETH in wei

	// deposit() takes no arguments but requires ETH value
	// Use WithValue to send ETH with the call
	depositCall := weth.MustInvoke("deposit").WithValue(wrapAmount)

	// Add returns nil because deposit() has no return value
	// The call is still added to the plan and will execute normally
	result := planner1.Add(depositCall)
	if result == nil {
		fmt.Println("✓ deposit() has no return value - planner.Add() returns nil")
		fmt.Println("  The call is still added and will execute on-chain")
	}

	plan1, err := planner1.Plan()
	if err != nil {
		log.Fatalf("Failed to compile wrap plan: %v", err)
	}

	fmt.Println("\nCompiled wrap plan:")
	fmt.Printf("  Commands: %d\n", len(plan1.Commands))
	fmt.Printf("  Encoded:  0x%s\n", hex.EncodeToString(plan1.Commands[0]))
	fmt.Println()

	// =========================================
	// Example 2: Simple WETH -> ETH unwrap
	// =========================================
	fmt.Println("--- Example 2: Simple WETH -> ETH Unwrap ---")

	planner2 := weiroll.New()
	unwrapAmount := big.NewInt(5e17) // 0.5 WETH

	// withdraw() takes an amount but returns nothing
	withdrawCall := weth.MustInvoke("withdraw", unwrapAmount)
	result = planner2.Add(withdrawCall)
	if result == nil {
		fmt.Println("✓ withdraw(uint256) has no return value - planner.Add() returns nil")
	}

	plan2, err := planner2.Plan()
	if err != nil {
		log.Fatalf("Failed to compile unwrap plan: %v", err)
	}

	fmt.Println("\nCompiled unwrap plan:")
	fmt.Printf("  Commands: %d\n", len(plan2.Commands))
	fmt.Printf("  Encoded:  0x%s\n", hex.EncodeToString(plan2.Commands[0]))
	fmt.Println()

	// =========================================
	// Example 3: Wrap and transfer in one tx
	// =========================================
	fmt.Println("--- Example 3: Wrap ETH and Transfer WETH ---")

	planner3 := weiroll.New()
	amount := big.NewInt(2e18) // 2 ETH

	// Step 1: Wrap ETH to WETH (no return value)
	planner3.Add(weth.MustInvoke("deposit").WithValue(amount))
	fmt.Println("✓ Added: deposit() with 2 ETH")

	// Step 2: Transfer the WETH to recipient (returns bool)
	transferResult := planner3.Add(weth.MustInvoke("transfer", recipientAddr, amount))
	if transferResult != nil {
		fmt.Println("✓ Added: transfer() returns bool - can be used in subsequent calls")
	}

	plan3, err := planner3.Plan()
	if err != nil {
		log.Fatalf("Failed to compile wrap+transfer plan: %v", err)
	}

	fmt.Println("\nCompiled wrap+transfer plan:")
	fmt.Printf("  Commands: %d\n", len(plan3.Commands))
	for i, cmd := range plan3.Commands {
		fmt.Printf("  Command %d: 0x%s\n", i, hex.EncodeToString(cmd))
	}
	fmt.Println()

	// =========================================
	// Example 4: Check balance, then unwrap
	// =========================================
	fmt.Println("--- Example 4: Check Balance and Unwrap ---")

	planner4 := weiroll.New()

	// Step 1: Get WETH balance (returns uint256)
	balance := planner4.Add(weth.MustInvoke("balanceOf", vmAddr).Static())
	fmt.Println("✓ Added: balanceOf() returns uint256")

	// Step 2: Use the balance return value as input to withdraw
	// This demonstrates using a return value from one call as input to another
	planner4.Add(weth.MustInvoke("withdraw", balance))
	fmt.Println("✓ Added: withdraw(balance) - uses balanceOf result")

	plan4, err := planner4.Plan()
	if err != nil {
		log.Fatalf("Failed to compile balance+unwrap plan: %v", err)
	}

	fmt.Println("\nCompiled balance+unwrap plan:")
	fmt.Printf("  Commands: %d\n", len(plan4.Commands))
	fmt.Printf("  State slots: %d\n", len(plan4.State))
	for i, cmd := range plan4.Commands {
		fmt.Printf("  Command %d: 0x%s\n", i, hex.EncodeToString(cmd))
	}
	fmt.Println()

	// =========================================
	// Example 5: Approve + Wrap workflow
	// =========================================
	fmt.Println("--- Example 5: Wrap and Approve for Spending ---")

	planner5 := weiroll.New()
	spenderAddr := common.HexToAddress("0x4444444444444444444444444444444444444444")
	approveAmount := big.NewInt(0).SetBytes(common.MaxHash.Bytes()) // Max approval

	// Step 1: Wrap ETH (10 ETH = 10 * 10^18 wei)
	tenETH := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	planner5.Add(weth.MustInvoke("deposit").WithValue(tenETH))
	fmt.Println("✓ Added: deposit() with 10 ETH")

	// Step 2: Approve spender (returns bool, but we ignore it)
	_ = planner5.Add(weth.MustInvoke("approve", spenderAddr, approveAmount))
	fmt.Println("✓ Added: approve() - return value ignored (assigned to _)")

	plan5, err := planner5.Plan()
	if err != nil {
		log.Fatalf("Failed to compile wrap+approve plan: %v", err)
	}

	fmt.Println("\nCompiled wrap+approve plan:")
	fmt.Printf("  Commands: %d\n", len(plan5.Commands))
	for i, cmd := range plan5.Commands {
		fmt.Printf("  Command %d: 0x%s\n", i, hex.EncodeToString(cmd))
	}
	fmt.Println()

	// =========================================
	// Summary
	// =========================================
	fmt.Println("=== Summary: How Weiroll Handles No-Return Functions ===")
	fmt.Println()
	fmt.Println("1. Functions with no return value (like WETH deposit/withdraw):")
	fmt.Println("   - planner.Add() returns nil instead of a *ReturnValue")
	fmt.Println("   - The call is still added to the plan and executes normally")
	fmt.Println("   - State changes happen on-chain (ETH ↔ WETH conversion)")
	fmt.Println()
	fmt.Println("2. Functions WITH return values (like balanceOf, transfer):")
	fmt.Println("   - planner.Add() returns a *ReturnValue")
	fmt.Println("   - This value can be used as input to subsequent calls")
	fmt.Println("   - The return value is stored in the weiroll state array")
	fmt.Println()
	fmt.Println("3. Ignoring return values:")
	fmt.Println("   - Assign to _ (blank identifier) if you don't need the return")
	fmt.Println("   - The optimizer won't allocate a state slot for unused returns")
	fmt.Println()
	fmt.Println("4. Sending ETH with calls:")
	fmt.Println("   - Use .WithValue(amount) on the Call before adding to planner")
	fmt.Println("   - This uses CALL_WITH_VALUE opcode in weiroll")
}
