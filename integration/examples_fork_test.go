package integration

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Mainnet contracts shared across the example fork tests.
var (
	mainnetWETH         = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	mainnetUSDC         = common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	mainnetUniV2Router  = common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")
	mainnetUniV2WETHUSDC = common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc")
	mainnetAaveV3Pool   = common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2")
	mainnetAUSDC        = common.HexToAddress("0x98C23E9d8f34FEFb1B7BD6a91B7FF122F4e16F5c")
)

const uniV2PairABI = `[
	{
		"name": "getReserves",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [
			{"name": "reserve0",            "type": "uint112"},
			{"name": "reserve1",            "type": "uint112"},
			{"name": "blockTimestampLast",  "type": "uint32"}
		]
	}
]`

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

const aaveV3PoolABI = `[
	{
		"name": "supply",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "asset",        "type": "address"},
			{"name": "amount",       "type": "uint256"},
			{"name": "onBehalfOf",   "type": "address"},
			{"name": "referralCode", "type": "uint16"}
		],
		"outputs": []
	}
]`

const erc20StdABI = `[
	{
		"name": "balanceOf",
		"type": "function",
		"stateMutability": "view",
		"inputs":  [{"name": "account", "type": "address"}],
		"outputs": [{"name": "",        "type": "uint256"}]
	},
	{
		"name": "approve",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "spender", "type": "address"},
			{"name": "amount",  "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	},
	{
		"name": "transfer",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "to",     "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	}
]`

func skipUnlessFork(t *testing.T) (*ethclient.Client, *bind.TransactOpts, common.Address) {
	t.Helper()
	if os.Getenv("FORK_TEST") != "1" {
		t.Skip("Set FORK_TEST=1 and MAINNET_RPC_URL to run fork tests")
	}
	if os.Getenv("MAINNET_RPC_URL") == "" {
		t.Skip("MAINNET_RPC_URL not set")
	}

	ctx := context.Background()
	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		t.Fatalf("Failed to connect to Anvil: %v", err)
	}
	t.Cleanup(client.Close)

	chainID, err := client.ChainID(ctx)
	if err != nil {
		t.Fatalf("ChainID: %v", err)
	}

	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(pk, chainID)
	if err != nil {
		t.Fatalf("NewKeyedTransactorWithChainID: %v", err)
	}
	from := crypto.PubkeyToAddress(pk.PublicKey)
	return client, auth, from
}

func setNonceAndGas(t *testing.T, ctx context.Context, client *ethclient.Client, auth *bind.TransactOpts, from common.Address, gasLimit uint64) {
	t.Helper()
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		t.Fatalf("PendingNonceAt: %v", err)
	}
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = gasLimit
}

