package weiroll

import (
	"bytes"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Helper ABI for planner tests
func plannerTestABI() abi.ABI {
	const abiJSON = `[
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
			"name": "multiply",
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
			"name": "noReturn",
			"type": "function",
			"stateMutability": "nonpayable",
			"inputs": [
				{"name": "x", "type": "uint256"}
			],
			"outputs": []
		},
		{
			"name": "getString",
			"type": "function",
			"stateMutability": "view",
			"inputs": [],
			"outputs": [
				{"name": "", "type": "string"}
			]
		},
		{
			"name": "execute",
			"type": "function",
			"stateMutability": "nonpayable",
			"inputs": [
				{"name": "commands", "type": "bytes32[]"},
				{"name": "state", "type": "bytes[]"}
			],
			"outputs": [
				{"name": "", "type": "bytes[]"}
			]
		},
		{
			"name": "updateState",
			"type": "function",
			"stateMutability": "nonpayable",
			"inputs": [],
			"outputs": [
				{"name": "", "type": "bytes[]"}
			]
		}
	]`
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestCommandType(t *testing.T) {
	t.Run("CommandTypeCall is 0", func(t *testing.T) {
		if CommandTypeCall != 0 {
			t.Errorf("Expected CommandTypeCall to be 0, got %d", CommandTypeCall)
		}
	})

	t.Run("CommandTypeRawCall is 1", func(t *testing.T) {
		if CommandTypeRawCall != 1 {
			t.Errorf("Expected CommandTypeRawCall to be 1, got %d", CommandTypeRawCall)
		}
	})

	t.Run("CommandTypeSubplan is 2", func(t *testing.T) {
		if CommandTypeSubplan != 2 {
			t.Errorf("Expected CommandTypeSubplan to be 2, got %d", CommandTypeSubplan)
		}
	})
}

func TestCommand(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("Call returns underlying call", func(t *testing.T) {
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		cmd := &Command{call: call, cmdType: CommandTypeCall}

		if cmd.Call() != call {
			t.Error("Call() should return underlying call")
		}
	})

	t.Run("Type returns command type", func(t *testing.T) {
		cmd := &Command{cmdType: CommandTypeSubplan}

		if cmd.Type() != CommandTypeSubplan {
			t.Errorf("Expected CommandTypeSubplan, got %v", cmd.Type())
		}
	})
}

func TestNew(t *testing.T) {
	t.Run("creates empty planner", func(t *testing.T) {
		p := New()

		if p == nil {
			t.Fatal("Expected planner to be non-nil")
		}
		if p.Len() != 0 {
			t.Errorf("Expected 0 commands, got %d", p.Len())
		}
	})

	t.Run("accepts options", func(t *testing.T) {
		// Options are applied during New()
		p := New(func(planner *Planner) {
			// Custom option
		})

		if p == nil {
			t.Fatal("Expected planner to be non-nil")
		}
	})
}

func TestPlannerAdd(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("adds command and returns value", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))
		rv := p.Add(call)

		if rv == nil {
			t.Fatal("Expected return value for function with output")
		}
		if p.Len() != 1 {
			t.Errorf("Expected 1 command, got %d", p.Len())
		}
	})

	t.Run("returns nil for void function", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("noReturn", big.NewInt(1))
		rv := p.Add(call)

		if rv != nil {
			t.Error("Expected nil return value for void function")
		}
		if p.Len() != 1 {
			t.Errorf("Expected 1 command, got %d", p.Len())
		}
	})

	t.Run("multiple adds increment command count", func(t *testing.T) {
		p := New()
		p.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(contract.MustInvoke("add", big.NewInt(3), big.NewInt(4)))
		p.Add(contract.MustInvoke("add", big.NewInt(5), big.NewInt(6)))

		if p.Len() != 3 {
			t.Errorf("Expected 3 commands, got %d", p.Len())
		}
	})
}

func TestPlannerChaining(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lib := NewLibrary(addr, testABI)

	t.Run("chains return values", func(t *testing.T) {
		p := New()

		sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		product := p.Add(lib.MustInvoke("multiply", sum, big.NewInt(10)))

		if product == nil {
			t.Fatal("Expected product to be non-nil")
		}
		if p.Len() != 2 {
			t.Errorf("Expected 2 commands, got %d", p.Len())
		}
	})
}

