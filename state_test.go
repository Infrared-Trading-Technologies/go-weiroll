package weiroll

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewStateManager(t *testing.T) {
	config := defaultPlanConfig()
	sm := newStateManager(config)

	if sm == nil {
		t.Fatal("Expected state manager to be non-nil")
	}

	if len(sm.state) != 0 {
		t.Errorf("Expected empty state, got %d slots", len(sm.state))
	}

	if len(sm.literalSlotMap) != 0 {
		t.Errorf("Expected empty literal map, got %d entries", len(sm.literalSlotMap))
	}

	if len(sm.returnSlotMap) != 0 {
		t.Errorf("Expected empty return map, got %d entries", len(sm.returnSlotMap))
	}

	if len(sm.freeSlots) != 0 {
		t.Errorf("Expected no free slots, got %d", len(sm.freeSlots))
	}

	if sm.nextSlot != 0 {
		t.Errorf("Expected nextSlot to be 0, got %d", sm.nextSlot)
	}
}

func TestAllocateLiteral(t *testing.T) {
	t.Run("allocates slot for literal", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		lit := Uint256(big.NewInt(100))
		slot, err := sm.allocateLiteral(lit)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Static value should not have dynamic flag
		if slot&DynamicSlotFlag != 0 {
			t.Error("Static literal should not have dynamic flag")
		}

		if len(sm.state) != 1 {
			t.Errorf("Expected 1 slot, got %d", len(sm.state))
		}
	})

	t.Run("deduplicates identical literals", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		lit1 := Uint256(big.NewInt(42))
		lit2 := Uint256(big.NewInt(42))

		slot1, err := sm.allocateLiteral(lit1)
		if err != nil {
			t.Fatalf("Expected no error for first literal, got %v", err)
		}

		slot2, err := sm.allocateLiteral(lit2)
		if err != nil {
			t.Fatalf("Expected no error for second literal, got %v", err)
		}

		if slot1 != slot2 {
			t.Errorf("Identical literals should share slot: got %d and %d", slot1, slot2)
		}

		if len(sm.state) != 1 {
			t.Errorf("Expected 1 slot (deduplicated), got %d", len(sm.state))
		}
	})

	t.Run("allocates different slots for different literals", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		lit1 := Uint256(big.NewInt(100))
		lit2 := Uint256(big.NewInt(200))

		slot1, err := sm.allocateLiteral(lit1)
		if err != nil {
			t.Fatalf("Expected no error for first literal, got %v", err)
		}

		slot2, err := sm.allocateLiteral(lit2)
		if err != nil {
			t.Fatalf("Expected no error for second literal, got %v", err)
		}

		// Mask off dynamic flag for comparison
		if slot1&^DynamicSlotFlag == slot2&^DynamicSlotFlag {
			t.Error("Different literals should have different slots")
		}

		if len(sm.state) != 2 {
			t.Errorf("Expected 2 slots, got %d", len(sm.state))
		}
	})

	t.Run("sets dynamic flag for dynamic types", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		lit := String("hello")
		slot, err := sm.allocateLiteral(lit)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if slot&DynamicSlotFlag == 0 {
			t.Error("String literal should have dynamic flag set")
		}
	})

	t.Run("returns error when slots exhausted", func(t *testing.T) {
		config := defaultPlanConfig()
		config.maxStateSlots = 2
		sm := newStateManager(config)

		// Allocate max slots
		for i := 0; i < 2; i++ {
			_, err := sm.allocateLiteral(Uint256(big.NewInt(int64(i))))
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
		}

		// Next allocation should fail
		_, err := sm.allocateLiteral(Uint256(big.NewInt(999)))
		if err != ErrSlotExhausted {
			t.Errorf("Expected ErrSlotExhausted, got %v", err)
		}
	})
}

