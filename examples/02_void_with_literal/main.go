// Case 2: Function returns void; the value is the literal input.
//
// Examples: WETH.deposit{value: x}() and ERC20.approve(spender, amount).
// The user already passed in the literal (the amount). It IS the ground
// truth — there is nothing to "recover."
//
// Pitfall: do NOT append `weth.balanceOf(holder)` after the deposit to
// "recover" the deposited amount. balanceOf returns the FULL balance
// (pre-existing + this deposit + any dust + any attacker-planted
// transfers), not the deposit delta. The literal input is strictly
// more accurate than any post-action balance read.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

const wethABI = `[
	{
		"name": "deposit",
		"type": "function",
		"stateMutability": "payable",
		"inputs":  [],
		"outputs": []
	},
	{
		"name": "approve",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs":  [
			{"name": "spender", "type": "address"},
			{"name": "amount",  "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	},
	{
		"name": "balanceOf",
		"type": "function",
		"stateMutability": "view",
		"inputs":  [{"name": "account", "type": "address"}],
		"outputs": [{"name": "",        "type": "uint256"}]
	}
]`

func main() {
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	spender := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D") // UniV2 Router

	weth := weiroll.NewContract(wethAddr, weiroll.MustParseABI(wethABI))

	planner := weiroll.New()

	// Wrap 1 ETH → 1 WETH. deposit() returns nothing — Add returns nil.
	wrapAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18))
	if rv := planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount)); rv != nil {
		log.Fatal("deposit() should have no return value")
	}

	// Approve the router to pull our 1 WETH. The amount is the literal
	// we just deposited; we use it directly, NOT a post-deposit
	// balanceOf read.
	planner.Add(weth.MustInvoke("approve", spender, wrapAmount))

	// COUNTER-EXAMPLE: do not do this.
	//   bal := planner.Add(weth.MustInvoke("balanceOf", vmAddr).Static())
	//   planner.Add(weth.MustInvoke("approve", spender, bal))
	// `bal` includes any pre-existing WETH at vmAddr plus dust from
	// attackers, not just the 1 WETH we deposited. The literal
	// `wrapAmount` is exact and forgery-proof.

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("Commands: %d (deposit + approve, no extra balance read)\n", len(plan.Commands))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}
	fmt.Printf("\nState slots: %d (the literal wrapAmount is shared between deposit's value and approve's amount)\n", len(plan.State))
}