// TestForkCase4_TupleReturnRoundTrip is the on-chain validation of the
// RawReturn -> TupleHelper.extract -> ReturnValue.As pattern.
//
// It would have caught the producer-slot encoding bug: if the producer
// return-slot byte has the dynamic flag set, writeTuple's `state[idx]`
// (unmasked) goes out of bounds and execute() reverts.
//
// The plan reads UniswapV2 WETH/USDC pair reserves via getReserves
// (which returns a real (uint112,uint112,uint32) tuple), extracts
// reserve0 and reserve1 as bytes32 words, then casts to uint256 and
// uses each as input to MathLib.add to produce identity outputs whose
// final state slots are observable. We compare those slots against
// reserve0/reserve1 fetched from a direct getReserves call.
func TestForkCase4_TupleReturnRoundTrip(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	_ = pk
	ctx := context.Background()

	// Deploy: VM, TupleHelper, MathLib.
	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy WeirollVM: %v", err)
	}
	t.Logf("WeirollVM:    %s", vmAddr.Hex())

	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	helperAddr, err := deployContract(ctx, client, auth, pk, "TupleHelper")
	if err != nil {
		t.Fatalf("deploy TupleHelper: %v", err)
	}
	t.Logf("TupleHelper:  %s", helperAddr.Hex())

	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	mathAddr, err := deployContract(ctx, client, auth, pk, "MathLib")
	if err != nil {
		t.Fatalf("deploy MathLib: %v", err)
	}
	t.Logf("MathLib:      %s", mathAddr.Hex())
	t.Logf("UniV2 pair:   %s", mainnetUniV2WETHUSDC.Hex())

	// Independent ground truth: directly call getReserves on the pair.
	pairABIParsed := weiroll.MustParseABI(uniV2PairABI)
	pairBound := bind.NewBoundContract(mainnetUniV2WETHUSDC, pairABIParsed, client, client, client)
	var truthOut []interface{} = []interface{}{new(*big.Int), new(*big.Int), new(uint32)}
	if err := pairBound.Call(&bind.CallOpts{Context: ctx}, &truthOut, "getReserves"); err != nil {
		t.Fatalf("direct getReserves: %v", err)
	}
	wantReserve0 := *truthOut[0].(**big.Int)
	wantReserve1 := *truthOut[1].(**big.Int)
	t.Logf("direct reserve0: %s", wantReserve0.String())
	t.Logf("direct reserve1: %s", wantReserve1.String())

	// Build the weiroll plan using the contract types we want to test.
	pair := weiroll.NewContract(mainnetUniV2WETHUSDC, pairABIParsed, weiroll.WithStaticCalls())
	helper := weiroll.NewContract(helperAddr, weiroll.MustParseABI(tupleHelperABI), weiroll.WithStaticCalls())
	math := weiroll.NewLibrary(mathAddr, weiroll.MustParseABI(mathLibABI))

	planner := weiroll.New()

	// Step 1: getReserves with .RawReturn(). Slot holds the entire
	// 96-byte returndata as length-prefixed bytes.
	rawReserves := planner.Add(pair.MustInvoke("getReserves").RawReturn())
	if rawReserves.Type().String() != "bytes" {
		t.Fatalf("rawReserves type want bytes, got %s", rawReserves.Type().String())
	}

	// Step 2: extract slot 0 (reserve0) and slot 1 (reserve1) as bytes32.
	r0B32 := planner.Add(helper.MustInvoke("extract", rawReserves, big.NewInt(0)))
	r1B32 := planner.Add(helper.MustInvoke("extract", rawReserves, big.NewInt(1)))

	// Step 3: re-type the bytes32 values as uint256 OFF-CHAIN, then
	// pipe through MathLib.add(x, 0) — a deliberate identity that
	// forces the planner to allocate a slot for the cast value (since
	// it's used as an input). The slot then survives in the final
	// state array, observable via eth_call.
	r0AsU256 := r0B32.MustAsType("uint256")
	r1AsU256 := r1B32.MustAsType("uint256")
	zero := big.NewInt(0)
	planner.Add(math.MustInvoke("add", r0AsU256, zero))
	planner.Add(math.MustInvoke("add", r1AsU256, zero))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	t.Logf("plan: %d commands, %d state slots", len(plan.Commands), len(plan.State))

	// Locate the slots that hold reserve0 and reserve1. The extract
	// commands' return slots are exactly where the bytes32 values
	// land in state.
	_, _, _, r0Slot, _, _ := weiroll.DecodeCommand(plan.Commands[1])
	_, _, _, r1Slot, _, _ := weiroll.DecodeCommand(plan.Commands[2])
	if r0Slot == weiroll.NoReturnSlot {
		t.Fatal("reserve0 extract slot was not allocated; check visibility")
	}
	if r1Slot == weiroll.NoReturnSlot {
		t.Fatal("reserve1 extract slot was not allocated; check visibility")
	}
	r0Slot &^= weiroll.DynamicSlotFlag
	r1Slot &^= weiroll.DynamicSlotFlag
	t.Logf("reserve0 slot: %d", r0Slot)
	t.Logf("reserve1 slot: %d", r1Slot)

	// Producer-slot regression check: the getReserves command (slot
	// byte at command index 0, byte 11) must be CLEAN (no 0x80). If
	// the encoder regresses and sets the flag, writeTuple panics
	// with out-of-bounds indexing and the eth_call below reverts.
	_, _, _, getReservesProdSlot, _, _ := weiroll.DecodeCommand(plan.Commands[0])
	if getReservesProdSlot&weiroll.DynamicSlotFlag != 0 {
		t.Fatalf("getReserves return-slot byte must be clean for tuple-return; got 0x%02x", getReservesProdSlot)
	}

	// Execute via eth_call (everything in the plan is static).
	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)

	var execOut []interface{} = []interface{}{new([][]byte)}
	if err := vmBound.Call(
		&bind.CallOpts{Context: ctx},
		&execOut,
		"execute",
		plan.CommandsAsBytes32(),
		plan.StateAsBytes(),
	); err != nil {
		t.Fatalf("VM.execute reverted: %v", err)
	}
	finalState := *execOut[0].(*[][]byte)
	t.Logf("final state slots: %d", len(finalState))

	if int(r0Slot) >= len(finalState) || int(r1Slot) >= len(finalState) {
		t.Fatalf("slot index out of range: r0=%d r1=%d slots=%d", r0Slot, r1Slot, len(finalState))
	}

	gotR0 := new(big.Int).SetBytes(finalState[r0Slot])
	gotR1 := new(big.Int).SetBytes(finalState[r1Slot])
	t.Logf("plan  reserve0:  %s", gotR0.String())
	t.Logf("plan  reserve1:  %s", gotR1.String())

	if gotR0.Cmp(wantReserve0) != 0 {
		t.Errorf("reserve0 mismatch: got %s, want %s", gotR0.String(), wantReserve0.String())
	}
	if gotR1.Cmp(wantReserve1) != 0 {
		t.Errorf("reserve1 mismatch: got %s, want %s", gotR1.String(), wantReserve1.String())
	}
}

