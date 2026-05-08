package weiroll

import (
	"fmt"
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
//
// A ReturnValue is opaque on the Go side: it cannot be indexed, split,
// or operated on arithmetically. To manipulate it, append another
// command (typically a helper-library call) that consumes it. To
// reinterpret it as a different ABI type without an extra command,
// use ReturnValue.As — useful for off-chain casts between types with
// the same slot encoding (e.g., bytes32 -> uint256).
type ReturnValue struct {
	command *Command
	abiType abi.Type
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

// As returns a new ReturnValue that points at the same return slot but
// is typed as abiType. This is an off-chain reinterpretation only — the
// VM stores the same bytes; only the Go-side type metadata changes.
//
// The cast must be encoding-compatible: both source and destination
// must be either static (32-byte slot) or both dynamic (length-prefixed
// bytes slot). Mixing static and dynamic types is rejected because the
// slot encodings differ.
//
// Common uses:
//   - bytes32 -> uint256 / address (all static, 32-byte slot)
//   - bytes -> uint256[] / a tuple-shaped dynamic type (all dynamic)
//
// Use this to skip an on-chain no-op cast (e.g., a deployed
// bytes32-to-uint256 helper) when the bytes already have the right
// shape. For real numeric conversions or unpacking, you still need a
// Solidity helper.
func (v *ReturnValue) As(abiType abi.Type) (*ReturnValue, error) {
	if isDynamicType(v.abiType) != isDynamicType(abiType) {
		return nil, &TypeMismatchError{
			Expected: v.abiType.String(),
			Got:      abiType.String(),
		}
	}
	return &ReturnValue{
		command: v.command,
		abiType: abiType,
	}, nil
}

// MustAs is like As but panics on incompatible casts. Use only when the
// cast is statically known to be valid.
func (v *ReturnValue) MustAs(abiType abi.Type) *ReturnValue {
	rv, err := v.As(abiType)
	if err != nil {
		panic(err)
	}
	return rv
}

// AsType is like As but accepts a type string (e.g., "uint256",
// "address", "bytes").
func (v *ReturnValue) AsType(typeStr string) (*ReturnValue, error) {
	t, err := abi.NewType(typeStr, "", nil)
	if err != nil {
		return nil, &EncodingError{Value: v, Err: err}
	}
	return v.As(t)
}

// MustAsType is like AsType but panics on incompatible casts or invalid
// type strings.
func (v *ReturnValue) MustAsType(typeStr string) *ReturnValue {
	rv, err := v.AsType(typeStr)
	if err != nil {
		panic(err)
	}
	return rv
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

// TupleValue represents a static tuple expanded into per-field state
// slots. Unlike LiteralValue, which occupies a single state slot,
// TupleValue allocates one slot per leaf field at planning time. This
// matches the on-chain VM's invariant that every static slot is 32
// bytes; a fully-static N-field tuple is 32*N bytes total and must be
// split into N slots.
//
// TupleValue is created via Tuple(...) and bound to the expected ABI
// type when used as a function argument. Leaves must be static
// literals or static-typed *ReturnValue (chained from a prior
// command). Dynamic-typed leaves, *StateValue, and *SubplanValue are
// rejected at bind time.
type TupleValue struct {
	abiType  abi.Type
	children []Value // populated by bind; nil before bind
	raw      []any   // input fields; nil after bind
}

func (v *TupleValue) isValue() {}

// IsDynamic returns false. TupleValue v1 only supports static tuples;
// dynamic-leaf tuples are rejected at bind time.
func (v *TupleValue) IsDynamic() bool {
	return false
}

// Type returns the bound ABI tuple type, or the zero abi.Type if the
// value has not yet been bound (i.e. used as an argument).
func (v *TupleValue) Type() abi.Type {
	return v.abiType
}

// Data panics. TupleValue is multi-slot and has no single-slot
// encoding; the planner allocates per-field slots via
// stateManager.getSlotsForValue. A panic surfaces accidental leakage
// into single-slot paths (e.g. literal dedup) loudly instead of
// silently corrupting state.
func (v *TupleValue) Data() []byte {
	panic("weiroll: TupleValue has no single-slot data; the planner allocates per-field slots via getSlotsForValue")
}

// Children returns the bound child values (one per tuple field). Nil
// before bind. Children of a nested static tuple are themselves
// *TupleValue; leaf fields are *LiteralValue or *ReturnValue.
func (v *TupleValue) Children() []Value {
	return v.children
}

// Tuple constructs an unbound TupleValue from a sequence of field
// values. Each field can be either a Value (e.g. *LiteralValue,
// *TupleValue for nesting) or a raw Go value (common.Address, *big.Int,
// etc.) that will be converted against the corresponding ABI tuple
// element type at bind time.
//
// Use Tuple when a contract method takes a fully-static tuple
// parameter — e.g. Uniswap V3's exactInputSingle params struct. Tuple
// expands into one state slot per leaf field, satisfying the VM's
// 32-byte static-slot invariant.
//
// Leaves must be static literals or static-typed *ReturnValue
// (chained from a prior command, e.g. multi-hop V3 swaps where one
// hop's amountOut feeds the next hop's amountIn). Dynamic-typed
// values, *StateValue, and *SubplanValue are rejected at bind time.
// Dynamic tuples (containing any dynamic field) should be passed as
// a single literal via the existing path.
func Tuple(fields ...any) *TupleValue {
	raw := make([]any, len(fields))
	copy(raw, fields)
	return &TupleValue{raw: raw}
}

// MustTuple is like Tuple. Provided for API symmetry with MustLiteral;
// it cannot fail at construction (binding errors surface only when the
// value is used as an argument).
func MustTuple(fields ...any) *TupleValue {
	return Tuple(fields...)
}

// bind resolves the unbound raw fields against expectedType (which
// must be a fully-static tuple type) and populates children. Calling
// bind twice with the same type is a no-op; calling bind on an
// already-bound TupleValue with a different type returns a
// TypeMismatchError.
func (v *TupleValue) bind(expectedType abi.Type) error {
	if v.children != nil {
		if v.abiType.String() != expectedType.String() {
			return &TypeMismatchError{
				Expected: expectedType.String(),
				Got:      v.abiType.String(),
			}
		}
		return nil
	}

	if expectedType.T != abi.TupleTy {
		return &TypeMismatchError{
			Expected: expectedType.String(),
			Got:      "tuple",
		}
	}

	if len(v.raw) != len(expectedType.TupleElems) {
		return fmt.Errorf(
			"weiroll: Tuple field count %d does not match expected tuple arity %d for %s",
			len(v.raw), len(expectedType.TupleElems), expectedType.String(),
		)
	}

	children := make([]Value, len(v.raw))
	for i, rawField := range v.raw {
		elemType := *expectedType.TupleElems[i]

		if isDynamicType(elemType) {
			return &EncodingError{Value: rawField, Err: ErrInvalidTupleField}
		}

		switch f := rawField.(type) {
		case *ReturnValue:
			// elemType is static (passed isDynamicType guard above).
			// Accept iff the ReturnValue's recorded type matches.
			// Mismatches surface as *TypeMismatchError, mirroring
			// toValue's behavior for arbitrary Value args.
			if f.Type().String() != elemType.String() {
				return &TypeMismatchError{
					Expected: elemType.String(),
					Got:      f.Type().String(),
				}
			}
			children[i] = f
		case *StateValue:
			return &EncodingError{Value: f, Err: ErrInvalidTupleField}
		case *SubplanValue:
			return &EncodingError{Value: f, Err: ErrInvalidTupleField}
		case *TupleValue:
			if err := f.bind(elemType); err != nil {
				return err
			}
			children[i] = f
		default:
			child, err := toValue(rawField, elemType)
			if err != nil {
				return err
			}
			children[i] = child
		}
	}

	v.abiType = expectedType
	v.children = children
	v.raw = nil
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

	// A static type that packs to >32 bytes (most commonly a fully-static
	// tuple struct) cannot fit in a single state slot — the on-chain VM
	// asserts every static slot is exactly 32 bytes. Reject here and
	// point the caller at weiroll.Tuple to expand into per-field slots.
	if !isDynamicType(abiType) && len(data) > 32 {
		return nil, &EncodingError{Value: value, Err: ErrStaticTupleTooLarge}
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
	// *TupleValue is checked first: an unbound TupleValue has an empty
	// Type() that would falsely fail the generic type-equality check
	// below. Bind here while expectedType is in scope.
	if tv, ok := v.(*TupleValue); ok {
		if err := tv.bind(expectedType); err != nil {
			return nil, err
		}
		return tv, nil
	}
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
