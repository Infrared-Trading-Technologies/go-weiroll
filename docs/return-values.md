# Return Values in go-weiroll

Reference for how `*weiroll.ReturnValue` works, what it costs, and how
to extract values from on-chain calls. Read before authoring a new
Action.

## Core type

`*weiroll.ReturnValue` represents one return slot from one weiroll
command — typed (`uint256`, `address`, `bytes`, etc.) and pipeable as
input to a downstream command. You **cannot** index, split, or operate
on it in Go. To manipulate it:

- Append another command (a helper-library call) that consumes it, or
- For encoding-compatible reinterpretations, use `ReturnValue.As`.

The number of `*ReturnValue`s your Action exposes equals the number of
weiroll commands it costs to produce them. Every output has on-chain
cost.

## Type-checking guarantees

go-weiroll checks `Value.Type().String()` against the destination
parameter's ABI type at `Invoke` / `MustInvoke` time. Mismatches
produce a `TypeMismatchError`. They are **not** silent — unless the
ABI you supplied is itself wrong (e.g., declares `uint256` for a
function that actually returns `int256`), in which case the encoding
will be off but go-weiroll has no way to know.

## The six cases

Sorted simplest to ugliest.

### Case 1 — Function returns the value you want

The clean path. One `Planner.Add`, wrap in your outputs struct.

```go
balance := planner.Add(usdc.MustInvoke("balanceOf", holder).Static())
planner.Add(usdc.MustInvoke("transfer", recipient, balance))
```

Reference: [examples/01_simple_return](../examples/01_simple_return/main.go),
fork test `TestForkCase1_BalanceOfTransfer`.

### Case 2 — Function returns void; value is the literal input

`WETH.deposit{value: x}()`, ERC20 `approve`. The literal you passed in
is the ground truth.

```go
planner.Add(weth.MustInvoke("deposit").WithValue(amount))
planner.Add(weth.MustInvoke("approve", spender, amount)) // same literal
```

**Anti-pattern.** Do not append `weth.balanceOf(holder)` to "recover"
the deposited amount. It captures pre-existing balance plus the
deposit, not the deposit delta. The literal is exact and forgery-proof.

Reference: [examples/02_void_with_literal](../examples/02_void_with_literal/main.go).

### Case 3 — Function returns void; value is recoverable but tricky

Aave V3 `Pool.supply` returns nothing. Downstream wants the resulting
aToken position.

Pitfalls:
- **Pre-existing balance contamination.** `aToken.balanceOf(onBehalf)`
  reports cumulative position. Only safe when `onBehalf` had a zero
  prior balance (per-user proxy).
- **Share-token unit confusion.** cTokens, yvTokens, ERC4626 vault
  shares — `balanceOf` is in *share* units, not underlying. Aave V3
  aTokens are an exception: `balanceOf` is in underlying (rebasing).
- **Fee-on-transfer underlying.** Some USDT configs, PAXG: deposited
  amount differs from input. Be explicit about which one your
  `*ReturnValue` represents.

If the underlying is plain ERC20, just reuse the supply amount as a
literal — saves one command per recipe.

Reference: [examples/03_balance_recovery](../examples/03_balance_recovery/main.go),
fork test `TestForkCase3_AaveSupplyAndATokenRead`.

### Case 4 — Function returns a tuple

Uniswap V3 `NPM.mint` returns `(uint256 tokenId, uint128 liquidity,
uint256 amount0, uint256 amount1)`. A `*ReturnValue` is one slot.
Tuples need an extraction helper.

Pattern:

```go
// 1. RawReturn captures the entire returndata as length-prefixed bytes.
mintRaw := planner.Add(npm.MustInvoke("mint", params).RawReturn())
// mintRaw.Type() == "bytes" (dynamic)

// 2. Extract one ABI word via a deployed helper. Returns bytes32.
tokenIdB32 := planner.Add(tupleHelper.MustInvoke("extract", mintRaw, big.NewInt(0)))

// 3. Re-type bytes32 -> uint256 OFF-CHAIN. Same slot, no extra command.
tokenId := tokenIdB32.MustAsType("uint256")

// 4. Use the typed value downstream.
planner.Add(npm.MustInvoke("transferFrom", vmAddr, user, tokenId))
```

Pitfalls:
- **Forgetting `.RawReturn()`** silently keeps only the first 32-byte
  output. Other fields are unreachable.
- **Without `.As`**, you'd need an on-chain `bytes32 -> uint256` cast
  helper, adding one command per field.
- **Don't expose every field.** Each extraction is one command. Pull
  only what downstream consumes.

The on-chain helper is one function:
```solidity
function extract(bytes calldata data, uint256 index)
    external pure returns (bytes32 word)
{
    uint256 offset = index * 32;
    require(data.length >= offset + 32, "OOB");
    assembly { word := calldataload(add(data.offset, offset)) }
}
```
Source: [integration/contracts/TupleHelper.sol](../integration/contracts/TupleHelper.sol).

References: [examples/04_tuple_returns](../examples/04_tuple_returns/main.go),
fork test `TestForkCase4_TupleReturnRoundTrip`.

### Case 5 — Dynamic-length returns (`bytes`, `string`, `T[]`)

go-weiroll's planner sets the dynamic-slot flag (`0x80`) automatically
on consumer arg bytes. Piping `swapResult` (typed `uint256[]`) into a
function that accepts `uint256[]` Just Works.

```go
swapResult := planner.Add(router.MustInvoke("swapExactTokensForTokens", ...))
amountOut  := planner.Add(math.MustInvoke("extractLastElement", swapResult))
```