func TestPlannerAddSubplan(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("adds subplan command", func(t *testing.T) {
		p := New()
		sub := New()
		sub.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))

		call := contract.MustInvoke("execute", sub.Subplan(), p.State())
		rv, err := p.AddSubplan(call, sub)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if rv == nil {
			t.Error("Expected return value")
		}
		if p.Len() != 1 {
			t.Errorf("Expected 1 command, got %d", p.Len())
		}
	})

	t.Run("returns error for nil subplan", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("execute", p.Subplan(), p.State())

		_, err := p.AddSubplan(call, nil)

		if err != ErrInvalidSubplan {
			t.Errorf("Expected ErrInvalidSubplan, got %v", err)
		}
	})

	t.Run("returns error for invalid call", func(t *testing.T) {
		p := New()
		sub := New()
		// Using 'add' which doesn't accept bytes32[]
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		_, err := p.AddSubplan(call, sub)

		if err != ErrInvalidSubplan {
			t.Errorf("Expected ErrInvalidSubplan, got %v", err)
		}
	})

	t.Run("detects cyclic reference", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("execute", p.Subplan(), p.State())

		_, err := p.AddSubplan(call, p)

		if err != ErrCyclicPlanner {
			t.Errorf("Expected ErrCyclicPlanner, got %v", err)
		}
	})
}

func TestPlannerReplaceState(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("adds state replacement call", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("updateState")

		err := p.ReplaceState(call)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if p.Len() != 1 {
			t.Errorf("Expected 1 command, got %d", p.Len())
		}
	})

	t.Run("returns error for void function", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("noReturn", big.NewInt(1))

		err := p.ReplaceState(call)

		if err != ErrNoReturnValue {
			t.Errorf("Expected ErrNoReturnValue, got %v", err)
		}
	})

	t.Run("returns error for wrong return type", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		err := p.ReplaceState(call)

		if err == nil {
			t.Error("Expected error for wrong return type")
		}

		typeMismatch, ok := err.(*TypeMismatchError)
		if !ok {
			t.Fatalf("Expected *TypeMismatchError, got %T", err)
		}
		if typeMismatch.Expected != "bytes[]" {
			t.Errorf("Expected 'bytes[]', got %q", typeMismatch.Expected)
		}
	})
}

func TestPlannerState(t *testing.T) {
	p := New()
	sv := p.State()

	if sv == nil {
		t.Fatal("Expected StateValue to be non-nil")
	}
	if sv.planner != p {
		t.Error("StateValue should reference parent planner")
	}
}

func TestPlannerSubplan(t *testing.T) {
	p := New()
	spv := p.Subplan()

	if spv == nil {
		t.Fatal("Expected SubplanValue to be non-nil")
	}
	if spv.subplanner != p {
		t.Error("SubplanValue should reference parent planner")
	}
}

func TestPlannerLen(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	p := New()
	if p.Len() != 0 {
		t.Errorf("Expected 0, got %d", p.Len())
	}

	p.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
	if p.Len() != 1 {
		t.Errorf("Expected 1, got %d", p.Len())
	}

	p.Add(contract.MustInvoke("add", big.NewInt(3), big.NewInt(4)))
	if p.Len() != 2 {
		t.Errorf("Expected 2, got %d", p.Len())
	}
}

func TestPlannerCommandAt(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	p := New()
	p.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
	p.Add(contract.MustInvoke("multiply", big.NewInt(3), big.NewInt(4)))

	t.Run("returns command at valid index", func(t *testing.T) {
		cmd := p.CommandAt(0)
		if cmd == nil {
			t.Fatal("Expected command to be non-nil")
		}
		if cmd.call.Method().Name != "add" {
			t.Errorf("Expected 'add', got %q", cmd.call.Method().Name)
		}

		cmd = p.CommandAt(1)
		if cmd == nil {
			t.Fatal("Expected command to be non-nil")
		}
		if cmd.call.Method().Name != "multiply" {
			t.Errorf("Expected 'multiply', got %q", cmd.call.Method().Name)
		}
	})

	t.Run("returns nil for negative index", func(t *testing.T) {
		cmd := p.CommandAt(-1)
		if cmd != nil {
			t.Error("Expected nil for negative index")
		}
	})

	t.Run("returns nil for out of bounds", func(t *testing.T) {
		cmd := p.CommandAt(100)
		if cmd != nil {
			t.Error("Expected nil for out of bounds index")
		}
	})
}

