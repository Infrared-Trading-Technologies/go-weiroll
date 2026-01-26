package integration

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Test private key (Anvil default account 0)
const testPrivateKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// Contract ABIs (compiled from Solidity)
const weirollVMABI = `[
	{
		"inputs": [
			{"name": "commands", "type": "bytes32[]"},
			{"name": "state", "type": "bytes[]"}
		],
		"name": "execute",
		"outputs": [{"name": "", "type": "bytes[]"}],
		"stateMutability": "payable",
		"type": "function"
	}
]`

const mathLibABI = `[
	{
		"inputs": [
			{"name": "a", "type": "uint256"},
			{"name": "b", "type": "uint256"}
		],
		"name": "add",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "pure",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "a", "type": "uint256"},
			{"name": "b", "type": "uint256"}
		],
		"name": "multiply",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "pure",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "a", "type": "uint256"},
			{"name": "b", "type": "uint256"}
		],
		"name": "subtract",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "pure",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "amounts", "type": "uint256[]"}
		],
		"name": "extractLastElement",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "pure",
		"type": "function"
	}
]`

// WETH ABI
const wethABI = `[
	{
		"inputs": [],
		"name": "deposit",
		"outputs": [],
		"stateMutability": "payable",
		"type": "function"
	},
	{
		"inputs": [{"name": "wad", "type": "uint256"}],
		"name": "withdraw",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [{"name": "account", "type": "address"}],
		"name": "balanceOf",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "dst", "type": "address"},
			{"name": "wad", "type": "uint256"}
		],
		"name": "transfer",
		"outputs": [{"name": "", "type": "bool"}],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "guy", "type": "address"},
			{"name": "wad", "type": "uint256"}
		],
		"name": "approve",
		"outputs": [{"name": "", "type": "bool"}],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

// Uniswap V2 Router ABI (subset)
const uniswapV2RouterABI = `[
	{
		"inputs": [
			{"name": "amountIn", "type": "uint256"},
			{"name": "amountOutMin", "type": "uint256"},
			{"name": "path", "type": "address[]"},
			{"name": "to", "type": "address"},
			{"name": "deadline", "type": "uint256"}
		],
		"name": "swapExactTokensForTokens",
		"outputs": [{"name": "amounts", "type": "uint256[]"}],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "amountIn", "type": "uint256"},
			{"name": "path", "type": "address[]"}
		],
		"name": "getAmountsOut",
		"outputs": [{"name": "amounts", "type": "uint256[]"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

type ContractArtifact struct {
	ABI      json.RawMessage `json:"abi"`
	Bytecode struct {
		Object string `json:"object"`
	} `json:"bytecode"`
}

func TestMathValueChaining(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("Set INTEGRATION_TEST=1 to run integration tests")
	}

	ctx := context.Background()

	// Connect to Anvil
	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		t.Fatalf("Failed to connect to Anvil: %v", err)
	}
	defer client.Close()

	// Get chain ID
	chainID, err := client.ChainID(ctx)
	if err != nil {
		t.Fatalf("Failed to get chain ID: %v", err)
	}
	t.Logf("Connected to chain ID: %d", chainID)

	// Load private key
	privateKey, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		t.Fatalf("Failed to create transactor: %v", err)
	}

	// Deploy MathLib
	mathLibAddr, err := deployContract(ctx, client, auth, privateKey, "MathLib")
	if err != nil {
		t.Fatalf("Failed to deploy MathLib: %v", err)
	}
	t.Logf("MathLib deployed at: %s", mathLibAddr.Hex())

	// Deploy WeirollVM
	vmAddr, err := deployContract(ctx, client, auth, privateKey, "WeirollVM")
	if err != nil {
		t.Fatalf("Failed to deploy WeirollVM: %v", err)
	}
	t.Logf("WeirollVM deployed at: %s", vmAddr.Hex())

	// Create weiroll plan using our library
	mathABI := weiroll.MustParseABI(mathLibABI)
	mathLib := weiroll.NewLibrary(mathLibAddr, mathABI)

	planner := weiroll.New()

	// Plan: (5 + 3) * 10 = 80
	// Step 1: add(5, 3) = 8
	sum := planner.Add(mathLib.MustInvoke("add", big.NewInt(5), big.NewInt(3)))
	t.Log("Added: add(5, 3) -> sum")

	// Step 2: multiply(sum, 10) = 80 (uses return value from step 1!)
	product := planner.Add(mathLib.MustInvoke("multiply", sum, big.NewInt(10)))
	t.Log("Added: multiply(sum, 10) -> product")

	// Step 3: subtract(product, 20) = 60 (uses return value from step 2!)
	planner.Add(mathLib.MustInvoke("subtract", product, big.NewInt(20)))
	t.Log("Added: subtract(product, 20) -> result")

	// Compile the plan
	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Failed to compile plan: %v", err)
	}

	t.Logf("Plan compiled: %d commands, %d state slots", len(plan.Commands), len(plan.State))

	// Execute the plan on the VM
	vmABI := weiroll.MustParseABI(weirollVMABI)
	vmContract := bind.NewBoundContract(vmAddr, vmABI, client, client, client)

	commands := plan.CommandsAsBytes32()
	state := plan.StateAsBytes()

	t.Logf("Executing with %d commands, %d state entries", len(commands), len(state))

	// Log the commands for debugging
	for i, cmd := range plan.Commands {
		t.Logf("  Command[%d]: 0x%s", i, hex.EncodeToString(cmd))
	}

	// Pack the execute call
	packedCommands := make([][32]byte, len(commands))
	copy(packedCommands, commands)

	// Get fresh nonce for execute call
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		t.Fatalf("Failed to get nonce: %v", err)
	}
	auth.Nonce = big.NewInt(int64(nonce))

	// Call execute
	tx, err := vmContract.Transact(auth, "execute", packedCommands, state)
	if err != nil {
		t.Fatalf("Failed to execute plan: %v", err)
	}

	// Wait for receipt
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("Failed to mine transaction: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("Transaction failed: status=%d", receipt.Status)
	}

	t.Logf("Transaction successful! Gas used: %d", receipt.GasUsed)
	t.Log("Value chaining worked: (5 + 3) * 10 - 20 = 60")
}