// TestForkCase1_BalanceOfTransfer is a fork-execution variant of
// examples/01_simple_return: read balanceOf, then transfer the exact
// same amount in one weiroll tx.
//
// Verifies the simple "function returns the value you want" pipe.
func TestForkCase1_BalanceOfTransfer(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	ctx := context.Background()

	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy VM: %v", err)
	}
	t.Logf("WeirollVM: %s", vmAddr.Hex())

	// Wrap 0.1 ETH and transfer it to the VM.
	wethABIParsed := weiroll.MustParseABI(wethABI)
	wethBound := bind.NewBoundContract(mainnetWETH, wethABIParsed, client, client, client)
	depositAmount := big.NewInt(1e17)

	setNonceAndGas(t, ctx, client, auth, from, 200_000)
	auth.Value = depositAmount
	tx, err := wethBound.Transact(auth, "deposit")
	if err != nil {
		t.Fatalf("deposit: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		t.Fatalf("WaitMined deposit: %v", err)
	}

	setNonceAndGas(t, ctx, client, auth, from, 200_000)
	tx, err = wethBound.Transact(auth, "transfer", vmAddr, depositAmount)
	if err != nil {
		t.Fatalf("transfer to VM: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		t.Fatalf("WaitMined transfer: %v", err)
	}

	// Plan: read VM's WETH balance, transfer that exact amount to a
	// recipient. No literal amount needed downstream.
	weth := weiroll.NewContract(mainnetWETH, wethABIParsed)
	recipient := common.HexToAddress("0xCAFE000000000000000000000000000000000001")

	planner := weiroll.New()
	balance := planner.Add(weth.MustInvoke("balanceOf", vmAddr).Static())
	planner.Add(weth.MustInvoke("transfer", recipient, balance))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Execute on chain.
	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)

	setNonceAndGas(t, ctx, client, auth, from, 500_000)
	tx, err = vmBound.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("VM.execute submit: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("WaitMined execute: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("execute reverted: status=%d gas=%d", receipt.Status, receipt.GasUsed)
	}

	// Verify recipient received exactly depositAmount.
	var recipBal *big.Int
	if err := wethBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&recipBal}, "balanceOf", recipient); err != nil {
		t.Fatalf("balanceOf recipient: %v", err)
	}
	if recipBal.Cmp(depositAmount) != 0 {
		t.Errorf("recipient balance: got %s, want %s", recipBal, depositAmount)
	}

	// VM should be drained.
	var vmBal *big.Int
	if err := wethBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&vmBal}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("balanceOf vm: %v", err)
	}
	if vmBal.Sign() != 0 {
		t.Errorf("VM should be drained; got %s", vmBal)
	}
}

