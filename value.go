package weiroll

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Value represents any value that can be used in weiroll commands.
// This is a sealed interface - only types within this package can implement it.
type Value interface {
	// isValue is unexported to seal the interface.
	isValue()

	// IsDynamic returns true if this value has a dynamic ABI type
	// (string, bytes, arrays, or tuples containing dynamic types).
	IsDynamic() bool

	// Type returns the ABI type of this value.
	Type() abi.Type

	// Data returns the ABI-encoded data for this value.
	// For ReturnValue, this returns nil as the data is determined at runtime.
	Data() []byte
}

// LiteralValue represents a constant value known at planning time.
type LiteralValue struct {
	abiType abi.Type
	data    []byte
}

func (v *LiteralValue) isValue() {}

// IsDynamic returns true if the literal has a dynamic ABI type.
func (v *LiteralValue) IsDynamic() bool {
	return isDynamicType(v.abiType)
}

// Type returns the ABI type of this literal.
func (v *LiteralValue) Type() abi.Type {
	return v.abiType
}

// Data returns the ABI-encoded data.
func (v *LiteralValue) Data() []byte {
	return v.data
}

// ReturnValue represents the output of a previously added command.
type ReturnValue struct {
	command *Command
	abiType abi.Type
	index   int // For multi-return functions, index into outputs
}

func (v *ReturnValue) isValue() {}

// IsDynamic returns true if the return value has a dynamic ABI type.
func (v *ReturnValue) IsDynamic() bool {
	return isDynamicType(v.abiType)
}

// Type returns the ABI type of this return value.
func (v *ReturnValue) Type() abi.Type {
	return v.abiType
}

// Data returns nil for return values (data is determined at runtime).
func (v *ReturnValue) Data() []byte {
	return nil
}

// Command returns the command that produces this return value.
func (v *ReturnValue) Command() *Command {
	return v.command
}

// StateValue represents the current planner state array.
// Used for subplan integration where the state needs to be passed to callbacks.
type StateValue struct {
	planner *Planner
}

func (v *StateValue) isValue() {}

// IsDynamic returns true (state is always bytes[]).
func (v *StateValue) IsDynamic() bool {
	return true
}

// Type returns the ABI type for bytes[].
func (v *StateValue) Type() abi.Type {
	// bytes[] type
	t, _ := abi.NewType("bytes[]", "", nil)
	return t
}

// Data returns nil (state data is determined at runtime).
func (v *StateValue) Data() []byte {
	return nil
}

// SubplanValue wraps a nested Planner for use as an argument.
type SubplanValue struct {
	subplanner *Planner
}

func (v *SubplanValue) isValue() {}

// IsDynamic returns true (subplan is encoded as bytes32[]).
func (v *SubplanValue) IsDynamic() bool {
	return true
}

// Type returns the ABI type for bytes32[].
func (v *SubplanValue) Type() abi.Type {
	t, _ := abi.NewType("bytes32[]", "", nil)
	return t
}

// Data returns nil (subplan data is built during planning).
func (v *SubplanValue) Data() []byte {
	return nil
}

// Planner returns the nested planner.
func (v *SubplanValue) Planner() *Planner {
	return v.subplanner
}

