// Case 3: Function returns void; the resulting value is recoverable
// but not trivially.
//
// Example: AavePool.supply(asset, amount, onBehalf, refCode) returns
// nothing, and downstream code wants the resulting aToken position.
// The "recovery" requires reading aToken.balanceOf(onBehalf), which
// has multiple footguns.
//
// Pitfalls (all real, all hit users):
//
//   1. Pre-existing balance contamination.
//      aToken.balanceOf(onBehalf) returns the cumulative position, not
//      this supply's delta. Any prior aToken balance inflates the read.
//      Only safe when onBehalf is freshly created (per-user proxy).
//
//   2. Rebasing.
//      aTokens accrue interest by rebasing balanceOf. Within one tx the
//      snapshot is fine; across blocks it drifts.
//
//   3. Share-token unit confusion (other protocols).
//      For cTokens, yvTokens, ERC4626 vault shares, balanceOf is in
//      *share* units, not underlying. Naming a field "usdcBal" while
//      it actually holds cUSDC shares is a bug.
//
//   4. Fee-on-transfer underlying.
//      If the supplied asset is fee-on-transfer (e.g., some USDT
//      configurations, PAXG), the deposited amount differs from the
//      input amount and from the post-action read. Be explicit about
//      which one you mean.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

const aaveV3PoolABI = `[
	{
		"name": "supply",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "asset",       "type": "address"},
			{"name": "amount",      "type": "uint256"},
			{"name": "onBehalfOf",  "type": "address"},
			{"name": "referralCode","type": "uint16"}
		],
		"outputs": []
	}
]`

const aTokenABI = `[
	{
		"name": "balanceOf",
		"type": "function",
		"stateMutability": "view",
		"inputs":  [{"name": "user", "type": "address"}],
		"outputs": [{"name": "",     "type": "uint256"}]
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
	pool := common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2")    // Aave V3 Pool
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")    // USDC
	aUSDC := common.HexToAddress("0x98C23E9d8f34FEFb1B7BD6a91B7FF122F4e16F5c")   // aEthUSDC v3
	onBehalf := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") // freshly minted proxy
	recipient := common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")

	aavePool := weiroll.NewContract(pool, weiroll.MustParseABI(aaveV3PoolABI))
	aToken := weiroll.NewContract(aUSDC, weiroll.MustParseABI(aTokenABI))

	planner := weiroll.New()

	// Step 1: Supply 100 USDC to Aave on behalf of `onBehalf`.
	//
	// IMPORTANT: this example assumes `onBehalf` is a fresh per-user
	// proxy with zero prior aUSDC balance. That assumption MUST be
	// loud in any Action you ship — silent assumptions get exploited.
	supplyAmount := new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000)) // 100 USDC (6 decimals)
	planner.Add(aavePool.MustInvoke("supply", usdc, supplyAmount, onBehalf, uint16(0)))

	// Step 2: Read the resulting aUSDC balance.
	//
	// For Aave V3 specifically, aToken.balanceOf is denominated in
	// underlying units (USDC), not share units — that's correct here.
	// For Compound v2 cTokens or any ERC4626 vault, the equivalent
	// read returns SHARES, and you'd need previewRedeem / convertToAssets
	// to get underlying.
	aBalance := planner.Add(aToken.MustInvoke("balanceOf", onBehalf).Static())

	// Step 3: Transfer the new aUSDC position somewhere else.
	planner.Add(aToken.MustInvoke("transfer", recipient, aBalance))

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("Commands: %d (supply + balanceOf + transfer)\n", len(plan.Commands))
	fmt.Println()
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	fmt.Println()
	fmt.Println("Cost note: each *ReturnValue you expose costs at least one")
	fmt.Println("command. Here, the balance read is one extra command vs.")
	fmt.Println("trusting the literal supplyAmount. That cost is the price")
	fmt.Println("of accommodating fee-on-transfer / rebasing / share tokens.")
	fmt.Println("If the underlying is plain USDC, you can skip the read")
	fmt.Println("entirely and reuse supplyAmount downstream — saving one")
	fmt.Println("command per recipe per user.")
}
