package weiroll

import (
	"encoding/hex"
)

// stateManager handles slot allocation, deduplication, and recycling.
type stateManager struct {
	state            [][]byte           // The state array
	literalSlotMap   map[string]uint8   // Literal hash -> slot for deduplication
	returnSlotMap    map[*Command]uint8 // Command -> its return slot
	freeSlots        []uint8            // Recycled slots available for reuse
	stateExpirations map[int][]uint8    // Command index -> slots freed after it
	config           *planConfig        // Plan configuration
	nextSlot         uint8              // Next slot to allocate
}

// newStateManager creates a new state manager.
func newStateManager(config *planConfig) *stateManager {
	return &stateManager{
		state:            make([][]byte, 0, 32),
		literalSlotMap:   make(map[string]uint8),
		returnSlotMap:    make(map[*Command]uint8),
		freeSlots:        make([]uint8, 0),
		stateExpirations: make(map[int][]uint8),
		config:           config,
		nextSlot:         0,
	}
}

// allocateLiteral adds a literal to state, with deduplication.
// Returns the slot index (with dynamic flag if applicable).
// Note: Literals never reuse freed slots because they need their initial
// value preserved throughout execution. Freed slots are only for return
// values which are written during execution.
func (sm *stateManager) allocateLiteral(lit *LiteralValue) (uint8, error) {
	// Create a key for deduplication
	key := hex.EncodeToString(lit.data)

	// Check for existing identical literal
	if slot, exists := sm.literalSlotMap[key]; exists {
		if lit.IsDynamic() {
			return slot | DynamicSlotFlag, nil
		}
		return slot, nil
	}

	// Always allocate a NEW slot for literals (don't reuse freed return slots)
	// This is because literals are in the initial state and must not be
	// overwritten by return values during execution.
	slot, err := sm.allocateNewSlot()
	if err != nil {
		return 0, err
	}

	sm.state[slot] = lit.data
	sm.literalSlotMap[key] = slot

	if lit.IsDynamic() {
		return slot | DynamicSlotFlag, nil
	}
	return slot, nil
}

// allocateReturn allocates a slot for a command's return value.
// lastUsage is the command index where this value is last used.
// Return slots CAN reuse freed slots since they're written during execution.
func (sm *stateManager) allocateReturn(cmd *Command, lastUsage int, isDynamic bool) (uint8, error) {
	slot, err := sm.allocateSlot()
	if err != nil {
		return 0, err
	}

	sm.returnSlotMap[cmd] = slot

	// Schedule slot for recycling after last usage (if optimization enabled)
	if sm.config.optimizeSlots {
		sm.stateExpirations[lastUsage] = append(sm.stateExpirations[lastUsage], slot)
	}

	if isDynamic {
		return slot | DynamicSlotFlag, nil
	}
	return slot, nil
}

// allocateSlot gets a free slot, either from recycled pool or new.
// Used for return value slots which can safely reuse freed slots.
func (sm *stateManager) allocateSlot() (uint8, error) {
	// Try to reuse a freed slot (if optimization enabled)
	if sm.config.optimizeSlots && len(sm.freeSlots) > 0 {
		slot := sm.freeSlots[len(sm.freeSlots)-1]
		sm.freeSlots = sm.freeSlots[:len(sm.freeSlots)-1]
		return slot, nil
	}

	return sm.allocateNewSlot()
}

// allocateNewSlot always allocates a fresh slot, never reusing freed ones.
// Used for literals which must preserve their initial value.
func (sm *stateManager) allocateNewSlot() (uint8, error) {
	if int(sm.nextSlot) >= sm.config.maxStateSlots {
		return 0, ErrSlotExhausted
	}

	slot := sm.nextSlot
	sm.nextSlot++
	sm.state = append(sm.state, nil) // Placeholder, will be filled later

	return slot, nil
}

// expireSlots marks slots as free after a command executes.
func (sm *stateManager) expireSlots(commandIndex int) {
	if slots, exists := sm.stateExpirations[commandIndex]; exists {
		sm.freeSlots = append(sm.freeSlots, slots...)
		delete(sm.stateExpirations, commandIndex)
	}
}

// getReturnSlot returns the slot for a command's return value.
func (sm *stateManager) getReturnSlot(cmd *Command) (uint8, bool) {
	slot, exists := sm.returnSlotMap[cmd]
	return slot, exists
}

// getSlotsForValue returns the state slot(s) for a Value. Most Value
// types occupy a single slot; *TupleValue expands into one slot per
// leaf field (recursively for nested static tuples).
//
// For literals, allocates if needed. For return values, looks up the
// existing slot. The returned slot indices already carry
// DynamicSlotFlag where applicable.
func (sm *stateManager) getSlotsForValue(v Value) ([]uint8, error) {
	switch val := v.(type) {
	case *LiteralValue:
		slot, err := sm.allocateLiteral(val)
		if err != nil {
			return nil, err
		}
		return []uint8{slot}, nil

	case *ReturnValue:
		slot, exists := sm.returnSlotMap[val.command]
		if !exists {
			return nil, ErrReturnValueNotVisible
		}
		if val.IsDynamic() {
			return []uint8{slot | DynamicSlotFlag}, nil
		}
		return []uint8{slot}, nil

	case *StateValue:
		return []uint8{StateSlotMarker}, nil

	case *SubplanValue:
		// Subplan commands are encoded separately
		// This returns a placeholder that will be replaced
		return []uint8{StateSlotMarker}, nil

	case *TupleValue:
		// Expand into per-field slots. Nested static tuples recurse
		// naturally and are flattened in declaration order.
		slots := make([]uint8, 0, len(val.children))
		for _, child := range val.children {
			childSlots, err := sm.getSlotsForValue(child)
			if err != nil {
				return nil, err
			}
			slots = append(slots, childSlots...)
		}
		return slots, nil

	default:
		return nil, &EncodingError{Value: v, Err: ErrReturnValueNotVisible}
	}
}

// finalize returns the completed state array as hex-encoded strings.
func (sm *stateManager) finalize() [][]byte {
	result := make([][]byte, len(sm.state))
	for i, data := range sm.state {
		if data == nil {
			result[i] = make([]byte, 32) // Zero-filled placeholder
		} else {
			result[i] = data
		}
	}
	return result
}

// finalizeAsHex returns the state as hex strings (for debugging/testing).
func (sm *stateManager) finalizeAsHex() []string {
	result := make([]string, len(sm.state))
	for i, data := range sm.state {
		if data == nil {
			result[i] = "0x" + hex.EncodeToString(make([]byte, 32))
		} else {
			result[i] = "0x" + hex.EncodeToString(data)
		}
	}
	return result
}
