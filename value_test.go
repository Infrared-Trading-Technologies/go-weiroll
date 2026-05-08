package weiroll

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

func TestLiteralValue(t *testing.T) {
	t.Run("Uint256", func(t *testing.T) {
		lit := Uint256(big.NewInt(12345))

		if lit.IsDynamic() {
			t.Error("uint256 should not be dynamic")
		}

		if lit.Type().String() != "uint256" {
			t.Errorf("Expected type uint256, got %s", lit.Type().String())
		}

		if len(lit.Data()) != 32 {
			t.Errorf("Expected 32 bytes, got %d", len(lit.Data()))
		}
	})

	t.Run("Int256", func(t *testing.T) {
		lit := Int256(big.NewInt(-100))

		if lit.IsDynamic() {
			t.Error("int256 should not be dynamic")
		}

		if lit.Type().String() != "int256" {
			t.Errorf("Expected type int256, got %s", lit.Type().String())
		}
	})

	t.Run("Address", func(t *testing.T) {
		addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		lit := Address(addr)

		if lit.IsDynamic() {
			t.Error("address should not be dynamic")
		}

		if lit.Type().String() != "address" {
			t.Errorf("Expected type address, got %s", lit.Type().String())
		}
	})

	t.Run("Bytes32", func(t *testing.T) {
		hash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
		lit := Bytes32(hash)

		if lit.IsDynamic() {
			t.Error("bytes32 should not be dynamic")
		}

		if lit.Type().String() != "bytes32" {
			t.Errorf("Expected type bytes32, got %s", lit.Type().String())
		}
	})

	t.Run("Bool true", func(t *testing.T) {
		lit := Bool(true)

		if lit.IsDynamic() {
			t.Error("bool should not be dynamic")
		}

		if lit.Type().String() != "bool" {
			t.Errorf("Expected type bool, got %s", lit.Type().String())
		}
	})

	t.Run("Bool false", func(t *testing.T) {
		lit := Bool(false)

		if lit.IsDynamic() {
			t.Error("bool should not be dynamic")
		}
	})

	t.Run("String (dynamic)", func(t *testing.T) {
		lit := String("hello world")

		if !lit.IsDynamic() {
			t.Error("string should be dynamic")
		}

		if lit.Type().String() != "string" {
			t.Errorf("Expected type string, got %s", lit.Type().String())
		}
	})

	t.Run("Bytes (dynamic)", func(t *testing.T) {
		lit := Bytes([]byte{0x01, 0x02, 0x03, 0x04})

		if !lit.IsDynamic() {
			t.Error("bytes should be dynamic")
		}

		if lit.Type().String() != "bytes" {
			t.Errorf("Expected type bytes, got %s", lit.Type().String())
		}
	})
}

func TestNewLiteral(t *testing.T) {
	t.Run("valid uint256", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		lit, err := NewLiteral(abiType, big.NewInt(100))

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("int conversion", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		lit, err := NewLiteral(abiType, 42)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("int64 conversion", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		lit, err := NewLiteral(abiType, int64(1000))

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("uint64 conversion", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		lit, err := NewLiteral(abiType, uint64(1000))

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("int32 conversion", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		lit, err := NewLiteral(abiType, int32(500))

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("uint32 conversion", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		lit, err := NewLiteral(abiType, uint32(500))

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})
}

func TestMustLiteral(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Unexpected panic: %v", r)
			}
		}()

		lit := MustLiteral(abiType, big.NewInt(100))
		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("panic on invalid type", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for invalid value")
			}
		}()

		// Passing a string where uint256 is expected should panic
		MustLiteral(abiType, "not a number")
	})
}

func TestNewLiteralFromType(t *testing.T) {
	t.Run("valid types", func(t *testing.T) {
		tests := []struct {
			typeStr string
			value   any
		}{
			{"uint256", big.NewInt(100)},
			{"int256", big.NewInt(-100)},
			{"address", common.Address{}},
			{"bytes32", common.Hash{}},
			{"bool", true},
			{"string", "hello"},
			{"bytes", []byte{1, 2, 3}},
		}

		for _, tt := range tests {
			t.Run(tt.typeStr, func(t *testing.T) {
				lit, err := NewLiteralFromType(tt.typeStr, tt.value)
				if err != nil {
					t.Fatalf("Unexpected error for %s: %v", tt.typeStr, err)
				}
				if lit == nil {
					t.Fatal("Expected non-nil literal")
				}
			})
		}
	})

	t.Run("invalid type string", func(t *testing.T) {
		_, err := NewLiteralFromType("invalid_type", 123)
		if err == nil {
			t.Error("Expected error for invalid type string")
		}
	})
}