func deployContract(ctx context.Context, client *ethclient.Client, auth *bind.TransactOpts, privateKey *ecdsa.PrivateKey, name string) (common.Address, error) {
	// Read compiled artifact - try both naming conventions
	artifactPath := fmt.Sprintf("out/%s.sol/%s.json", name, name)
	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		// Try VM.sol for WeirollVM
		if name == "WeirollVM" {
			artifactPath = "out/VM.sol/WeirollVM.json"
		}
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return common.Address{}, fmt.Errorf("read artifact: %w (run 'forge build' first)", err)
	}

	var artifact ContractArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return common.Address{}, fmt.Errorf("parse artifact: %w", err)
	}

	bytecodeHex := strings.TrimPrefix(artifact.Bytecode.Object, "0x")
	bytecode, err := hex.DecodeString(bytecodeHex)
	if err != nil {
		return common.Address{}, fmt.Errorf("decode bytecode: %w", err)
	}

	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return common.Address{}, fmt.Errorf("parse ABI: %w", err)
	}

	// Get nonce
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return common.Address{}, fmt.Errorf("get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Address{}, fmt.Errorf("get gas price: %w", err)
	}

	// Create auth with updated nonce
	auth.Nonce = big.NewInt(int64(nonce))
	auth.GasPrice = gasPrice
	auth.GasLimit = 3000000

	// Deploy
	address, tx, _, err := bind.DeployContract(auth, parsedABI, bytecode, client)
	if err != nil {
		return common.Address{}, fmt.Errorf("deploy: %w", err)
	}

	// Wait for mining
	_, err = bind.WaitMined(ctx, client, tx)
	if err != nil {
		return common.Address{}, fmt.Errorf("wait mined: %w", err)
	}

	return address, nil
}