// TestForkCase6_SelfBalancePattern: wrap + swap + balanceOf(self) +
// approve + Aave supply, all in one weiroll plan, on a per-user-proxy
// VM. Validates the full self-balance recipe and confirms the Aave
// supply succeeds with the dynamically-piped USDC amount.
func TestForkCase6_SelfBalancePattern(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	ctx := context.Background()

	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy VM: %v", err)
	}
	t.Logf("WeirollVM: %s", vmAddr.Hex())

	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	mathAddr, err := deployContract(ctx, client, auth, pk, "MathLib")
	if err != nil {
		t.Fatalf("deploy MathLib: %v", err)
	}

	// Plan: wrap 0.1 ETH, approve router, swap WETH->USDC to VM,
	// extract last hop, approve Aave, supply onBehalf of `from`.
	weth := weiroll.NewContract(mainnetWETH, weiroll.MustParseABI(wethABI))
	usdc := weiroll.NewContract(mainnetUSDC, weiroll.MustParseABI(erc20StdABI))
	router := weiroll.NewContract(mainnetUniV2Router, weiroll.MustParseABI(uniswapV2RouterABI))
	math := weiroll.NewLibrary(mathAddr, weiroll.MustParseABI(mathLibABI))
	aave := weiroll.NewContract(mainnetAaveV3Pool, weiroll.MustParseABI(aaveV3PoolABI))

	wrapAmount := big.NewInt(1e17) // 0.1 ETH
	deadline := big.NewInt(time.Now().Unix() + 3600)
	maxApprove := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	planner := weiroll.New()
	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))
	planner.Add(weth.MustInvoke("approve", mainnetUniV2Router, maxApprove))
	swapResult := planner.Add(router.MustInvoke(
		"swapExactTokensForTokens",
		wrapAmount,
		big.NewInt(0),
		[]common.Address{mainnetWETH, mainnetUSDC},
		vmAddr, // tokens land at the VM
		deadline,
	))

	// Prefer the swap's own returned amount over balanceOf(self) —
	// see the example for the rationale. We still also do a
	// balanceOf as a sanity probe but don't use it in the supply.
	usdcOut := planner.Add(math.MustInvoke("extractLastElement", swapResult))
	usdcBalProbe := planner.Add(usdc.MustInvoke("balanceOf", vmAddr).Static())
	_ = usdcBalProbe

	planner.Add(usdc.MustInvoke("approve", mainnetAaveV3Pool, maxApprove))
	planner.Add(aave.MustInvoke("supply", mainnetUSDC, usdcOut, from, uint16(0)))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	t.Logf("plan: %d commands, %d state slots", len(plan.Commands), len(plan.State))

	// Pre-balance check on aUSDC for `from`.
	aTokenABI := weiroll.MustParseABI(erc20StdABI)
	aToken := bind.NewBoundContract(mainnetAUSDC, aTokenABI, client, client, client)
	var aBefore *big.Int
	if err := aToken.Call(&bind.CallOpts{Context: ctx}, &[]interface{}{&aBefore}, "balanceOf", from); err != nil {
		t.Fatalf("aToken balanceOf before: %v", err)
	}
	t.Logf("aUSDC before: %s", aBefore)

	// Execute on chain.
	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)

	setNonceAndGas(t, ctx, client, auth, from, 1_500_000)
	auth.Value = wrapAmount
	tx, err := vmBound.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("VM.execute submit: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("WaitMined execute: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("execute reverted: status=%d gas=%d", receipt.Status, receipt.GasUsed)
	}
	t.Logf("execute gas used: %d", receipt.GasUsed)

	var aAfter *big.Int
	if err := aToken.Call(&bind.CallOpts{Context: ctx}, &[]interface{}{&aAfter}, "balanceOf", from); err != nil {
		t.Fatalf("aToken balanceOf after: %v", err)
	}
	delta := new(big.Int).Sub(aAfter, aBefore)
	t.Logf("aUSDC after:  %s (+%s)", aAfter, delta)
	if delta.Sign() <= 0 {
		t.Errorf("expected aUSDC delta > 0, got %s", delta)
	}
}

