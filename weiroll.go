// Package weiroll provides a Go implementation of the weiroll command planner
// for Ethereum smart contract operation chaining.
//
// Weiroll is a virtual machine that batches Ethereum operations via a scripted
// command sequence. This library allows you to build plans that:
//   - Chain multiple contract calls into a single atomic transaction
//   - Pass return values between operations without separate transactions
//   - Support CALL, STATICCALL, and CALL_WITH_VALUE
//
// # Basic Usage
//
// Create a planner, add contract calls, and compile:
//
//	// Parse contract ABIs
//	mathABI := weiroll.MustParseABI(mathABIJSON)
//	tokenABI := weiroll.MustParseABI(tokenABIJSON)
//
//	// Wrap contracts (all calls go through CALL).
//	mathLib := weiroll.NewContract(mathAddr, mathABI)
//	token := weiroll.NewContract(tokenAddr, tokenABI)
//
//	// Build plan
//	planner := weiroll.New()
//
//	sum := planner.Add(mathLib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
//	product := planner.Add(mathLib.MustInvoke("multiply", sum, big.NewInt(10)))
//	planner.Add(token.MustInvoke("transfer", recipient, product))
//
//	// Compile
//	plan, err := planner.Plan()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Execute via weiroll VM contract
//	commands := plan.CommandsAsBytes32()
//	state := plan.StateAsBytes()
//
// # Contract Types
//
// All contracts are wrapped with NewContract(). The default is CALL; pass
// WithStaticCalls() for STATICCALL. The VM has no DELEGATECALL dispatcher
// branch — commands emitting FLAG_CT_DELEGATECALL (0x00) revert with
// "Invalid calltype".
//
// # Value Types
//
// Values in weiroll can be:
//
//   - Literals: Constant values known at planning time (created automatically
//     from Go values or explicitly with Uint256(), Address(), etc.)
//
//   - Return Values: Outputs from previous commands, returned by Planner.Add()
//
//   - State Values: References to the planner's state array, for subplans
//
// # Command Encoding
//
// Commands are encoded as 32-byte (standard) or 64-byte (extended) packed
// structures. Standard commands support up to 6 arguments; extended commands
// support up to 32 arguments.
//
// # State Management
//
// The planner automatically manages state slots:
//
//   - Literal deduplication: Identical values share slots
//   - Slot recycling: Slots are reused after their last usage
//   - Dynamic type handling: Proper encoding for strings, bytes, and arrays
//
// # References
//
// For more information about the weiroll VM, see:
//   - https://github.com/weiroll/weiroll (Solidity VM implementation)
//   - https://github.com/weiroll/weiroll.js (JavaScript planner)
package weiroll