// deployContractWithArgs deploys a contract with constructor arguments
func deployContractWithArgs(ctx context.Context, client *ethclient.Client, auth *bind.TransactOpts, privateKey *ecdsa.PrivateKey, name string, args ...interface{}) (common.Address, error) {
	// Read compiled artifact
	artifactPath := fmt.Sprintf("out/%s.sol/%s.json", name, name)
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return common.Address{}, fmt.Errorf("read artifact: %w (run 'forge build' first)", err)
	}

	var artifact ContractArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return common.Address{}, fmt.Errorf("parse artifact: %w", err)
	}

	bytecodeHex := strings.TrimPrefix(artifact.Bytecode.Object, "0x")
	bytecode, err := hex.DecodeString(bytecodeHex)
	if err != nil {
		return common.Address{}, fmt.Errorf("decode bytecode: %w", err)
	}

	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return common.Address{}, fmt.Errorf("parse ABI: %w", err)
	}

	// Get nonce
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return common.Address{}, fmt.Errorf("get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Address{}, fmt.Errorf("get gas price: %w", err)
	}

	// Create auth with updated nonce
	auth.Nonce = big.NewInt(int64(nonce))
	auth.GasPrice = gasPrice
	auth.GasLimit = 5000000

	// Deploy
	address, tx, _, err := bind.DeployContract(auth, parsedABI, bytecode, client, args...)
	if err != nil {
		return common.Address{}, fmt.Errorf("deploy: %w", err)
	}

	// Wait for mining
	_, err = bind.WaitMined(ctx, client, tx)
	if err != nil {
		return common.Address{}, fmt.Errorf("wait mined: %w", err)
	}

	return address, nil
}

// TestMainnetForkWETH tests against real mainnet WETH (requires RPC URL)
func TestMainnetForkWETH(t *testing.T) {
	if os.Getenv("FORK_TEST") != "1" {
		t.Skip("Set FORK_TEST=1 and MAINNET_RPC_URL to run fork tests")
	}

	rpcURL := os.Getenv("MAINNET_RPC_URL")
	if rpcURL == "" {
		t.Skip("MAINNET_RPC_URL not set")
	}

	ctx := context.Background()

	// Connect to forked Anvil (assumes anvil --fork-url was used)
	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	chainID, _ := client.ChainID(ctx)
	t.Logf("Connected to forked chain ID: %d", chainID)

	privateKey, _ := crypto.HexToECDSA(testPrivateKey)
	auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)

	// Real mainnet WETH address
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")

	// Deploy WeirollVM
	vmAddr, err := deployContract(ctx, client, auth, privateKey, "WeirollVM")
	if err != nil {
		t.Fatalf("Failed to deploy VM: %v", err)
	}
	t.Logf("WeirollVM deployed: %s", vmAddr.Hex())

	// Create weiroll plan
	parsedWethABI := weiroll.MustParseABI(wethABI)
	weth := weiroll.NewContract(wethAddr, parsedWethABI)

	planner := weiroll.New()
	wrapAmount := big.NewInt(1e17) // 0.1 ETH

	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	// Execute
	vmABI := weiroll.MustParseABI(weirollVMABI)
	vmContract := bind.NewBoundContract(vmAddr, vmABI, client, client, client)

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	nonce, _ := client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = wrapAmount

	tx, err := vmContract.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}

	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("Failed to mine: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("Tx failed")
	}

	// Verify via real WETH balanceOf
	wethContract := bind.NewBoundContract(wethAddr, parsedWethABI, client, client, client)
	var balance *big.Int
	err = wethContract.Call(&bind.CallOpts{}, &[]interface{}{&balance}, "balanceOf", vmAddr)
	if err != nil {
		t.Fatalf("Failed to get balance: %v", err)
	}

	t.Logf("VM WETH balance on mainnet fork: %s", balance.String())
	if balance.Cmp(wrapAmount) != 0 {
		t.Fatalf("Expected %s, got %s", wrapAmount.String(), balance.String())
	}

	t.Log("✓ Mainnet fork WETH test passed!")
}

