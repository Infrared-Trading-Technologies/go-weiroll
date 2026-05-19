package weiroll

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Helper to create a test ABI with various methods
func testABI() abi.ABI {
	const abiJSON = `[
		{
			"name": "add",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [
				{"name": "a", "type": "uint256"},
				{"name": "b", "type": "uint256"}
			],
			"outputs": [
				{"name": "", "type": "uint256"}
			]
		},
		{
			"name": "noReturn",
			"type": "function",
			"stateMutability": "nonpayable",
			"inputs": [
				{"name": "x", "type": "uint256"}
			],
			"outputs": []
		},
		{
			"name": "multiReturn",
			"type": "function",
			"stateMutability": "view",
			"inputs": [],
			"outputs": [
				{"name": "a", "type": "uint256"},
				{"name": "b", "type": "bool"}
			]
		},
		{
			"name": "dynamicArgs",
			"type": "function",
			"stateMutability": "pure",
			"inputs": [
				{"name": "s", "type": "string"},
				{"name": "b", "type": "bytes"}
			],
			"outputs": [
				{"name": "", "type": "bytes"}
			]
		}
	]`
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestNewCall(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("creates call with correct arguments", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		call, err := contract.Invoke("add", big.NewInt(1), big.NewInt(2))

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if call == nil {
			t.Fatal("Expected call to be non-nil")
		}
		if call.Contract() != contract {
			t.Error("Call should reference original contract")
		}
		if call.Method().Name != "add" {
			t.Errorf("Expected method name 'add', got %q", call.Method().Name)
		}
		if len(call.Args()) != 2 {
			t.Errorf("Expected 2 args, got %d", len(call.Args()))
		}
	})

	t.Run("returns error for wrong argument count", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		_, err := contract.Invoke("add", big.NewInt(1)) // Missing second arg

		if err == nil {
			t.Fatal("Expected error for wrong argument count")
		}

		argErr, ok := err.(*ArgumentError)
		if !ok {
			t.Fatalf("Expected *ArgumentError, got %T", err)
		}
		if argErr.Method != "add" {
			t.Errorf("Expected method 'add', got %q", argErr.Method)
		}
	})

	t.Run("handles external contracts with CALL", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		call, err := contract.Invoke("add", big.NewInt(1), big.NewInt(2))

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if call.Flags().CallType() != FlagCall {
			t.Errorf("Expected CALL, got %v", call.Flags().CallType())
		}
	})

	t.Run("handles static contracts with STATICCALL", func(t *testing.T) {
		contract := NewContract(addr, testABI, WithStaticCalls())
		call, err := contract.Invoke("add", big.NewInt(1), big.NewInt(2))

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if call.Flags().CallType() != FlagStaticCall {
			t.Errorf("Expected STATICCALL, got %v", call.Flags().CallType())
		}
	})
}

func TestCallContract(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)
	call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

	if call.Contract() != contract {
		t.Error("Contract() should return original contract")
	}
}

func TestCallMethod(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)
	call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

	method := call.Method()
	if method.Name != "add" {
		t.Errorf("Expected method name 'add', got %q", method.Name)
	}
	if len(method.Inputs) != 2 {
		t.Errorf("Expected 2 inputs, got %d", len(method.Inputs))
	}
}

func TestCallArgs(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)
	call := contract.MustInvoke("add", big.NewInt(100), big.NewInt(200))

	args := call.Args()
	if len(args) != 2 {
		t.Fatalf("Expected 2 args, got %d", len(args))
	}

	// Verify args are LiteralValues
	for i, arg := range args {
		if _, ok := arg.(*LiteralValue); !ok {
			t.Errorf("Arg %d should be *LiteralValue, got %T", i, arg)
		}
	}
}

func TestCallFlags(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("contract defaults to CALL", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		if call.Flags().CallType() != FlagCall {
			t.Errorf("Expected CALL (0x01), got 0x%02x", call.Flags().CallType())
		}
	})
}

func TestCallEthValue(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("returns nil when no value set", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		if call.EthValue() != nil {
			t.Errorf("Expected nil value, got %v", call.EthValue())
		}
	})

	t.Run("returns value when set", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)).
			WithValue(big.NewInt(1e18))

		if call.EthValue() == nil {
			t.Fatal("Expected value to be set")
		}
		if call.EthValue().Cmp(big.NewInt(1e18)) != 0 {
			t.Errorf("Expected 1e18, got %v", call.EthValue())
		}
	})
}

func TestCallHasReturnValue(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("returns true for function with output", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		if !call.HasReturnValue() {
			t.Error("Expected HasReturnValue() to be true")
		}
	})

	t.Run("returns false for function without output", func(t *testing.T) {
		call := contract.MustInvoke("noReturn", big.NewInt(1))

		if call.HasReturnValue() {
			t.Error("Expected HasReturnValue() to be false")
		}
	})
}

