package weiroll

import (
	"math/big"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Sample ABI JSON for testing
const testABIJSON = `[
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
		"name": "transfer",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "to", "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [
			{"name": "", "type": "bool"}
		]
	},
	{
		"name": "getValue",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	}
]`

func TestContractType(t *testing.T) {
	t.Run("External is 0", func(t *testing.T) {
		if External != 0 {
			t.Errorf("Expected External to be 0, got %d", External)
		}
	})

	t.Run("StaticExternal is 1", func(t *testing.T) {
		if StaticExternal != 1 {
			t.Errorf("Expected StaticExternal to be 1, got %d", StaticExternal)
		}
	})
}

func TestNewContract(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")

	t.Run("creates external contract", func(t *testing.T) {
		contract := NewContract(addr, parsed)

		if contract == nil {
			t.Fatal("Expected contract to be non-nil")
		}
		if contract.Type() != External {
			t.Errorf("Expected External type, got %v", contract.Type())
		}
		if contract.Address() != addr {
			t.Errorf("Expected address %s, got %s", addr.Hex(), contract.Address().Hex())
		}
	})

	t.Run("external uses CALL", func(t *testing.T) {
		contract := NewContract(addr, parsed)
		flags := contract.defaultFlags()

		if flags.CallType() != FlagCall {
			t.Errorf("Expected CALL, got 0x%02x", flags.CallType())
		}
	})

	t.Run("with static calls uses STATICCALL", func(t *testing.T) {
		contract := NewContract(addr, parsed, WithStaticCalls())

		if contract.Type() != StaticExternal {
			t.Errorf("Expected StaticExternal type, got %v", contract.Type())
		}

		flags := contract.defaultFlags()
		if flags.CallType() != FlagStaticCall {
			t.Errorf("Expected STATICCALL, got 0x%02x", flags.CallType())
		}
	})
}

func TestContractAddress(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	contract := NewContract(addr, parsed)

	if contract.Address() != addr {
		t.Errorf("Expected address %s, got %s", addr.Hex(), contract.Address().Hex())
	}
}

func TestContractABI(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, parsed)

	resultABI := contract.ABI()

	// Verify ABI has expected methods
	if _, ok := resultABI.Methods["add"]; !ok {
		t.Error("Expected 'add' method in ABI")
	}
	if _, ok := resultABI.Methods["transfer"]; !ok {
		t.Error("Expected 'transfer' method in ABI")
	}
	if _, ok := resultABI.Methods["getValue"]; !ok {
		t.Error("Expected 'getValue' method in ABI")
	}
}

func TestContractInvoke(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, parsed)

	t.Run("creates call for valid method", func(t *testing.T) {
		call, err := contract.Invoke("add", big.NewInt(1), big.NewInt(2))

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if call == nil {
			t.Fatal("Expected call to be non-nil")
		}
		if call.Method().Name != "add" {
			t.Errorf("Expected method 'add', got %q", call.Method().Name)
		}
	})

	t.Run("returns error for unknown method", func(t *testing.T) {
		_, err := contract.Invoke("nonexistent", big.NewInt(1))

		if err == nil {
			t.Fatal("Expected error for unknown method")
		}

		notFound, ok := err.(*MethodNotFoundError)
		if !ok {
			t.Fatalf("Expected *MethodNotFoundError, got %T", err)
		}
		if notFound.Method != "nonexistent" {
			t.Errorf("Expected method 'nonexistent', got %q", notFound.Method)
		}
		if notFound.Contract != addr {
			t.Errorf("Expected contract %s, got %s", addr.Hex(), notFound.Contract.Hex())
		}
	})

	t.Run("returns error for wrong argument count", func(t *testing.T) {
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

	t.Run("handles method with no args", func(t *testing.T) {
		call, err := contract.Invoke("getValue")

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(call.Args()) != 0 {
			t.Errorf("Expected 0 args, got %d", len(call.Args()))
		}
	})

	t.Run("handles address argument", func(t *testing.T) {
		recipient := common.HexToAddress("0x9999999999999999999999999999999999999999")
		call, err := contract.Invoke("transfer", recipient, big.NewInt(100))

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(call.Args()) != 2 {
			t.Errorf("Expected 2 args, got %d", len(call.Args()))
		}
	})
}

func TestContractMustInvoke(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, parsed)

	t.Run("returns call for valid method", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		if call == nil {
			t.Fatal("Expected call to be non-nil")
		}
		if call.Method().Name != "add" {
			t.Errorf("Expected method 'add', got %q", call.Method().Name)
		}
	})

	t.Run("panics for unknown method", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for unknown method")
			}
		}()

		contract.MustInvoke("nonexistent", big.NewInt(1))
	})

	t.Run("panics for wrong argument count", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for wrong argument count")
			}
		}()

		contract.MustInvoke("add", big.NewInt(1)) // Missing second arg
	})
}

func TestContractHasMethod(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, parsed)

	t.Run("returns true for existing method", func(t *testing.T) {
		if !contract.HasMethod("add") {
			t.Error("Expected HasMethod('add') to be true")
		}
		if !contract.HasMethod("transfer") {
			t.Error("Expected HasMethod('transfer') to be true")
		}
		if !contract.HasMethod("getValue") {
			t.Error("Expected HasMethod('getValue') to be true")
		}
	})

	t.Run("returns false for non-existing method", func(t *testing.T) {
		if contract.HasMethod("nonexistent") {
			t.Error("Expected HasMethod('nonexistent') to be false")
		}
		if contract.HasMethod("") {
			t.Error("Expected HasMethod('') to be false")
		}
	})
}

