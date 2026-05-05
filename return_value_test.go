package weiroll

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// returnValueTestABI exercises the cases relevant to RawReturn typing,
// dynamic-flag propagation, and the As cast.
func returnValueTestABI() abi.ABI {
	const abiJSON = `[
		{
			"name": "tupleReturn",
			"type": "function",
			"stateMutability": "view",
			"inputs": [],
			"outputs": [
				{"name": "tokenId",   "type": "uint256"},
				{"name": "liquidity", "type": "uint128"},
				{"name": "amount0",   "type": "uint256"},
				{"name": "amount1",   "type": "uint256"}
			]
		},
		{
			"name": "consumeBytes",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [{"name": "data", "type": "bytes"}],
			"outputs": [{"name": "", "type": "bytes32"}]
		},
		{
			"name": "consumeUint",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [{"name": "x", "type": "uint256"}],
			"outputs": []
		},
		{
			"name": "consumeAddress",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [{"name": "a", "type": "address"}],
			"outputs": []
		},
		{
			"name": "returnBytes32",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [],
			"outputs": [{"name": "", "type": "bytes32"}]
		},
		{
			"name": "returnUintArray",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [],
			"outputs": [{"name": "", "type": "uint256[]"}]
		},
		{
			"name": "consumeUintArray",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [{"name": "xs", "type": "uint256[]"}],
			"outputs": []
		}
	]`
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestRawReturnRetypesToBytes(t *testing.T) {
	testABI := returnValueTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("ReturnValue is typed as bytes after RawReturn", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("tupleReturn").RawReturn())

		if rv == nil {
			t.Fatal("Expected non-nil ReturnValue")
		}
		if rv.Type().String() != "bytes" {
			t.Errorf("Expected bytes, got %s", rv.Type().String())
		}
		if !rv.IsDynamic() {
			t.Error("RawReturn ReturnValue must be dynamic")
		}
	})

	t.Run("without RawReturn, type is first ABI output", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("tupleReturn"))

		if rv == nil {
			t.Fatal("Expected non-nil ReturnValue")
		}
		if rv.Type().String() != "uint256" {
			t.Errorf("Expected uint256 (first output), got %s", rv.Type().String())
		}
		if rv.IsDynamic() {
			t.Error("First static output should not be dynamic")
		}
	})

	t.Run("RawReturn pipes into a bytes parameter", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("tupleReturn").RawReturn())

		// Should not error: rv is bytes, consumeBytes takes bytes.
		_, err := contract.Invoke("consumeBytes", rv)
		if err != nil {
			t.Errorf("Piping bytes-typed RawReturn into bytes arg should succeed: %v", err)
		}
	})

	t.Run("RawReturn rejected when piped into a uint256 parameter", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("tupleReturn").RawReturn())

		_, err := contract.Invoke("consumeUint", rv)
		if err == nil {
			t.Fatal("Expected type-mismatch error piping bytes into uint256")
		}
		if _, ok := err.(*ArgumentError); !ok {
			t.Errorf("Expected ArgumentError, got %T", err)
		}
	})
}