func TestAllocateReturn(t *testing.T) {
	t.Run("allocates slot for return value", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		cmd := &Command{}
		slot, err := sm.allocateReturn(cmd, 5, false)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if slot&DynamicSlotFlag != 0 {
			t.Error("Static return should not have dynamic flag")
		}

		// Verify command is mapped
		storedSlot, exists := sm.returnSlotMap[cmd]
		if !exists {
			t.Error("Command should be in return slot map")
		}
		if storedSlot != slot {
			t.Errorf("Stored slot %d doesn't match returned slot %d", storedSlot, slot)
		}
	})

	t.Run("sets dynamic flag for dynamic return", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		cmd := &Command{}
		slot, err := sm.allocateReturn(cmd, 5, true)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if slot&DynamicSlotFlag == 0 {
			t.Error("Dynamic return should have dynamic flag set")
		}
	})

	t.Run("schedules expiration when optimization enabled", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true
		sm := newStateManager(config)

		cmd := &Command{}
		_, err := sm.allocateReturn(cmd, 10, false)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(sm.stateExpirations[10]) != 1 {
			t.Errorf("Expected 1 expiration at command 10, got %d", len(sm.stateExpirations[10]))
		}
	})

	t.Run("skips expiration when optimization disabled", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = false
		sm := newStateManager(config)

		cmd := &Command{}
		_, err := sm.allocateReturn(cmd, 10, false)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(sm.stateExpirations) != 0 {
			t.Error("Expirations should not be scheduled when optimization disabled")
		}
	})
}

func TestAllocateSlot(t *testing.T) {
	t.Run("allocates sequential slots", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		for i := 0; i < 5; i++ {
			slot, err := sm.allocateSlot()
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if slot != uint8(i) {
				t.Errorf("Expected slot %d, got %d", i, slot)
			}
		}
	})

	t.Run("reuses freed slots when optimization enabled", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true
		sm := newStateManager(config)

		// Allocate some slots
		for i := 0; i < 3; i++ {
			sm.allocateSlot()
		}

		// Manually free slot 1
		sm.freeSlots = append(sm.freeSlots, 1)

		// Next allocation should reuse slot 1
		slot, err := sm.allocateSlot()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if slot != 1 {
			t.Errorf("Expected reused slot 1, got %d", slot)
		}
	})

	t.Run("ignores freed slots when optimization disabled", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = false
		sm := newStateManager(config)

		// Allocate some slots
		for i := 0; i < 3; i++ {
			sm.allocateSlot()
		}

		// Manually add a free slot (shouldn't happen in practice with optimization off)
		sm.freeSlots = append(sm.freeSlots, 1)

		// Next allocation should be slot 3, not reusing 1
		slot, err := sm.allocateSlot()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if slot != 3 {
			t.Errorf("Expected new slot 3, got %d", slot)
		}
	})

	t.Run("respects max state slots limit", func(t *testing.T) {
		config := defaultPlanConfig()
		config.maxStateSlots = 5
		sm := newStateManager(config)

		for i := 0; i < 5; i++ {
			_, err := sm.allocateSlot()
			if err != nil {
				t.Fatalf("Expected no error for slot %d, got %v", i, err)
			}
		}

		_, err := sm.allocateSlot()
		if err != ErrSlotExhausted {
			t.Errorf("Expected ErrSlotExhausted, got %v", err)
		}
	})
}

func TestExpireSlots(t *testing.T) {
	t.Run("frees slots at expiration", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true
		sm := newStateManager(config)

		// Schedule expirations
		sm.stateExpirations[5] = []uint8{1, 2}

		if len(sm.freeSlots) != 0 {
			t.Error("Free slots should be empty before expiration")
		}

		sm.expireSlots(5)

		if len(sm.freeSlots) != 2 {
			t.Errorf("Expected 2 free slots, got %d", len(sm.freeSlots))
		}

		if sm.freeSlots[0] != 1 || sm.freeSlots[1] != 2 {
			t.Errorf("Expected freed slots [1, 2], got %v", sm.freeSlots)
		}
	})

	t.Run("removes expiration entry", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		sm.stateExpirations[5] = []uint8{1}
		sm.expireSlots(5)

		if _, exists := sm.stateExpirations[5]; exists {
			t.Error("Expiration entry should be removed after processing")
		}
	})

	t.Run("handles missing expiration gracefully", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		// Should not panic
		sm.expireSlots(999)

		if len(sm.freeSlots) != 0 {
			t.Error("No slots should be freed for missing expiration")
		}
	})
}