func TestMustLiteralFromType(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Unexpected panic: %v", r)
			}
		}()

		lit := MustLiteralFromType("uint256", big.NewInt(100))
		if lit == nil {
			t.Fatal("Expected non-nil literal")
		}
	})

	t.Run("panic on invalid type", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for invalid type")
			}
		}()

		MustLiteralFromType("invalid_type", 123)
	})
}

func TestReturnValue(t *testing.T) {
	abiType, _ := abi.NewType("uint256", "", nil)
	cmd := &Command{
		call:       nil,
		cmdType:    CommandTypeCall,
		returnSlot: -1,
	}

	rv := &ReturnValue{
		command: cmd,
		abiType: abiType,
	}

	t.Run("IsDynamic", func(t *testing.T) {
		if rv.IsDynamic() {
			t.Error("uint256 return value should not be dynamic")
		}
	})

	t.Run("Type", func(t *testing.T) {
		if rv.Type().String() != "uint256" {
			t.Errorf("Expected type uint256, got %s", rv.Type().String())
		}
	})

	t.Run("Data returns nil", func(t *testing.T) {
		if rv.Data() != nil {
			t.Error("Return value Data() should return nil")
		}
	})

	t.Run("Command", func(t *testing.T) {
		if rv.Command() != cmd {
			t.Error("Command() should return the associated command")
		}
	})
}

func TestReturnValueDynamic(t *testing.T) {
	abiType, _ := abi.NewType("string", "", nil)
	rv := &ReturnValue{
		command: nil,
		abiType: abiType,
	}

	if !rv.IsDynamic() {
		t.Error("string return value should be dynamic")
	}
}

func TestStateValue(t *testing.T) {
	planner := New()
	sv := planner.State()

	t.Run("IsDynamic", func(t *testing.T) {
		if !sv.IsDynamic() {
			t.Error("StateValue should always be dynamic")
		}
	})

	t.Run("Type", func(t *testing.T) {
		if sv.Type().String() != "bytes[]" {
			t.Errorf("Expected type bytes[], got %s", sv.Type().String())
		}
	})

	t.Run("Data returns nil", func(t *testing.T) {
		if sv.Data() != nil {
			t.Error("StateValue Data() should return nil")
		}
	})
}

func TestSubplanValue(t *testing.T) {
	subplanner := New()
	spv := &SubplanValue{subplanner: subplanner}

	t.Run("IsDynamic", func(t *testing.T) {
		if !spv.IsDynamic() {
			t.Error("SubplanValue should always be dynamic")
		}
	})

	t.Run("Type", func(t *testing.T) {
		if spv.Type().String() != "bytes32[]" {
			t.Errorf("Expected type bytes32[], got %s", spv.Type().String())
		}
	})

	t.Run("Data returns nil", func(t *testing.T) {
		if spv.Data() != nil {
			t.Error("SubplanValue Data() should return nil")
		}
	})

	t.Run("Planner", func(t *testing.T) {
		if spv.Planner() != subplanner {
			t.Error("Planner() should return the associated subplanner")
		}
	})
}

func TestIsDynamicType(t *testing.T) {
	tests := []struct {
		typeStr   string
		isDynamic bool
	}{
		{"uint256", false},
		{"int256", false},
		{"address", false},
		{"bytes32", false},
		{"bool", false},
		{"uint8", false},
		{"bytes1", false},
		{"string", true},
		{"bytes", true},
		{"uint256[]", true},
		{"address[]", true},
		{"bytes32[]", true},
	}

	for _, tt := range tests {
		t.Run(tt.typeStr, func(t *testing.T) {
			abiType, err := abi.NewType(tt.typeStr, "", nil)
			if err != nil {
				t.Fatalf("Failed to create type: %v", err)
			}

			if isDynamicType(abiType) != tt.isDynamic {
				t.Errorf("Expected isDynamicType=%v for %s, got %v", tt.isDynamic, tt.typeStr, !tt.isDynamic)
			}
		})
	}
}

