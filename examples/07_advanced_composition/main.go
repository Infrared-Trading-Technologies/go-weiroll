// Advanced composition: multiple "hard to get" return values flowing
// across a single weiroll transaction.
//
// The recipe — zap from ETH into a Uniswap V3 LP NFT delivered to the
// user, in one tx, with no off-chain orchestration between steps:
//
//   1. Wrap 1 ETH at the executor                       (Case 2)
//   2. Approve UniV2 router to spend WETH               (Case 1)
//   3. Swap 0.5 WETH -> USDC via UniV2 (returns uint256[]) (Case 5)
//   4. Extract the swap's last hop amount               (Case 5)
//   5. Approve NPM for both tokens                      (Case 1)
//   6. MintAdapter.mintFlat — flat-args wrapper around NPM.mint that
//      lets each amount come from a separate slot. .RawReturn()      (Case 4)
//   7. TupleHelper.extract slot 0 of the mint returndata             (Case 4)
//   8. .As("uint256") off-chain reinterpretation                     (Case 4)
//   9. NPM.transferFrom(executor, user, tokenId)                     (Case 4)
//
// Three "hard to get" return values flow through this recipe and each
// gets consumed downstream:
//
//   - swap result (uint256[]):      extracted, fed into mintFlat
//   - mint return blob (bytes):     RawReturn'd, extracted, retyped
//   - tokenId (uint256):            piped into transferFrom
//
// Why MintAdapter exists:
//
//   NPM.mint takes a single tuple parameter (MintParams). go-weiroll
//   encodes a Go struct as one literal slot — meaning you can't pipe
//   a *ReturnValue into one tuple field while leaving the rest as
//   literals. The standard workaround is a tiny library contract that
//   re-exposes the function with FLAT arguments, one per ABI word.
//   integration/contracts/MintAdapter.sol is exactly that.
//
// Pitfalls combined here:
//
//   - amount0Desired comes from the swap's return, not balanceOf. The
//     swap output is exact and forgery-proof (Case 5). balanceOf would
//     be poisoned by any prior or planted token at the executor (Case 6
//     anti-pattern).
//
//   - amount1Desired is the literal 0.5 WETH we kept after swapping
//     half — Case 2 reasoning. No balanceOf needed.
//
//   - tickLower / tickUpper must respect the fee tier's tick spacing
//     (10 for fee=500). Wrong spacing reverts in the V3 pool.
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

const wethABI = `[
	{"name":"deposit","type":"function","stateMutability":"payable","inputs":[],"outputs":[]},
	{"name":"approve","type":"function","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]}
]`

const erc20ABI = `[
	{"name":"approve","type":"function","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]}
]`

const uniV2RouterABI = `[
	{
		"name":"swapExactTokensForTokens","type":"function","stateMutability":"nonpayable",
		"inputs":[
			{"name":"amountIn","type":"uint256"},
			{"name":"amountOutMin","type":"uint256"},
			{"name":"path","type":"address[]"},
			{"name":"to","type":"address"},
			{"name":"deadline","type":"uint256"}
		],
		"outputs":[{"name":"amounts","type":"uint256[]"}]
	}
]`

const mathLibABI = `[
	{"name":"extractLastElement","type":"function","stateMutability":"pure","inputs":[{"name":"amounts","type":"uint256[]"}],"outputs":[{"name":"","type":"uint256"}]}
]`

// MintAdapter (DELEGATECALL'd library). Source:
// integration/contracts/MintAdapter.sol.
const mintAdapterABI = `[
	{
		"name":"mintFlat","type":"function","stateMutability":"nonpayable",
		"inputs":[
			{"name":"token0","type":"address"},
			{"name":"token1","type":"address"},
			{"name":"fee","type":"uint24"},
			{"name":"tickLower","type":"int24"},
			{"name":"tickUpper","type":"int24"},
			{"name":"amount0Desired","type":"uint256"},
			{"name":"amount1Desired","type":"uint256"},
			{"name":"amount0Min","type":"uint256"},
			{"name":"amount1Min","type":"uint256"},
			{"name":"recipient","type":"address"},
			{"name":"deadline","type":"uint256"}
		],
		"outputs":[
			{"name":"tokenId","type":"uint256"},
			{"name":"liquidity","type":"uint128"},
			{"name":"amount0","type":"uint256"},
			{"name":"amount1","type":"uint256"}
		]
	}
]`

const npmABI = `[
	{
		"name":"transferFrom","type":"function","stateMutability":"nonpayable",
		"inputs":[
			{"name":"from","type":"address"},
			{"name":"to","type":"address"},
			{"name":"tokenId","type":"uint256"}
		],
		"outputs":[]
	}
]`

const tupleHelperABI = `[
	{"name":"extract","type":"function","stateMutability":"pure","inputs":[{"name":"data","type":"bytes"},{"name":"index","type":"uint256"}],"outputs":[{"name":"word","type":"bytes32"}]}
]`