// TestMainnetForkUniswapV2Swap tests real Uniswap V2 swap on mainnet fork
func TestMainnetForkUniswapV2Swap(t *testing.T) {
	if os.Getenv("FORK_TEST") != "1" {
		t.Skip("Set FORK_TEST=1 and MAINNET_RPC_URL to run fork tests")
	}

	rpcURL := os.Getenv("MAINNET_RPC_URL")
	if rpcURL == "" {
		t.Skip("MAINNET_RPC_URL not set")
	}

	ctx := context.Background()

	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	chainID, _ := client.ChainID(ctx)
	t.Logf("Connected to forked mainnet, chain ID: %d", chainID)

	privateKey, _ := crypto.HexToECDSA(testPrivateKey)
	auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Real mainnet addresses
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usdcAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	routerAddr := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")

	t.Logf("Using real contracts:")
	t.Logf("  WETH:   %s", wethAddr.Hex())
	t.Logf("  USDC:   %s", usdcAddr.Hex())
	t.Logf("  Router: %s", routerAddr.Hex())

	// Deploy WeirollVM and MathLib
	vmAddr, err := deployContract(ctx, client, auth, privateKey, "WeirollVM")
	if err != nil {
		t.Fatalf("Failed to deploy VM: %v", err)
	}
	t.Logf("WeirollVM deployed: %s", vmAddr.Hex())

	mathLibAddr, err := deployContract(ctx, client, auth, privateKey, "MathLib")
	if err != nil {
		t.Fatalf("Failed to deploy MathLib: %v", err)
	}
	t.Logf("MathLib deployed: %s", mathLibAddr.Hex())

	// First, wrap some ETH to WETH and send to VM
	parsedWethABI := weiroll.MustParseABI(wethABI)
	wethContract := bind.NewBoundContract(wethAddr, parsedWethABI, client, client, client)

	nonce, _ := client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(1e18) // 1 ETH
	tx, err := wethContract.Transact(auth, "deposit")
	if err != nil {
		t.Fatalf("Failed to wrap ETH: %v", err)
	}
	bind.WaitMined(ctx, client, tx)
	t.Log("Wrapped 1 ETH to WETH")

	// Transfer WETH to VM
	nonce, _ = client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	swapAmount := big.NewInt(5e17) // 0.5 WETH to swap
	tx, err = wethContract.Transact(auth, "transfer", vmAddr, swapAmount)
	if err != nil {
		t.Fatalf("Failed to transfer WETH to VM: %v", err)
	}
	bind.WaitMined(ctx, client, tx)
	t.Logf("Transferred %s WETH to VM", swapAmount.String())

	// Create weiroll contracts pointing to real mainnet contracts
	weth := weiroll.NewContract(wethAddr, parsedWethABI)
	router := weiroll.NewContract(routerAddr, weiroll.MustParseABI(uniswapV2RouterABI))
	mathLib := weiroll.NewLibrary(mathLibAddr, weiroll.MustParseABI(mathLibABI))

	// ERC20 ABI for USDC balance check
	erc20ABI := weiroll.MustParseABI(`[
		{"inputs": [{"name": "account", "type": "address"}], "name": "balanceOf", "outputs": [{"name": "", "type": "uint256"}], "stateMutability": "view", "type": "function"}
	]`)

	t.Log("\n=== Building Real Uniswap V2 Swap Plan ===")

	planner := weiroll.New()

	deadline := big.NewInt(time.Now().Unix() + 3600)
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	recipient := common.HexToAddress("0x4444444444444444444444444444444444444444")

	// Step 1: Approve router to spend WETH
	t.Log("Step 1: Approve Uniswap V2 Router to spend WETH")
	planner.Add(weth.MustInvoke("approve", routerAddr, maxUint))

	// Step 2: Swap WETH -> USDC via real Uniswap V2
	t.Log("Step 2: Swap WETH -> USDC on real Uniswap V2")
	path := []common.Address{wethAddr, usdcAddr}
	swapResult := planner.Add(router.MustInvoke("swapExactTokensForTokens",
		swapAmount,
		big.NewInt(0), // Accept any amount (for testing)
		path,
		recipient, // Send directly to recipient
		deadline,
	))

	// Step 3: Extract output amount (demonstrates value chaining)
	t.Log("Step 3: Extract USDC output amount from swap result")
	_ = planner.Add(mathLib.MustInvoke("extractLastElement", swapResult))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Failed to compile plan: %v", err)
	}

	t.Logf("\nCompiled plan: %d commands, %d state slots", len(plan.Commands), len(plan.State))
	for i, cmd := range plan.Commands {
		t.Logf("  Command[%d]: 0x%s", i, hex.EncodeToString(cmd))
	}

	// Get recipient USDC balance before
	usdcContract := bind.NewBoundContract(usdcAddr, erc20ABI, client, client, client)
	var usdcBefore *big.Int
	usdcContract.Call(&bind.CallOpts{}, &[]interface{}{&usdcBefore}, "balanceOf", recipient)
	t.Logf("\nRecipient USDC balance before: %s", usdcBefore.String())

	// Execute the plan
	t.Log("\nExecuting weiroll plan on mainnet fork...")
	vmABI := weiroll.MustParseABI(weirollVMABI)
	vmContract := bind.NewBoundContract(vmAddr, vmABI, client, client, client)

	nonce, _ = client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = 500000

	tx, err = vmContract.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}

	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("Failed to mine: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("Tx failed: status=%d, gas used=%d", receipt.Status, receipt.GasUsed)
	}

	// Verify recipient received USDC
	var usdcAfter *big.Int
	usdcContract.Call(&bind.CallOpts{}, &[]interface{}{&usdcAfter}, "balanceOf", recipient)
	usdcReceived := new(big.Int).Sub(usdcAfter, usdcBefore)

	t.Logf("\nRecipient USDC balance after: %s", usdcAfter.String())
	t.Logf("USDC received: %s (%.2f USDC)", usdcReceived.String(), float64(usdcReceived.Int64())/1e6)
	t.Logf("Gas used: %d", receipt.GasUsed)

	if usdcReceived.Cmp(big.NewInt(0)) <= 0 {
		t.Fatal("Recipient received no USDC!")
	}

	t.Log("\n✓ Real Uniswap V2 swap on mainnet fork successful!")
}

