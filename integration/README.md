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

### TestMathValueChaining
Basic value chaining test on fresh Anvil (no fork needed): `(5 + 3) * 10 - 20 = 60`

### TestMainnetForkWETH
Tests against real mainnet WETH contract:
- Wrap ETH → WETH via `deposit()`
- Verifies balance after wrap

### TestMainnetForkWETHWrapUnwrap
Tests wrap + partial unwrap in a single transaction using real mainnet WETH:
1. `deposit()` - wrap 1 ETH to WETH
2. `withdraw()` - unwrap 0.3 ETH back

Key insight: `deposit()` and `withdraw()` have **no return values**, demonstrating how weiroll handles void functions.

### TestMainnetForkUniswapV2Swap
Tests a real WETH → USDC swap on forked mainnet using the actual Uniswap V2 Router at `0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D`.

### TestMainnetForkMultiHopSwap
**The flagship test!** Demonstrates chained swaps with real contracts:
1. Approve router for WETH
2. Swap WETH → USDC on real Uniswap V2
3. Extract USDC output amount using MathLib
4. Approve router for USDC  
5. Swap USDC → DAI using **the chained output from step 2**

This demonstrates the core weiroll value proposition: using return values from one call as inputs to subsequent calls, all executed atomically.

```bash
./run_test.sh --fork --rpc $MAINNET_RPC_URL --test TestMainnetForkMultiHopSwap
```

## Real Mainnet Contracts Used

| Contract          | Address                                      |
| ----------------- | -------------------------------------------- |
| WETH              | `0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2` |
| USDC              | `0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48` |
| DAI               | `0x6B175474E89094C44Da98b954EedeAC495271d0F` |
| Uniswap V2 Router | `0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D` |

## Contracts (Deployed by Tests)

- `contracts/VM.sol` - Weiroll VM implementation
- `contracts/MathLib.sol` - Math operations + `extractLastElement()` helper

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
forge build

# In one terminal, start Anvil
anvil --port 8545

# In another terminal, run tests
INTEGRATION_TEST=1 go test -v -run TestMathValueChaining
```

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

## Extending the Tests

To test with real protocols (like Uniswap V2), you would:
1. Fork mainnet with Anvil: `anvil --fork-url $RPC_URL`
2. Use real contract addresses
3. Fund your test account with tokens
4. Execute the swap plan

See `../examples/uniswap-v2-swap/` for an example plan (without execution).
