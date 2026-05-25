package weiroll

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// CommandType specifies the type of command operation.
type CommandType uint8

const (
	// CommandTypeCall is a normal function call.
	CommandTypeCall CommandType = iota

	// CommandTypeRawCall is a state replacement call.
	CommandTypeRawCall

	// CommandTypeSubplan is a nested planner execution.
	CommandTypeSubplan
)

// Command represents a single operation in the plan.
type Command struct {
	call       *Call
	cmdType    CommandType
	returnSlot int // -1 if no return value stored
}

// Call returns the underlying function call.
func (c *Command) Call() *Call {
	return c.call
}

// Type returns the command type.
func (c *Command) Type() CommandType {
	return c.cmdType
}

// Planner builds a sequence of weiroll commands.
type Planner struct {
	commands []*Command
	parent   *Planner // For subplan validation and cycle detection
}

// New creates a new Planner with the given options.
func New(opts ...PlannerOption) *Planner {
	p := &Planner{
		commands: make([]*Command, 0, 16),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Add adds a function call to the plan and returns its return value (if any).
// Returns nil if the function has no return value.
//
// The returned *ReturnValue is typed as the call's effective return type:
// the first ABI output, or bytes if RawReturn was set on the call.
func (p *Planner) Add(call *Call) *ReturnValue {
	cmd := &Command{
		call:       call,
		cmdType:    CommandTypeCall,
		returnSlot: -1,
	}
	p.commands = append(p.commands, cmd)

	if !call.HasReturnValue() {
		return nil
	}

	return &ReturnValue{
		command: cmd,
		abiType: *call.EffectiveReturnType(),
	}
}

// AddRawCall adds a value-bearing call whose calldata is the raw bytes in `data`
// (or empty for a receive() invocation). The call bypasses ABI binding entirely:
// no Contract or Method is required, the selector field of the encoded command
// is zero, and the VM dispatcher delivers `data` verbatim to the target via the
// FLAG_DATA path. Pass nil or an empty slice to forward ETH to a receive()-only
// target.
//
// The returned *ReturnValue is nil because raw calls declare no ABI return
// type. To capture returndata, build the call with WithRawCalldata().RawReturn()
// via a normal Contract/Invoke flow and use Planner.Add.
func (p *Planner) AddRawCall(target common.Address, value *big.Int, data []byte) *ReturnValue {
	if value == nil {
		value = big.NewInt(0)
	}
	call := &Call{
		contract: &Contract{
			address:      target,
			contractType: External,
		},
		method:      abi.Method{ID: make([]byte, 4)},
		flags:       FlagCallWithValue | FlagData,
		value:       new(big.Int).Set(value),
		rawCalldata: RawBytes(data),
	}
	return p.Add(call)
}

// AddRawCallV adds a value-bearing call whose ETH value is sourced at runtime
// from a prior command's return slot, and whose calldata is the raw bytes in
// `data` (or empty for a receive() invocation). This is the ref-piped sibling
// of AddRawCall: use it when the amount to forward isn't known at plan-build
// time and must be computed by an earlier command (e.g., a balance read).
//
// `valueRef` must be non-nil and must reference a static (32-byte) return slot.
// Dynamic-typed return values (bytes, string, dynamic arrays) are rejected at
// Plan() time because the VM requires the value slot to be exactly 32 bytes
// (see VM.sol's VALUECALL branch).
//
// Pass nil or an empty slice for `data` to forward ETH to a receive()-only
// target. The returned *ReturnValue is nil — raw calls declare no ABI return
// type.
func (p *Planner) AddRawCallV(target common.Address, valueRef *ReturnValue, data []byte) *ReturnValue {
	call := &Call{
		contract: &Contract{
			address:      target,
			contractType: External,
		},
		method:      abi.Method{ID: make([]byte, 4)},
		flags:       FlagCallWithValue | FlagData,
		valueRef:    valueRef,
		rawCalldata: RawBytes(data),
	}
	return p.Add(call)
}

// AddSubplan adds a subplan execution for callbacks like flash loans.
// The call must accept a bytes32[] argument for the subplan commands
// and may accept a bytes[] argument for the state.
func (p *Planner) AddSubplan(call *Call, subplanner *Planner) (*ReturnValue, error) {
	if err := validateSubplan(call, subplanner); err != nil {
		return nil, err
	}

	// Check for cycles
	if err := p.checkCycle(subplanner); err != nil {
		return nil, err
	}

	// Mark subplan's parent for cycle detection
	subplanner.parent = p

	cmd := &Command{
		call:       call,
		cmdType:    CommandTypeSubplan,
		returnSlot: -1,
	}
	p.commands = append(p.commands, cmd)

	if !call.HasReturnValue() {
		return nil, nil
	}

	return &ReturnValue{
		command: cmd,
		abiType: *call.EffectiveReturnType(),
	}, nil
}

// ReplaceState adds a call that replaces the planner state.
// The function must return bytes[].
func (p *Planner) ReplaceState(call *Call) error {
	if !call.HasReturnValue() {
		return ErrNoReturnValue
	}

	retType := call.ReturnType()
	if retType.String() != "bytes[]" {
		return &TypeMismatchError{Expected: "bytes[]", Got: retType.String()}
	}

	cmd := &Command{
		call:       call,
		cmdType:    CommandTypeRawCall,
		returnSlot: -1,
	}
	p.commands = append(p.commands, cmd)
	return nil
}

// State returns a StateValue for use in subplan calls.
func (p *Planner) State() *StateValue {
	return &StateValue{planner: p}
}

// Subplan returns a SubplanValue for use in function calls.
func (p *Planner) Subplan() *SubplanValue {
	return &SubplanValue{subplanner: p}
}

// Len returns the number of commands in the planner.
func (p *Planner) Len() int {
	return len(p.commands)
}

// CommandAt returns the command at the given index.
func (p *Planner) CommandAt(i int) *Command {
	if i < 0 || i >= len(p.commands) {
		return nil
	}
	return p.commands[i]
}

// Clone returns a deep copy of the Planner. The clone is independent: calling
// Plan() on it does not mutate the original, and adding/removing commands on
// the clone is isolated. ReturnValue, StateValue, and SubplanValue references
// are rewired to point at the cloned commands and planners. Subplans reachable
// through SubplanValue or StateValue are cloned recursively (with shared
// references preserved across the cloned tree).
//
// Contracts, ABI methods, and LiteralValue data are treated as immutable and
// shared with the original. The parent pointer is reset to nil; re-attach via
// AddSubplan if the clone is meant to be used as a nested planner.
//
// Concurrency: the source planner must not be mutated while Clone runs (same
// convention as every other method on Planner — there is no internal locking).
// Once Clone returns, the original and the clone are fully independent:
// operations on each, including Plan, Add, and AddSubplan, may run
// concurrently from separate goroutines without further synchronization.
func (p *Planner) Clone() *Planner {
	ctx := &cloneContext{
		commands: make(map[*Command]*Command),
		planners: make(map[*Planner]*Planner),
	}
	return ctx.clonePlanner(p)
}

// cloneContext carries shared maps so that ReturnValue / SubplanValue /
// StateValue references can be rewired consistently across an entire planner
// tree, even when sibling subplans cross-reference each other's commands.
type cloneContext struct {
	commands map[*Command]*Command
	planners map[*Planner]*Planner
}

func (ctx *cloneContext) clonePlanner(p *Planner) *Planner {
	if p == nil {
		return nil
	}
	if existing, ok := ctx.planners[p]; ok {
		return existing
	}

	clone := &Planner{
		commands: make([]*Command, len(p.commands)),
		// parent is intentionally nil: the clone is free-standing until the
		// caller re-attaches it via AddSubplan.
	}
	ctx.planners[p] = clone

	// Pass 1: allocate new Command structs and register the mapping so
	// ReturnValue references encountered during call cloning resolve to the
	// new pointers (regardless of forward/backward command order or sibling
	// subplan references).
	for i, oldCmd := range p.commands {
		clone.commands[i] = &Command{
			cmdType:    oldCmd.cmdType,
			returnSlot: -1, // reset so a fresh Plan() starts from a clean slate
		}
		ctx.commands[oldCmd] = clone.commands[i]
	}

	// Pass 2: clone each Call, rewiring its args.
	for i, oldCmd := range p.commands {
		clone.commands[i].call = ctx.cloneCall(oldCmd.call)
	}

	return clone
}

func (ctx *cloneContext) cloneCall(c *Call) *Call {
	if c == nil {
		return nil
	}
	out := &Call{
		contract:  c.contract,
		method:    c.method,
		flags:     c.flags,
		rawReturn: c.rawReturn,
	}
	if c.value != nil {
		out.value = new(big.Int).Set(c.value)
	}
	out.args = make([]Value, len(c.args))
	for i, arg := range c.args {
		out.args[i] = ctx.rewireValue(arg)
	}
	if c.rawCalldata != nil {
		out.rawCalldata = ctx.rewireValue(c.rawCalldata)
	}
	return out
}

func (ctx *cloneContext) rewireValue(v Value) Value {
	switch val := v.(type) {
	case *LiteralValue:
		// Immutable byte data — safe to share.
		return val
	case *ReturnValue:
		if newCmd, ok := ctx.commands[val.command]; ok {
			return &ReturnValue{
				command: newCmd,
				abiType: val.abiType,
			}
		}
		// References a command outside the cloned subtree; preserve as-is.
		return val
	case *StateValue:
		return &StateValue{planner: ctx.clonePlanner(val.planner)}
	case *SubplanValue:
		return &SubplanValue{subplanner: ctx.clonePlanner(val.subplanner)}
	case *TupleValue:
		// Children may include *ReturnValue leaves whose underlying
		// *Command was cloned; those need rewriting. Literals and
		// nested tuples whose own children don't rewire end up
		// returning themselves, so we share them. Allocate a new
		// children slice only when at least one child changed pointer.
		var newChildren []Value
		for i, child := range val.children {
			rewired := ctx.rewireValue(child)
			if rewired != child && newChildren == nil {
				newChildren = make([]Value, len(val.children))
				copy(newChildren, val.children)
			}
			if newChildren != nil {
				newChildren[i] = rewired
			}
		}
		if newChildren == nil {
			return val
		}
		return &TupleValue{
			abiType:  val.abiType,
			children: newChildren,
		}
	default:
		return val
	}
}

// ForEachCommand iterates over all commands in the planner.
// The callback receives the index and command. Return false to stop iteration.
func (p *Planner) ForEachCommand(fn func(int, *Command) bool) {
	for i, cmd := range p.commands {
		if !fn(i, cmd) {
			return
		}
	}
}

// Plan compiles all commands into executable format.
// Returns the encoded commands and initial state array.
func (p *Planner) Plan(opts ...PlanOption) (*CompiledPlan, error) {
	cfg := defaultPlanConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if len(p.commands) > cfg.maxCommands {
		return nil, ErrTooManyArguments
	}

	// Phase 1: Visibility analysis
	visibility := p.analyzeVisibility()

	// Phase 2: Build state and encode commands
	state := newStateManager(cfg)
	encoder := NewCommandEncoder()

	encodedCommands := make([][]byte, 0, len(p.commands))

	for i, cmd := range p.commands {
		// Allocate return slot if this command's return value is used
		if lastUsage, used := visibility[cmd]; used {
			isDynamic := false
			if cmd.call.HasReturnValue() {
				isDynamic = isDynamicType(*cmd.call.EffectiveReturnType())
			}
			slot, err := state.allocateReturn(cmd, lastUsage, isDynamic)
			if err != nil {
				return nil, &PlanError{CommandIndex: i, Method: cmd.call.method.Name, Err: err}
			}
			cmd.returnSlot = int(slot & ^uint8(DynamicSlotFlag))
		}

		// Build argument slots
		argSlots, err := p.buildArgSlots(cmd, state)
		if err != nil {
			return nil, &PlanError{CommandIndex: i, Method: cmd.call.method.Name, Err: err}
		}

		// Determine return slot.
		//
		// Producer-side dynamic flag rules:
		//   - Naturally-dynamic returns (uint256[], string, bytes) need
		//     the flag so the VM's writeOutputs takes the variable-
		//     length branch, which masks the slot byte before storing.
		//   - RawReturn (FlagTupleReturn) is handled by writeTuple,
		//     which uses the slot byte UNMASKED (CommandBuilder.sol
		//     line 151: `state[idx]`). Setting the flag there would
		//     index out of bounds. The consumer side still gets the
		//     flag via the ReturnValue's bytes typing, which is what
		//     makes downstream reads dynamic.
		returnSlot := uint8(NoReturnSlot)
		if cmd.returnSlot >= 0 {
			returnSlot = uint8(cmd.returnSlot)
			if cmd.call.HasReturnValue() && !cmd.call.rawReturn &&
				isDynamicType(*cmd.call.ReturnType()) {
				returnSlot |= DynamicSlotFlag
			}
		}

		// Encode command
		isExtended := len(argSlots) > MaxStandardArgs
		flags := cmd.call.computeFlags(isExtended)

		encoded, err := encoder.EncodeCommand(
			cmd.call.Selector(),
			flags,
			argSlots,
			returnSlot,
			cmd.call.contract.Address(),
		)
		if err != nil {
			return nil, &PlanError{CommandIndex: i, Method: cmd.call.method.Name, Err: err}
		}
		encodedCommands = append(encodedCommands, encoded)

		// Expire slots after this command
		state.expireSlots(i)
	}

	return &CompiledPlan{
		Commands: encodedCommands,
		State:    state.finalize(),
	}, nil
}

// buildArgSlots builds the argument slot array for a command. Public
// arity is preserved (one entry in args == one ABI input parameter),
// but a single *TupleValue arg expands into multiple state slots, so
// the returned slot list may be longer than args.
//
// FLAG_DATA detour: when the call is configured for raw calldata, the
// VM dispatcher reads byte 0 of indices as the value slot and byte 1 as
// the calldata slot, ignoring selector + args. We bypass normal arg
// resolution and emit exactly [valueSlot, dataSlot].
func (p *Planner) buildArgSlots(cmd *Command, state *stateManager) ([]uint8, error) {
	if cmd.call.flags&FlagData != 0 {
		return p.buildRawCalldataSlots(cmd, state)
	}

	args := cmd.call.Args()
	slots := make([]uint8, 0, len(args)+1)

	// For FLAG_CT_VALUECALL without FLAG_DATA, the on-chain VM reads the
	// value slot from byte 0 of indices and the ABI arg slots from bytes
	// 1..N (see VM.sol's VALUECALL branch:
	//   bytes memory v = state[uint8(bytes1(indices))];
	//   ... state.buildInputs(selector, indices << 8 | END_OF_ARGS);
	// ). The value slot MUST come first. Appending it last only happens
	// to work when len(args) == 0 — the "first" and "only" slot collapse
	// — and silently corrupts any call that mixes a value forward with
	// one or more ABI args.
	if cmd.call.value != nil && cmd.call.value.Sign() > 0 {
		valueLit := Uint256(cmd.call.value)
		slot, err := state.allocateLiteral(valueLit)
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}

	for _, arg := range args {
		argSlots, err := state.getSlotsForValue(arg)
		if err != nil {
			return nil, err
		}
		slots = append(slots, argSlots...)
	}

	return slots, nil
}

// buildRawCalldataSlots resolves the indices for a FLAG_DATA value call:
// [valueSlot, calldataSlot]. The value is sourced either from a literal
// (cmd.call.value, allocated as a uint256) or from a prior command's return
// slot (cmd.call.valueRef). In both cases the VM requires the value slot to
// be exactly 32 bytes, so dynamic-typed return values are rejected here. The
// calldata source is resolved through the normal state path.
func (p *Planner) buildRawCalldataSlots(cmd *Command, state *stateManager) ([]uint8, error) {
	var valueSlot uint8
	switch {
	case cmd.call.valueRef != nil:
		if cmd.call.valueRef.IsDynamic() {
			return nil, &EncodingError{Value: cmd.call.valueRef, Err: ErrInvalidCallType}
		}
		slots, err := state.getSlotsForValue(cmd.call.valueRef)
		if err != nil {
			return nil, err
		}
		if len(slots) != 1 {
			return nil, ErrInvalidCallType
		}
		// Static type: getSlotsForValue did not set DynamicSlotFlag. The byte is
		// safe to use as the value slot index directly.
		valueSlot = slots[0]
	case cmd.call.value != nil:
		valueLit := Uint256(cmd.call.value)
		slot, err := state.allocateLiteral(valueLit)
		if err != nil {
			return nil, err
		}
		valueSlot = slot
	default:
		return nil, ErrInvalidCallType
	}

	if cmd.call.rawCalldata == nil {
		return nil, ErrInvalidCallType
	}
	dataSlots, err := state.getSlotsForValue(cmd.call.rawCalldata)
	if err != nil {
		return nil, err
	}
	if len(dataSlots) != 1 {
		return nil, ErrInvalidCallType
	}

	return []uint8{valueSlot, dataSlots[0]}, nil
}

// analyzeVisibility determines the last command index that uses each
// command's return value. Returns a map from command to its last
// usage index.
//
// Walks args at the public-arity level and recurses into *TupleValue
// children so that a *ReturnValue nested inside a Tuple registers
// its consumer at the right command index — without this, the
// producer's return slot would never be allocated and getSlotsForValue
// would later fail with ErrReturnValueNotVisible.
func (p *Planner) analyzeVisibility() map[*Command]int {
	visibility := make(map[*Command]int)

	for i, cmd := range p.commands {
		for _, arg := range cmd.call.Args() {
			recordVisibility(visibility, arg, i)
		}
		// FLAG_DATA: the calldata source may be a *ReturnValue from a prior
		// command. Record its visibility so the producer's return slot is
		// allocated and remains live through this command.
		if cmd.call.rawCalldata != nil {
			recordVisibility(visibility, cmd.call.rawCalldata, i)
		}
		// AddRawCallV: the value source is a *ReturnValue from a prior
		// command. Same visibility requirement as rawCalldata.
		if cmd.call.valueRef != nil {
			recordVisibility(visibility, cmd.call.valueRef, i)
		}
	}

	return visibility
}

// recordVisibility marks v's contribution to the visibility map.
// *ReturnValue records its producer at cmdIndex; *TupleValue recurses.
// Other Value kinds contribute no visibility.
func recordVisibility(vis map[*Command]int, v Value, cmdIndex int) {
	switch val := v.(type) {
	case *ReturnValue:
		vis[val.command] = cmdIndex
	case *TupleValue:
		for _, child := range val.children {
			recordVisibility(vis, child, cmdIndex)
		}
	}
}

// checkCycle checks for cyclic planner references.
func (p *Planner) checkCycle(sub *Planner) error {
	visited := make(map[*Planner]bool)
	current := p

	for current != nil {
		if visited[current] {
			return ErrCyclicPlanner
		}
		visited[current] = true
		if current == sub {
			return ErrCyclicPlanner
		}
		current = current.parent
	}

	return nil
}

// validateSubplan validates that a call is suitable for subplan execution.
func validateSubplan(call *Call, sub *Planner) error {
	if sub == nil {
		return ErrInvalidSubplan
	}

	// Check that the call has appropriate argument types
	// (should accept bytes32[] for commands)
	hasCommandsArg := false
	for _, input := range call.method.Inputs {
		if input.Type.String() == "bytes32[]" {
			hasCommandsArg = true
			break
		}
	}

	if !hasCommandsArg {
		return ErrInvalidSubplan
	}

	return nil
}

// CompiledPlan contains the output of Plan(), ready for VM execution.
type CompiledPlan struct {
	Commands [][]byte // Each command is 32 bytes (or 64 for extended)
	State    [][]byte // Initial state array
}

// CommandsAsBytes32 returns commands as [][32]byte for contract calls.
func (cp *CompiledPlan) CommandsAsBytes32() [][32]byte {
	result := make([][32]byte, 0, len(cp.Commands))
	for _, cmd := range cp.Commands {
		if len(cmd) >= 32 {
			var b [32]byte
			copy(b[:], cmd[:32])
			result = append(result, b)
		}
		// For extended commands, add the second word
		if len(cmd) >= 64 {
			var b [32]byte
			copy(b[:], cmd[32:64])
			result = append(result, b)
		}
	}
	return result
}

// StateAsBytes returns state as [][]byte for contract calls.
func (cp *CompiledPlan) StateAsBytes() [][]byte {
	return cp.State
}

// CommandCount returns the number of logical commands (not including extended words).
func (cp *CompiledPlan) CommandCount() int {
	count := 0
	for _, cmd := range cp.Commands {
		if len(cmd) == 32 {
			count++
		} else if len(cmd) == 64 {
			count++ // Extended command counts as one
		}
	}
	return count
}