func TestCallReturnType(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("returns type for function with output", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		retType := call.ReturnType()
		if retType == nil {
			t.Fatal("Expected return type to be non-nil")
		}
		if retType.T != abi.UintTy {
			t.Errorf("Expected UintTy, got %v", retType.T)
		}
	})

	t.Run("returns nil for function without output", func(t *testing.T) {
		call := contract.MustInvoke("noReturn", big.NewInt(1))

		if call.ReturnType() != nil {
			t.Error("Expected nil return type")
		}
	})

	t.Run("returns first type for multi-return function", func(t *testing.T) {
		call := contract.MustInvoke("multiReturn")

		retType := call.ReturnType()
		if retType == nil {
			t.Fatal("Expected return type to be non-nil")
		}
		// First output is uint256
		if retType.T != abi.UintTy {
			t.Errorf("Expected UintTy (first return), got %v", retType.T)
		}
	})
}

func TestCallSelector(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)
	call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

	selector := call.Selector()

	// add(uint256,uint256) has known selector
	// keccak256("add(uint256,uint256)")[:4]
	if selector == [4]byte{0, 0, 0, 0} {
		t.Error("Selector should not be zero")
	}

	// Verify it matches method ID
	var expected [4]byte
	copy(expected[:], call.Method().ID[:4])
	if selector != expected {
		t.Errorf("Selector mismatch: got %x, expected %x", selector, expected)
	}
}

func TestCallWithValue(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("creates new call with value", func(t *testing.T) {
		original := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		withValue := original.WithValue(big.NewInt(1e18))

		// Original unchanged
		if original.EthValue() != nil {
			t.Error("Original call should not have value")
		}
		if original.Flags().CallType() != FlagCall {
			t.Error("Original call should still be CALL")
		}

		// New call has value
		if withValue.EthValue() == nil || withValue.EthValue().Cmp(big.NewInt(1e18)) != 0 {
			t.Error("New call should have value 1e18")
		}
		if withValue.Flags().CallType() != FlagCallWithValue {
			t.Errorf("Expected CALL_WITH_VALUE, got 0x%02x", withValue.Flags().CallType())
		}
	})

	t.Run("creates deep copy of value", func(t *testing.T) {
		original := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		amt := big.NewInt(1e18)
		withValue := original.WithValue(amt)

		// Modify original amount
		amt.SetInt64(999)

		// Call value should be unaffected
		if withValue.EthValue().Cmp(big.NewInt(1e18)) != 0 {
			t.Error("Value should be deep copied")
		}
	})
}

func TestCallStatic(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("creates new call with STATICCALL", func(t *testing.T) {
		original := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		static := original.Static()

		// Original unchanged
		if original.Flags().CallType() != FlagCall {
			t.Error("Original call should still be CALL")
		}

		// New call is STATICCALL
		if static.Flags().CallType() != FlagStaticCall {
			t.Errorf("Expected STATICCALL, got 0x%02x", static.Flags().CallType())
		}
	})
}

func TestCallRawReturn(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("creates new call with raw return flag", func(t *testing.T) {
		original := contract.MustInvoke("multiReturn")
		raw := original.RawReturn()

		// Original unchanged
		if original.Flags()&FlagTupleReturn != 0 {
			t.Error("Original call should not have tuple return flag")
		}

		// New call has flag
		if raw.Flags()&FlagTupleReturn == 0 {
			t.Error("New call should have tuple return flag")
		}
	})
}

