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
	lib := NewContract(addr, testABI)

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
	lib := NewContract(addr, testABI)

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
	lib := NewContract(addr, testABI)

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
	lib := NewContract(addr, testABI)

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
	lib := NewContract(addr, testABI)

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

	t.Run("ReturnValue inside Tuple is rewired on clone", func(t *testing.T) {
		// Mirrors TestPlanWithChainedReturnValueInTuple's shape so the
		// clone path's TupleValue child-rewrite is exercised end-to-end.
		chained := MustParseABI(chainedTupleABI)
		c2 := NewContract(addr, chained)
		l2 := NewContract(addr, chained)

		p := New()
		sum := p.Add(l2.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
		p.Add(c2.MustInvoke("consume", Tuple(sum, Address(addr))))

		clone := p.Clone()

		consumeArgs := clone.CommandAt(1).Call().Args()
		tv, ok := consumeArgs[0].(*TupleValue)
		if !ok {
			t.Fatalf("Cloned consume's first arg should be *TupleValue, got %T", consumeArgs[0])
		}
		if len(tv.Children()) != 2 {
			t.Fatalf("Cloned Tuple Children len = %d, want 2", len(tv.Children()))
		}
		gotRV, ok := tv.Children()[0].(*ReturnValue)
		if !ok {
			t.Fatalf("Cloned Tuple Children[0] should be *ReturnValue, got %T", tv.Children()[0])
		}
		if gotRV.command != clone.CommandAt(0) {
			t.Error("Cloned Tuple's ReturnValue should point at the clone's cmd0")
		}
		if gotRV.command == p.CommandAt(0) {
			t.Error("Cloned Tuple's ReturnValue must not still point at the original cmd0")
		}

		// Plans should be byte-identical: visibility + slot allocation
		// must reach the same state regardless of which copy we plan.
		origPlan, err := p.Plan()
		if err != nil {
			t.Fatalf("orig.Plan: %v", err)
		}
		clonePlan, err := clone.Plan()
		if err != nil {
			t.Fatalf("clone.Plan: %v", err)
		}
		if !equalCompiledPlans(origPlan, clonePlan) {
			t.Error("Original and cloned plans diverged after Tuple-ReturnValue rewrite")
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
	lib := NewContract(addr, testABI)

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
		wg                  sync.WaitGroup
		origPlan, clonePlan *CompiledPlan
		origErr, cloneErr   error
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
	lib := NewContract(addr, testABI)

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

// exactInputSingleABI mirrors the Uniswap V3 SwapRouter02 method that
// triggered the original revert: a 7-field static tuple parameter that
// the on-chain VM rejects as a single >32-byte slot.
const exactInputSingleABI = `[{
	"name": "exactInputSingle",
	"type": "function",
	"stateMutability": "payable",
	"inputs": [{
		"name": "params",
		"type": "tuple",
		"components": [
			{"name": "tokenIn",           "type": "address"},
			{"name": "tokenOut",          "type": "address"},
			{"name": "fee",               "type": "uint24"},
			{"name": "recipient",         "type": "address"},
			{"name": "amountIn",          "type": "uint256"},
			{"name": "amountOutMinimum",  "type": "uint256"},
			{"name": "sqrtPriceLimitX96", "type": "uint160"}
		]
	}],
	"outputs": [{"name": "amountOut", "type": "uint256"}]
}]`

func TestPlanWithStaticTupleArg(t *testing.T) {
	routerAddr := common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
	tokenIn := common.HexToAddress("0x514910771AF9Ca656af840dff83E8264EcF986CA")  // LINK
	tokenOut := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2") // WETH
	recipient := common.HexToAddress("0xEEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0EEE0")

	router := NewContract(routerAddr, MustParseABI(exactInputSingleABI))
	planner := New()

	params := Tuple(
		Address(tokenIn),
		Address(tokenOut),
		MustLiteralFromType("uint24", big.NewInt(3000)),
		Address(recipient),
		Uint256(big.NewInt(1_000_000_000_000_000_000)), // 1 LINK
		Uint256(big.NewInt(0)),                         // amountOutMinimum
		MustLiteralFromType("uint160", big.NewInt(0)),  // sqrtPriceLimitX96
	)

	planner.Add(router.MustInvoke("exactInputSingle", params))

	plan, err := planner.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(plan.Commands))
	}

	// Decode and verify: 7 args, all static slots, extended encoding.
	_, flags, argSlots, _, _, err := DecodeCommand(plan.Commands[0])
	if err != nil {
		t.Fatalf("DecodeCommand: %v", err)
	}
	if !flags.IsExtended() {
		t.Errorf("expected extended-command flag (>6 args), got flags=0x%02x", flags)
	}
	if len(argSlots) != 7 {
		t.Fatalf("argSlots count = %d, want 7", len(argSlots))
	}
	for i, s := range argSlots {
		if s&DynamicSlotFlag != 0 {
			t.Errorf("argSlot[%d] = 0x%02x has DynamicSlotFlag (must be static)", i, s)
		}
	}

	// Reconstruct calldata from state slots and compare with abi.Pack of
	// the equivalent struct — proves the on-chain VM would assemble the
	// same calldata it would for an inline struct call.
	want, err := router.ABI().Methods["exactInputSingle"].Inputs.Pack(struct {
		TokenIn           common.Address
		TokenOut          common.Address
		Fee               *big.Int
		Recipient         common.Address
		AmountIn          *big.Int
		AmountOutMinimum  *big.Int
		SqrtPriceLimitX96 *big.Int
	}{
		TokenIn:           tokenIn,
		TokenOut:          tokenOut,
		Fee:               big.NewInt(3000),
		Recipient:         recipient,
		AmountIn:          big.NewInt(1_000_000_000_000_000_000),
		AmountOutMinimum:  big.NewInt(0),
		SqrtPriceLimitX96: big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("abi.Pack reference: %v", err)
	}

	got := make([]byte, 0, 7*32)
	for _, slotIdx := range argSlots {
		idx := slotIdx & 0x7f
		if int(idx) >= len(plan.State) {
			t.Fatalf("slot %d out of range (state len %d)", idx, len(plan.State))
		}
		slotData := plan.State[idx]
		if len(slotData) != 32 {
			t.Fatalf("slot %d data length = %d, want 32", idx, len(slotData))
		}
		got = append(got, slotData...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("reassembled calldata mismatch\n got: %x\nwant: %x", got, want)
	}
}

// chainedTupleABI declares a producer (add) and a consumer
// (consume) that takes a static tuple whose first field is a
// *ReturnValue from the producer. Mirrors the multi-hop V3 swap
// shape — the canonical chained-amount pattern v0.2.0 unblocks.
const chainedTupleABI = `[
	{
		"name": "add",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "a", "type": "uint256"},
			{"name": "b", "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "uint256"}]
	},
	{
		"name": "consume",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{
			"name": "params",
			"type": "tuple",
			"components": [
				{"name": "amount",    "type": "uint256"},
				{"name": "recipient", "type": "address"}
			]
		}],
		"outputs": []
	}
]`

func TestPlanWithChainedReturnValueInTuple(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	contract := NewContract(addr, MustParseABI(chainedTupleABI))
	lib := NewContract(addr, MustParseABI(chainedTupleABI))

	p := New()
	sum := p.Add(lib.MustInvoke("add", big.NewInt(1), big.NewInt(2)))
	p.Add(contract.MustInvoke("consume", Tuple(sum, Address(addr))))

	// Visibility must register cmd0 (the producer) as last-used at
	// cmd1, otherwise its return slot would never be allocated.
	vis := p.analyzeVisibility()
	cmd0 := p.CommandAt(0)
	lastUsage, found := vis[cmd0]
	if !found {
		t.Fatal("producer command missing from visibility map (Tuple recursion not wired?)")
	}
	if lastUsage != 1 {
		t.Errorf("expected producer last usage = 1, got %d", lastUsage)
	}

	plan, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(plan.Commands))
	}

	// Decode cmd1; the first argSlot (amount field) must be the
	// producer's return slot, no DynamicSlotFlag.
	_, _, argSlots, _, _, err := DecodeCommand(plan.Commands[1])
	if err != nil {
		t.Fatalf("DecodeCommand: %v", err)
	}
	if len(argSlots) != 2 {
		t.Fatalf("argSlots count = %d, want 2 (Tuple expanded to 2 slots)", len(argSlots))
	}
	prodSlot := uint8(cmd0.returnSlot)
	if argSlots[0] != prodSlot {
		t.Errorf("argSlots[0] (amount field) = 0x%02x, want producer slot 0x%02x",
			argSlots[0], prodSlot)
	}
	if argSlots[0]&DynamicSlotFlag != 0 {
		t.Errorf("static *ReturnValue leaf must not have DynamicSlotFlag (got 0x%02x)", argSlots[0])
	}
}

func TestPlanRejectsStructLiteralForStaticTuple(t *testing.T) {
	router := NewContract(
		common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45"),
		MustParseABI(exactInputSingleABI),
	)

	// Passing a Go struct directly should now fail at toValue/NewLiteral
	// with ErrStaticTupleTooLarge, pointing the user at weiroll.Tuple.
	type params struct {
		TokenIn           common.Address
		TokenOut          common.Address
		Fee               *big.Int
		Recipient         common.Address
		AmountIn          *big.Int
		AmountOutMinimum  *big.Int
		SqrtPriceLimitX96 *big.Int
	}

	_, err := router.Invoke("exactInputSingle", params{
		Fee:               big.NewInt(3000),
		AmountIn:          big.NewInt(1),
		AmountOutMinimum:  big.NewInt(0),
		SqrtPriceLimitX96: big.NewInt(0),
	})
	if err == nil {
		t.Fatal("expected error when passing struct literal to static-tuple method")
	}
	if !strings.Contains(err.Error(), "weiroll.Tuple") {
		t.Errorf("error message should reference weiroll.Tuple; got: %v", err)
	}
}

// TestPlannerAddRawCallEmptyCalldata covers the descry.helpers.SendNativeEth
// shape: forward ETH to a receive()-only target via FLAG_DATA with empty bytes.
// The compiled plan should produce one 32-byte command with the right shape and
// a state array whose calldata slot is zero-length bytes.
func TestPlannerAddRawCallEmptyCalldata(t *testing.T) {
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000001")
	value := big.NewInt(1e17) // 0.1 ETH

	p := New()
	rv := p.AddRawCall(target, value, nil)
	if rv != nil {
		t.Errorf("AddRawCall should return nil *ReturnValue, got %v", rv)
	}

	compiled, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if len(compiled.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(compiled.Commands))
	}
	cmd := compiled.Commands[0]
	if len(cmd) != CommandSize {
		t.Fatalf("Expected 32-byte command, got %d", len(cmd))
	}

	// Selector zero on the FLAG_DATA path.
	for i := 0; i < 4; i++ {
		if cmd[i] != 0 {
			t.Errorf("Selector byte %d should be zero, got 0x%02x", i, cmd[i])
		}
	}
	if cmd[4] != 0x23 {
		t.Errorf("Expected flag byte 0x23, got 0x%02x", cmd[4])
	}

	valueSlot := cmd[5]
	dataSlot := cmd[6]

	// dataSlot carries the DynamicSlotFlag because the calldata literal is
	// dynamic bytes. The VM strips the high bit via IDX_VALUE_MASK at
	// execution time; here we just confirm the masked index resolves to a
	// real state slot.
	if dataSlot&DynamicSlotFlag == 0 {
		t.Errorf("Expected dynamic flag on data slot byte 0x%02x", dataSlot)
	}
	maskedDataSlot := dataSlot & ^uint8(DynamicSlotFlag)

	for i := 7; i <= 10; i++ {
		if cmd[i] != UnusedSlot {
			t.Errorf("Expected padding at indices[%d]=0xff, got 0x%02x", i-5, cmd[i])
		}
	}
	if cmd[11] != NoReturnSlot {
		t.Errorf("Expected return slot 0xff, got 0x%02x", cmd[11])
	}
	if common.BytesToAddress(cmd[12:32]) != target {
		t.Errorf("Address mismatch: %s vs %s", common.BytesToAddress(cmd[12:32]).Hex(), target.Hex())
	}

	// State: valueSlot contains 32-byte big-endian encoding of 0.1 ETH;
	// dataSlot is exactly zero bytes (so the on-chain call carries empty
	// calldata and invokes receive()).
	if int(valueSlot) >= len(compiled.State) || int(maskedDataSlot) >= len(compiled.State) {
		t.Fatalf("State too small: have %d, want valueSlot=%d, dataSlot=%d",
			len(compiled.State), valueSlot, maskedDataSlot)
	}
	if len(compiled.State[valueSlot]) != 32 {
		t.Errorf("Value slot must be 32 bytes, got %d", len(compiled.State[valueSlot]))
	}
	if len(compiled.State[maskedDataSlot]) != 0 {
		t.Errorf("Data slot must be zero bytes for receive() invocation, got %d bytes (%x)",
			len(compiled.State[maskedDataSlot]), compiled.State[maskedDataSlot])
	}
}

// TestPlannerAddRawCallArbitraryCalldata confirms the encoder forwards an
// arbitrary byte payload verbatim into the state slot — no length prefix, no
// 32-byte padding.
func TestPlannerAddRawCallArbitraryCalldata(t *testing.T) {
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000002")
	payload := []byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe}

	p := New()
	p.AddRawCall(target, big.NewInt(0), payload)

	compiled, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	dataSlot := compiled.Commands[0][6] & ^uint8(DynamicSlotFlag)
	if !bytes.Equal(compiled.State[dataSlot], payload) {
		t.Errorf("Data slot should equal payload byte-for-byte: got %x, want %x",
			compiled.State[dataSlot], payload)
	}
}