func TestGetReturnSlot(t *testing.T) {
	t.Run("returns slot for known command", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		cmd := &Command{}
		sm.returnSlotMap[cmd] = 5

		slot, exists := sm.getReturnSlot(cmd)
		if !exists {
			t.Error("Expected command to exist")
		}
		if slot != 5 {
			t.Errorf("Expected slot 5, got %d", slot)
		}
	})

	t.Run("returns false for unknown command", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		cmd := &Command{}
		_, exists := sm.getReturnSlot(cmd)
		if exists {
			t.Error("Expected command to not exist")
		}
	})
}

func TestGetSlotForValue(t *testing.T) {
	config := defaultPlanConfig()

	t.Run("handles LiteralValue", func(t *testing.T) {
		sm := newStateManager(config)
		lit := Uint256(big.NewInt(42))

		slot, err := sm.getSlotForValue(lit)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if slot&DynamicSlotFlag != 0 {
			t.Error("uint256 should not have dynamic flag")
		}
	})

	t.Run("handles ReturnValue", func(t *testing.T) {
		sm := newStateManager(config)

		cmd := &Command{}
		sm.returnSlotMap[cmd] = 3

		rv := &ReturnValue{command: cmd}
		slot, err := sm.getSlotForValue(rv)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if slot != 3 {
			t.Errorf("Expected slot 3, got %d", slot)
		}
	})

	t.Run("returns error for unknown ReturnValue", func(t *testing.T) {
		sm := newStateManager(config)

		rv := &ReturnValue{command: &Command{}}
		_, err := sm.getSlotForValue(rv)

		if err != ErrReturnValueNotVisible {
			t.Errorf("Expected ErrReturnValueNotVisible, got %v", err)
		}
	})

	t.Run("handles StateValue", func(t *testing.T) {
		sm := newStateManager(config)

		sv := &StateValue{}
		slot, err := sm.getSlotForValue(sv)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if slot != StateSlotMarker {
			t.Errorf("Expected StateSlotMarker (0xFE), got 0x%02x", slot)
		}
	})

	t.Run("handles SubplanValue", func(t *testing.T) {
		sm := newStateManager(config)

		spv := &SubplanValue{}
		slot, err := sm.getSlotForValue(spv)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if slot != StateSlotMarker {
			t.Errorf("Expected StateSlotMarker (0xFE), got 0x%02x", slot)
		}
	})
}

func TestFinalize(t *testing.T) {
	t.Run("returns empty state for no allocations", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		result := sm.finalize()
		if len(result) != 0 {
			t.Errorf("Expected empty state, got %d slots", len(result))
		}
	})

	t.Run("returns state with literal data", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		sm.allocateLiteral(Uint256(big.NewInt(100)))
		result := sm.finalize()

		if len(result) != 1 {
			t.Fatalf("Expected 1 slot, got %d", len(result))
		}
		if len(result[0]) != 32 {
			t.Errorf("Expected 32 bytes, got %d", len(result[0]))
		}
	})

	t.Run("fills nil slots with zeros", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		// Allocate a slot but leave data nil
		sm.allocateSlot()
		sm.state[0] = nil

		result := sm.finalize()
		if len(result[0]) != 32 {
			t.Errorf("Expected 32 zero bytes, got %d bytes", len(result[0]))
		}

		// Check all zeros
		for _, b := range result[0] {
			if b != 0 {
				t.Error("Expected zero-filled slot")
				break
			}
		}
	})
}