func TestIsDynamicTypeTuple(t *testing.T) {
	t.Run("tuple with static elements", func(t *testing.T) {
		abiType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "uint256"},
			{Name: "b", Type: "address"},
		})
		if err != nil {
			t.Fatalf("Failed to create type: %v", err)
		}

		if isDynamicType(abiType) {
			t.Error("Tuple with only static elements should not be dynamic")
		}
	})

	t.Run("tuple with dynamic element", func(t *testing.T) {
		abiType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "uint256"},
			{Name: "b", Type: "string"},
		})
		if err != nil {
			t.Fatalf("Failed to create type: %v", err)
		}

		if !isDynamicType(abiType) {
			t.Error("Tuple with dynamic element should be dynamic")
		}
	})
}

func TestToValue(t *testing.T) {
	abiType, _ := abi.NewType("uint256", "", nil)

	t.Run("converts Go value to LiteralValue", func(t *testing.T) {
		val, err := toValue(big.NewInt(100), abiType)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if _, ok := val.(*LiteralValue); !ok {
			t.Error("Expected LiteralValue")
		}
	})

	t.Run("returns existing Value unchanged", func(t *testing.T) {
		lit := Uint256(big.NewInt(100))
		val, err := toValue(lit, abiType)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if val != lit {
			t.Error("Expected same Value to be returned")
		}
	})

	t.Run("type mismatch error", func(t *testing.T) {
		stringType, _ := abi.NewType("string", "", nil)
		lit := Uint256(big.NewInt(100))

		_, err := toValue(lit, stringType)
		if err == nil {
			t.Error("Expected type mismatch error")
		}

		if _, ok := err.(*TypeMismatchError); !ok {
			t.Errorf("Expected TypeMismatchError, got %T", err)
		}
	})
}

func TestConvertToABIType(t *testing.T) {
	abiType, _ := abi.NewType("uint256", "", nil)

	tests := []struct {
		name  string
		input any
	}{
		{"int", 42},
		{"int64", int64(42)},
		{"uint64", uint64(42)},
		{"int32", int32(42)},
		{"uint32", uint32(42)},
		{"*big.Int", big.NewInt(42)},
		{"*uint256.Int", uint256.NewInt(42)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToABIType(tt.input, abiType)
			if result == nil {
				t.Error("Expected non-nil result")
			}

			// For numeric types, result should be *big.Int
			if _, ok := result.(*big.Int); !ok {
				t.Errorf("Expected *big.Int, got %T", result)
			}
		})
	}

	t.Run("non-numeric passthrough", func(t *testing.T) {
		addr := common.Address{1, 2, 3}
		result := convertToABIType(addr, abiType)
		if result != addr {
			t.Error("Non-numeric types should pass through unchanged")
		}
	})

	t.Run("nil *uint256.Int does not panic", func(t *testing.T) {
		result := convertToABIType((*uint256.Int)(nil), abiType)
		b, ok := result.(*big.Int)
		if !ok {
			t.Fatalf("Expected *big.Int, got %T", result)
		}
		if b != nil {
			t.Errorf("Expected typed nil *big.Int, got %v", b)
		}
	})
}

func TestUint256FromU256(t *testing.T) {
	lit := Uint256FromU256(uint256.NewInt(12345))

	if lit.IsDynamic() {
		t.Error("uint256 should not be dynamic")
	}

	if lit.Type().String() != "uint256" {
		t.Errorf("Expected type uint256, got %s", lit.Type().String())
	}

	if len(lit.Data()) != 32 {
		t.Errorf("Expected 32 bytes, got %d", len(lit.Data()))
	}

	// Encoded bytes must match the *big.Int constructor for the same value.
	want := Uint256(big.NewInt(12345)).Data()
	if !bytes.Equal(lit.Data(), want) {
		t.Errorf("Uint256FromU256 encoding mismatch:\n got  %x\n want %x", lit.Data(), want)
	}
}

