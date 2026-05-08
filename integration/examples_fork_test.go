package integration

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	weiroll "github.com/branched-services/go-weiroll"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Mainnet contracts shared across the example fork tests.
var (
	mainnetWETH          = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	mainnetUSDC          = common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	mainnetUniV2Router   = common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")
	mainnetUniV2WETHUSDC = common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc")
	mainnetAaveV3Pool    = common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2")
	mainnetAUSDC         = common.HexToAddress("0x98C23E9d8f34FEFb1B7BD6a91B7FF122F4e16F5c")
	mainnetUniV3Router02 = common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
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

// simulateExecute does a static eth_call on VM.execute and returns the
// revert error (or nil on success). On revert, the WeirollVM's
// ExecutionFailed custom error is surfaced inside the error string,
// including the failing command_index. bind.Transact's receipt-only
// failure path swallows this; eth_call exposes it.
func simulateExecute(
	ctx context.Context,
	client *ethclient.Client,
	from, vmAddr common.Address,
	plan *weiroll.CompiledPlan,
	value *big.Int,
) error {
	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	data, err := vmABIParsed.Pack("execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		return fmt.Errorf("pack execute: %w", err)
	}
	msg := ethereum.CallMsg{
		From:  from,
		To:    &vmAddr,
		Value: value,
		Data:  data,
	}
	if _, err := client.CallContract(ctx, msg, nil); err != nil {
		return err
	}
	return nil
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

	// Independent ground truth: raw call + Unpack. bind.BoundContract.Call
	// misdecodes multi-return ABI outputs as a single tuple; raw Unpack
	// returns each output element directly.
	pairABIParsed := weiroll.MustParseABI(uniV2PairABI)
	getReservesData, err := pairABIParsed.Pack("getReserves")
	if err != nil {
		t.Fatalf("pack getReserves: %v", err)
	}
	getReservesRet, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &mainnetUniV2WETHUSDC,
		Data: getReservesData,
	}, nil)
	if err != nil {
		t.Fatalf("call getReserves: %v", err)
	}
	unpacked, err := pairABIParsed.Unpack("getReserves", getReservesRet)
	if err != nil {
		t.Fatalf("unpack getReserves: %v", err)
	}
	if len(unpacked) != 3 {
		t.Fatalf("getReserves: expected 3 outputs, got %d", len(unpacked))
	}
	wantReserve0, ok := unpacked[0].(*big.Int)
	if !ok {
		t.Fatalf("reserve0 type: %T", unpacked[0])
	}
	wantReserve1, ok := unpacked[1].(*big.Int)
	if !ok {
		t.Fatalf("reserve1 type: %T", unpacked[1])
	}
	t.Logf("direct reserve0: %s", wantReserve0.String())
	t.Logf("direct reserve1: %s", wantReserve1.String())

	// Build the weiroll plan using the contract types we want to test.
	pair := weiroll.NewContract(mainnetUniV2WETHUSDC, pairABIParsed, weiroll.WithStaticCalls())
	helper := weiroll.NewContract(helperAddr, weiroll.MustParseABI(tupleHelperABI), weiroll.WithStaticCalls())
	math := weiroll.NewContract(mathAddr, weiroll.MustParseABI(mathLibABI))

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
	math := weiroll.NewContract(mathAddr, weiroll.MustParseABI(mathLibABI))
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

	// Pre-simulate to surface revert reasons (bind.Transact would only
	// give us status=0 with no command index).
	if err := simulateExecute(ctx, client, from, vmAddr, plan, wrapAmount); err != nil {
		t.Fatalf("execute simulation reverted: %v", err)
	}

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
	math := weiroll.NewContract(mathAddr, weiroll.MustParseABI(mathLibABI))

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

	// Pre-simulate to surface the failing command index on revert.
	if err := simulateExecute(ctx, client, from, vmAddr, plan, wrapAmount); err != nil {
		t.Fatalf("execute simulation reverted: %v", err)
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
//
// Pattern reference — "inline-ETH + delegatecall adapter":
//
// Two recipe traits combine here that don't elsewhere in the suite:
//
//  1. The user funds the recipe by attaching ETH to execute() (auth.Value
//     != 0), so a downstream WETH.deposit().WithValue(...) can wrap it
//     without a prior funding tx. Cases 6 and 3 use the same UX, but
//     only call pure helpers (registered via NewContract / CALL), so
//     CALLVALUE never reaches a nonpayable dispatcher.
//
//  2. NPM.mint takes a single MintParams struct of 11 primitive fields,
//     and go-weiroll's encoder has no tuple-flattening path — every
//     argument occupies its own state slot. The standard fix is an
//     adapter contract (MintAdapter) that exposes mintFlat(11 primitive
//     args) and rebuilds the struct internally. The adapter must be
//     delegatecalled so msg.sender to NPM is the VM (which holds the
//     WETH/USDC approvals and receives the LP NFT).
//
// Combining (1) and (2) means CALLVALUE is preserved from the outer
// execute() through the DELEGATECALL into MintAdapter. Solidity's
// nonpayable dispatcher would revert with empty returndata (visible as
// ExecutionFailed(_, _, "Unknown")). MintAdapter therefore declares
// mintFlat as `external payable` and uses the `address(this) != _SELF`
// guard to reject direct CALLs that could lock ETH in the adapter
// singleton. See MintAdapter.sol for the full convention.
//
// The pre-funded variant (auth.Value == 0, WETH transferred to VM
// out-of-band) sidesteps both points — see TestMainnetForkUniswapV2Swap
// and TestMainnetForkMultiHopSwap for that pattern.
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
	math := weiroll.NewContract(mathAddr, weiroll.MustParseABI(mathLibABI))
	adapter := weiroll.NewLibrary(adapterAddr, weiroll.MustParseABI(mintAdapterABI))
	helper := weiroll.NewContract(helperAddr, weiroll.MustParseABI(tupleHelperABI))
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

	// Pre-simulate to surface the failing command index on revert.
	if err := simulateExecute(ctx, client, from, vmAddr, plan, wrapAmount); err != nil {
		t.Fatalf("execute simulation reverted: %v", err)
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

// uniV3Router02ABI captures SwapRouter02.exactInputSingle, which takes a
// 7-field fully-static tuple parameter — the exact shape that triggered
// the original "Static state variables must be 32 bytes" revert. The
// regression below builds the params via weiroll.Tuple(...) and runs a
// real WETH -> USDC swap through the VM. Without the per-field slot
// expansion, the on-chain VM would reject the >32-byte static slot.
const uniV3Router02ABI = `[
	{
		"name": "exactInputSingle",
		"type": "function",
		"stateMutability": "payable",
		"inputs": [{
			"name": "params",
			"type": "tuple",
			"components": [
				{"name": "tokenIn",           "type": "address"},
				{"name": "tokenOut",          "type": "address"},
				{"name": "fee",               "type": "uint24"},
				{"name": "recipient",         "type": "address"},
				{"name": "amountIn",          "type": "uint256"},
				{"name": "amountOutMinimum",  "type": "uint256"},
				{"name": "sqrtPriceLimitX96", "type": "uint160"}
			]
		}],
		"outputs": [{"name": "amountOut", "type": "uint256"}]
	}
]`

// TestForkUniV3SwapStaticTupleInput is the on-chain regression for the
// static-tuple-input bug. It builds a WETH -> USDC swap on Uniswap V3
// SwapRouter02 (the same router from the original failure) using
// weiroll.Tuple(...) for the 7-field static params struct, then executes
// the plan via VM.execute.
//
// What this catches: if Tuple's per-field slot expansion regresses,
// the params struct is packed as a single 224-byte LiteralValue with
// no DynamicSlotFlag, and the VM reverts at CommandBuilder.sol:46
// (`Static state variables must be 32 bytes`) before any real work
// happens. A success path here proves the calldata layout the VM
// assembles is byte-identical to what the router expects.
func TestForkUniV3SwapStaticTupleInput(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	ctx := context.Background()

	// Deploy WeirollVM.
	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy WeirollVM: %v", err)
	}
	t.Logf("WeirollVM:        %s", vmAddr.Hex())
	t.Logf("V3 SwapRouter02:  %s", mainnetUniV3Router02.Hex())

	// Step 1: wrap 1 ETH on the test account.
	wethABIParsed := weiroll.MustParseABI(wethABI)
	wethBound := bind.NewBoundContract(mainnetWETH, wethABIParsed, client, client, client)

	setNonceAndGas(t, ctx, client, auth, from, 200_000)
	auth.Value = big.NewInt(1e18) // 1 ETH
	tx, err := wethBound.Transact(auth, "deposit")
	if err != nil {
		t.Fatalf("WETH.deposit: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		t.Fatalf("WETH.deposit mined: %v", err)
	}
	t.Log("Wrapped 1 ETH -> WETH on test account")

	// Step 2: transfer 0.5 WETH to the VM (the VM will be the swap caller).
	swapAmount := big.NewInt(5e17) // 0.5 WETH
	setNonceAndGas(t, ctx, client, auth, from, 100_000)
	tx, err = wethBound.Transact(auth, "transfer", vmAddr, swapAmount)
	if err != nil {
		t.Fatalf("WETH.transfer to VM: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		t.Fatalf("WETH.transfer mined: %v", err)
	}
	t.Logf("Funded VM with %s wei WETH", swapAmount)

	// Step 3: build the weiroll plan.
	//   cmd[0]: WETH.approve(router, MAX) — VM grants the router pull rights.
	//   cmd[1]: SwapRouter02.exactInputSingle(Tuple(...)) — the swap, with
	//           the test EOA as recipient (ignored by VM, used by router).
	weth := weiroll.NewContract(mainnetWETH, wethABIParsed)
	router := weiroll.NewContract(mainnetUniV3Router02, weiroll.MustParseABI(uniV3Router02ABI))

	planner := weiroll.New()
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	planner.Add(weth.MustInvoke("approve", mainnetUniV3Router02, maxUint))

	params := weiroll.Tuple(
		weiroll.Address(mainnetWETH),                                  // tokenIn
		weiroll.Address(mainnetUSDC),                                  // tokenOut
		weiroll.MustLiteralFromType("uint24", big.NewInt(500)),        // fee = 0.05%
		weiroll.Address(from),                                         // recipient
		weiroll.Uint256(swapAmount),                                   // amountIn
		weiroll.Uint256(big.NewInt(0)),                                // amountOutMinimum
		weiroll.MustLiteralFromType("uint160", big.NewInt(0)),         // sqrtPriceLimitX96
	)
	planner.Add(router.MustInvoke("exactInputSingle", params))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	t.Logf("plan: %d commands, %d state slots", len(plan.Commands), len(plan.State))

	// Sanity-check that command 1 is encoded as an extended command with
	// 7 distinct static slot bytes — Tuple expansion must produce
	// per-field slots; a regression would yield a single dynamic-flagged
	// slot and the VM revert below would fire.
	_, flags, argSlots, _, _, err := weiroll.DecodeCommand(plan.Commands[1])
	if err != nil {
		t.Fatalf("DecodeCommand: %v", err)
	}
	if !flags.IsExtended() {
		t.Errorf("exactInputSingle should be an extended command (>6 args); flags=0x%02x", flags)
	}
	if len(argSlots) != 7 {
		t.Errorf("argSlots len = %d, want 7", len(argSlots))
	}
	for i, s := range argSlots {
		if s&weiroll.DynamicSlotFlag != 0 {
			t.Errorf("argSlot[%d]=0x%02x has DynamicSlotFlag (must be static)", i, s)
		}
	}

	// eth_call simulation first: surfaces any revert with the real
	// error string (e.g. the static-slot-length revert if Tuple
	// regressed) instead of the receipt-only "tx failed" path.
	if err := simulateExecute(ctx, client, from, vmAddr, plan, big.NewInt(0)); err != nil {
		t.Fatalf("simulateExecute reverted: %v", err)
	}

	// Capture USDC balance before, transact, capture after.
	erc20 := weiroll.MustParseABI(erc20StdABI)
	usdcBound := bind.NewBoundContract(mainnetUSDC, erc20, client, client, client)
	var usdcBefore *big.Int
	if err := usdcBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&usdcBefore}, "balanceOf", from); err != nil {
		t.Fatalf("USDC.balanceOf before: %v", err)
	}

	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)

	setNonceAndGas(t, ctx, client, auth, from, 600_000)
	tx, err = vmBound.Transact(auth, "execute",
		plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("VM.execute send: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("VM.execute mined: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("VM.execute reverted: status=%d gas=%d txhash=%s",
			receipt.Status, receipt.GasUsed, receipt.TxHash.Hex())
	}
	t.Logf("VM.execute gas used: %d", receipt.GasUsed)

	var usdcAfter *big.Int
	if err := usdcBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&usdcAfter}, "balanceOf", from); err != nil {
		t.Fatalf("USDC.balanceOf after: %v", err)
	}
	delta := new(big.Int).Sub(usdcAfter, usdcBefore)
	t.Logf("USDC delta on recipient: %s (~%.2f USDC)", delta, float64(delta.Int64())/1e6)
	if delta.Sign() <= 0 {
		t.Fatalf("expected positive USDC delta, got %s", delta)
	}

	// Avoid linter complaints if a future change drops `time` usage.
	_ = time.Second
}

