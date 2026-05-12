# go-weiroll

A Go implementation of the [weiroll](https://github.com/weiroll/weiroll) command planner for Ethereum smart contract operation chaining.

## Overview

Weiroll is a virtual machine that batches Ethereum operations via a scripted command sequence. This library allows you to:

- Chain multiple contract calls into a single atomic transaction
- Pass return values between operations without separate transactions
- Support CALL, STATICCALL, and CALL_WITH_VALUE

## Installation

```bash
go get github.com/branched-services/go-weiroll@latest
```

Or pin to a specific version:

```bash
go get github.com/branched-services/go-weiroll@v0.0.1
```

## Quick Start

```go
package main

import (
    "math/big"
    "github.com/branched-services/go-weiroll"
    "github.com/ethereum/go-ethereum/common"
)

func main() {
    // Parse contract ABIs
    mathABI := weiroll.MustParseABI(mathABIJSON)
    tokenABI := weiroll.MustParseABI(tokenABIJSON)

    // Wrap contracts. All targets are called via CALL.
    math := weiroll.NewContract(mathLibAddr, mathABI)      // CALL
    token := weiroll.NewContract(tokenAddr, tokenABI)      // CALL

    // Build plan
    planner := weiroll.New()

    sum := planner.Add(math.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
    product := planner.Add(math.MustInvoke("multiply", sum, big.NewInt(10)))
    planner.Add(token.MustInvoke("transfer", recipient, product))

    // Compile
    plan, err := planner.Plan()
    if err != nil {
        panic(err)
    }

    // Execute via weiroll VM contract
    commands := plan.CommandsAsBytes32()
    state := plan.StateAsBytes()
}
```

## Features

### Contract Types

- **External** (`NewContract`): CALL. The target gets its own frame.
- **Static** (`NewContract` with `WithStaticCalls()`): STATICCALL by default.

```go
contract := weiroll.NewContract(addr, abi)                            // CALL
readOnly := weiroll.NewContract(addr, abi, weiroll.WithStaticCalls()) // STATICCALL default
```

The VM rejects `FLAG_CT_DELEGATECALL` (0x00) with `revert("Invalid calltype")`. Any helper that previously needed DELEGATECALL semantics (acting AS the VM, using its approvals) must be reworked: have the VM `approve(adapter, amount)` before the adapter call so the adapter can pull tokens via standard CALL.

### Call Modifiers

Modifiers return a new `*Call`; chain them rather than calling on a stored value.

```go
// Send ETH with the call
planner.Add(weth.MustInvoke("deposit").WithValue(big.NewInt(1e18)))

// Force STATICCALL
planner.Add(token.MustInvoke("balanceOf", who).Static())

// Capture full returndata as bytes (for multi-return / struct-shaped returns).
// The resulting *ReturnValue is typed as `bytes`; use .As/.MustAs to retype
// downstream when piping into a typed argument.
raw := planner.Add(adapter.MustInvoke("mintFlat", ...).RawReturn())
```

### Value Types

```go
// Literals are created automatically from Go values
planner.Add(contract.MustInvoke("method", big.NewInt(100)))

// Or explicitly
weiroll.Uint256(big.NewInt(100))
weiroll.Address(common.Address{})
weiroll.Bytes32(common.Hash{})
weiroll.Bool(true)
weiroll.String("hello")
weiroll.Bytes([]byte{1, 2, 3})

// Return values from previous commands
sum := planner.Add(math.MustInvoke("add", 1, 2))
planner.Add(math.MustInvoke("multiply", sum, 3))  // uses sum

// Retype a return value off-chain between encoding-compatible types
// (bytes32 → uint256, bytes → uint256[], …). Rejects mixing static and
// dynamic types since their slot encodings differ.
tokenIdB32 := planner.Add(helper.MustInvoke("extract", raw, big.NewInt(0)))
tokenId := tokenIdB32.MustAsType("uint256")
```

### Plan Options

```go
plan, err := planner.Plan(
    weiroll.WithSlotOptimization(true),   // Enable slot recycling (default)
    weiroll.WithMaxCommands(256),          // Max command limit
)
```

## Command Encoding

Commands are encoded as 32-byte (standard) or 64-byte (extended for >6 args) packed structures:

**Standard (≤6 args):**
```
[selector:4][flags:1][arg0-5:6][return:1][address:20]
```

**Extended (>6 args):**
```
Word 1: [selector:4][flags|0x40:1][0xff×6:6][return:1][address:20]
Word 2: [up to 32 arg slots, 0xff-padded]
```

The VM reads `indices = commands[i+1]` for extended commands — Word 1's input bytes are not used for arg routing and are filled with `0xff`. Flag bits: `0x40 = EXTENDED`, `0x80 = TUPLE_RETURN` (RawReturn), `0x03` mask = call type (`0x01` CALL, `0x02` STATICCALL, `0x03` CALL_WITH_VALUE). `0x00` is reserved and rejected by the VM.

## State Management

The planner automatically optimizes state usage:

- **Literal Deduplication**: Identical values share the same slot
- **Slot Recycling**: Slots are reused after their last usage
- **Max 127 Slots**: Enforced limit with clear error messages

## Requirements

- Go 1.22+
- github.com/ethereum/go-ethereum

## Versioning

This project follows [Semantic Versioning](https://semver.org/). See the [releases](https://github.com/branched-services/go-weiroll/releases) for available versions, and [`docs/releasing.md`](./docs/releasing.md) for the release workflow.

## Testing

```bash
# Unit tests (no chain needed)
go test -v ./...

# Integration tests against a fresh Anvil
./integration/run_test.sh

# Integration tests against an Anvil mainnet fork (real WETH, Uniswap, Aave, NPM)
./integration/run_test.sh --fork --rpc "$MAINNET_RPC_URL"
```

The fork tests under `integration/examples_fork_test.go` double as worked examples for the patterns above (pre-funded VM, inline-ETH + pure helpers). See [`integration/README.md`](./integration/README.md) for the test inventory.

## Examples

See the [examples](./examples) directory for usage examples organized by case number (01_simple_return through 06_self_balance), plus the `basic`, `uniswap-v2-swap`, and `weth-wrap-unwrap` walkthroughs.

## License

MIT

## References

- [weiroll VM (Solidity)](https://github.com/weiroll/weiroll)
- [weiroll.js (JavaScript)](https://github.com/weiroll/weiroll.js)