func TestIsValue(t *testing.T) {
	t.Run("LiteralValue is Value", func(t *testing.T) {
		lit := Uint256(big.NewInt(100))
		if !isValue(lit) {
			t.Error("LiteralValue should be recognized as Value")
		}
	})

	t.Run("ReturnValue is Value", func(t *testing.T) {
		abiType, _ := abi.NewType("uint256", "", nil)
		rv := &ReturnValue{abiType: abiType}
		if !isValue(rv) {
			t.Error("ReturnValue should be recognized as Value")
		}
	})

	t.Run("StateValue is Value", func(t *testing.T) {
		sv := &StateValue{}
		if !isValue(sv) {
			t.Error("StateValue should be recognized as Value")
		}
	})

	t.Run("SubplanValue is Value", func(t *testing.T) {
		spv := &SubplanValue{}
		if !isValue(spv) {
			t.Error("SubplanValue should be recognized as Value")
		}
	})

	t.Run("non-Value types", func(t *testing.T) {
		if isValue("string") {
			t.Error("string should not be Value")
		}
		if isValue(123) {
			t.Error("int should not be Value")
		}
		if isValue(nil) {
			t.Error("nil should not be Value")
		}
	})
}

func TestLiteralValueDataEncoding(t *testing.T) {
	t.Run("uint256 encoding", func(t *testing.T) {
		lit := Uint256(big.NewInt(256))
		data := lit.Data()

		if len(data) != 32 {
			t.Errorf("Expected 32 bytes, got %d", len(data))
		}

		// 256 = 0x100, should be at the end of the 32-byte encoding
		if data[31] != 0x00 || data[30] != 0x01 {
			t.Error("Incorrect encoding for value 256")
		}
	})

	t.Run("address encoding", func(t *testing.T) {
		addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		lit := Address(addr)
		data := lit.Data()

		if len(data) != 32 {
			t.Errorf("Expected 32 bytes, got %d", len(data))
		}

		// Address is right-padded in ABI encoding (last 20 bytes)
		for i := 0; i < 12; i++ {
			if data[i] != 0 {
				t.Errorf("Expected zero padding at byte %d", i)
			}
		}
	})

	t.Run("bool true encoding", func(t *testing.T) {
		lit := Bool(true)
		data := lit.Data()

		if len(data) != 32 {
			t.Errorf("Expected 32 bytes, got %d", len(data))
		}

		if data[31] != 1 {
			t.Error("Expected 1 for true")
		}
	})

	t.Run("bool false encoding", func(t *testing.T) {
		lit := Bool(false)
		data := lit.Data()

		if data[31] != 0 {
			t.Error("Expected 0 for false")
		}
	})
}

func TestDynamicTypeDataEncoding(t *testing.T) {
	t.Run("string skips offset", func(t *testing.T) {
		lit := String("hello")
		data := lit.Data()

		// For dynamic types, the offset (first 32 bytes) is skipped
		// The data should start with length, then the actual string
		if len(data) < 32 {
			t.Error("String data too short")
		}
	})

	t.Run("bytes skips offset", func(t *testing.T) {
		lit := Bytes([]byte{0x01, 0x02, 0x03})
		data := lit.Data()

		if len(data) < 32 {
			t.Error("Bytes data too short")
		}
	})
}

// staticTupleType returns the same shape as Uniswap V3's exactInputSingle
// params: (address, address, uint24, address, uint256, uint256, uint160).
// 7 static fields — the canonical bug repro.
func staticTupleType(t *testing.T) abi.Type {
	t.Helper()
	abiType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "tokenIn", Type: "address"},
		{Name: "tokenOut", Type: "address"},
		{Name: "fee", Type: "uint24"},
		{Name: "recipient", Type: "address"},
		{Name: "amountIn", Type: "uint256"},
		{Name: "amountOutMinimum", Type: "uint256"},
		{Name: "sqrtPriceLimitX96", Type: "uint160"},
	})
	if err != nil {
		t.Fatalf("staticTupleType: %v", err)
	}
	return abiType
}