// TestForkUniV3MultiHopWithChainedAmount is the on-chain regression
// for the v0.2.0 lift: a *ReturnValue from one command can occupy a
// static field inside a weiroll.Tuple bound to the next command.
//
// Pattern: WETH -> USDC -> WETH using two SwapRouter02.exactInputSingle
// hops, where hop 2's amountIn is hop 1's *ReturnValue (the swap's
// uint256 amountOut). Hop 1 sends USDC to the VM; hop 2 spends that
// USDC and ships WETH back to the user.
//
// Why a round trip: it lets us assert that hop 2 consumed *exactly*
// what hop 1 produced — a chain regression would either leave a USDC
// remainder at the VM (chained slot read 0 or the wrong value) or
// revert outright (insufficient balance).
func TestForkUniV3MultiHopWithChainedAmount(t *testing.T) {
	pk, err := crypto.HexToECDSA(testPrivateKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	client, auth, from := skipUnlessFork(t)
	ctx := context.Background()

	setNonceAndGas(t, ctx, client, auth, from, 3_000_000)
	vmAddr, err := deployContract(ctx, client, auth, pk, "WeirollVM")
	if err != nil {
		t.Fatalf("deploy WeirollVM: %v", err)
	}
	t.Logf("WeirollVM:        %s", vmAddr.Hex())
	t.Logf("V3 SwapRouter02:  %s", mainnetUniV3Router02.Hex())

	wethABIParsed := weiroll.MustParseABI(wethABI)
	weth := weiroll.NewContract(mainnetWETH, wethABIParsed)
	usdc := weiroll.NewContract(mainnetUSDC, weiroll.MustParseABI(erc20StdABI))
	router := weiroll.NewContract(mainnetUniV3Router02, weiroll.MustParseABI(uniV3Router02ABI))

	swapAmount := big.NewInt(5e17) // 0.5 ETH worth
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	planner := weiroll.New()

	// cmd 0: wrap ETH at the VM (CALL frame; CALLVALUE preserved through
	// the payable WETH.deposit dispatcher).
	planner.Add(weth.MustInvoke("deposit").WithValue(swapAmount))
	// cmd 1: approve router to pull WETH from VM.
	planner.Add(weth.MustInvoke("approve", mainnetUniV3Router02, maxUint))

	// cmd 2: hop 1 — WETH -> USDC, recipient = VM (so USDC lands at VM).
	hop1Params := weiroll.Tuple(
		weiroll.Address(mainnetWETH),
		weiroll.Address(mainnetUSDC),
		weiroll.MustLiteralFromType("uint24", big.NewInt(500)),
		weiroll.Address(vmAddr),
		weiroll.Uint256(swapAmount),
		weiroll.Uint256(big.NewInt(0)),
		weiroll.MustLiteralFromType("uint160", big.NewInt(0)),
	)
	hop1Out := planner.Add(router.MustInvoke("exactInputSingle", hop1Params))

	// cmd 3: approve router to pull USDC from VM for hop 2.
	planner.Add(usdc.MustInvoke("approve", mainnetUniV3Router02, maxUint))

	// cmd 4: hop 2 — USDC -> WETH, amountIn = hop1Out (chained
	// *ReturnValue inside Tuple). Recipient = user EOA.
	hop2Params := weiroll.Tuple(
		weiroll.Address(mainnetUSDC),
		weiroll.Address(mainnetWETH),
		weiroll.MustLiteralFromType("uint24", big.NewInt(500)),
		weiroll.Address(from),
		hop1Out, // <-- the entire point of v0.2.0
		weiroll.Uint256(big.NewInt(0)),
		weiroll.MustLiteralFromType("uint160", big.NewInt(0)),
	)
	planner.Add(router.MustInvoke("exactInputSingle", hop2Params))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	t.Logf("plan: %d commands, %d state slots", len(plan.Commands), len(plan.State))

	// Static shape assertions. Both hops must be extended commands with
	// 7 all-static argSlots.
	_, hop1Flags, hop1ArgSlots, hop1ReturnSlot, _, err := weiroll.DecodeCommand(plan.Commands[2])
	if err != nil {
		t.Fatalf("DecodeCommand hop1: %v", err)
	}
	if !hop1Flags.IsExtended() {
		t.Errorf("hop1 should be extended (>6 args); flags=0x%02x", hop1Flags)
	}
	if len(hop1ArgSlots) != 7 {
		t.Fatalf("hop1 argSlots = %d, want 7", len(hop1ArgSlots))
	}
	for i, s := range hop1ArgSlots {
		if s&weiroll.DynamicSlotFlag != 0 {
			t.Errorf("hop1 argSlot[%d]=0x%02x has DynamicSlotFlag (must be static)", i, s)
		}
	}
	if hop1ReturnSlot == weiroll.NoReturnSlot {
		t.Fatal("hop1 should have a return slot (consumed by hop 2)")
	}
	hop1ProdSlot := hop1ReturnSlot &^ weiroll.DynamicSlotFlag

	_, hop2Flags, hop2ArgSlots, _, _, err := weiroll.DecodeCommand(plan.Commands[4])
	if err != nil {
		t.Fatalf("DecodeCommand hop2: %v", err)
	}
	if !hop2Flags.IsExtended() {
		t.Errorf("hop2 should be extended (>6 args); flags=0x%02x", hop2Flags)
	}
	if len(hop2ArgSlots) != 7 {
		t.Fatalf("hop2 argSlots = %d, want 7", len(hop2ArgSlots))
	}
	for i, s := range hop2ArgSlots {
		if s&weiroll.DynamicSlotFlag != 0 {
			t.Errorf("hop2 argSlot[%d]=0x%02x has DynamicSlotFlag (must be static)", i, s)
		}
	}

	// Wire-level proof: hop2's amountIn position (Tuple field index 4)
	// must reference hop1's return slot. If the chain regressed, this
	// argSlot would point at some unrelated slot and the on-chain
	// behavior below would diverge.
	if hop2ArgSlots[4] != hop1ProdSlot {
		t.Errorf("hop2 amountIn slot = 0x%02x, want hop1 producer slot 0x%02x",
			hop2ArgSlots[4], hop1ProdSlot)
	}
	t.Logf("chained slot: hop1 returns to slot %d, hop2 amountIn reads slot %d",
		hop1ProdSlot, hop2ArgSlots[4])

	// Pre-simulate to surface revert reasons (would fire if visibility
	// recursion regressed and the producer slot stayed unallocated).
	if err := simulateExecute(ctx, client, from, vmAddr, plan, swapAmount); err != nil {
		t.Fatalf("simulateExecute reverted: %v", err)
	}

	// Capture user/VM token balances pre-execution.
	erc20 := weiroll.MustParseABI(erc20StdABI)
	usdcBound := bind.NewBoundContract(mainnetUSDC, erc20, client, client, client)
	wethBound := bind.NewBoundContract(mainnetWETH, wethABIParsed, client, client, client)

	var userWETHBefore *big.Int
	if err := wethBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&userWETHBefore}, "balanceOf", from); err != nil {
		t.Fatalf("WETH.balanceOf(user) before: %v", err)
	}
	var vmUSDCBefore *big.Int
	if err := usdcBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&vmUSDCBefore}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("USDC.balanceOf(vm) before: %v", err)
	}
	if vmUSDCBefore.Sign() != 0 {
		t.Fatalf("VM expected to start with zero USDC; got %s", vmUSDCBefore)
	}

	// Execute the plan. auth.Value funds the WETH.deposit at cmd 0.
	vmABIParsed := weiroll.MustParseABI(weirollVMABI)
	vmBound := bind.NewBoundContract(vmAddr, vmABIParsed, client, client, client)

	setNonceAndGas(t, ctx, client, auth, from, 1_500_000)
	auth.Value = swapAmount
	tx, err := vmBound.Transact(auth, "execute", plan.CommandsAsBytes32(), plan.StateAsBytes())
	if err != nil {
		t.Fatalf("VM.execute send: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("VM.execute mined: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("VM.execute reverted: status=%d gas=%d txhash=%s",
			receipt.Status, receipt.GasUsed, receipt.TxHash.Hex())
	}
	t.Logf("VM.execute gas used: %d", receipt.GasUsed)

	// Behavioral assertions. The decisive one is vmUSDCAfter == 0:
	// only a correctly-chained amountIn = hop1Out can drain the VM
	// exactly. A broken chain would leave a USDC remainder (or revert).
	var userWETHAfter *big.Int
	if err := wethBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&userWETHAfter}, "balanceOf", from); err != nil {
		t.Fatalf("WETH.balanceOf(user) after: %v", err)
	}
	userWETHDelta := new(big.Int).Sub(userWETHAfter, userWETHBefore)
	t.Logf("user WETH delta: %s wei (%.6f WETH)",
		userWETHDelta, float64(userWETHDelta.Int64())/1e18)
	if userWETHDelta.Sign() <= 0 {
		t.Errorf("expected user WETH delta > 0 (round trip should deliver WETH back), got %s",
			userWETHDelta)
	}

	var vmUSDCAfter *big.Int
	if err := usdcBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&vmUSDCAfter}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("USDC.balanceOf(vm) after: %v", err)
	}
	if vmUSDCAfter.Sign() != 0 {
		t.Errorf("VM should have 0 USDC after hop 2 (chained amountIn must drain hop 1's output); got %s",
			vmUSDCAfter)
	}

	var vmWETHAfter *big.Int
	if err := wethBound.Call(&bind.CallOpts{Context: ctx},
		&[]interface{}{&vmWETHAfter}, "balanceOf", vmAddr); err != nil {
		t.Fatalf("WETH.balanceOf(vm) after: %v", err)
	}
	if vmWETHAfter.Sign() != 0 {
		t.Errorf("VM should have 0 WETH after hop 1 (sent to router); got %s", vmWETHAfter)
	}
}