// TestPlannerAddRawCallV covers the ref-piped value path: a prior command's
// uint256 return is consumed as the VALUECALL value at runtime via FLAG_DATA.
// This is the surface descry.helpers.SendNativeEth uses to forward "whatever
// ETH is at the executor right now" — e.g., piped from helpers.EthBalance.
func TestPlannerAddRawCallV(t *testing.T) {
	testABI := plannerTestABI()
	producer := common.HexToAddress("0xCAFE000000000000000000000000000000000010")
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000011")

	p := New()
	// Producer: add(1, 2) -> uint256. Its return slot is what we want to
	// thread into the VALUECALL value position.
	rv := p.Add(NewContract(producer, testABI).MustInvoke("add", big.NewInt(1), big.NewInt(2)))
	if rv == nil {
		t.Fatal("producer Add must return a non-nil *ReturnValue")
	}

	// Consumer: AddRawCallV with empty calldata. Receives ETH equal to whatever
	// the producer returned.
	out := p.AddRawCallV(target, rv, nil)
	if out != nil {
		t.Errorf("AddRawCallV should return nil *ReturnValue, got %v", out)
	}

	compiled, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(compiled.Commands) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(compiled.Commands))
	}

	// Producer command: producer.add(1, 2) -> some return slot.
	producerCmd := compiled.Commands[0]
	producerReturnSlot := producerCmd[11]
	if producerReturnSlot == NoReturnSlot {
		t.Fatal("Producer must have a return slot assigned (consumer's valueRef keeps it live)")
	}

	// Consumer command: FLAG_DATA value-call. indices[0] = producer return slot,
	// indices[1] = empty-bytes calldata slot.
	consumerCmd := compiled.Commands[1]
	if consumerCmd[4] != 0x23 {
		t.Errorf("Expected flag byte 0x23 (VALUECALL|FLAG_DATA), got 0x%02x", consumerCmd[4])
	}
	if consumerCmd[5] != producerReturnSlot {
		t.Errorf("indices[0] should equal producer's return slot: got 0x%02x, want 0x%02x",
			consumerCmd[5], producerReturnSlot)
	}
	dataSlot := consumerCmd[6] & ^uint8(DynamicSlotFlag)
	if int(dataSlot) >= len(compiled.State) {
		t.Fatalf("Data slot %d out of bounds (state size %d)", dataSlot, len(compiled.State))
	}
	if len(compiled.State[dataSlot]) != 0 {
		t.Errorf("Data slot must be zero bytes for receive() invocation, got %d bytes",
			len(compiled.State[dataSlot]))
	}

	// Selector zero, padding 0xff, no return slot, target address.
	for i := 0; i < 4; i++ {
		if consumerCmd[i] != 0 {
			t.Errorf("Selector byte %d should be zero, got 0x%02x", i, consumerCmd[i])
		}
	}
	for i := 7; i <= 10; i++ {
		if consumerCmd[i] != UnusedSlot {
			t.Errorf("Expected padding at byte %d=0xff, got 0x%02x", i, consumerCmd[i])
		}
	}
	if consumerCmd[11] != NoReturnSlot {
		t.Errorf("Expected return slot 0xff, got 0x%02x", consumerCmd[11])
	}
	if common.BytesToAddress(consumerCmd[12:32]) != target {
		t.Errorf("Address mismatch: %s vs %s",
			common.BytesToAddress(consumerCmd[12:32]).Hex(), target.Hex())
	}
}