// TestForkCase3_AaveSupplyAndATokenRead: wrap + swap to USDC at VM,
// approve + supply to Aave on behalf of VM, then read aUSDC.balanceOf(VM)
// and pipe to a downstream consumer. Mirrors examples/03 but with the
// per-user-proxy assumption (VM is empty before this flow).
func TestForkCase3_AaveSupplyAndATokenRead(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	ctx := context.Background()

	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy VM: %v", err)
	}
	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	mathAddr, err := deployContract(ctx, client, auth, pk, "MathLib")
	if err != nil {
		t.Fatalf("deploy MathLib: %v", err)
	}

	// Pre-flight: confirm VM has zero aUSDC (the per-user-proxy
	// assumption that makes the post-supply balanceOf read meaningful).
	aTokenBound := bind.NewBoundContract(mainnetAUSDC, weiroll.MustParseABI(erc20StdABI), client, client, client)
	var aBefore *big.Int
	if err := aTokenBound.Call(&bind.CallOpts{Context: ctx}, &[]interface{}{&aBefore}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("aUSDC pre-balance: %v", err)
	}
	if aBefore.Sign() != 0 {
		t.Fatalf("VM expected to start with zero aUSDC; got %s", aBefore)
	}

	// Build the recipe: wrap, swap to VM, approve Aave, supply onBehalf=VM,
	// then read aUSDC.balanceOf(VM) and require it's positive (we pipe
	// it into a no-op identity to force slot allocation).
	weth := weiroll.NewContract(mainnetWETH, weiroll.MustParseABI(wethABI))
	usdc := weiroll.NewContract(mainnetUSDC, weiroll.MustParseABI(erc20StdABI))
	aToken := weiroll.NewContract(mainnetAUSDC, weiroll.MustParseABI(erc20StdABI))
	router := weiroll.NewContract(mainnetUniV2Router, weiroll.MustParseABI(uniswapV2RouterABI))
	aave := weiroll.NewContract(mainnetAaveV3Pool, weiroll.MustParseABI(aaveV3PoolABI))
	math := weiroll.NewLibrary(mathAddr, weiroll.MustParseABI(mathLibABI))

	wrapAmount := big.NewInt(1e17) // 0.1 ETH
	maxApprove := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	deadline := big.NewInt(time.Now().Unix() + 3600)

	planner := weiroll.New()
	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))
	planner.Add(weth.MustInvoke("approve", mainnetUniV2Router, maxApprove))
	swapResult := planner.Add(router.MustInvoke(
		"swapExactTokensForTokens",
		wrapAmount, big.NewInt(0),
		[]common.Address{mainnetWETH, mainnetUSDC},
		vmAddr, deadline,
	))
	usdcOut := planner.Add(math.MustInvoke("extractLastElement", swapResult))
	planner.Add(usdc.MustInvoke("approve", mainnetAaveV3Pool, maxApprove))
	planner.Add(aave.MustInvoke("supply", mainnetUSDC, usdcOut, vmAddr, uint16(0)))

	// Now the recovery read: aUSDC.balanceOf(VM). Force a slot
	// allocation by piping into MathLib.add(x, 0).
	aBal := planner.Add(aToken.MustInvoke("balanceOf", vmAddr).Static())
	planner.Add(math.MustInvoke("add", aBal, big.NewInt(0)))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)

	setNonceAndGas(t, ctx, client, auth, from, 1_500_000)
	auth.Value = wrapAmount
	tx, err := vmBound.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("VM.execute submit: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("WaitMined execute: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("execute reverted: status=%d gas=%d", receipt.Status, receipt.GasUsed)
	}

	// Confirm aUSDC.balanceOf(VM) post-supply matches what the recipe
	// would have piped downstream. (Aave V3 aTokens report in
	// underlying units, not share units — that's intentional.)
	var aAfter *big.Int
	if err := aTokenBound.Call(&bind.CallOpts{Context: ctx}, &[]interface{}{&aAfter}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("aUSDC post-balance: %v", err)
	}
	t.Logf("aUSDC after supply: %s", aAfter)
	if aAfter.Sign() <= 0 {
		t.Fatalf("expected aUSDC > 0 after supply, got %s", aAfter)
	}

	// Sanity: the VM has no leftover USDC.
	usdcBound := bind.NewBoundContract(mainnetUSDC, weiroll.MustParseABI(erc20StdABI), client, client, client)
	var usdcLeft *big.Int
	if err := usdcBound.Call(&bind.CallOpts{Context: ctx}, &[]interface{}{&usdcLeft}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("USDC post-balance: %v", err)
	}
	if usdcLeft.Sign() != 0 {
		t.Errorf("VM should have no USDC left; got %s", usdcLeft)
	}
}

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