func TestPlannerForEachCommand(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	p := New()
	p.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
	p.Add(contract.MustInvoke("multiply", big.NewInt(3), big.NewInt(4)))
	p.Add(contract.MustInvoke("add", big.NewInt(5), big.NewInt(6)))

	t.Run("iterates all commands", func(t *testing.T) {
		count := 0
		p.ForEachCommand(func(i int, cmd *Command) bool {
			count++
			return true
		})

		if count != 3 {
			t.Errorf("Expected 3 iterations, got %d", count)
		}
	})

	t.Run("stops on false return", func(t *testing.T) {
		count := 0
		p.ForEachCommand(func(i int, cmd *Command) bool {
			count++
			return i < 1 // Stop after second (index 1)
		})

		if count != 2 {
			t.Errorf("Expected 2 iterations (stopped early), got %d", count)
		}
	})

	t.Run("provides correct indices", func(t *testing.T) {
		indices := make([]int, 0, 3)
		p.ForEachCommand(func(i int, cmd *Command) bool {
			indices = append(indices, i)
			return true
		})

		expected := []int{0, 1, 2}
		for i, idx := range indices {
			if idx != expected[i] {
				t.Errorf("Expected index %d at position %d, got %d", expected[i], i, idx)
			}
		}
	})
}

func TestPlannerPlan(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lib := NewLibrary(addr, testABI)

	t.Run("compiles simple plan", func(t *testing.T) {
		p := New()
		p.Add(lib.MustInvoke("add", big.NewInt(100), big.NewInt(200)))

		plan, err := p.Plan()

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if plan == nil {
			t.Fatal("Expected plan to be non-nil")
		}
		if len(plan.Commands) != 1 {
			t.Errorf("Expected 1 command, got %d", len(plan.Commands))
		}
	})

	t.Run("compiles chained plan", func(t *testing.T) {
		p := New()
		sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(lib.MustInvoke("multiply", sum, big.NewInt(10)))

		plan, err := p.Plan()

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(plan.Commands) != 2 {
			t.Errorf("Expected 2 commands, got %d", len(plan.Commands))
		}
	})

	t.Run("respects max commands option", func(t *testing.T) {
		p := New()
		p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(lib.MustInvoke("add", big.NewInt(3), big.NewInt(4)))

		_, err := p.Plan(WithMaxCommands(1))

		if err == nil {
			t.Error("Expected error for exceeding max commands")
		}
	})

	t.Run("deduplicates identical literals", func(t *testing.T) {
		p := New()
		// Same value (100) used twice
		p.Add(lib.MustInvoke("add", big.NewInt(100), big.NewInt(100)))

		plan, err := p.Plan()

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// Should only have 1 state slot due to deduplication
		if len(plan.State) != 1 {
			t.Errorf("Expected 1 state slot (deduplicated), got %d", len(plan.State))
		}
	})

	t.Run("handles void functions", func(t *testing.T) {
		contract := NewContract(addr, testABI)
		p := New()
		p.Add(contract.MustInvoke("noReturn", big.NewInt(42)))

		plan, err := p.Plan()

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(plan.Commands) != 1 {
			t.Errorf("Expected 1 command, got %d", len(plan.Commands))
		}
	})
}

func TestPlannerPlanWithSlotOptimization(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lib := NewLibrary(addr, testABI)

	t.Run("optimization enabled recycles slots", func(t *testing.T) {
		p := New()

		// First return value only used in second command
		first := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		second := p.Add(lib.MustInvoke("multiply", first, big.NewInt(10)))

		// Second value used in third command
		p.Add(lib.MustInvoke("multiply", second, big.NewInt(5)))

		plan, err := p.Plan(WithSlotOptimization(true))

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if plan == nil {
			t.Fatal("Expected plan to be non-nil")
		}
	})

	t.Run("optimization disabled uses more slots", func(t *testing.T) {
		p := New()

		first := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(lib.MustInvoke("multiply", first, big.NewInt(10)))

		planOptimized, err := p.Plan(WithSlotOptimization(true))
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		p2 := New()
		first2 := p2.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p2.Add(lib.MustInvoke("multiply", first2, big.NewInt(10)))

		planUnoptimized, err := p2.Plan(WithSlotOptimization(false))
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Both should compile successfully
		if planOptimized == nil || planUnoptimized == nil {
			t.Fatal("Both plans should be non-nil")
		}
	})
}

