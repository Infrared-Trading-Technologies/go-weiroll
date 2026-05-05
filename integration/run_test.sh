#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Parse arguments
FORK_MODE=false
RPC_URL=""
TEST_PATTERN=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --fork)
            FORK_MODE=true
            shift
            ;;
        --rpc)
            RPC_URL="$2"
            shift 2
            ;;
        --test)
            TEST_PATTERN="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--fork] [--rpc <RPC_URL>] [--test <pattern>]"
            exit 1
            ;;
    esac
done

echo "=== Weiroll Integration Tests ==="
echo ""

# Check for Foundry
if ! command -v forge &> /dev/null; then
    echo "Error: Foundry (forge) not found. Install from https://getfoundry.sh"
    exit 1
fi

if ! command -v anvil &> /dev/null; then
    echo "Error: Anvil not found. Install from https://getfoundry.sh"
    exit 1
fi

# Compile contracts
echo "1. Compiling contracts with Forge..."
forge build --silent
echo "   ✓ Contracts compiled"

# Start Anvil
echo "2. Starting Anvil..."
if [ "$FORK_MODE" = true ]; then
    if [ -z "$RPC_URL" ]; then
        RPC_URL="${MAINNET_RPC_URL:-}"
    fi
    if [ -z "$RPC_URL" ]; then
        echo "   Error: Fork mode requires --rpc <URL> or MAINNET_RPC_URL env var"
        exit 1
    fi
    echo "   Mode: Mainnet Fork"
    anvil --fork-url "$RPC_URL" --port 8545 &> /dev/null &
else
    echo "   Mode: Fresh Chain"
    anvil --port 8545 &> /dev/null &
fi
ANVIL_PID=$!

# Cleanup on exit
cleanup() {
    echo ""
    echo "Cleaning up..."
    kill $ANVIL_PID 2>/dev/null || true
}
trap cleanup EXIT

# Wait for Anvil to be ready (fork mode takes longer)
echo "   Waiting for Anvil to be ready..."
MAX_WAIT=30
WAIT_COUNT=0
while ! curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' http://localhost:8545 > /dev/null 2>&1; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
        echo "   Error: Anvil failed to start within ${MAX_WAIT}s"
        exit 1
    fi
done
echo "   ✓ Anvil running (PID: $ANVIL_PID)"

# Run the Go tests
echo "3. Running integration tests..."
echo ""

cd "$SCRIPT_DIR"

if [ "$FORK_MODE" = true ]; then
    export FORK_TEST=1
    export MAINNET_RPC_URL="$RPC_URL"
fi

export INTEGRATION_TEST=1

if [ -n "$TEST_PATTERN" ]; then
    go test -v -run "$TEST_PATTERN" -timeout 300s
else
    go test -v -timeout 300s
fi

echo ""
echo "=== Tests Complete ==="
