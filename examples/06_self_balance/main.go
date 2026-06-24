// Case 6: Value lives in storage on another contract — balanceOf(self)
// mid-recipe.
//
// The most ergonomic and the most dangerous pattern in weiroll-style
// composition. Read every warning below before shipping anything that
// uses this.
//
// Pattern: after a step that lands tokens at the executor (the weiroll
// VM contract that's running the plan), read the executor's balance
// and feed it into the next step. Common shape:
//
//	wrap ETH -> swap on Uniswap -> read USDC.balanceOf(executor)
//	                               -> supply USDC to Aave on behalf of user
//
// go-weiroll has no `Self()` sentinel: you must pass the executor
// address as a literal at plan time. That's a deliberate scope choice —
// the right answer depends on the user's deployment (per-user proxy
// vs shared executor) and threat model, which the SDK above
// go-weiroll should encode.
//
// Pitfalls (every one of these has burned someone):
//
//  1. Pre-existing balance contamination.
//     balanceOf(executor) returns TOTAL balance, not "what this recipe
//     produced." Dust, leftover from a prior step, or attacker-planted
//     transfers all inflate the read. In a per-user proxy this is an
//     accounting bug; in a shared executor it can be exploitable.
//
//  2. Forced sends are unstoppable.
//     Anyone can transfer an ERC20 to your executor address. ETH can
//     be force-credited via coinbase payments (selfdestruct as a
//     forced-send was effectively retired in EIP-6780/Cancun, but
//     direct transfers and coinbase tips still work). You cannot
//     prevent your executor from receiving tokens between commands.
//
//  3. Threat model depends on executor design.
//     - Per-user proxy (each wallet -> cloned VM): risk limited to
//     your own dust. The bug surface is small and recoverable.
//     - Shared executor (one VM serves many users): another user's
//     mid-flight balance, or an attacker's planted transfer, can
//     poison the read. This pattern requires snapshot-diff (read
//     before AND after, subtract) — a single post-action read is
//     wrong by construction.
//
//  4. ERC777 reentrancy.
//     ERC777 transfers fire `tokensReceived` hooks that can mutate
//     state between two balanceOf reads in the same transaction.
//     Rare but real, and audit-flagged.
//
//  5. Share / wrapper token unit confusion.
//     cTokens, yvTokens, ERC4626 vault shares: balanceOf is in share
//     units, not underlying. Naming a `*ReturnValue` "usdcBal" while
//     it actually holds cUSDC shares will break any downstream Action
//     that expects underlying.
//
//  6. Rebasing tokens.
//     stETH, AMPL, aTokens: balance changes between blocks. Within a
//     single tx the snapshot is fine; if your recipe spans blocks
//     (it can't, weiroll is one tx) you'd be wrong, but within tx
//     this is moot.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"time"

	weiroll "github.com/Infrared-Trading-Technologies/go-weiroll"
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
		"inputs": [
			{"name": "spender", "type": "address"},
			{"name": "amount",  "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	}
]`

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

const erc20ABI = `[
	{
		"name": "balanceOf",
		"type": "function",
		"stateMutability": "view",
		"inputs":  [{"name": "account", "type": "address"}],
		"outputs": [{"name": "",        "type": "uint256"}]
	},
	{
		"name": "approve",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "spender", "type": "address"},
			{"name": "amount",  "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	}
]`

const aaveV3PoolABI = `[
	{
		"name": "supply",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "asset",        "type": "address"},
			{"name": "amount",       "type": "uint256"},
			{"name": "onBehalfOf",   "type": "address"},
			{"name": "referralCode", "type": "uint16"}
		],
		"outputs": []
	}
]`

const mathLibABI = `[
	{
		"name": "extractLastElement",
		"type": "function",
		"stateMutability": "pure",
		"inputs":  [{"name": "amounts", "type": "uint256[]"}],
		"outputs": [{"name": "",        "type": "uint256"}]
	}
]`

func main() {
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usdcAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	router := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")
	pool := common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2")
	mathAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD02")

	// THIS IS THE EXECUTOR ADDRESS — the weiroll VM contract that
	// runs this plan. Reads of balanceOf(executor) capture WHATEVER
	// balance the executor currently holds, not just what this recipe
	// produced. Treat as a per-user proxy here for safety.
	executor := common.HexToAddress("0xEEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0")

	// `onBehalf` is the user we're depositing to Aave for.
	onBehalf := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	weth := weiroll.NewContract(wethAddr, weiroll.MustParseABI(wethABI))
	usdc := weiroll.NewContract(usdcAddr, weiroll.MustParseABI(erc20ABI))
	uniV2 := weiroll.NewContract(router, weiroll.MustParseABI(uniV2RouterABI))
	aave := weiroll.NewContract(pool, weiroll.MustParseABI(aaveV3PoolABI))
	math := weiroll.NewContract(mathAddr, weiroll.MustParseABI(mathLibABI))

	planner := weiroll.New()

	wrapAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18))
	deadline := big.NewInt(time.Now().Unix() + 3600)
	maxApprove := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	// Step 1: wrap 1 ETH at the executor.
	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))

	// Step 2: approve the router to spend our WETH.
	planner.Add(weth.MustInvoke("approve", router, wrapAmount))

	// Step 3: swap WETH -> USDC. Tokens land at `executor` (the `to`).
	swapResult := planner.Add(uniV2.MustInvoke(
		"swapExactTokensForTokens",
		wrapAmount,
		big.NewInt(0), // production: real slippage bound
		[]common.Address{wethAddr, usdcAddr},
		executor,
		deadline,
	))

	// Step 4 (cheaper): pull the swap output amount from the returned
	// uint256[] directly — exact, no balance read needed. This avoids
	// pitfalls 1, 2, 4, 5, 6 entirely.
	usdcAmountFromSwap := planner.Add(math.MustInvoke("extractLastElement", swapResult))

	// Step 4 (alternative — DEMONSTRATION ONLY): read balanceOf(executor).
	// This is the dangerous shortcut. We compute it but don't use it,
	// so the plan stays correct. Compare it against usdcAmountFromSwap
	// off-chain in tests to detect contamination.
	usdcAmountFromBalance := planner.Add(usdc.MustInvoke("balanceOf", executor).Static())
	_ = usdcAmountFromBalance // intentionally unused

	// Step 5: approve Aave pool to pull USDC, then supply onBehalf of
	// the user. We use the swap-output amount, NOT balanceOf.
	planner.Add(usdc.MustInvoke("approve", pool, maxApprove))
	planner.Add(aave.MustInvoke("supply", usdcAddr, usdcAmountFromSwap, onBehalf, uint16(0)))

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("Commands: %d\n", len(plan.Commands))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	fmt.Println()
	fmt.Println("Decision rule:")
	fmt.Println("  - If the producing call returns the value you want")
	fmt.Println("    (e.g., swap returns uint256[]), prefer extracting")
	fmt.Println("    from that return — it's exact and forgery-proof.")
	fmt.Println("  - Reach for balanceOf(executor) ONLY when no upstream")
	fmt.Println("    call returns the value you need (some void-returning")
	fmt.Println("    transfer-style functions). Even then, prefer:")
	fmt.Println("      a) per-user proxies, where contamination is bounded;")
	fmt.Println("      b) snapshot-diff (read before, read after, subtract);")
	fmt.Println("      c) loud godoc on any Action that assumes a clean executor.")
}
