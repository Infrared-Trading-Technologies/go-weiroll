# WETH Wrap/Unwrap Example

This example demonstrates how to use go-weiroll for WETH (Wrapped Ether) operations, with a focus on handling functions that have **no return values**.

## Key Concepts

### Functions Without Return Values

WETH's `deposit()` and `withdraw(uint256)` functions don't return anything:

```solidity
// WETH interface (simplified)
function deposit() external payable;           // No return value
function withdraw(uint wad) external;          // No return value
function balanceOf(address) view returns (uint);  // Returns uint256
function transfer(address, uint) returns (bool);  // Returns bool
```

### How Weiroll Handles This

When you add a call to the planner:

1. **Functions with return values** → `planner.Add()` returns a `*ReturnValue` that can be used as input to subsequent calls

2. **Functions without return values** → `planner.Add()` returns `nil`, but the call is still added and will execute normally

```go
// deposit() has no return - Add() returns nil
result := planner.Add(weth.MustInvoke("deposit").WithValue(amount))
// result == nil, but the deposit call is in the plan

// balanceOf() returns uint256 - Add() returns *ReturnValue
balance := planner.Add(weth.MustInvoke("balanceOf", vmAddr).Static())
// balance can be used as input to other calls

// Use the return value from balanceOf as input to withdraw
planner.Add(weth.MustInvoke("withdraw", balance))
```

### Sending ETH with Calls

Use `.WithValue(amount)` to send ETH with a call:

```go
depositCall := weth.MustInvoke("deposit").WithValue(big.NewInt(1e18))
planner.Add(depositCall)
```

This internally uses the `CALL_WITH_VALUE` opcode in weiroll.

## Running the Example

```bash
cd examples/weth-wrap-unwrap
go run main.go
```

## Example Output

```
=== WETH Wrap/Unwrap Example ===

--- Example 1: Simple ETH -> WETH Wrap ---
✓ deposit() has no return value - planner.Add() returns nil
  The call is still added and will execute on-chain
...
```

## Common Patterns

### Pattern 1: Wrap and Transfer
```go
planner := weiroll.New()
planner.Add(weth.MustInvoke("deposit").WithValue(amount))
planner.Add(weth.MustInvoke("transfer", recipient, amount))
```

### Pattern 2: Check Balance and Unwrap All
```go
planner := weiroll.New()
balance := planner.Add(weth.MustInvoke("balanceOf", myAddress).Static())
planner.Add(weth.MustInvoke("withdraw", balance))
```

### Pattern 3: Wrap and Approve for DEX
```go
planner := weiroll.New()
planner.Add(weth.MustInvoke("deposit").WithValue(amount))
planner.Add(weth.MustInvoke("approve", routerAddress, maxUint256))
// ... followed by swap calls
```

## Ignoring Return Values

If a function returns a value but you don't need it, simply assign to `_`:

```go
_ = planner.Add(weth.MustInvoke("approve", spender, amount))
```

The optimizer won't allocate a state slot for unused return values.