func TestFinalizeAsHex(t *testing.T) {
	t.Run("returns hex strings", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		sm.allocateLiteral(Uint256(big.NewInt(1)))
		result := sm.finalizeAsHex()

		if len(result) != 1 {
			t.Fatalf("Expected 1 slot, got %d", len(result))
		}

		if len(result[0]) < 2 || result[0][:2] != "0x" {
			t.Error("Expected hex string to start with '0x'")
		}
	})

	t.Run("formats nil slots as zeros", func(t *testing.T) {
		config := defaultPlanConfig()
		sm := newStateManager(config)

		sm.allocateSlot()
		sm.state[0] = nil

		result := sm.finalizeAsHex()

		// 32 bytes = 64 hex chars + "0x" prefix
		expected := "0x0000000000000000000000000000000000000000000000000000000000000000"
		if result[0] != expected {
			t.Errorf("Expected %s, got %s", expected, result[0])
		}
	})
}

func TestSlotRecyclingIntegration(t *testing.T) {
	t.Run("recycling reduces slot count", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true
		sm := newStateManager(config)

		// Simulate allocating and freeing
		cmd1 := &Command{}
		cmd2 := &Command{}

		// Allocate return for cmd1, expires at command 2
		sm.allocateReturn(cmd1, 2, false)

		// Allocate return for cmd2, expires at command 3
		sm.allocateReturn(cmd2, 3, false)

		// At this point, 2 slots used
		if sm.nextSlot != 2 {
			t.Errorf("Expected 2 slots allocated, got %d", sm.nextSlot)
		}

		// Expire cmd1's slot
		sm.expireSlots(2)

		// Free slots should have 1 entry
		if len(sm.freeSlots) != 1 {
			t.Errorf("Expected 1 free slot, got %d", len(sm.freeSlots))
		}

		// Next allocation should reuse
		newSlot, _ := sm.allocateSlot()
		if newSlot != 0 {
			t.Errorf("Expected reused slot 0, got %d", newSlot)
		}
	})
}

func TestDynamicValueSlots(t *testing.T) {
	config := defaultPlanConfig()

	t.Run("bytes value has dynamic flag", func(t *testing.T) {
		sm := newStateManager(config)
		lit := Bytes([]byte{1, 2, 3})

		slot, err := sm.getSlotForValue(lit)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if slot&DynamicSlotFlag == 0 {
			t.Error("bytes should have dynamic flag")
		}
	})

	t.Run("address value has no dynamic flag", func(t *testing.T) {
		sm := newStateManager(config)
		lit := Address(common.HexToAddress("0x1234567890123456789012345678901234567890"))

		slot, err := sm.getSlotForValue(lit)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if slot&DynamicSlotFlag != 0 {
			t.Error("address should not have dynamic flag")
		}
	})

	t.Run("bool value has no dynamic flag", func(t *testing.T) {
		sm := newStateManager(config)
		lit := Bool(true)

		slot, err := sm.getSlotForValue(lit)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if slot&DynamicSlotFlag != 0 {
			t.Error("bool should not have dynamic flag")
		}
	})
}

