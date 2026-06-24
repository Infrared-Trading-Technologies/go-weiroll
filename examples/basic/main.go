// Package main demonstrates basic usage of the go-weiroll library.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/Infrared-Trading-Technologies/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

// Example ABI for a Math library contract
const mathLibraryABI = `[
	{
		"name": "add",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "a", "type": "uint256"},
			{"name": "b", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	},
	{
		"name": "multiply",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "a", "type": "uint256"},
			{"name": "b", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	}
]`

// Example ABI for an ERC20 token contract
const tokenABI = `[
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
	}
]`

func main() {
	// Parse ABIs
	mathABI := weiroll.MustParseABI(mathLibraryABI)
	erc20ABI := weiroll.MustParseABI(tokenABI)

	// Contract addresses (these would be real deployed addresses)
	mathLibAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tokenAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	recipientAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")

	// Create contract wrappers. Pure-compute helpers are CALL'd.
	mathLib := weiroll.NewContract(mathLibAddr, mathABI)
	token := weiroll.NewContract(tokenAddr, erc20ABI)

	// Create a new planner
	planner := weiroll.New()

	// Build a plan that:
	// 1. Adds 100 + 200 = 300
	// 2. Multiplies result by 2 = 600
	// 3. Transfers that amount of tokens

	// Add first call: add(100, 200)
	sum := planner.Add(mathLib.MustInvoke("add", big.NewInt(100), big.NewInt(200)))
	fmt.Println("Added: add(100, 200) -> returns sum")

	// Add second call: multiply(sum, 2) - using the return value from add
	product := planner.Add(mathLib.MustInvoke("multiply", sum, big.NewInt(2)))
	fmt.Println("Added: multiply(sum, 2) -> returns product")

	// Add third call: transfer(recipient, product) - using the return value from multiply
	planner.Add(token.MustInvoke("transfer", recipientAddr, product))
	fmt.Println("Added: transfer(recipient, product)")

	// Compile the plan
	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Failed to compile plan: %v", err)
	}

	// Print the compiled plan
	fmt.Printf("\nCompiled Plan:\n")
	fmt.Printf("  Commands: %d\n", len(plan.Commands))
	fmt.Printf("  State slots: %d\n", len(plan.State))

	fmt.Printf("\nEncoded Commands:\n")
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	fmt.Printf("\nInitial State:\n")
	for i, state := range plan.State {
		if len(state) > 0 {
			fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(state))
		} else {
			fmt.Printf("  [%d] (empty - return value slot)\n", i)
		}
	}

	// Get the commands and state for contract execution
	commands := plan.CommandsAsBytes32()
	state := plan.StateAsBytes()

	fmt.Printf("\nReady for execution:\n")
	fmt.Printf("  Commands (bytes32[]): %d entries\n", len(commands))
	fmt.Printf("  State (bytes[]): %d entries\n", len(state))
}
