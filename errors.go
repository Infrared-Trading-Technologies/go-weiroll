// Package weiroll provides a Go implementation of the weiroll command planner
// for Ethereum smart contract operation chaining.
package weiroll

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// Sentinel errors for common failure conditions.
var (
	// ErrCyclicPlanner indicates a planner references itself through subplans.
	ErrCyclicPlanner = errors.New("weiroll: cyclic planner reference detected")

	// ErrInvalidSubplan indicates the subplan call doesn't meet requirements.
	ErrInvalidSubplan = errors.New("weiroll: invalid subplan configuration")

	// ErrSlotExhausted indicates the state slot limit (127) was exceeded.
	ErrSlotExhausted = errors.New("weiroll: state slot limit exceeded (max 127)")

	// ErrTooManyArguments indicates a function has too many arguments.
	ErrTooManyArguments = errors.New("weiroll: too many arguments (max 32 for extended commands)")

	// ErrReturnValueNotVisible indicates a return value was used before it was created.
	ErrReturnValueNotVisible = errors.New("weiroll: return value not visible at this point")

	// ErrInvalidCallType indicates an operation isn't valid for the call type.
	ErrInvalidCallType = errors.New("weiroll: invalid operation for this call type")

	// ErrNoReturnValue indicates the function has no return value to capture.
	ErrNoReturnValue = errors.New("weiroll: function has no return value")

	// ErrStaticTupleTooLarge indicates a static tuple was packed into a
	// single state slot and exceeds the 32-byte slot size enforced by the
	// on-chain VM. Use weiroll.Tuple(field1, field2, ...) to flatten the
	// tuple into per-field state slots.
	ErrStaticTupleTooLarge = errors.New(
		"weiroll: static tuple exceeds 32-byte slot — use weiroll.Tuple(field1, field2, ...) to expand into per-field state slots")

	// ErrInvalidTupleField indicates a leaf inside weiroll.Tuple has an
	// unsupported type. Static literals and static-typed *ReturnValue
	// are supported; dynamic-typed values, *StateValue, and
	// *SubplanValue are rejected.
	ErrInvalidTupleField = errors.New(
		"weiroll: invalid leaf type inside weiroll.Tuple (supports static literals and static-typed *ReturnValue)")
)

// MethodNotFoundError indicates the contract doesn't have the requested method.
type MethodNotFoundError struct {
	Contract common.Address
	Method   string
}

func (e *MethodNotFoundError) Error() string {
	return fmt.Sprintf("weiroll: method %q not found in contract %s", e.Method, e.Contract.Hex())
}

// ArgumentError indicates an issue with a function argument.
type ArgumentError struct {
	Method string
	Index  int
	Err    error
}

func (e *ArgumentError) Error() string {
	return fmt.Sprintf("weiroll: argument %d for method %q: %v", e.Index, e.Method, e.Err)
}

func (e *ArgumentError) Unwrap() error {
	return e.Err
}

// TypeMismatchError indicates a value's type doesn't match the expected parameter type.
type TypeMismatchError struct {
	Expected string
	Got      string
}

func (e *TypeMismatchError) Error() string {
	return fmt.Sprintf("weiroll: type mismatch: expected %s, got %s", e.Expected, e.Got)
}

// PlanError wraps errors that occur during planning.
type PlanError struct {
	CommandIndex int
	Method       string
	Err          error
}

func (e *PlanError) Error() string {
	if e.Method != "" {
		return fmt.Sprintf("weiroll: command %d (%s): %v", e.CommandIndex, e.Method, e.Err)
	}
	return fmt.Sprintf("weiroll: command %d: %v", e.CommandIndex, e.Err)
}

func (e *PlanError) Unwrap() error {
	return e.Err
}

// EncodingError indicates a failure during value or command encoding.
type EncodingError struct {
	Value any
	Err   error
}

func (e *EncodingError) Error() string {
	return fmt.Sprintf("weiroll: encoding error for value %T: %v", e.Value, e.Err)
}

func (e *EncodingError) Unwrap() error {
	return e.Err
}