func main() {
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usdcAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	router := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")
	npmAddr := common.HexToAddress("0xC36442b4a4522E871399CD717aBDD847Ab11FE88")
	mathAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD02")
	helperAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD01")
	adapterAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD03")
	vmAddr := common.HexToAddress("0xEEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0")
	user := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	weth := weiroll.NewContract(wethAddr, weiroll.MustParseABI(wethABI))
	usdc := weiroll.NewContract(usdcAddr, weiroll.MustParseABI(erc20ABI))
	uniV2 := weiroll.NewContract(router, weiroll.MustParseABI(uniV2RouterABI))
	math := weiroll.NewLibrary(mathAddr, weiroll.MustParseABI(mathLibABI))
	adapter := weiroll.NewLibrary(adapterAddr, weiroll.MustParseABI(mintAdapterABI))
	helper := weiroll.NewLibrary(helperAddr, weiroll.MustParseABI(tupleHelperABI))
	npm := weiroll.NewContract(npmAddr, weiroll.MustParseABI(npmABI))

	wrapAmount := big.NewInt(1e18)
	swapAmount := new(big.Int).Div(wrapAmount, big.NewInt(2))      // 0.5 WETH out
	mintWethAmount := new(big.Int).Sub(wrapAmount, swapAmount)     // 0.5 WETH retained
	deadline := big.NewInt(time.Now().Unix() + 3600)
	maxApprove := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	planner := weiroll.New()

	// 1. Wrap.
	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))

	// 2. Approve router.
	planner.Add(weth.MustInvoke("approve", router, maxApprove))

	// 3. Swap 0.5 WETH -> USDC.
	swapResult := planner.Add(uniV2.MustInvoke(
		"swapExactTokensForTokens",
		swapAmount, big.NewInt(0),
		[]common.Address{wethAddr, usdcAddr},
		vmAddr, deadline,
	))

	// 4. Pull USDC out of the swap's uint256[].
	usdcOut := planner.Add(math.MustInvoke("extractLastElement", swapResult))

	// 5. Approve NPM for both legs.
	planner.Add(weth.MustInvoke("approve", npmAddr, maxApprove))
	planner.Add(usdc.MustInvoke("approve", npmAddr, maxApprove))

	// 6. Mint via the flat-args adapter, with .RawReturn() so the
	//    entire 128-byte (4-word) returndata lands in one slot. Note
	//    that amount0Desired comes from `usdcOut` — a *ReturnValue —
	//    while amount1Desired is the literal mintWethAmount.
	mintRaw := planner.Add(adapter.MustInvoke(
		"mintFlat",
		usdcAddr, wethAddr,
		big.NewInt(500),       // fee
		big.NewInt(-887220),   // tickLower (spacing 10)
		big.NewInt(887220),    // tickUpper
		usdcOut,               // <-- piped from swap output
		mintWethAmount,        // <-- literal we kept after swapping half
		big.NewInt(0), big.NewInt(0),
		vmAddr, deadline,
	).RawReturn())

	// 7. Extract slot 0 (tokenId) as bytes32.
	tokenIdB32 := planner.Add(helper.MustInvoke("extract", mintRaw, big.NewInt(0)))

	// 8. Re-type as uint256 off-chain. No on-chain command.
	tokenId := tokenIdB32.MustAsType("uint256")

	// 9. Ship the LP NFT to the user.
	planner.Add(npm.MustInvoke("transferFrom", vmAddr, user, tokenId))

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("Commands: %d  state slots: %d\n\n", len(plan.Commands), len(plan.State))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	// Slot wiring sanity: the value-flow chain we care about.
	_, _, _, swapRet, _, _ := weiroll.DecodeCommand(plan.Commands[2])
	_, _, extractArgs, extractRet, _, _ := weiroll.DecodeCommand(plan.Commands[3])
	mintCmd := plan.Commands[6] // extended command
	_, _, mintArgs, mintRet, _, _ := weiroll.DecodeCommand(mintCmd)
	_, _, _, tupleRet, _, _ := weiroll.DecodeCommand(plan.Commands[7])
	_, _, xferArgs, _, _, _ := weiroll.DecodeCommand(plan.Commands[8])

	fmt.Println()
	fmt.Printf("  swap return slot:        0x%02x  (dynamic=%v) -- uint256[]\n",
		swapRet, swapRet&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("  extractLast first arg:   0x%02x  <- consumes swap output (dynamic=%v)\n",
		extractArgs[0], extractArgs[0]&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("  extractLast return slot: 0x%02x  <- this is `usdcOut`\n", extractRet)
	fmt.Printf("  mintFlat amount0 arg:    0x%02x  <- pipes usdcOut into amount0Desired\n",
		mintArgs[5])
	fmt.Printf("  mintFlat return slot:    0x%02x  (dynamic=%v) -- RawReturn, byte must be CLEAN\n",
		mintRet, mintRet&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("  tupleHelper.extract ret: 0x%02x  <- tokenId as bytes32\n", tupleRet)
	fmt.Printf("  transferFrom tokenId:    0x%02x  <- same slot, retyped via .As\n",
		xferArgs[2])
}