func TestTupleValue(t *testing.T) {
	addrA := common.HexToAddress("0x000000000000000000000000000000000000000A")
	addrB := common.HexToAddress("0x000000000000000000000000000000000000000B")
	addrC := common.HexToAddress("0x000000000000000000000000000000000000000C")

	t.Run("construction is unbound", func(t *testing.T) {
		tv := Tuple(Address(addrA), big.NewInt(123))
		if tv.Children() != nil {
			t.Error("Children() should be nil before bind")
		}
		// Type() returns the zero abi.Type before bind.
		if tv.Type().T != 0 || len(tv.Type().TupleElems) != 0 {
			t.Error("Type() should be zero-value before bind")
		}
		if tv.IsDynamic() {
			t.Error("TupleValue.IsDynamic() should be false (v1 static-only)")
		}
	})

	t.Run("binds against matching tuple type", func(t *testing.T) {
		tt := staticTupleType(t)
		tv := Tuple(
			Address(addrA),
			Address(addrB),
			MustLiteralFromType("uint24", big.NewInt(500)),
			Address(addrC),
			Uint256(big.NewInt(1_000_000)),
			Uint256(big.NewInt(0)),
			MustLiteralFromType("uint160", big.NewInt(0)),
		)
		if err := tv.bind(tt); err != nil {
			t.Fatalf("bind: %v", err)
		}
		if got := len(tv.Children()); got != 7 {
			t.Fatalf("Children() len = %d, want 7", got)
		}
		if tv.Type().String() != tt.String() {
			t.Errorf("Type() = %s, want %s", tv.Type().String(), tt.String())
		}
	})

	t.Run("rejects non-tuple expected type", func(t *testing.T) {
		nonTuple, _ := abi.NewType("uint256", "", nil)
		tv := Tuple(big.NewInt(1))
		err := tv.bind(nonTuple)
		if err == nil {
			t.Fatal("expected error binding to non-tuple")
		}
		if _, ok := err.(*TypeMismatchError); !ok {
			t.Errorf("expected *TypeMismatchError, got %T: %v", err, err)
		}
	})

	t.Run("rejects field count mismatch", func(t *testing.T) {
		tt := staticTupleType(t) // 7 fields
		tv := Tuple(Address(addrA), big.NewInt(1))
		if err := tv.bind(tt); err == nil {
			t.Fatal("expected field-count mismatch error")
		}
	})

	t.Run("rejects dynamic-leaf field", func(t *testing.T) {
		dynLeaf, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "uint256"},
			{Name: "b", Type: "string"}, // dynamic
		})
		if err != nil {
			t.Fatalf("type: %v", err)
		}
		tv := Tuple(big.NewInt(1), "hello")
		err = tv.bind(dynLeaf)
		if err == nil {
			t.Fatal("expected error for dynamic leaf")
		}
		if !errorIs(err, ErrInvalidTupleField) {
			t.Errorf("expected ErrInvalidTupleField, got: %v", err)
		}
	})

	t.Run("rejects ReturnValue leaf", func(t *testing.T) {
		// Construct a *ReturnValue manually (in-package).
		rvType, _ := abi.NewType("uint256", "", nil)
		rv := &ReturnValue{command: &Command{}, abiType: rvType}

		// Two-field static tuple of (uint256, uint256).
		tt, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "uint256"},
			{Name: "b", Type: "uint256"},
		})
		tv := Tuple(big.NewInt(1), rv)
		err := tv.bind(tt)
		if err == nil {
			t.Fatal("expected error for ReturnValue leaf")
		}
		if !errorIs(err, ErrInvalidTupleField) {
			t.Errorf("expected ErrInvalidTupleField, got: %v", err)
		}
	})

	t.Run("binds nested static tuple", func(t *testing.T) {
		// outer: (uint256, (address, uint24))
		inner, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "address"},
			{Name: "b", Type: "uint24"},
		})
		outer, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "x", Type: "uint256"},
			{Name: "inner", Type: "tuple", Components: []abi.ArgumentMarshaling{
				{Name: "a", Type: "address"},
				{Name: "b", Type: "uint24"},
			}},
		})
		_ = inner

		tv := Tuple(
			big.NewInt(42),
			Tuple(Address(addrA), MustLiteralFromType("uint24", big.NewInt(3000))),
		)
		if err := tv.bind(outer); err != nil {
			t.Fatalf("bind: %v", err)
		}
		if len(tv.Children()) != 2 {
			t.Fatalf("outer Children len = %d, want 2", len(tv.Children()))
		}
		innerChild, ok := tv.Children()[1].(*TupleValue)
		if !ok {
			t.Fatalf("inner child should be *TupleValue, got %T", tv.Children()[1])
		}
		if len(innerChild.Children()) != 2 {
			t.Errorf("inner Children len = %d, want 2", len(innerChild.Children()))
		}
	})

	t.Run("Data panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic from TupleValue.Data()")
			}
		}()
		tv := Tuple(big.NewInt(1))
		_ = tv.Data()
	})

	t.Run("idempotent bind same type", func(t *testing.T) {
		tt := staticTupleType(t)
		tv := Tuple(
			Address(addrA),
			Address(addrB),
			MustLiteralFromType("uint24", big.NewInt(500)),
			Address(addrC),
			Uint256(big.NewInt(1)),
			Uint256(big.NewInt(0)),
			MustLiteralFromType("uint160", big.NewInt(0)),
		)
		if err := tv.bind(tt); err != nil {
			t.Fatalf("first bind: %v", err)
		}
		if err := tv.bind(tt); err != nil {
			t.Errorf("second bind with same type should be no-op, got: %v", err)
		}
	})
}