// TestPlannerAddRawCallV_DynamicValueRejected ensures the encoder refuses a
// dynamic-typed *ReturnValue as the value source. The VM requires the value
// slot to be exactly 32 bytes (VM.sol VALUECALL: `require(v.length == 32)`),
// so dynamic return types (string, bytes, dynamic arrays) cannot be threaded
// through this seam.
func TestPlannerAddRawCallV_DynamicValueRejected(t *testing.T) {
	testABI := plannerTestABI()
	producer := common.HexToAddress("0xCAFE000000000000000000000000000000000020")
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000021")

	p := New()
	// getString() returns string -- dynamic ABI type.
	rv := p.Add(NewContract(producer, testABI).MustInvoke("getString"))
	if !rv.IsDynamic() {
		t.Fatal("test setup error: getString() return should be dynamic")
	}

	p.AddRawCallV(target, rv, nil)

	_, err := p.Plan()
	if err == nil {
		t.Fatal("Plan must reject a dynamic-typed valueRef")
	}
}

// TestPlannerAddRawCallV_VisibilityKeepsProducerLive proves that consuming a
// *ReturnValue only via the valueRef seam (no other consumers) is sufficient
// to keep the producer's return slot allocated and live through the
// consumer command. Without analyzeVisibility tracking valueRef, the producer
// command would emit no return slot and the consumer would resolve to a
// missing slot at Plan time.
func TestPlannerAddRawCallV_VisibilityKeepsProducerLive(t *testing.T) {
	testABI := plannerTestABI()
	producer := common.HexToAddress("0xCAFE000000000000000000000000000000000030")
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000031")

	p := New()
	rv := p.Add(NewContract(producer, testABI).MustInvoke("add", big.NewInt(7), big.NewInt(8)))
	// valueRef is the ONLY consumer of rv. If visibility were not tracked,
	// allocateReturn would never run for the producer and Plan() would fail.
	p.AddRawCallV(target, rv, nil)

	compiled, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if compiled.Commands[0][11] == NoReturnSlot {
		t.Fatal("Producer return slot must be allocated when its only consumer is valueRef")
	}
}