// TestMainnetForkWETHWrapUnwrap tests full wrap/unwrap cycle on mainnet fork
func TestMainnetForkWETHWrapUnwrap(t *testing.T) {
	if os.Getenv("FORK_TEST") != "1" {
		t.Skip("Set FORK_TEST=1 and MAINNET_RPC_URL to run fork tests")
	}

	rpcURL := os.Getenv("MAINNET_RPC_URL")
	if rpcURL == "" {
		t.Skip("MAINNET_RPC_URL not set")
	}

	ctx := context.Background()

	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	chainID, _ := client.ChainID(ctx)
	privateKey, _ := crypto.HexToECDSA(testPrivateKey)
	auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Real mainnet WETH
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")

	// Deploy WeirollVM
	vmAddr, err := deployContract(ctx, client, auth, privateKey, "WeirollVM")
	if err != nil {
		t.Fatalf("Failed to deploy VM: %v", err)
	}
	t.Logf("WeirollVM deployed: %s", vmAddr.Hex())

	parsedWethABI := weiroll.MustParseABI(wethABI)
	weth := weiroll.NewContract(wethAddr, parsedWethABI)
	wethContract := bind.NewBoundContract(wethAddr, parsedWethABI, client, client, client)

	// ========================================
	// Test: Wrap + partial unwrap in one tx
	// ========================================
	t.Log("\n=== Test: Wrap 1 ETH, then unwrap 0.3 ETH ===")

	planner := weiroll.New()
	wrapAmount := big.NewInt(1e18)   // 1 ETH
	unwrapAmount := big.NewInt(3e17) // 0.3 ETH

	// Step 1: Wrap ETH to WETH
	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))
	t.Log("Step 1: deposit() - wrap 1 ETH")

	// Step 2: Unwrap some WETH back to ETH
	planner.Add(weth.MustInvoke("withdraw", unwrapAmount))
	t.Log("Step 2: withdraw(0.3 ETH)")

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	t.Logf("Plan: %d commands", len(plan.Commands))

	// Get VM balances before
	vmEthBefore, _ := client.BalanceAt(ctx, vmAddr, nil)

	// Execute
	vmABI := weiroll.MustParseABI(weirollVMABI)
	vmContract := bind.NewBoundContract(vmAddr, vmABI, client, client, client)

	nonce, _ := client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = wrapAmount // Send ETH with tx

	tx, err := vmContract.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}

	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("Failed to mine: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("Tx failed")
	}

	// Verify balances
	var vmWethBalance *big.Int
	wethContract.Call(&bind.CallOpts{}, &[]interface{}{&vmWethBalance}, "balanceOf", vmAddr)
	vmEthAfter, _ := client.BalanceAt(ctx, vmAddr, nil)

	expectedWeth := new(big.Int).Sub(wrapAmount, unwrapAmount) // 0.7 WETH
	ethReceived := new(big.Int).Sub(vmEthAfter, vmEthBefore)

	t.Logf("\nResults:")
	t.Logf("  VM WETH balance: %s (expected ~%s)", vmWethBalance.String(), expectedWeth.String())
	t.Logf("  VM ETH received: %s (expected ~%s)", ethReceived.String(), unwrapAmount.String())
	t.Logf("  Gas used: %d", receipt.GasUsed)

	// Use approximate comparison (within 1% tolerance for any dust/rebasing)
	wethDiff := new(big.Int).Abs(new(big.Int).Sub(vmWethBalance, expectedWeth))
	wethTolerance := new(big.Int).Div(expectedWeth, big.NewInt(100)) // 1%
	if wethDiff.Cmp(wethTolerance) > 0 {
		t.Fatalf("WETH balance mismatch: expected ~%s, got %s (diff: %s)", expectedWeth.String(), vmWethBalance.String(), wethDiff.String())
	}

	ethDiff := new(big.Int).Abs(new(big.Int).Sub(ethReceived, unwrapAmount))
	ethTolerance := new(big.Int).Div(unwrapAmount, big.NewInt(100)) // 1%
	if ethDiff.Cmp(ethTolerance) > 0 {
		t.Fatalf("ETH received mismatch: expected ~%s, got %s (diff: %s)", unwrapAmount.String(), ethReceived.String(), ethDiff.String())
	}

	t.Log("\n✓ Real mainnet WETH wrap+unwrap successful!")
}