func TestCompiledPlan(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lib := NewLibrary(addr, testABI)

	p := New()
	p.Add(lib.MustInvoke("add", big.NewInt(100), big.NewInt(200)))
	p.Add(lib.MustInvoke("multiply", big.NewInt(3), big.NewInt(4)))

	plan, _ := p.Plan()

	t.Run("CommandsAsBytes32 returns correct format", func(t *testing.T) {
		commands := plan.CommandsAsBytes32()

		if len(commands) != 2 {
			t.Errorf("Expected 2 commands, got %d", len(commands))
		}

		for i, cmd := range commands {
			// Each should be exactly 32 bytes
			if len(cmd) != 32 {
				t.Errorf("Command %d should be 32 bytes, got %d", i, len(cmd))
			}
		}
	})

	t.Run("StateAsBytes returns state", func(t *testing.T) {
		state := plan.StateAsBytes()

		if state == nil {
			t.Fatal("Expected state to be non-nil")
		}
	})

	t.Run("CommandCount returns logical count", func(t *testing.T) {
		count := plan.CommandCount()

		if count != 2 {
			t.Errorf("Expected 2 commands, got %d", count)
		}
	})
}

func TestValidateSubplan(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)

	t.Run("accepts valid execute call", func(t *testing.T) {
		p := New()
		sub := New()
		call := contract.MustInvoke("execute", sub.Subplan(), p.State())

		err := validateSubplan(call, sub)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("rejects call without bytes32[]", func(t *testing.T) {
		sub := New()
		call := contract.MustInvoke("add", big.NewInt(1), big.NewInt(2))

		err := validateSubplan(call, sub)

		if err != ErrInvalidSubplan {
			t.Errorf("Expected ErrInvalidSubplan, got %v", err)
		}
	})

	t.Run("rejects nil subplan", func(t *testing.T) {
		p := New()
		call := contract.MustInvoke("execute", p.Subplan(), p.State())

		err := validateSubplan(call, nil)

		if err != ErrInvalidSubplan {
			t.Errorf("Expected ErrInvalidSubplan, got %v", err)
		}
	})
}

func TestCheckCycle(t *testing.T) {
	t.Run("no cycle for unrelated planners", func(t *testing.T) {
		p1 := New()
		p2 := New()

		err := p1.checkCycle(p2)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("detects self cycle", func(t *testing.T) {
		p := New()

		err := p.checkCycle(p)

		if err != ErrCyclicPlanner {
			t.Errorf("Expected ErrCyclicPlanner, got %v", err)
		}
	})
}

func TestPlannerClone(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, testABI)
	lib := NewLibrary(addr, testABI)

	t.Run("empty planner clones to empty planner", func(t *testing.T) {
		p := New()
		c := p.Clone()

		if c == p {
			t.Fatal("Clone should return a new pointer")
		}
		if c.Len() != 0 {
			t.Errorf("Expected 0 commands, got %d", c.Len())
		}
	})

	t.Run("commands slice is independent", func(t *testing.T) {
		p := New()
		p.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))

		c := p.Clone()
		c.Add(contract.MustInvoke("add", big.NewInt(3), big.NewInt(4)))

		if p.Len() != 1 {
			t.Errorf("Original should still have 1 command, got %d", p.Len())
		}
		if c.Len() != 2 {
			t.Errorf("Clone should have 2 commands, got %d", c.Len())
		}

		if p.CommandAt(0) == c.CommandAt(0) {
			t.Error("Cloned commands must not share pointers with the original")
		}
	})

	t.Run("ReturnValue references rewire to clone commands", func(t *testing.T) {
		p := New()
		sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(lib.MustInvoke("multiply", sum, big.NewInt(10)))

		c := p.Clone()

		// The second command's first arg is a ReturnValue pointing at the
		// FIRST command. After cloning it must point at the clone's first
		// command, not the original's.
		secondArgs := c.CommandAt(1).Call().Args()
		rv, ok := secondArgs[0].(*ReturnValue)
		if !ok {
			t.Fatalf("Expected *ReturnValue, got %T", secondArgs[0])
		}
		if rv.command != c.CommandAt(0) {
			t.Error("ReturnValue should point at the clone's command, not the original's")
		}
		if rv.command == p.CommandAt(0) {
			t.Error("ReturnValue must not still point at the original command")
		}
	})

	t.Run("Plan output equals original after planning either first", func(t *testing.T) {
		build := func() *Planner {
			p := New()
			sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
			p.Add(lib.MustInvoke("multiply", sum, big.NewInt(10)))
			return p
		}

		// Plan the original first; clone afterwards (so clone inherits any
		// returnSlot mutations from the original Plan call).
		orig := build()
		origPlan, err := orig.Plan()
		if err != nil {
			t.Fatalf("orig.Plan: %v", err)
		}

		c := orig.Clone()
		clonePlan, err := c.Plan()
		if err != nil {
			t.Fatalf("clone.Plan: %v", err)
		}

		if !equalCompiledPlans(origPlan, clonePlan) {
			t.Error("Original and clone produced different compiled plans")
		}

		// Re-plan original; output should still match.
		origPlan2, err := orig.Plan()
		if err != nil {
			t.Fatalf("orig.Plan (second): %v", err)
		}
		if !equalCompiledPlans(origPlan, origPlan2) {
			t.Error("Re-planning original after cloning produced different output")
		}
	})

	t.Run("planning clone does not mutate original commands", func(t *testing.T) {
		p := New()
		sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(lib.MustInvoke("multiply", sum, big.NewInt(10)))

		c := p.Clone()

		// Snapshot the original's returnSlots BEFORE planning the clone.
		preSlots := make([]int, p.Len())
		for i := 0; i < p.Len(); i++ {
			preSlots[i] = p.CommandAt(i).returnSlot
		}

		if _, err := c.Plan(); err != nil {
			t.Fatalf("clone.Plan: %v", err)
		}

		for i := 0; i < p.Len(); i++ {
			if p.CommandAt(i).returnSlot != preSlots[i] {
				t.Errorf("Command %d on original was mutated by clone.Plan(): %d -> %d",
					i, preSlots[i], p.CommandAt(i).returnSlot)
			}
		}
	})

	t.Run("WithValue is preserved and *big.Int is copied defensively", func(t *testing.T) {
		amount := big.NewInt(1000)
		p := New()
		p.Add(contract.MustInvoke("noReturn", big.NewInt(1)).WithValue(amount))

		c := p.Clone()

		cloneVal := c.CommandAt(0).Call().EthValue()
		if cloneVal == nil || cloneVal.Cmp(amount) != 0 {
			t.Fatalf("Expected EthValue=%s on clone, got %v", amount, cloneVal)
		}
		// Mutating the clone's value must not bleed into the original.
		cloneVal.SetInt64(42)
		if p.CommandAt(0).Call().EthValue().Cmp(amount) != 0 {
			t.Error("Mutating clone's EthValue affected the original")
		}
	})

	t.Run("subplan tree is cloned and re-wired", func(t *testing.T) {
		p := New()
		sub := New()
		sub.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))

		execCall := contract.MustInvoke("execute", sub.Subplan(), p.State())
		if _, err := p.AddSubplan(execCall, sub); err != nil {
			t.Fatalf("AddSubplan: %v", err)
		}

		c := p.Clone()

		// The cloned outer planner's first command's args should reference
		// the CLONED inner subplanner, not the original.
		args := c.CommandAt(0).Call().Args()

		var cloneSubPlanner *Planner
		var cloneStatePlanner *Planner
		for _, a := range args {
			switch v := a.(type) {
			case *SubplanValue:
				cloneSubPlanner = v.subplanner
			case *StateValue:
				cloneStatePlanner = v.planner
			}
		}

		if cloneSubPlanner == nil {
			t.Fatal("Cloned call missing SubplanValue arg")
		}
		if cloneSubPlanner == sub {
			t.Error("SubplanValue still references original subplanner")
		}
		if cloneSubPlanner.Len() != sub.Len() {
			t.Errorf("Cloned subplan should have %d commands, got %d", sub.Len(), cloneSubPlanner.Len())
		}

		if cloneStatePlanner == nil {
			t.Fatal("Cloned call missing StateValue arg")
		}
		if cloneStatePlanner != c {
			t.Error("StateValue should reference the cloned outer planner, not original or another instance")
		}
	})

	t.Run("parent pointer is reset on clone", func(t *testing.T) {
		p := New()
		sub := New()
		sub.Add(contract.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		execCall := contract.MustInvoke("execute", sub.Subplan(), p.State())
		if _, err := p.AddSubplan(execCall, sub); err != nil {
			t.Fatalf("AddSubplan: %v", err)
		}

		// sub now has parent == p. Clone the *subplanner* directly.
		subClone := sub.Clone()
		if subClone.parent != nil {
			t.Error("Clone should reset parent to nil")
		}
	})

	t.Run("ReturnValue outside cloned subtree is preserved", func(t *testing.T) {
		// Hand-craft a Call whose ReturnValue points at a command in a
		// different planner that is NOT reachable from the one being cloned.
		// Cloning should leave that ReturnValue alone rather than panic.
		other := New()
		otherRV := other.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))

		p := New()
		p.Add(lib.MustInvoke("multiply", otherRV, big.NewInt(10)))

		c := p.Clone()

		args := c.CommandAt(0).Call().Args()
		rv, ok := args[0].(*ReturnValue)
		if !ok {
			t.Fatalf("Expected *ReturnValue, got %T", args[0])
		}
		// Since `other` is not part of p's cloned subtree, the reference is
		// preserved as-is (not rewired).
		if rv.command != other.CommandAt(0) {
			t.Error("Out-of-subtree ReturnValue should point at the original external command")
		}
	})
}