func TestContractMethodNames(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, parsed)

	names := contract.MethodNames()

	// Sort for consistent comparison
	sort.Strings(names)
	expected := []string{"add", "getValue", "transfer"}
	sort.Strings(expected)

	if len(names) != len(expected) {
		t.Fatalf("Expected %d methods, got %d", len(expected), len(names))
	}

	for i, name := range expected {
		if names[i] != name {
			t.Errorf("Expected method %q at index %d, got %q", name, i, names[i])
		}
	}
}

func TestContractDefaultFlags(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("External returns CALL", func(t *testing.T) {
		contract := NewContract(addr, parsed)
		flags := contract.defaultFlags()

		if flags != FlagCall {
			t.Errorf("Expected FlagCall, got 0x%02x", flags)
		}
	})

	t.Run("StaticExternal returns STATICCALL", func(t *testing.T) {
		contract := NewContract(addr, parsed, WithStaticCalls())
		flags := contract.defaultFlags()

		if flags != FlagStaticCall {
			t.Errorf("Expected FlagStaticCall, got 0x%02x", flags)
		}
	})
}

func TestParseABI(t *testing.T) {
	t.Run("parses valid ABI", func(t *testing.T) {
		parsed, err := ParseABI(testABIJSON)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(parsed.Methods) != 3 {
			t.Errorf("Expected 3 methods, got %d", len(parsed.Methods))
		}
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		_, err := ParseABI("invalid json")

		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("returns error for non-ABI JSON", func(t *testing.T) {
		_, err := ParseABI(`{"foo": "bar"}`)

		if err == nil {
			t.Error("Expected error for non-ABI JSON")
		}
	})

	t.Run("returns empty ABI for empty array", func(t *testing.T) {
		parsed, err := ParseABI("[]")

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(parsed.Methods) != 0 {
			t.Errorf("Expected 0 methods, got %d", len(parsed.Methods))
		}
	})
}

func TestMustParseABI(t *testing.T) {
	t.Run("returns ABI for valid JSON", func(t *testing.T) {
		parsed := MustParseABI(testABIJSON)

		if len(parsed.Methods) != 3 {
			t.Errorf("Expected 3 methods, got %d", len(parsed.Methods))
		}
	})

	t.Run("panics for invalid JSON", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for invalid JSON")
			}
		}()

		MustParseABI("invalid json")
	})
}

func TestWithStaticCalls(t *testing.T) {
	parsed := MustParseABI(testABIJSON)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("changes contract type to StaticExternal", func(t *testing.T) {
		contract := NewContract(addr, parsed, WithStaticCalls())

		if contract.Type() != StaticExternal {
			t.Errorf("Expected StaticExternal, got %v", contract.Type())
		}
	})

	t.Run("affects default call flags", func(t *testing.T) {
		contract := NewContract(addr, parsed, WithStaticCalls())
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		if call.Flags().CallType() != FlagStaticCall {
			t.Errorf("Expected STATICCALL, got 0x%02x", call.Flags().CallType())
		}
	})
}

func TestContractWithDifferentABIs(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("handles complex tuple ABI", func(t *testing.T) {
		tupleABI := `[{
			"name": "getStruct",
			"type": "function",
			"inputs": [],
			"outputs": [{
				"name": "",
				"type": "tuple",
				"components": [
					{"name": "value", "type": "uint256"},
					{"name": "name", "type": "string"}
				]
			}]
		}]`

		parsed := MustParseABI(tupleABI)
		contract := NewContract(addr, parsed)

		if !contract.HasMethod("getStruct") {
			t.Error("Expected contract to have 'getStruct' method")
		}
	})

	t.Run("handles bytes and string types", func(t *testing.T) {
		bytesABI := `[{
			"name": "process",
			"type": "function",
			"inputs": [
				{"name": "data", "type": "bytes"},
				{"name": "message", "type": "string"}
			],
			"outputs": [
				{"name": "", "type": "bytes32"}
			]
		}]`

		parsed := MustParseABI(bytesABI)
		contract := NewContract(addr, parsed)

		call, err := contract.Invoke("process", []byte{1, 2, 3}, "hello")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		args := call.Args()
		if len(args) != 2 {
			t.Fatalf("Expected 2 args, got %d", len(args))
		}

		// Both args should be dynamic
		if !args[0].IsDynamic() {
			t.Error("bytes arg should be dynamic")
		}
		if !args[1].IsDynamic() {
			t.Error("string arg should be dynamic")
		}
	})

	t.Run("handles array types", func(t *testing.T) {
		arrayABI := `[{
			"name": "sum",
			"type": "function",
			"inputs": [
				{"name": "values", "type": "uint256[]"}
			],
			"outputs": [
				{"name": "", "type": "uint256"}
			]
		}]`

		parsed := MustParseABI(arrayABI)
		contract := NewContract(addr, parsed)

		if !contract.HasMethod("sum") {
			t.Error("Expected contract to have 'sum' method")
		}
	})
}

func TestParseABIWithStringsReader(t *testing.T) {
	// Verify internal use of strings.Reader works correctly
	abiJSON := testABIJSON
	parsed, err := abi.JSON(strings.NewReader(abiJSON))

	if err != nil {
		t.Fatalf("Expected no error from abi.JSON, got %v", err)
	}

	if len(parsed.Methods) != 3 {
		t.Errorf("Expected 3 methods, got %d", len(parsed.Methods))
	}
}