func TestCallWithRawCalldata(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("sets FLAG_DATA, switches to VALUECALL, defaults value to zero", func(t *testing.T) {
		original := contract.MustInvoke("noReturn", big.NewInt(1))
		raw := original.WithRawCalldata(nil)

		// Original unchanged.
		if original.Flags()&FlagData != 0 {
			t.Error("Original call should not have FLAG_DATA")
		}
		if original.HasRawCalldata() {
			t.Error("Original call should not report HasRawCalldata")
		}
		if original.EthValue() != nil {
			t.Error("Original call should still have nil value")
		}

		// New call: FLAG_DATA set, call type VALUECALL, value defaulted to 0.
		if !raw.HasRawCalldata() {
			t.Error("WithRawCalldata-derived call should report HasRawCalldata")
		}
		if raw.Flags()&FlagData == 0 {
			t.Errorf("Expected FLAG_DATA set, got flags 0x%02x", raw.Flags())
		}
		if raw.Flags().CallType() != FlagCallWithValue {
			t.Errorf("Expected CALL_WITH_VALUE call type, got 0x%02x", raw.Flags().CallType())
		}
		if raw.EthValue() == nil || raw.EthValue().Sign() != 0 {
			t.Errorf("Expected default value=0, got %v", raw.EthValue())
		}
		if raw.RawCalldata() == nil {
			t.Fatal("RawCalldata should be non-nil")
		}
	})

	t.Run("preserves preceding WithValue amount", func(t *testing.T) {
		raw := contract.MustInvoke("noReturn", big.NewInt(1)).
			WithValue(big.NewInt(7e17)).
			WithRawCalldata([]byte{0xde, 0xad})

		if raw.EthValue() == nil || raw.EthValue().Cmp(big.NewInt(7e17)) != 0 {
			t.Errorf("Expected WithValue(7e17) to survive WithRawCalldata, got %v", raw.EthValue())
		}
		if raw.Flags()&FlagData == 0 || raw.Flags().CallType() != FlagCallWithValue {
			t.Errorf("Expected FLAG_DATA + VALUECALL, got flags 0x%02x", raw.Flags())
		}
	})

	t.Run("RawCalldata stores bytes verbatim (no ABI length prefix)", func(t *testing.T) {
		payload := []byte{0xde, 0xad, 0xbe, 0xef}
		raw := contract.MustInvoke("noReturn", big.NewInt(1)).WithRawCalldata(payload)

		lit, ok := raw.RawCalldata().(*LiteralValue)
		if !ok {
			t.Fatalf("Expected RawCalldata to be *LiteralValue, got %T", raw.RawCalldata())
		}
		if !bytes.Equal(lit.Data(), payload) {
			t.Errorf("Literal data should equal payload byte-for-byte: got %x, want %x", lit.Data(), payload)
		}
	})

	t.Run("input slice is defensively copied", func(t *testing.T) {
		payload := []byte{0x01, 0x02}
		raw := contract.MustInvoke("noReturn", big.NewInt(1)).WithRawCalldata(payload)

		// Mutate the caller's slice.
		payload[0] = 0xff

		lit := raw.RawCalldata().(*LiteralValue)
		if lit.Data()[0] == 0xff {
			t.Error("Mutating the source slice bled into the Call's rawCalldata")
		}
	})

	t.Run("nil payload yields empty state slot", func(t *testing.T) {
		raw := contract.MustInvoke("noReturn", big.NewInt(1)).WithRawCalldata(nil)
		lit := raw.RawCalldata().(*LiteralValue)
		if len(lit.Data()) != 0 {
			t.Errorf("Expected zero-byte data for nil payload, got %d bytes", len(lit.Data()))
		}
	})
}

func TestCallClone(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("clone creates independent copy", func(t *testing.T) {
		original := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		modified := original.WithValue(big.NewInt(100))

		// Verify independence
		if original.EthValue() != nil {
			t.Error("Original should not be modified by WithValue")
		}
		if modified.EthValue().Cmp(big.NewInt(100)) != 0 {
			t.Error("Modified call should have value 100")
		}
	})

	t.Run("chained modifiers create independent copies", func(t *testing.T) {
		original := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		step1 := original.Static()
		step2 := step1.RawReturn()

		// Each step should be independent
		if original.Flags()&FlagTupleReturn != 0 {
			t.Error("Original should not have tuple return")
		}
		if step1.Flags()&FlagTupleReturn != 0 {
			t.Error("Step1 should not have tuple return")
		}
		if step2.Flags()&FlagTupleReturn == 0 {
			t.Error("Step2 should have tuple return")
		}
	})
}

func TestCallValidate(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("valid CALL without value", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		if err := call.validate(); err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("valid CALL_WITH_VALUE", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)).
			WithValue(big.NewInt(1e18))

		if err := call.validate(); err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("valid STATICCALL without value", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)).
			Static()

		if err := call.validate(); err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

}

func TestCallComputeFlags(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("standard command without extended flag", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		flags := call.computeFlags(false)

		if flags&FlagExtendedCommand != 0 {
			t.Error("Standard command should not have extended flag")
		}
	})

	t.Run("extended command has extended flag", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		flags := call.computeFlags(true)

		if flags&FlagExtendedCommand == 0 {
			t.Error("Extended command should have extended flag")
		}
	})

	t.Run("raw return adds tuple flag", func(t *testing.T) {
		call := contract.MustInvoke("multiReturn").RawReturn()
		flags := call.computeFlags(false)

		if flags&FlagTupleReturn == 0 {
			t.Error("Raw return should have tuple flag")
		}
	})
}

func TestCallWithDynamicArgs(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("handles string and bytes args", func(t *testing.T) {
		call, err := contract.Invoke("dynamicArgs", "hello", []byte{1, 2, 3})

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		args := call.Args()
		if len(args) != 2 {
			t.Fatalf("Expected 2 args, got %d", len(args))
		}

		// String should be dynamic
		if !args[0].IsDynamic() {
			t.Error("String arg should be dynamic")
		}

		// Bytes should be dynamic
		if !args[1].IsDynamic() {
			t.Error("Bytes arg should be dynamic")
		}
	})
}

func TestCallWithReturnValue(t *testing.T) {
	testABI := testABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("accepts ReturnValue as argument", func(t *testing.T) {
		// Create a mock return value
		retType := testABI.Methods["add"].Outputs[0].Type
		rv := &ReturnValue{
			command: nil, // nil for testing purposes
			abiType: retType,
		}

		call, err := contract.Invoke("add", rv, big.NewInt(2))
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		args := call.Args()
		if len(args) != 2 {
			t.Fatalf("Expected 2 args, got %d", len(args))
		}

		// First arg should be the ReturnValue
		if _, ok := args[0].(*ReturnValue); !ok {
			t.Errorf("First arg should be *ReturnValue, got %T", args[0])
		}
	})
}