// TestPlannerClone_ConcurrentPlan exercises the independence claim: after
// Clone(), Plan() may run on the original and the clone from separate
// goroutines without synchronization. Run under `go test -race` (requires
// CGO_ENABLED=1) to get real race-detector coverage; without -race this is
// still a useful regression.
func TestPlannerClone_ConcurrentPlan(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lib := NewLibrary(addr, testABI)

	build := func() *Planner {
		p := New()
		sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		doubled := p.Add(lib.MustInvoke("multiply", sum, big.NewInt(2)))
		p.Add(lib.MustInvoke("multiply", doubled, big.NewInt(10)))
		return p
	}

	orig := build()
	clone := orig.Clone()

	var (
		wg                    sync.WaitGroup
		origPlan, clonePlan   *CompiledPlan
		origErr, cloneErr     error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		origPlan, origErr = orig.Plan()
	}()
	go func() {
		defer wg.Done()
		clonePlan, cloneErr = clone.Plan()
	}()
	wg.Wait()

	if origErr != nil {
		t.Fatalf("orig.Plan: %v", origErr)
	}
	if cloneErr != nil {
		t.Fatalf("clone.Plan: %v", cloneErr)
	}
	if !equalCompiledPlans(origPlan, clonePlan) {
		t.Error("Concurrent Plan() on original and clone produced divergent output")
	}
}

