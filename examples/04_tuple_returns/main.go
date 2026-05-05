// Case 4: Function returns a tuple.
//
// Example: Uniswap V3 NonfungiblePositionManager.mint returns
//   (uint256 tokenId, uint128 liquidity, uint256 amount0, uint256 amount1)
//
// A weiroll *ReturnValue is one slot. Tuples need extraction helpers.
//
// The pattern:
//
//   1. Call the producer with .RawReturn(). The VM stores the entire
//      returndata as a length-prefixed bytes blob in the slot. The
//      *ReturnValue you get back is typed as `bytes` (dynamic).
//
//   2. Call a tuple-extraction helper that takes (bytes, index) and
//      returns bytes32 (one ABI word from the tuple).
//
//   3. If the field's real type is uint256/address/bool (anything
//      32-byte static), use ReturnValue.As to reinterpret the bytes32
//      as the correct type — off-chain, no extra command. Useful for
//      static-to-static reinterpretation only; mixing static and
//      dynamic types is rejected.
//
// Pitfalls:
//
//   - Forgetting .RawReturn() on a multi-output method silently keeps
//     ONLY the first 32-byte output. The other fields are unreachable.
//
//   - Without .As, you would need an on-chain bytes32->uint256 cast
//     helper (one extra command per field). With .As it's free.
//
//   - Exposing every tuple field by default forces every caller to
//     pay for extractions they don't use. Two extra commands per
//     field adds up fast — extract only what downstream actually
//     consumes.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

// MintParams matches Uniswap V3 NPM's mint() input tuple shape exactly.
// Field order must match the ABI tuple components.
type MintParams struct {
	Token0         common.Address
	Token1         common.Address
	Fee            *big.Int
	TickLower      *big.Int
	TickUpper      *big.Int
	Amount0Desired *big.Int
	Amount1Desired *big.Int
	Amount0Min     *big.Int
	Amount1Min     *big.Int
	Recipient      common.Address
	Deadline       *big.Int
}

// NonfungiblePositionManager.mint - real Uniswap V3 mainnet ABI subset.
const npmABI = `[
	{
		"name": "mint",
		"type": "function",
		"stateMutability": "payable",
		"inputs": [
			{
				"name": "params",
				"type": "tuple",
				"components": [
					{"name": "token0",        "type": "address"},
					{"name": "token1",        "type": "address"},
					{"name": "fee",           "type": "uint24"},
					{"name": "tickLower",     "type": "int24"},
					{"name": "tickUpper",     "type": "int24"},
					{"name": "amount0Desired","type": "uint256"},
					{"name": "amount1Desired","type": "uint256"},
					{"name": "amount0Min",    "type": "uint256"},
					{"name": "amount1Min",    "type": "uint256"},
					{"name": "recipient",     "type": "address"},
					{"name": "deadline",      "type": "uint256"}
				]
			}
		],
		"outputs": [
			{"name": "tokenId",   "type": "uint256"},
			{"name": "liquidity", "type": "uint128"},
			{"name": "amount0",   "type": "uint256"},
			{"name": "amount1",   "type": "uint256"}
		]
	}
]`

// Hypothetical TupleHelper. Deploy once, reuse for every tuple-returning
// method in your protocol set. Returns one 32-byte ABI word so callers
// can re-type it via ReturnValue.As. The on-chain implementation is a
// few lines of assembly:
//
//   function extract(bytes calldata data, uint256 index)
//       external pure returns (bytes32) {
//       require(data.length >= 32 * (index + 1), "OOB");
//       return bytes32(data[32 * index : 32 * (index + 1)]);
//   }
const tupleHelperABI = `[
	{
		"name": "extract",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "data",  "type": "bytes"},
			{"name": "index", "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bytes32"}]
	}
]`

// A consumer that takes a tokenId — used here just to show the cast
// pipes correctly.
const npmConsumerABI = `[
	{
		"name": "increaseLiquidity",
		"type": "function",
		"stateMutability": "payable",
		"inputs": [
			{
				"name": "params",
				"type": "tuple",
				"components": [
					{"name": "tokenId",        "type": "uint256"},
					{"name": "amount0Desired", "type": "uint256"},
					{"name": "amount1Desired", "type": "uint256"},
					{"name": "amount0Min",     "type": "uint256"},
					{"name": "amount1Min",     "type": "uint256"},
					{"name": "deadline",       "type": "uint256"}
				]
			}
		],
		"outputs": [
			{"name": "liquidity", "type": "uint128"},
			{"name": "amount0",   "type": "uint256"},
			{"name": "amount1",   "type": "uint256"}
		]
	}
]`