// isDynamicType checks if an ABI type is dynamic (variable-length encoding).
func isDynamicType(t abi.Type) bool {
	switch t.T {
	case abi.StringTy, abi.BytesTy, abi.SliceTy:
		return true
	case abi.ArrayTy:
		return isDynamicType(*t.Elem)
	case abi.TupleTy:
		for _, elem := range t.TupleElems {
			if isDynamicType(*elem) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// NewLiteral creates a literal value from a Go value.
// Supported types:
//   - *big.Int, int64, uint64 (for uint256/int256)
//   - common.Address (for address)
//   - [N]byte (for bytesN)
//   - []byte (for bytes)
//   - string (for string)
//   - bool (for bool)
//   - common.Hash (for bytes32)
func NewLiteral(abiType abi.Type, value any) (*LiteralValue, error) {
	args := abi.Arguments{{Type: abiType}}

	// Handle special conversions
	convertedValue := convertToABIType(value, abiType)

	data, err := args.Pack(convertedValue)
	if err != nil {
		return nil, &EncodingError{Value: value, Err: err}
	}

	// For dynamic types, skip the offset prefix (first 32 bytes)
	if isDynamicType(abiType) && len(data) > 32 {
		data = data[32:]
	}

	return &LiteralValue{
		abiType: abiType,
		data:    data,
	}, nil
}

// MustLiteral is like NewLiteral but panics on error.
// Use only with compile-time constant values.
func MustLiteral(abiType abi.Type, value any) *LiteralValue {
	v, err := NewLiteral(abiType, value)
	if err != nil {
		panic(err)
	}
	return v
}

// NewLiteralFromType creates a literal using an ABI type string.
// Example types: "uint256", "address", "bytes32", "string", "bool"
func NewLiteralFromType(typeStr string, value any) (*LiteralValue, error) {
	abiType, err := abi.NewType(typeStr, "", nil)
	if err != nil {
		return nil, &EncodingError{Value: value, Err: err}
	}
	return NewLiteral(abiType, value)
}

// MustLiteralFromType is like NewLiteralFromType but panics on error.
func MustLiteralFromType(typeStr string, value any) *LiteralValue {
	v, err := NewLiteralFromType(typeStr, value)
	if err != nil {
		panic(err)
	}
	return v
}

// convertToABIType handles common Go type conversions for ABI encoding.
func convertToABIType(value any, abiType abi.Type) any {
	switch v := value.(type) {
	case int:
		return big.NewInt(int64(v))
	case int64:
		return big.NewInt(v)
	case uint64:
		return new(big.Int).SetUint64(v)
	case int32:
		return big.NewInt(int64(v))
	case uint32:
		return new(big.Int).SetUint64(uint64(v))
	case *uint256.Int:
		if v == nil {
			return (*big.Int)(nil)
		}
		return v.ToBig()
	default:
		return v
	}
}

// Uint256 creates a uint256 literal from a *big.Int.
func Uint256(v *big.Int) *LiteralValue {
	return MustLiteralFromType("uint256", v)
}

// Uint256FromU256 creates a uint256 literal from a *uint256.Int.
func Uint256FromU256(v *uint256.Int) *LiteralValue {
	return MustLiteralFromType("uint256", v)
}

// Int256 creates an int256 literal from a *big.Int.
func Int256(v *big.Int) *LiteralValue {
	return MustLiteralFromType("int256", v)
}

// Address creates an address literal from a common.Address.
func Address(v common.Address) *LiteralValue {
	return MustLiteralFromType("address", v)
}

// Bytes32 creates a bytes32 literal from a common.Hash or [32]byte.
func Bytes32(v common.Hash) *LiteralValue {
	return MustLiteralFromType("bytes32", v)
}

// Bool creates a bool literal.
func Bool(v bool) *LiteralValue {
	return MustLiteralFromType("bool", v)
}

// String creates a string literal.
func String(v string) *LiteralValue {
	return MustLiteralFromType("string", v)
}

// Bytes creates a bytes literal.
func Bytes(v []byte) *LiteralValue {
	return MustLiteralFromType("bytes", v)
}

// isValue checks if a value implements the Value interface.
func isValue(v any) bool {
	_, ok := v.(Value)
	return ok
}

// toValue converts any value to a Value, creating a LiteralValue if needed.
func toValue(v any, expectedType abi.Type) (Value, error) {
	if val, ok := v.(Value); ok {
		// Type checking
		if val.Type().String() != expectedType.String() {
			return nil, &TypeMismatchError{
				Expected: expectedType.String(),
				Got:      val.Type().String(),
			}
		}
		return val, nil
	}
	return NewLiteral(expectedType, v)
}