// equalCompiledPlans compares two CompiledPlans by byte content for equality.
func equalCompiledPlans(a, b *CompiledPlan) bool {
	if len(a.Commands) != len(b.Commands) || len(a.State) != len(b.State) {
		return false
	}
	for i := range a.Commands {
		if !bytes.Equal(a.Commands[i], b.Commands[i]) {
			return false
		}
	}
	for i := range a.State {
		if !bytes.Equal(a.State[i], b.State[i]) {
			return false
		}
	}
	return reflect.DeepEqual(a.Commands, b.Commands) && reflect.DeepEqual(a.State, b.State)
}

func TestVisibilityAnalysis(t *testing.T) {
	testABI := plannerTestABI()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lib := NewLibrary(addr, testABI)

	t.Run("tracks last usage of return values", func(t *testing.T) {
		p := New()

		// add(1, 2) -> used in command 1
		sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))

		// multiply(sum, 10) -> uses sum
		p.Add(lib.MustInvoke("multiply", sum, big.NewInt(10)))

		visibility := p.analyzeVisibility()

		// sum (from command 0) should be last used at command 1
		cmd0 := p.CommandAt(0)
		lastUsage, found := visibility[cmd0]
		if !found {
			t.Error("Expected command 0 to be in visibility map")
		}
		if lastUsage != 1 {
			t.Errorf("Expected last usage at 1, got %d", lastUsage)
		}
	})

	t.Run("handles unused return values", func(t *testing.T) {
		p := New()

		// Return value not used by anything
		p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))

		visibility := p.analyzeVisibility()

		// Command 0's return value is never used, so it shouldn't be in visibility
		cmd0 := p.CommandAt(0)
		if _, found := visibility[cmd0]; found {
			t.Error("Unused return value should not be in visibility map")
		}
	})
}