func TestRawReturnProducerSlotIsClean(t *testing.T) {
	// CommandBuilder.writeTuple uses the producer's return-slot byte
	// UNMASKED (`state[idx]`, no IDX_VALUE_MASK). Setting the dynamic
	// flag there would put the slot index out of bounds and revert
	// on-chain. The dynamic flag must only appear on the CONSUMER's
	// arg byte, where buildInputs masks it correctly.
	testABI := returnValueTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	p := New()
	rv := p.Add(contract.MustInvoke("tupleReturn").RawReturn())
	p.Add(contract.MustInvoke("consumeBytes", rv))

	plan, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(plan.Commands))
	}

	_, _, _, prodReturnSlot, _, err := DecodeCommand(plan.Commands[0])
	if err != nil {
		t.Fatalf("DecodeCommand failed: %v", err)
	}
	if prodReturnSlot == NoReturnSlot {
		t.Fatal("Expected return slot to be allocated")
	}
	if prodReturnSlot&DynamicSlotFlag != 0 {
		t.Errorf("RawReturn producer slot must NOT have dynamic flag (writeTuple uses the byte unmasked); got 0x%02x", prodReturnSlot)
	}

	// Consumer arg byte MUST have the flag, since the slot holds
	// length-prefixed bytes and buildInputs masks correctly.
	_, _, consArgs, _, _, _ := DecodeCommand(plan.Commands[1])
	if len(consArgs) == 0 {
		t.Fatal("Consumer command has no arg slots")
	}
	if consArgs[0]&DynamicSlotFlag == 0 {
		t.Errorf("Consumer arg byte for a RawReturn slot must have dynamic flag; got 0x%02x", consArgs[0])
	}

	// And both must point at the same actual slot.
	if (prodReturnSlot & ^uint8(DynamicSlotFlag)) != (consArgs[0] & ^uint8(DynamicSlotFlag)) {
		t.Errorf("Producer slot %d != consumer slot %d",
			prodReturnSlot&^uint8(DynamicSlotFlag),
			consArgs[0]&^uint8(DynamicSlotFlag))
	}
}

func TestReturnValueAs(t *testing.T) {
	testABI := returnValueTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("static-to-static cast: bytes32 to uint256", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("returnBytes32"))

		uintRV := rv.MustAsType("uint256")
		if uintRV.Type().String() != "uint256" {
			t.Errorf("Expected uint256, got %s", uintRV.Type().String())
		}
		if uintRV.IsDynamic() {
			t.Error("uint256 cast should not be dynamic")
		}
		if uintRV.Command() != rv.Command() {
			t.Error("Cast should preserve underlying command")
		}

		// Should pipe into a uint256 parameter without a TypeMismatchError.
		if _, err := contract.Invoke("consumeUint", uintRV); err != nil {
			t.Errorf("Piping cast into uint256 arg failed: %v", err)
		}
	})

	t.Run("static-to-static cast: bytes32 to address", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("returnBytes32"))

		addrRV := rv.MustAsType("address")
		if _, err := contract.Invoke("consumeAddress", addrRV); err != nil {
			t.Errorf("Piping cast into address arg failed: %v", err)
		}
	})

	t.Run("dynamic-to-dynamic cast: bytes to uint256[]", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("tupleReturn").RawReturn())

		arrRV, err := rv.AsType("uint256[]")
		if err != nil {
			t.Fatalf("dynamic-to-dynamic cast should succeed: %v", err)
		}
		if !arrRV.IsDynamic() {
			t.Error("uint256[] cast should be dynamic")
		}
		if _, err := contract.Invoke("consumeUintArray", arrRV); err != nil {
			t.Errorf("Piping cast into uint256[] arg failed: %v", err)
		}
	})

	t.Run("rejects static-to-dynamic", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("returnBytes32"))

		_, err := rv.AsType("bytes")
		if err == nil {
			t.Fatal("Expected error casting static bytes32 to dynamic bytes")
		}
		if _, ok := err.(*TypeMismatchError); !ok {
			t.Errorf("Expected TypeMismatchError, got %T", err)
		}
	})

	t.Run("rejects dynamic-to-static", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("returnUintArray"))

		_, err := rv.AsType("uint256")
		if err == nil {
			t.Fatal("Expected error casting dynamic uint256[] to static uint256")
		}
	})

	t.Run("MustAs panics on incompatible cast", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for static-to-dynamic MustAs")
			}
		}()
		p := New()
		rv := p.Add(contract.MustInvoke("returnBytes32"))
		rv.MustAsType("bytes")
	})

	t.Run("MustAsType panics on invalid type string", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for invalid type string")
			}
		}()
		p := New()
		rv := p.Add(contract.MustInvoke("returnBytes32"))
		rv.MustAsType("not_a_real_type")
	})

	t.Run("AsType uses identical command pointer", func(t *testing.T) {
		p := New()
		rv := p.Add(contract.MustInvoke("returnBytes32"))
		cast := rv.MustAsType("uint256")

		// The cast must reference the same producer command, otherwise
		// the planner allocates a separate (unused) slot.
		if cast.Command() != rv.Command() {
			t.Error("Cast lost reference to original command")
		}
	})
}

