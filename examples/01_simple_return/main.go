// Case 1: Function returns the value you want.
//
// The clean path. The function's ABI return type already matches what
// you need downstream, so one Planner.Add gives you a typed
// *ReturnValue you can pipe straight into the next call.
//
// Cost: one weiroll command per output. No extraction, no casts.
//
// Pitfall: the ABI return type must match the documented type of any
// downstream argument. Mismatches are caught at Invoke time with a
// TypeMismatchError — they're not silent in go-weiroll. But if the
// ABI you parse is itself wrong (e.g., declares uint256 when the real
// function returns int256), the encoding will be off.
package main

import (
	"encoding/hex"
	"fmt"
	"log"

	weiroll "github.com/Infrared-Trading-Technologies/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

// USDC on Ethereum mainnet. balanceOf returns a single uint256 — the
// canonical "function returns what you want" shape.
const erc20ABI = `[
	{
		"name": "balanceOf",
		"type": "function",
		"stateMutability": "view",
		"inputs":  [{"name": "account", "type": "address"}],
		"outputs": [{"name": "",        "type": "uint256"}]
	},
	{
		"name": "transfer",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs":  [
			{"name": "to",     "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	}
]`

func main() {
	usdcAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	holder := common.HexToAddress("0x1111111111111111111111111111111111111111")
	recipient := common.HexToAddress("0x2222222222222222222222222222222222222222")

	usdc := weiroll.NewContract(usdcAddr, weiroll.MustParseABI(erc20ABI))

	planner := weiroll.New()

	// Step 1: Read holder's USDC balance.
	// balanceOf returns uint256 directly — exactly what transfer needs.
	balance := planner.Add(usdc.MustInvoke("balanceOf", holder).Static())
	fmt.Printf("balance ReturnValue type: %s (dynamic=%v)\n",
		balance.Type().String(), balance.IsDynamic())

	// Step 2: Transfer that exact amount to the recipient.
	// No extraction, no cast — the type matches the consumer.
	planner.Add(usdc.MustInvoke("transfer", recipient, balance))

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("\nCommands: %d, state slots: %d\n", len(plan.Commands), len(plan.State))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	// Sanity: the second command must reference the slot allocated by
	// the first. The dynamic flag must NOT be set on either side, since
	// uint256 is static.
	_, _, _, prodSlot, _, _ := weiroll.DecodeCommand(plan.Commands[0])
	_, _, consArgs, _, _, _ := weiroll.DecodeCommand(plan.Commands[1])
	fmt.Printf("\nProducer return slot: 0x%02x\n", prodSlot)
	fmt.Printf("Consumer arg slots:   %v\n", consArgs)
}
