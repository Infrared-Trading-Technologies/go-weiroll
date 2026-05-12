# Weiroll Integration Tests

This directory contains integration tests that verify the go-weiroll library works correctly with an actual weiroll VM deployed on Anvil using **mainnet fork mode**.

## Prerequisites

- [Foundry](https://getfoundry.sh) (for `forge` and `anvil`)
- Go 1.22+
- A mainnet RPC URL (Alchemy, Infura, etc.)

## Running the Tests

The tests use **mainnet fork mode** to interact with real deployed contracts (WETH, Uniswap V2, etc.):

```bash
# Run all fork tests
./run_test.sh --fork --rpc https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY

# Run specific test
./run_test.sh --fork --rpc $MAINNET_RPC_URL --test TestMainnetForkMultiHopSwap

# Run just the basic math test (no fork needed)
./run_test.sh --test TestMathValueChaining
```

This will:
1. Compile the Solidity contracts with Forge
2. Start Anvil with mainnet fork
3. Run the Go integration tests against real contracts
4. Clean up Anvil when done

## Available Tests

### Basic / non-fork

- **TestMathValueChaining** — value chaining on fresh Anvil: `(5 + 3) * 10 - 20 = 60`.

### Mainnet fork — pre-funded VM (`auth.Value == 0`)

- **TestMainnetForkWETH** — `WETH.deposit` against real WETH; verifies balance.
- **TestMainnetForkWETHWrapUnwrap** — wrap + partial unwrap in one tx. Demonstrates void-return functions.
- **TestMainnetForkUniswapV2Swap** — real WETH → USDC swap via the actual UniV2 router.
- **TestMainnetForkMultiHopSwap** — flagship: WETH → USDC → DAI with the second swap consuming the chained amount from the first. Single atomic tx.

### Mainnet fork — case-numbered worked examples

These mirror `../examples/01..07` and are the reference patterns for writing real recipes.

- **TestForkCase1_BalanceOfTransfer** — read VM balance, transfer that amount.
- **TestForkCase4_TupleReturnRoundTrip** — `RawReturn` + `TupleHelper.extract` + `MustAsType` to read individual fields out of a multi-return ABI output (UniV2 pair `getReserves`).
- **TestForkCase6_SelfBalancePattern** — inline ETH + pure helpers: wrap, swap, `extractLastElement`, approve Aave, supply. The "self-balance" probe is a no-op visibility prod.
- **TestForkCase3_AaveSupplyAndATokenRead** — same shape as case 6, then reads aUSDC.balanceOf(VM) post-supply and pipes it into a downstream consumer.

### Two patterns the fork tests demonstrate

| Pattern | Tests | Notes |
|---|---|---|
| Pre-funded VM, value=0 | UniswapV2Swap, MultiHopSwap, Case4, Case1 | Cleanest contract surface; multi-tx UX or pre-existing balances. |
| Inline ETH, only pure helpers | Case6, Case3 | All helpers register as `NewContract` (CALL); the VM has no DELEGATECALL path. |

## Real Mainnet Contracts Used

| Contract          | Address                                      |
| ----------------- | -------------------------------------------- |
| WETH              | `0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2` |
| USDC              | `0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48` |
| DAI               | `0x6B175474E89094C44Da98b954EedeAC495271d0F` |
| Uniswap V2 Router | `0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D` |

## Contracts (Deployed by Tests)

- `contracts/VM.sol` — Weiroll VM (the abstract `VM` + concrete `WeirollVM`).
- `contracts/CommandBuilder.sol` — input-encoding + output-writing library used by the VM.
- `contracts/MathLib.sol` — pure math helpers + `extractLastElement(uint256[])`. Registered with `NewContract` (CALL).
- `contracts/TupleHelper.sol` — `extract(bytes,uint256) → bytes32` for decoding `RawReturn` blobs. Registered with `NewContract`.

## Manual Testing

```bash
# Compile contracts
forge build

# Start Anvil (in another terminal)
anvil --port 8545

# Or with mainnet fork:
anvil --fork-url $MAINNET_RPC_URL --port 8545

# Run tests
INTEGRATION_TEST=1 go test -v

# Run fork tests
INTEGRATION_TEST=1 FORK_TEST=1 MAINNET_RPC_URL=$RPC go test -v -run TestMainnetFork
```

## How Value Chaining Works

```
Step 1: swapExactTokensForTokens(1 WETH, ...) → returns uint256[] amounts
Step 2: extractLastElement(amounts)           → returns uint256 (output amount)
Step 3: swapExactTokensForTokens(output, ...) → uses result from step 2!
```

All three operations execute atomically in a single transaction.

## Understanding the Output

When you run the test, you'll see:
- Deployed contract addresses
- Encoded weiroll commands (32-byte hex strings)
- Gas used for execution
- Confirmation that value chaining worked

Example output:
```
Command[0]: 0x771602f7000102ffffffff00...  # add(5, 3)
Command[1]: 0x165c4a16000004ffffffff03...  # multiply(result, 10)
Command[2]: 0x3ef5e445000300ffffffffff...  # subtract(result, 20)
Transaction successful! Gas used: 52476
```

## Adding a new fork test

1. Add the test to `examples_fork_test.go` and gate it with `skipUnlessFork(t)`.
2. Decide your funding pattern (pre-funded vs inline ETH) — see the patterns table above.
3. All helpers register with `NewContract` (CALL or STATICCALL); the VM rejects `FLAG_CT_DELEGATECALL` with `revert("Invalid calltype")`.
4. Before `bind.Transact`, call `simulateExecute` (defined at the top of `examples_fork_test.go`) so a revert surfaces the failing command index + reason via the VM's `ExecutionFailed` custom error. Without it, `bind.Transact` only reports `status=0 gas=N`.