func TestRawReturnEndToEndCastPattern(t *testing.T) {
	// Smoke test the Case 4 pattern from the authoring guide:
	//   raw := planner.Add(npm.MustInvoke("mint").RawReturn())
	//   typed := raw.MustAsType("uint256[]")  // off-chain cast
	//   planner.Add(consumer.MustInvoke("consume", typed))
	testABI := returnValueTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	p := New()
	raw := p.Add(contract.MustInvoke("tupleReturn").RawReturn())
	typed := raw.MustAsType("uint256[]")
	p.Add(contract.MustInvoke("consumeUintArray", typed))

	plan, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(plan.Commands))
	}

	// Producer slot byte must be CLEAN (writeTuple takes the unmasked
	// byte). Consumer arg byte must have the dynamic flag (buildInputs
	// masks correctly).
	_, _, _, prodReturnSlot, _, _ := DecodeCommand(plan.Commands[0])
	_, _, consArgSlots, _, _, _ := DecodeCommand(plan.Commands[1])

	if prodReturnSlot&DynamicSlotFlag != 0 {
		t.Errorf("RawReturn producer return slot must be clean (writeTuple is unmasked): 0x%02x", prodReturnSlot)
	}
	if len(consArgSlots) == 0 {
		t.Fatal("Consumer command has no argument slots")
	}
	if consArgSlots[0]&DynamicSlotFlag == 0 {
		t.Errorf("Consumer arg slot missing dynamic flag: 0x%02x", consArgSlots[0])
	}
	consIdx := consArgSlots[0] & ^uint8(DynamicSlotFlag)
	if prodReturnSlot != consIdx {
		t.Errorf("Producer slot %d != consumer slot %d", prodReturnSlot, consIdx)
	}
}

func TestRawReturnDoesNotMutateExistingType(t *testing.T) {
	// Sanity: calling RawReturn returns a new Call; the original Call
	// must keep its declared first-output type.
	testABI := returnValueTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	original := contract.MustInvoke("tupleReturn")
	raw := original.RawReturn()

	if got := original.EffectiveReturnType().String(); got != "uint256" {
		t.Errorf("Original effective type should be uint256, got %s", got)
	}
	if got := raw.EffectiveReturnType().String(); got != "bytes" {
		t.Errorf("RawReturn effective type should be bytes, got %s", got)
	}
}

// Ensure Clone preserves the RawReturn-typed ReturnValue correctly.
func TestCloneRawReturnTyping(t *testing.T) {
	testABI := returnValueTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	p := New()
	raw := p.Add(contract.MustInvoke("tupleReturn").RawReturn())
	p.Add(contract.MustInvoke("consumeBytes", raw))

	clone := p.Clone()

	plan, err := clone.Plan()
	if err != nil {
		t.Fatalf("Clone Plan failed: %v", err)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(plan.Commands))
	}
	// Producer slot byte must remain CLEAN after Clone — the
	// rawReturn flag must propagate so writeTuple is still used.
	_, _, _, prodReturnSlot, _, _ := DecodeCommand(plan.Commands[0])
	if prodReturnSlot&DynamicSlotFlag != 0 {
		t.Errorf("Cloned RawReturn producer slot must not have dynamic flag: 0x%02x", prodReturnSlot)
	}
	// Consumer side preserves the dynamic flag.
	_, _, consArgs, _, _, _ := DecodeCommand(plan.Commands[1])
	if len(consArgs) == 0 || consArgs[0]&DynamicSlotFlag == 0 {
		t.Errorf("Cloned consumer arg must have dynamic flag: %v", consArgs)
	}
}

func TestEffectiveReturnTypeNilForVoid(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	call := contract.MustInvoke("noReturn", big.NewInt(0))
	if call.EffectiveReturnType() != nil {
		t.Error("Void function should have nil EffectiveReturnType")
	}
}
