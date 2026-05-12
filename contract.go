package weiroll

import (
	"io"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ContractType specifies how the contract's methods will be called.
type ContractType uint8

const (
	// External contracts are called via CALL.
	External ContractType = iota

	// StaticExternal contracts are called via STATICCALL.
	StaticExternal
)

// Contract wraps an Ethereum contract for use with the weiroll planner.
type Contract struct {
	address      common.Address
	abi          abi.ABI
	contractType ContractType
}

// ContractOption configures a Contract.
type ContractOption func(*Contract)

// WithStaticCalls sets the contract to use STATICCALL by default.
func WithStaticCalls() ContractOption {
	return func(c *Contract) {
		c.contractType = StaticExternal
	}
}

// NewContract creates a Contract wrapper for external contracts.
// External contracts are called via CALL (or STATICCALL with WithStaticCalls option).
func NewContract(address common.Address, contractABI abi.ABI, opts ...ContractOption) *Contract {
	c := &Contract{
		address:      address,
		abi:          contractABI,
		contractType: External,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Address returns the contract address.
func (c *Contract) Address() common.Address {
	return c.address
}

// ABI returns the contract ABI.
func (c *Contract) ABI() abi.ABI {
	return c.abi
}

// Type returns the contract type.
func (c *Contract) Type() ContractType {
	return c.contractType
}

// Invoke creates a Call for the named method with the given arguments.
// Arguments can be Go values (converted to LiteralValue) or Value types.
func (c *Contract) Invoke(methodName string, args ...any) (*Call, error) {
	method, ok := c.abi.Methods[methodName]
	if !ok {
		return nil, &MethodNotFoundError{Contract: c.address, Method: methodName}
	}

	return newCall(c, method, args)
}

// MustInvoke is like Invoke but panics on error.
func (c *Contract) MustInvoke(methodName string, args ...any) *Call {
	call, err := c.Invoke(methodName, args...)
	if err != nil {
		panic(err)
	}
	return call
}

// HasMethod returns true if the contract has a method with the given name.
func (c *Contract) HasMethod(methodName string) bool {
	_, ok := c.abi.Methods[methodName]
	return ok
}

// MethodNames returns all method names in the contract ABI.
func (c *Contract) MethodNames() []string {
	names := make([]string, 0, len(c.abi.Methods))
	for name := range c.abi.Methods {
		names = append(names, name)
	}
	return names
}

// defaultFlags returns the default call flags based on contract type.
func (c *Contract) defaultFlags() CallFlags {
	switch c.contractType {
	case StaticExternal:
		return FlagStaticCall
	default:
		return FlagCall
	}
}

// ParseABI parses a JSON ABI string into an abi.ABI.
// This is a convenience function for creating contracts from ABI JSON.
func ParseABI(abiJSON string) (abi.ABI, error) {
	return abi.JSON(strings.NewReader(abiJSON))
}

// MustParseABI is like ParseABI but panics on error.
func MustParseABI(abiJSON string) abi.ABI {
	parsed, err := ParseABI(abiJSON)
	if err != nil {
		panic(err)
	}
	return parsed
}

// Ensure io package is used (for documentation and future use).
var _ io.Reader = (*strings.Reader)(nil)