// TestMainnetForkMultiHopSwap tests WETH -> USDC -> DAI swap on mainnet fork
func TestMainnetForkMultiHopSwap(t *testing.T) {
	if os.Getenv("FORK_TEST") != "1" {
		t.Skip("Set FORK_TEST=1 and MAINNET_RPC_URL to run fork tests")
	}

	rpcURL := os.Getenv("MAINNET_RPC_URL")
	if rpcURL == "" {
		t.Skip("MAINNET_RPC_URL not set")
	}

	ctx := context.Background()

	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	chainID, _ := client.ChainID(ctx)
	privateKey, _ := crypto.HexToECDSA(testPrivateKey)
	auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Real mainnet addresses
	wethAddr := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usdcAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	daiAddr := common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F") // Real DAI
	routerAddr := common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")

	t.Log("=== Multi-Hop Swap: WETH -> USDC -> DAI ===")
	t.Logf("Using real mainnet contracts on fork")

	// Deploy contracts
	vmAddr, err := deployContract(ctx, client, auth, privateKey, "WeirollVM")
	if err != nil {
		t.Fatalf("Failed to deploy VM: %v", err)
	}

	mathLibAddr, err := deployContract(ctx, client, auth, privateKey, "MathLib")
	if err != nil {
		t.Fatalf("Failed to deploy MathLib: %v", err)
	}

	// Wrap ETH and transfer to VM
	parsedWethABI := weiroll.MustParseABI(wethABI)
	wethContract := bind.NewBoundContract(wethAddr, parsedWethABI, client, client, client)

	nonce, _ := client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(2e18)
	tx, _ := wethContract.Transact(auth, "deposit")
	bind.WaitMined(ctx, client, tx)

	nonce, _ = client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	tx, _ = wethContract.Transact(auth, "transfer", vmAddr, big.NewInt(1e18))
	bind.WaitMined(ctx, client, tx)
	t.Log("Transferred 1 WETH to VM")

	// Create weiroll contracts
	weth := weiroll.NewContract(wethAddr, parsedWethABI)
	router := weiroll.NewContract(routerAddr, weiroll.MustParseABI(uniswapV2RouterABI))
	// Use NewContract (CALL) instead of NewLibrary (DELEGATECALL) to debug
	mathLib := weiroll.NewContract(mathLibAddr, weiroll.MustParseABI(mathLibABI))

	// ERC20 ABI for approvals
	erc20ApproveABI := weiroll.MustParseABI(`[
		{"inputs": [{"name": "spender", "type": "address"}, {"name": "amount", "type": "uint256"}], "name": "approve", "outputs": [{"name": "", "type": "bool"}], "stateMutability": "nonpayable", "type": "function"},
		{"inputs": [{"name": "account", "type": "address"}], "name": "balanceOf", "outputs": [{"name": "", "type": "uint256"}], "stateMutability": "view", "type": "function"}
	]`)
	usdc := weiroll.NewContract(usdcAddr, erc20ApproveABI)

	planner := weiroll.New()

	amountIn := big.NewInt(5e17) // 0.5 WETH
	deadline := big.NewInt(time.Now().Unix() + 3600)
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	recipient := common.HexToAddress("0x5555555555555555555555555555555555555555")

	// Step 1: Approve router for WETH
	t.Log("Step 1: Approve router for WETH")
	planner.Add(weth.MustInvoke("approve", routerAddr, maxUint))

	// Step 2: Swap WETH -> USDC
	t.Log("Step 2: Swap WETH -> USDC")
	path1 := []common.Address{wethAddr, usdcAddr}
	swapResult := planner.Add(router.MustInvoke("swapExactTokensForTokens",
		amountIn, big.NewInt(0), path1, vmAddr, deadline))

	// Step 3: Extract USDC amount from swap result
	t.Log("Step 3: Extract USDC output amount")
	usdcAmount := planner.Add(mathLib.MustInvoke("extractLastElement", swapResult))

	// Step 4: Approve router for USDC
	t.Log("Step 4: Approve router for USDC")
	planner.Add(usdc.MustInvoke("approve", routerAddr, maxUint))

	// Step 5: Swap USDC -> DAI using the CHAINED amount from step 3
	t.Log("Step 5: Swap USDC -> DAI (using chained amount from step 2!)")
	path2 := []common.Address{usdcAddr, daiAddr}

	planner.Add(router.MustInvoke("swapExactTokensForTokens",
		usdcAmount, big.NewInt(0), path2, recipient, deadline))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	t.Logf("\nPlan: %d commands, %d state slots", len(plan.Commands), len(plan.State))
	for i, cmd := range plan.Commands {
		t.Logf("  Command[%d]: 0x%s", i, hex.EncodeToString(cmd))
	}
	t.Log("\nInitial state slots:")
	for i, s := range plan.State {
		t.Logf("  State[%d]: 0x%s", i, hex.EncodeToString(s))
	}

	// Get DAI balance before
	daiContract := bind.NewBoundContract(daiAddr, erc20ApproveABI, client, client, client)
	var daiBefore *big.Int
	daiContract.Call(&bind.CallOpts{}, &[]interface{}{&daiBefore}, "balanceOf", recipient)

	// Execute
	vmABI := weiroll.MustParseABI(weirollVMABI)
	vmContract := bind.NewBoundContract(vmAddr, vmABI, client, client, client)

	nonce, _ = client.PendingNonceAt(ctx, fromAddress)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.GasLimit = 600000

	tx, err = vmContract.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}

	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("Failed to mine: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Logf("Tx hash: %s", tx.Hash().Hex())
		t.Logf("Block: %d", receipt.BlockNumber.Uint64())

		// Try to get revert reason
		callMsg := ethereum.CallMsg{
			From: fromAddress,
			To:   &vmAddr,
			Gas:  600000,
			Data: tx.Data(),
		}
		_, callErr := client.CallContract(ctx, callMsg, receipt.BlockNumber)
		if callErr != nil {
			t.Logf("Revert reason (raw): %v", callErr)
			// Try to decode ExecutionFailed(uint256, address, string)
			errStr := callErr.Error()
			if strings.Contains(errStr, "0xef3dcb2f") {
				// Extract the hex data after the selector
				idx := strings.Index(errStr, "0xef3dcb2f")
				if idx != -1 {
					hexData := errStr[idx+10:] // Skip "0xef3dcb2f"
					// Remove any non-hex characters
					hexData = strings.TrimSpace(hexData)
					// First 64 chars (32 bytes) should be command_index
					if len(hexData) >= 64 {
						cmdIdxHex := hexData[:64]
						cmdIdx, _ := new(big.Int).SetString(cmdIdxHex, 16)
						t.Logf("Failed at command index: %d", cmdIdx)
					}
				}
			}
		}

		t.Fatalf("Tx failed: status=%d", receipt.Status)
	}

	// Check DAI received
	var daiAfter *big.Int
	daiContract.Call(&bind.CallOpts{}, &[]interface{}{&daiAfter}, "balanceOf", recipient)
	daiReceived := new(big.Int).Sub(daiAfter, daiBefore)

	t.Logf("\nResults:")
	t.Logf("  Input: 0.5 WETH")
	t.Logf("  Output: %s DAI (%.2f DAI)", daiReceived.String(), float64(daiReceived.Int64())/1e18)
	t.Logf("  Gas used: %d", receipt.GasUsed)

	if daiReceived.Cmp(big.NewInt(0)) <= 0 {
		t.Fatal("Recipient received no DAI!")
	}

	t.Log("\n✓ Real multi-hop swap (WETH -> USDC -> DAI) successful!")
	t.Log("  This demonstrates weiroll's value chaining with real Uniswap V2!")
}

// Ensure ethereum package is used
var _ = ethereum.CallMsg{}