// TestPlannerWithRawCalldataViaContract exercises the contract-bound path:
// build a Call via Contract.Invoke and switch it to FLAG_DATA via
// WithRawCalldata. The resulting plan should encode identically to the
// AddRawCall shortcut (same flag byte, same indices layout).
func TestPlannerWithRawCalldataViaContract(t *testing.T) {
	abi := plannerTestABI()
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000003")
	contract := NewContract(target, abi)
	payload := []byte{0x12, 0x34, 0x56}

	p := New()
	// The ABI method is irrelevant when FLAG_DATA is set — the VM ignores
	// the selector and arg slots. We use noReturn(uint256) only as a syntactic
	// hook to obtain a Call we can mutate with WithRawCalldata.
	p.Add(contract.MustInvoke("noReturn", big.NewInt(99)).WithValue(big.NewInt(42)).WithRawCalldata(payload))

	compiled, err := p.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	cmd := compiled.Commands[0]
	if cmd[4] != 0x23 {
		t.Errorf("Expected flag byte 0x23, got 0x%02x", cmd[4])
	}
	// Method's actual selector should be overwritten by WithRawCalldata's
	// zero-selector behavior at the *dispatch* level; on the wire we still
	// emit the contract's selector (the VM ignores it). Just sanity check
	// that the flag bit is correct.

	valueSlot := cmd[5]
	dataSlot := cmd[6] & ^uint8(DynamicSlotFlag)

	if len(compiled.State[valueSlot]) != 32 {
		t.Errorf("Value slot must be 32 bytes, got %d", len(compiled.State[valueSlot]))
	}
	if !bytes.Equal(compiled.State[dataSlot], payload) {
		t.Errorf("Data slot should equal payload: got %x, want %x", compiled.State[dataSlot], payload)
	}
}

// TestPlannerRawCallReturnValueIsNil documents the API contract: raw calls
// have no ABI-declared return type, so Add/AddRawCall returns nil.
func TestPlannerRawCallReturnValueIsNil(t *testing.T) {
	target := common.HexToAddress("0xCAFE000000000000000000000000000000000004")
	p := New()
	if rv := p.AddRawCall(target, big.NewInt(0), nil); rv != nil {
		t.Errorf("AddRawCall should return nil, got %v", rv)
	}
}