// TestLiteralsNeverReuseFreedReturnSlots tests that literals always get fresh
// slots and never reuse freed return value slots. This is critical because:
//  1. Literals are in the INITIAL state array
//  2. Return values are written DURING execution
//  3. If a literal reused a freed return slot, the return value would overwrite
//     the literal data during execution, corrupting later operations that need it.
//
// Scenario being tested (the bug that was fixed):
// - Command 0: some operation
// - Command 1: returns dynamic data, stored in slot N, used by Command 2
// - Command 2: uses Command 1's return (last usage, so slot N is freed after)
// - Command 3: has a NEW literal argument
// - BUG: literal was being allocated to freed slot N
// - FIX: literal must get a NEW slot, not reuse slot N
func TestLiteralsNeverReuseFreedReturnSlots(t *testing.T) {
	t.Run("literal allocation ignores freed return slots", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true // Enable slot optimization
		sm := newStateManager(config)

		// Simulate the scenario:
		// 1. Allocate some initial literals (like function args)
		lit1 := Uint256(big.NewInt(100))
		slot1, _ := sm.allocateLiteral(lit1)

		lit2 := Uint256(big.NewInt(200))
		slot2, _ := sm.allocateLiteral(lit2)

		// 2. Create a mock command and allocate its return slot
		mockCmd := &Command{returnSlot: -1}
		lastUsage := 2 // Will be "freed" after command index 2
		returnSlot, err := sm.allocateReturn(mockCmd, lastUsage, true)
		if err != nil {
			t.Fatalf("Failed to allocate return: %v", err)
		}
		returnSlotIdx := returnSlot & ^uint8(DynamicSlotFlag)

		// 3. Expire the slot (simulating that we've processed past lastUsage)
		sm.expireSlots(lastUsage)

		// Verify the return slot is now in the free list
		if len(sm.freeSlots) != 1 {
			t.Fatalf("Expected 1 free slot, got %d", len(sm.freeSlots))
		}
		if sm.freeSlots[0] != returnSlotIdx {
			t.Fatalf("Expected freed slot %d in freeSlots, got %d", returnSlotIdx, sm.freeSlots[0])
		}

		// 4. Now allocate a NEW literal - this is where the bug would manifest
		// The literal should NOT get the freed return slot
		lit3 := Uint256(big.NewInt(999)) // Different value, not deduplicated
		slot3, err := sm.allocateLiteral(lit3)
		if err != nil {
			t.Fatalf("Failed to allocate literal: %v", err)
		}

		// KEY ASSERTION: The new literal must NOT use the freed return slot
		if slot3 == returnSlotIdx {
			t.Errorf("BUG: Literal was allocated to freed return slot %d! "+
				"This would cause the return value to overwrite the literal during execution.",
				returnSlotIdx)
		}

		// The literal should get a fresh slot
		expectedSlot := uint8(3) // slot 0, 1, 2 already used, so next is 3
		if slot3 != expectedSlot {
			t.Errorf("Expected literal to get fresh slot %d, got slot %d", expectedSlot, slot3)
		}

		// The free slots should still contain the return slot (not consumed by literal)
		if len(sm.freeSlots) != 1 {
			t.Errorf("Expected freed return slot to remain available, but freeSlots has %d entries", len(sm.freeSlots))
		}

		// Verify the state array has correct data
		if sm.state[slot3] == nil {
			t.Error("Literal slot should have data")
		}

		// Cleanup: verify we used the expected slots
		t.Logf("Slot allocation: lit1=%d, lit2=%d, return=%d, lit3=%d",
			slot1, slot2, returnSlotIdx, slot3)
	})

	t.Run("return slots can reuse freed return slots", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true
		sm := newStateManager(config)

		// Allocate first return slot
		cmd1 := &Command{returnSlot: -1}
		slot1, _ := sm.allocateReturn(cmd1, 1, false)

		// Expire it
		sm.expireSlots(1)

		// Allocate second return slot - should reuse the freed one
		cmd2 := &Command{returnSlot: -1}
		slot2, _ := sm.allocateReturn(cmd2, 2, false)

		// Return slots SHOULD reuse freed slots (that's the optimization)
		if slot2 != slot1 {
			t.Errorf("Expected return slot to reuse freed slot %d, but got %d", slot1, slot2)
		}
	})

	t.Run("dynamic literal also gets fresh slot", func(t *testing.T) {
		config := defaultPlanConfig()
		config.optimizeSlots = true
		sm := newStateManager(config)

		// Allocate and free a return slot
		cmd := &Command{returnSlot: -1}
		returnSlot, _ := sm.allocateReturn(cmd, 0, true)
		returnSlotIdx := returnSlot & ^uint8(DynamicSlotFlag)
		sm.expireSlots(0)

		// Allocate a dynamic literal (bytes is dynamic type)
		dynamicLit := Bytes([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
		litSlot, _ := sm.allocateLiteral(dynamicLit)
		litSlotIdx := litSlot & ^uint8(DynamicSlotFlag)

		// Dynamic literal should also not reuse the freed slot
		if litSlotIdx == returnSlotIdx {
			t.Errorf("BUG: Dynamic literal was allocated to freed return slot %d!", returnSlotIdx)
		}
	})
}
