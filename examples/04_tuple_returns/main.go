// Case 4: Function returns a tuple.
//
// Example: Uniswap V3 NonfungiblePositionManager.mint returns
//   (uint256 tokenId, uint128 liquidity, uint256 amount0, uint256 amount1)
//
// A weiroll *ReturnValue is one slot. Tuples need extraction helpers.
//
// The pattern, demonstrated end-to-end below:
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
//      as the correct type — off-chain, no extra command.
//
//   4. Use the typed *ReturnValue as input to a downstream call.
//      Below, the extracted tokenId flows directly into
//      NPM.transferFrom to ship the freshly-minted LP NFT to the user.
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
//     consumes. Here we only extract slot 0 (tokenId); liquidity,
//     amount0, and amount1 stay buried in the bytes blob.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/common"
)

// NPM's mint() takes a fully-static 11-field tuple. The on-chain VM
// requires every static state slot to be exactly 32 bytes, so the
// tuple must be expanded into per-field slots — packing all 11 fields
// (11*32 = 352 bytes) into one slot would revert at the VM's
// `Static state variables must be 32 bytes` guard. The
// expansion is done explicitly via weiroll.Tuple(...) below.

// NPM ABI: mint (tuple-returning) and transferFrom (the downstream
// consumer of the extracted tokenId). NPM is also an ERC721, so the
// LP-position NFT can be transferred with transferFrom.
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
	},
	{
		"name": "transferFrom",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "from",    "type": "address"},
			{"name": "to",      "type": "address"},
			{"name": "tokenId", "type": "uint256"}
		],
		"outputs": []
	}
]`

// TupleHelper. The on-chain implementation lives at
// integration/contracts/TupleHelper.sol — deploy once, reuse for every
// tuple-returning method. Returns one 32-byte ABI word so callers can
// re-type it via ReturnValue.As.
const tupleHelperABI = `[
	{
		"name": "extract",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "data",  "type": "bytes"},
			{"name": "index", "type": "uint256"}
		],
		"outputs": [{"name": "word", "type": "bytes32"}]
	}
]`

func main() {
	npmAddr := common.HexToAddress("0xC36442b4a4522E871399CD717aBDD847Ab11FE88")    // Uniswap V3 NPM
	helperAddr := common.HexToAddress("0x0000000000000000000000000000000000DEAD01") // your deployment
	vmAddr := common.HexToAddress("0xEEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0")     // weiroll executor
	user := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")        // who gets the NFT

	npm := weiroll.NewContract(npmAddr, weiroll.MustParseABI(npmABI))
	helper := weiroll.NewLibrary(helperAddr, weiroll.MustParseABI(tupleHelperABI))

	planner := weiroll.New()

	// mint params — token0 < token1 by address (USDC < WETH on mainnet).
	// Recipient is the executor itself, so the freshly-minted NFT lands
	// at the VM. We then transfer it to `user` using the extracted
	// tokenId, all in the same weiroll tx.
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")

	// Build the params tuple field-by-field. Each field becomes its
	// own 32-byte state slot; weiroll.Tuple binds against the ABI
	// tuple type pulled from the method signature at Invoke time.
	mintParams := weiroll.Tuple(
		weiroll.Address(usdc),
		weiroll.Address(weth),
		weiroll.MustLiteralFromType("uint24", big.NewInt(500)),
		weiroll.MustLiteralFromType("int24", big.NewInt(-887220)), // tick spacing 10 for fee 500
		weiroll.MustLiteralFromType("int24", big.NewInt(887220)),
		weiroll.Uint256(big.NewInt(1_000_000_000)), // 1000 USDC, 6 decimals
		weiroll.Uint256(big.NewInt(5e17)),          // 0.5 WETH
		weiroll.Uint256(big.NewInt(0)),
		weiroll.Uint256(big.NewInt(0)),
		weiroll.Address(vmAddr),
		weiroll.Uint256(big.NewInt(2_000_000_000)),
	)

	// Step 1: mint with .RawReturn(). The slot now holds the entire
	// 128-byte returndata as bytes. mintRaw.Type() == "bytes".
	mintRaw := planner.Add(npm.MustInvoke("mint", mintParams).RawReturn())
	fmt.Printf("[1] mintRaw.Type()    = %s (dynamic=%v)\n",
		mintRaw.Type().String(), mintRaw.IsDynamic())

	// Step 2: extract the first 32-byte word (tokenId) via TupleHelper.
	tokenIdB32 := planner.Add(helper.MustInvoke("extract", mintRaw, big.NewInt(0)))
	fmt.Printf("[2] tokenIdB32.Type() = %s\n", tokenIdB32.Type().String())

	// Step 3: re-type bytes32 -> uint256 OFF-CHAIN. Both are static
	// 32-byte slots; the on-chain bytes are identical.
	tokenId := tokenIdB32.MustAsType("uint256")
	fmt.Printf("[3] tokenId.Type()    = %s (same slot, retyped via .As)\n",
		tokenId.Type().String())

	// Step 4: USE the extracted tokenId. Transfer the LP NFT from the
	// VM to the user. This is what closes the Case 4 loop — the value
	// you went through RawReturn -> extract -> .As to get is now an
	// argument to a real downstream call.
	planner.Add(npm.MustInvoke("transferFrom", vmAddr, user, tokenId))
	fmt.Printf("[4] transferFrom queued: VM -> user, tokenId from slot\n")

	plan, err := planner.Plan()
	if err != nil {
		log.Fatalf("Plan failed: %v", err)
	}

	fmt.Printf("\nCommands: %d  (mint + extract + transferFrom; .As is free)\n", len(plan.Commands))
	for i, cmd := range plan.Commands {
		fmt.Printf("  [%d] 0x%s\n", i, hex.EncodeToString(cmd))
	}

	// Slot-encoding sanity:
	//   - mint's return slot byte is CLEAN (no 0x80). writeTuple uses
	//     it unmasked, so any flag bit would put the slot index out of
	//     bounds and revert on chain.
	//   - extract's first arg byte HAS 0x80, because it's reading the
	//     dynamic bytes the producer wrote. buildInputs masks correctly.
	//   - transferFrom's tokenId arg slot is the SAME slot as
	//     tokenIdB32, since .As only changed the off-chain type.
	_, _, _, mintReturnSlot, _, _ := weiroll.DecodeCommand(plan.Commands[0])
	_, _, extractArgs, extractReturnSlot, _, _ := weiroll.DecodeCommand(plan.Commands[1])
	_, _, transferArgs, _, _, _ := weiroll.DecodeCommand(plan.Commands[2])
	fmt.Println()
	fmt.Printf("  mint return slot byte:        0x%02x (dynamic=%v)\n",
		mintReturnSlot, mintReturnSlot&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("  extract first arg byte:       0x%02x (dynamic=%v)\n",
		extractArgs[0], extractArgs[0]&weiroll.DynamicSlotFlag != 0)
	fmt.Printf("  extract return slot byte:     0x%02x\n", extractReturnSlot)
	fmt.Printf("  transferFrom tokenId arg:     0x%02x  <- same slot as extract's return\n",
		transferArgs[2])
}