func main() {
	npmAddr := common.HexToAddress("0xC36442b4a4522E871399CD717aBDD847Ab11FE88")    // Uniswap V3 NPM
	helperAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD01") // your deployment

	npm := weiroll.NewContract(npmAddr, weiroll.MustParseABI(npmABI))
	helper := weiroll.NewLibrary(helperAddr, weiroll.MustParseABI(tupleHelperABI))
	npmConsumer := weiroll.NewContract(npmAddr, weiroll.MustParseABI(npmConsumerABI))

	planner := weiroll.New()

	// Build the mint params tuple. Real values would come from user
	// input. The Go struct's field order must match the ABI tuple.
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	mintParams := MintParams{
		Token0:         weth,
		Token1:         usdc,
		Fee:            big.NewInt(500),
		TickLower:      big.NewInt(-887270),
		TickUpper:      big.NewInt(887270),
		Amount0Desired: big.NewInt(1e18),
		Amount1Desired: big.NewInt(1_000_000_000),
		Amount0Min:     big.NewInt(0),
		Amount1Min:     big.NewInt(0),
		Recipient:      common.HexToAddress("0xCAFECAFECAFECAFECAFECAFECAFECAFECAFECAFE"),
		Deadline:       big.NewInt(2_000_000_000),
	}

	// Step 1: mint with .RawReturn(). The slot now holds the entire
	// 128-byte returndata as bytes. mintRaw.Type() == "bytes" and
	// IsDynamic() == true.
	mintRaw := planner.Add(npm.MustInvoke("mint", mintParams).RawReturn())
	fmt.Printf("mintRaw.Type() = %s (dynamic=%v)\n",
		mintRaw.Type().String(), mintRaw.IsDynamic())

	// Step 2: extract the first 32-byte word (the tokenId) using the
	// TupleHelper. The helper returns bytes32.
	tokenIdB32 := planner.Add(helper.MustInvoke("extract", mintRaw, big.NewInt(0)))
	fmt.Printf("tokenIdB32.Type() = %s\n", tokenIdB32.Type().String())

	// Step 3: re-type bytes32 -> uint256 OFF-CHAIN.
	// Both are static 32-byte slots; the on-chain bytes are identical.
	// .As avoids a second on-chain command.
	tokenId := tokenIdB32.MustAsType("uint256")
	fmt.Printf("tokenId.Type()    = %s (same slot, retyped off-chain)\n",
		tokenId.Type().String())

	// At this point, `tokenId` is a *ReturnValue typed as uint256
	// pointing at the same slot as `tokenIdB32`. You can pass it to
	// any function expecting uint256 — e.g., NPM.positions(tokenId)
	// or NPM.increaseLiquidity(struct{tokenId, ...}). Building the
	// consumer struct requires referencing tokenId through the slot
	// system; that's a separate concern from the cast itself.
	_ = npmConsumer

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("\nCommands: %d (mint + extract; the .As cast is free)\n", len(plan.Commands))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	// Slot-encoding sanity:
	//   - Producer return slot byte is CLEAN (no 0x80). The VM's
	//     writeTuple uses this byte unmasked, so any flag bit would
	//     index out of bounds.
	//   - Consumer arg byte HAS 0x80, because the slot holds dynamic
	//     length-prefixed bytes and buildInputs masks the index.
	_, _, _, prodReturnSlot, _, _ := weiroll.DecodeCommand(plan.Commands[0])
	_, _, consArgs, _, _, _ := weiroll.DecodeCommand(plan.Commands[1])
	fmt.Printf("\nmint return slot byte:    0x%02x (dynamic=%v) -- writeTuple uses unmasked\n",
		prodReturnSlot, prodReturnSlot&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("extract first arg byte:   0x%02x (dynamic=%v) -- buildInputs masks the flag\n",
		consArgs[0], consArgs[0]&weiroll.DynamicSlotFlag != 0)
}
