package weiroll

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// bytesABIType is the ABI type used for RawReturn return slots, where
// the VM stores the entire returndata as a length-prefixed bytes blob.
var bytesABIType = func() abi.Type {
	t, err := abi.NewType("bytes", "", nil)
	if err != nil {
		panic(err)
	}
	return t
}()

// Call represents a pending contract call that can be added to a Planner.
// Call is immutable - modifier methods return new instances.
type Call struct {
	contract  *Contract
	method    abi.Method
	args      []Value
	flags     CallFlags
	value     *big.Int // ETH value for CALL_WITH_VALUE
	rawReturn bool     // Wrap return as raw bytes
}

// newCall creates a Call from a contract, method, and arguments.
// Arguments are converted to Values using the method's input types.
func newCall(contract *Contract, method abi.Method, rawArgs []any) (*Call, error) {
	if len(rawArgs) != len(method.Inputs) {
		return nil, &ArgumentError{
			Method: method.Name,
			Index:  len(rawArgs),
			Err:    ErrTooManyArguments,
		}
	}

	args := make([]Value, len(rawArgs))

	for i, arg := range rawArgs {
		val, err := toValue(arg, method.Inputs[i].Type)
		if err != nil {
			return nil, &ArgumentError{
				Method: method.Name,
				Index:  i,
				Err:    err,
			}
		}
		args[i] = val
	}

	return &Call{
		contract:  contract,
		method:    method,
		args:      args,
		flags:     contract.defaultFlags(),
		value:     nil,
		rawReturn: false,
	}, nil
}

// Contract returns the target contract for this call.
func (c *Call) Contract() *Contract {
	return c.contract
}

// Method returns the ABI method for this call.
func (c *Call) Method() abi.Method {
	return c.method
}

// Args returns the arguments for this call.
func (c *Call) Args() []Value {
	return c.args
}

// Flags returns the call flags.
func (c *Call) Flags() CallFlags {
	return c.flags
}

// EthValue returns the ETH value for this call (nil if none).
func (c *Call) EthValue() *big.Int {
	return c.value
}

// HasReturnValue returns true if the method has a return value.
func (c *Call) HasReturnValue() bool {
	return len(c.method.Outputs) > 0
}

// ReturnType returns the ABI type of the first ABI-declared return value,
// if any. This reflects the contract ABI as written; it does NOT account
// for RawReturn. Use EffectiveReturnType for the type actually stored in
// the weiroll return slot.
func (c *Call) ReturnType() *abi.Type {
	if len(c.method.Outputs) == 0 {
		return nil
	}
	return &c.method.Outputs[0].Type
}

// EffectiveReturnType returns the ABI type of the value that the weiroll
// VM stores in the return slot for this call. When RawReturn is set the
// VM captures the entire returndata as a length-prefixed bytes blob, so
// this returns bytes (dynamic). Otherwise it returns the first ABI output.
func (c *Call) EffectiveReturnType() *abi.Type {
	if !c.HasReturnValue() {
		return nil
	}
	if c.rawReturn {
		return &bytesABIType
	}
	return &c.method.Outputs[0].Type
}

// Selector returns the 4-byte function selector.
func (c *Call) Selector() [4]byte {
	var sel [4]byte
	copy(sel[:], c.method.ID[:4])
	return sel
}

// WithValue attaches ETH value to the call.
// This converts the call to CALL_WITH_VALUE.
// Only valid for external (non-library) contracts.
//
// Returns a new Call with the value set.
func (c *Call) WithValue(amount *big.Int) *Call {
	clone := c.clone()
	clone.value = new(big.Int).Set(amount)
	clone.flags = (clone.flags &^ FlagCallTypeMask) | FlagCallWithValue
	return clone
}

// Static forces the call to use STATICCALL.
// Only valid for external contracts (not libraries).
//
// Returns a new Call with STATICCALL flag set.
func (c *Call) Static() *Call {
	clone := c.clone()
	clone.flags = (clone.flags &^ FlagCallTypeMask) | FlagStaticCall
	return clone
}

// RawReturn captures the entire returndata as a length-prefixed bytes
// blob in the return slot. Use this for methods with multiple ABI
// return values (tuples) so a downstream extraction helper can read
// individual fields. After RawReturn, the *ReturnValue produced by
// Planner.Add is typed as bytes (dynamic) and pipes correctly into any
// argument expecting bytes; reinterpret it with ReturnValue.As if the
// extractor expects a different dynamic type, or chain a typed cast
// helper for static fields.
//
// Returns a new Call with the tuple return flag set.
func (c *Call) RawReturn() *Call {
	clone := c.clone()
	clone.rawReturn = true
	clone.flags |= FlagTupleReturn
	return clone
}

// clone creates a shallow copy of the Call.
func (c *Call) clone() *Call {
	clone := *c
	// Deep copy the args slice
	clone.args = make([]Value, len(c.args))
	copy(clone.args, c.args)
	return &clone
}

// validate checks if the Call is valid for its call type.
func (c *Call) validate() error {
	callType := c.flags.CallType()

	// Value transfer only valid for CALL_WITH_VALUE. This subsumes the
	// "STATICCALL can't send value" check because FlagStaticCall (0x02) is
	// not FlagCallWithValue (0x03).
	if c.value != nil && c.value.Sign() > 0 && callType != FlagCallWithValue {
		return ErrInvalidCallType
	}

	return nil
}

// computeFlags computes the final flags for encoding.
func (c *Call) computeFlags(isExtended bool) CallFlags {
	flags := c.flags
	if isExtended {
		flags |= FlagExtendedCommand
	}
	if c.rawReturn {
		flags |= FlagTupleReturn
	}
	return flags
}
