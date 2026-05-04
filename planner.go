package weiroll

import "math/big"

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
		abiType: *call.ReturnType(),
		index:   0,
	}
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
		abiType: *call.ReturnType(),
		index:   0,
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
				index:   val.index,
			}
		}
		// References a command outside the cloned subtree; preserve as-is.
		return val
	case *StateValue:
		return &StateValue{planner: ctx.clonePlanner(val.planner)}
	case *SubplanValue:
		return &SubplanValue{subplanner: ctx.clonePlanner(val.subplanner)}
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
				isDynamic = isDynamicType(*cmd.call.ReturnType())
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

		// Determine return slot
		returnSlot := uint8(NoReturnSlot)
		if cmd.returnSlot >= 0 {
			returnSlot = uint8(cmd.returnSlot)
			if cmd.call.HasReturnValue() && isDynamicType(*cmd.call.ReturnType()) {
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

// buildArgSlots builds the argument slot array for a command.
func (p *Planner) buildArgSlots(cmd *Command, state *stateManager) ([]uint8, error) {
	args := cmd.call.Args()
	slots := make([]uint8, len(args))

	for i, arg := range args {
		slot, err := state.getSlotForValue(arg)
		if err != nil {
			return nil, err
		}
		slots[i] = slot
	}

	// If call has value, add it as an extra argument
	if cmd.call.value != nil && cmd.call.value.Sign() > 0 {
		valueLit := Uint256(cmd.call.value)
		slot, err := state.allocateLiteral(valueLit)
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}

	return slots, nil
}

// analyzeVisibility determines the last command index that uses each command's return value.
// Returns a map from command to its last usage index.
func (p *Planner) analyzeVisibility() map[*Command]int {
	visibility := make(map[*Command]int)

	for i, cmd := range p.commands {
		for _, arg := range cmd.call.Args() {
			if rv, ok := arg.(*ReturnValue); ok {
				visibility[rv.command] = i
			}
		}
	}

	return visibility
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