const npmTransferABI = `[
	{
		"name":"transferFrom","type":"function","stateMutability":"nonpayable",
		"inputs":[
			{"name":"from","type":"address"},
			{"name":"to","type":"address"},
			{"name":"tokenId","type":"uint256"}
		],
		"outputs":[]
	},
	{
		"name":"balanceOf","type":"function","stateMutability":"view",
		"inputs":[{"name":"owner","type":"address"}],
		"outputs":[{"name":"","type":"uint256"}]
	}
]`

// TestForkCase7_AdvancedComposition runs examples/07_advanced_composition
// end-to-end on a mainnet fork. It exercises three "hard to get" return
// values in one tx:
//
//   - swap result (uint256[]) extracted via MathLib.extractLastElement
//   - mint return blob (bytes) captured via .RawReturn()
//   - tokenId (uint256) extracted via TupleHelper + .As, then piped
//     into NPM.transferFrom to ship the LP NFT to the user
//
// Setup: send 1 ETH with the execute call, expect the user wallet to
// hold a UniV3 LP NFT after the tx mines.
func TestForkCase7_AdvancedComposition(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	ctx := context.Background()

	// Deploy: VM, MathLib, TupleHelper, MintAdapter.
	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy WeirollVM: %v", err)
	}
	t.Logf("WeirollVM:    %s", vmAddr.Hex())

	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	mathAddr, err := deployContract(ctx, client, auth, pk, "MathLib")
	if err != nil {
		t.Fatalf("deploy MathLib: %v", err)
	}
	t.Logf("MathLib:      %s", mathAddr.Hex())

	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	helperAddr, err := deployContract(ctx, client, auth, pk, "TupleHelper")
	if err != nil {
		t.Fatalf("deploy TupleHelper: %v", err)
	}
	t.Logf("TupleHelper:  %s", helperAddr.Hex())

	setNonceAndGas(t, ctx, client, auth, from, 2_000_000)
	adapterAddr, err := deployContract(ctx, client, auth, pk, "MintAdapter")
	if err != nil {
		t.Fatalf("deploy MintAdapter: %v", err)
	}
	t.Logf("MintAdapter:  %s", adapterAddr.Hex())

	// Beneficiary of the LP NFT — distinct from `from` so the assertion
	// is meaningful even if `from` already holds Uniswap positions.
	user := common.HexToAddress("0xCAFEBABE00000000000000000000000000000007")

	npm := common.HexToAddress("0xC36442b4a4522E871399CD717aBDD847Ab11FE88")

	// Build the same plan as examples/07_advanced_composition.
	weth := weiroll.NewContract(mainnetWETH, weiroll.MustParseABI(wethABI))
	usdc := weiroll.NewContract(mainnetUSDC, weiroll.MustParseABI(erc20StdABI))
	uniV2 := weiroll.NewContract(mainnetUniV2Router, weiroll.MustParseABI(uniswapV2RouterABI))
	math := weiroll.NewLibrary(mathAddr, weiroll.MustParseABI(mathLibABI))
	adapter := weiroll.NewLibrary(adapterAddr, weiroll.MustParseABI(mintAdapterABI))
	helper := weiroll.NewLibrary(helperAddr, weiroll.MustParseABI(tupleHelperABI))
	npmCtx := weiroll.NewContract(npm, weiroll.MustParseABI(npmTransferABI))

	wrapAmount := big.NewInt(1e18)
	swapAmount := new(big.Int).Div(wrapAmount, big.NewInt(2))
	mintWeth := new(big.Int).Sub(wrapAmount, swapAmount)
	deadline := big.NewInt(time.Now().Unix() + 3600)
	maxApprove := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	planner := weiroll.New()
	planner.Add(weth.MustInvoke("deposit").WithValue(wrapAmount))
	planner.Add(weth.MustInvoke("approve", mainnetUniV2Router, maxApprove))
	swapResult := planner.Add(uniV2.MustInvoke(
		"swapExactTokensForTokens",
		swapAmount, big.NewInt(0),
		[]common.Address{mainnetWETH, mainnetUSDC},
		vmAddr, deadline,
	))
	usdcOut := planner.Add(math.MustInvoke("extractLastElement", swapResult))
	planner.Add(weth.MustInvoke("approve", npm, maxApprove))
	planner.Add(usdc.MustInvoke("approve", npm, maxApprove))

	mintRaw := planner.Add(adapter.MustInvoke(
		"mintFlat",
		mainnetUSDC, mainnetWETH,
		big.NewInt(500),
		big.NewInt(-887220), big.NewInt(887220),
		usdcOut,           // pipe from swap result
		mintWeth,          // literal we kept
		big.NewInt(0), big.NewInt(0),
		vmAddr, deadline,
	).RawReturn())

	tokenIdB32 := planner.Add(helper.MustInvoke("extract", mintRaw, big.NewInt(0)))
	tokenId := tokenIdB32.MustAsType("uint256")
	planner.Add(npmCtx.MustInvoke("transferFrom", vmAddr, user, tokenId))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	t.Logf("plan: %d commands, %d state slots", len(plan.Commands), len(plan.State))

	// Producer-slot regression check: mintFlat command (index 6) MUST
	// have a clean return-slot byte. If it gains the dynamic flag,
	// writeTuple goes out of bounds.
	_, _, _, mintProdSlot, _, _ := weiroll.DecodeCommand(plan.Commands[6])
	if mintProdSlot&weiroll.DynamicSlotFlag != 0 {
		t.Fatalf("mintFlat return slot byte must be clean for tuple-return; got 0x%02x", mintProdSlot)
	}

	// Pre-balance check: the user owns no NPM NFTs before this tx.
	npmBound := bind.NewBoundContract(npm, weiroll.MustParseABI(npmTransferABI), client, client, client)
	var nftBefore *big.Int
	if err := npmBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&nftBefore}, "balanceOf", user); err != nil {
		t.Fatalf("NPM.balanceOf(user) before: %v", err)
	}

	// Execute. We need to send 1 ETH alongside (auth.Value).
	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)
	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	auth.Value = wrapAmount
	tx, err := vmBound.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("VM.execute submit: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("WaitMined execute: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("execute reverted: status=%d gas=%d txhash=%s",
			receipt.Status, receipt.GasUsed, receipt.TxHash.Hex())
	}
	t.Logf("execute gas used: %d", receipt.GasUsed)

	// Verify: user now owns exactly one more LP NFT than before.
	var nftAfter *big.Int
	if err := npmBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&nftAfter}, "balanceOf", user); err != nil {
		t.Fatalf("NPM.balanceOf(user) after: %v", err)
	}
	delta := new(big.Int).Sub(nftAfter, nftBefore)
	t.Logf("user NPM NFTs: %s -> %s (+%s)", nftBefore, nftAfter, delta)
	if delta.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("expected user to receive exactly 1 NFT, got delta=%s", delta)
	}
}