Pitfalls:
- **Static destination.** Piping a dynamic `*ReturnValue` into a
  static argument is rejected at Invoke time (`TypeMismatchError`).
- **Heavier state.** Exposing a full array when only one element is
  consumed wastes a slot. Expose the scalar instead.

Reference: [examples/05_dynamic_returns](../examples/05_dynamic_returns/main.go),
fork test `TestMainnetForkUniswapV2Swap`.

### Case 6 — Value lives in storage on another contract (`balanceOf(self)` mid-recipe)

The most dangerous pattern. After a step lands tokens at the executor,
read `token.balanceOf(executor)` and feed it forward.

go-weiroll has no `Self()` sentinel: pass the executor address as a
literal at plan time. That's a deliberate scope choice — the right
answer depends on per-user-proxy vs shared-executor deployment.

Pitfalls (every one of these has burned someone):

1. **Pre-existing balance contamination.** `balanceOf(executor)`
   returns total balance, not "what this recipe produced." Dust,
   leftovers, attacker-planted transfers all inflate the read.
2. **Forced sends are unstoppable.** Anyone can `transfer` an ERC20
   to your executor. ETH can be force-credited via coinbase payments
   (selfdestruct as forced-send was retired in EIP-6780/Cancun).
3. **Threat model depends on executor design.** Per-user proxy: risk
   bounded to your own dust. Shared executor: requires snapshot-diff
   (read before AND after, subtract).
4. **ERC777 reentrancy.** `tokensReceived` hooks fire between two
   reads in the same transaction.
5. **Share / wrapper unit confusion.** `balanceOf` of cTokens,
   yvTokens, ERC4626 shares is in shares, not underlying.
6. **Rebasing tokens.** stETH, AMPL, aTokens. Within one tx fine,
   across blocks not.

**Decision rule.** If the producing call's return value contains the
amount you need, prefer extracting from it (forgery-proof). Reach for
`balanceOf(executor)` only when no upstream return covers it.

Reference: [examples/06_self_balance](../examples/06_self_balance/main.go),
fork test `TestForkCase6_SelfBalancePattern`.

## Composing cases

Real recipes interleave them: dynamic-return extraction (Case 5)
feeds a tuple-returning call whose tuple field (Case 4) feeds a
self-balance read (Case 6). Compose by piping the `*ReturnValue` from
one case directly into the consuming command of the next.

## `ReturnValue.As`

```go
// As reinterprets a slot as a different ABI type, off-chain.
//   - Static  -> Static   (32-byte slot encoding identical):  OK
//   - Dynamic -> Dynamic  (length-prefixed encoding identical): OK
//   - Static <-> Dynamic:                                    rejected
func (v *ReturnValue) As(abiType abi.Type) (*ReturnValue, error)
func (v *ReturnValue) MustAs(abiType abi.Type) *ReturnValue
func (v *ReturnValue) AsType(typeStr string) (*ReturnValue, error)
func (v *ReturnValue) MustAsType(typeStr string) *ReturnValue
```

Common uses: `bytes32 -> uint256` / `bytes32 -> address` after a
tuple extraction; `bytes -> uint256[]` when you know the dynamic
blob is shaped like an array.

`As` does not change the slot's contents — only the Go-side type
metadata. For real numeric conversions (e.g., `int128` to `uint256`
with sign extension), you still need a Solidity helper.

## Tuple-input handling

For fully-static tuple inputs (e.g. Uniswap V3 `SwapRouter02.exactInputSingle`'s
7-field params struct), use `weiroll.Tuple(field1, field2, ...)` — the planner
expands the tuple into per-field state slots so each component can be a literal
or a `*ReturnValue`. See `integration/examples_fork_test.go:TestForkUniV3SwapStaticTupleInput`
for the canonical pattern.

For mixed static/dynamic or fully-dynamic tuple inputs that the planner cannot
expand, write a tiny flattening adapter contract that re-exposes the function
with flat arguments and have the VM `approve(adapter, amount)` first so the
adapter can pull tokens via a regular CALL (the VM has no DELEGATECALL dispatcher).

## RawReturn slot encoding

This is internal to the planner — relevant only if you're auditing
encoded commands. The Solidity VM has two return-handling paths:

| Producer flag | VM path        | Slot byte handling      |
|---------------|----------------|-------------------------|
| (none)        | `writeOutputs` | masked with `0x7f`      |
| `FlagTupleReturn` | `writeTuple` | **unmasked** `state[idx]` |

Implication: for `RawReturn` calls the producer return-slot byte
**must be clean** (no `0x80`), or `state[idx]` indexes out of bounds
and the tx reverts. The dynamic flag still appears on the consumer's
arg byte (where `buildInputs` masks correctly). go-weiroll handles
this automatically; the unit test
`TestRawReturnProducerSlotIsClean` is the regression guard.

## Authoring checklist

Before shipping a new Action:

1. Which case (1–6) does this fall into? If multiple, handle the worst.
2. For every `*ReturnValue` in your outputs: is it a true delta from
   this action, or a state read? State reads are not deltas. Naming a
   state read like a delta is a bug.
3. Is the underlying token fee-on-transfer, rebasing, or a share
   token? If yes, the literal input ≠ the post-action balance.
4. If returning a tuple field: did you `.RawReturn()` and add the
   extraction? If the field is 32-byte static, did you `.As` to skip
   an on-chain cast?
5. Count the extra commands your Action costs. Anything beyond the
   core call is paid by every user, every recipe.
6. Does the Action assume a clean executor? If yes, that assumption
   must be loud in the godoc. Silent assumptions get exploited.