func TestNewLiteralStrictGuard(t *testing.T) {
	t.Run("static tuple of static fields rejected", func(t *testing.T) {
		// Two-field static tuple — packs to 64 bytes, > 32-byte slot.
		tt, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "uint256"},
			{Name: "b", Type: "uint256"},
		})
		// A Go struct shape that geth's ABI Pack accepts for this tuple.
		val := struct {
			A *big.Int
			B *big.Int
		}{big.NewInt(1), big.NewInt(2)}

		_, err := NewLiteral(tt, val)
		if err == nil {
			t.Fatal("expected ErrStaticTupleTooLarge")
		}
		if !errorIs(err, ErrStaticTupleTooLarge) {
			t.Errorf("expected ErrStaticTupleTooLarge, got: %v", err)
		}
	})

	t.Run("dynamic tuple still works", func(t *testing.T) {
		tt, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "a", Type: "uint256"},
			{Name: "b", Type: "string"},
		})
		val := struct {
			A *big.Int
			B string
		}{big.NewInt(1), "hello"}

		lit, err := NewLiteral(tt, val)
		if err != nil {
			t.Fatalf("dynamic tuple should succeed, got: %v", err)
		}
		if !lit.IsDynamic() {
			t.Error("dynamic tuple LiteralValue should report IsDynamic=true")
		}
	})
}

// errorIs walks the error chain looking for target. Avoids importing
// errors here; matches the in-package style of comparing sentinels.
func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		return false
	}
	return false
}

func TestToValueTuple(t *testing.T) {
	tt := staticTupleType(t)

	t.Run("binds *TupleValue against expected tuple type", func(t *testing.T) {
		addr := common.HexToAddress("0x000000000000000000000000000000000000000A")
		tv := Tuple(
			Address(addr),
			Address(addr),
			MustLiteralFromType("uint24", big.NewInt(500)),
			Address(addr),
			Uint256(big.NewInt(1)),
			Uint256(big.NewInt(0)),
			MustLiteralFromType("uint160", big.NewInt(0)),
		)
		got, err := toValue(tv, tt)
		if err != nil {
			t.Fatalf("toValue: %v", err)
		}
		gotTV, ok := got.(*TupleValue)
		if !ok {
			t.Fatalf("expected *TupleValue, got %T", got)
		}
		if len(gotTV.Children()) != 7 {
			t.Errorf("Children() len = %d, want 7", len(gotTV.Children()))
		}
	})

	t.Run("propagates bind errors", func(t *testing.T) {
		nonTuple, _ := abi.NewType("uint256", "", nil)
		tv := Tuple(big.NewInt(1))
		_, err := toValue(tv, nonTuple)
		if err == nil {
			t.Fatal("expected error binding TupleValue to non-tuple type")
		}
	})
}