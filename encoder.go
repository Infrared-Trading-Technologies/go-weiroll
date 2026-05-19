package weiroll

import (
	"github.com/ethereum/go-ethereum/common"
)

// Command encoding constants.
const (
	// CommandSize is the standard command size in bytes.
	CommandSize = 32

	// ExtendedCommandSize is the size for commands with >6 arguments.
	ExtendedCommandSize = 64

	// MaxStandardArgs is the maximum arguments for a standard command.
	MaxStandardArgs = 6

	// MaxExtendedArgs is the maximum arguments for an extended command.
	MaxExtendedArgs = 32

	// MaxStateSlots is the maximum number of state slots available.
	MaxStateSlots = 127

	// DynamicSlotFlag is OR'd with slot index to mark dynamic types.
	DynamicSlotFlag = 0x80

	// StateSlotMarker is a special slot value for planner state reference.
	StateSlotMarker = 0xFE

	// NoReturnSlot indicates no return value is stored.
	NoReturnSlot = 0xFF

	// UnusedSlot is used to pad unused argument slots.
	UnusedSlot = 0xFF
)

// CallFlags represents the execution flags for a command.
type CallFlags uint8

const (
	// FlagCall uses regular CALL (for external contracts).
	FlagCall CallFlags = 0x01

	// FlagStaticCall uses STATICCALL (for read-only calls).
	FlagStaticCall CallFlags = 0x02

	// FlagCallWithValue uses CALL with ETH value transfer.
	FlagCallWithValue CallFlags = 0x03

	// FlagCallTypeMask masks the call type bits.
	FlagCallTypeMask CallFlags = 0x03

	// FlagData, combined with FlagCallWithValue, makes the VM dispatcher take
	// the call's calldata verbatim from a state slot instead of building it
	// from selector + ABI-encoded args. Empty bytes in the slot -> zero-byte
	// calldata (invokes the target's receive()). The selector field of the
	// command is ignored on this path. Bit 5.
	FlagData CallFlags = 0x20

	// FlagExtendedCommand indicates an extended command (>6 args). Bit 6.
	FlagExtendedCommand CallFlags = 0x40

	// FlagTupleReturn wraps multi-return values as raw bytes. Bit 7.
	FlagTupleReturn CallFlags = 0x80
)

// CallType returns just the call type portion of the flags.
func (f CallFlags) CallType() CallFlags {
	return f & FlagCallTypeMask
}

// IsExtended returns true if this is an extended command.
func (f CallFlags) IsExtended() bool {
	return f&FlagExtendedCommand != 0
}

// HasTupleReturn returns true if return values are wrapped as bytes.
func (f CallFlags) HasTupleReturn() bool {
	return f&FlagTupleReturn != 0
}

// CommandEncoder handles the encoding of weiroll commands.
type CommandEncoder struct{}

// NewCommandEncoder creates a new command encoder.
func NewCommandEncoder() *CommandEncoder {
	return &CommandEncoder{}
}

// Encode produces a 32-byte standard command encoding.
// Format: [selector:4][flags:1][arg0-5:6][return:1][address:20]
func (e *CommandEncoder) Encode(
	selector [4]byte,
	flags CallFlags,
	argSlots []uint8,
	returnSlot uint8,
	address common.Address,
) []byte {
	cmd := make([]byte, CommandSize)

	// Bytes 0-3: Function selector
	copy(cmd[0:4], selector[:])

	// Byte 4: Flags
	cmd[4] = byte(flags)

	// Bytes 5-10: Argument slots (up to 6, pad with 0xFF)
	for i := 0; i < MaxStandardArgs; i++ {
		if i < len(argSlots) {
			cmd[5+i] = argSlots[i]
		} else {
			cmd[5+i] = UnusedSlot
		}
	}

	// Byte 11: Return slot
	cmd[11] = returnSlot

	// Bytes 12-31: Contract address
	copy(cmd[12:32], address.Bytes())

	return cmd
}

// EncodeExtended produces a 64-byte extended command for 7+ arguments.
// Format:
//
//	Word 1: [selector:4][flags|EXTENDED:1][0xff×6:6][returnSlot:1][address:20]
//	Word 2: [arg slots padded to 32 bytes with 0xFF]
//
// VM._execute reads `indices = commands[++i]` for extended commands, so
// the input arg specifiers MUST live entirely in Word 2; Word 1's input
// bytes (5..10) are unused and filled with 0xff.
func (e *CommandEncoder) EncodeExtended(
	selector [4]byte,
	flags CallFlags,
	argSlots []uint8,
	returnSlot uint8,
	address common.Address,
) []byte {
	cmd := make([]byte, ExtendedCommandSize)

	// Word 1: First 32 bytes

	// Bytes 0-3: Function selector
	copy(cmd[0:4], selector[:])

	// Byte 4: Flags with EXTENDED bit set
	cmd[4] = byte(flags | FlagExtendedCommand)

	// Bytes 5-10: VM ignores these for extended commands. Fill with the
	// end-of-args sentinel so an accidental short-command read also
	// produces a no-arg call rather than reading garbage slots.
	for i := 0; i < MaxStandardArgs; i++ {
		cmd[5+i] = UnusedSlot
	}

	// Byte 11: Return slot
	cmd[11] = returnSlot

	// Bytes 12-31: Contract address
	copy(cmd[12:32], address.Bytes())

	// Word 2: ALL argument slots (up to 32), padded with 0xff.
	for i := 0; i < 32; i++ {
		if i < len(argSlots) {
			cmd[32+i] = argSlots[i]
		} else {
			cmd[32+i] = UnusedSlot
		}
	}

	return cmd
}

// EncodeCommand encodes a command, choosing standard or extended format.
func (e *CommandEncoder) EncodeCommand(
	selector [4]byte,
	flags CallFlags,
	argSlots []uint8,
	returnSlot uint8,
	address common.Address,
) ([]byte, error) {
	if len(argSlots) > MaxExtendedArgs {
		return nil, ErrTooManyArguments
	}

	if len(argSlots) <= MaxStandardArgs {
		return e.Encode(selector, flags, argSlots, returnSlot, address), nil
	}

	return e.EncodeExtended(selector, flags, argSlots, returnSlot, address), nil
}

// DecodeCommand decodes a command byte slice into its components.
// Useful for debugging and testing.
func DecodeCommand(cmd []byte) (
	selector [4]byte,
	flags CallFlags,
	argSlots []uint8,
	returnSlot uint8,
	address common.Address,
	err error,
) {
	if len(cmd) < CommandSize {
		err = ErrTooManyArguments // Reusing error, could create a new one
		return
	}

	copy(selector[:], cmd[0:4])
	flags = CallFlags(cmd[4])

	if flags.IsExtended() && len(cmd) >= ExtendedCommandSize {
		// Extended command: all up-to-32 args live in Word 2; Word 1's
		// input-slot bytes (5..10) are unused (see EncodeExtended).
		argSlots = make([]uint8, 0, MaxExtendedArgs)
		for i := 0; i < 32; i++ {
			if cmd[32+i] != UnusedSlot {
				argSlots = append(argSlots, cmd[32+i])
			}
		}
	} else {
		// Standard command: up to 6 args
		argSlots = make([]uint8, 0, MaxStandardArgs)
		for i := 0; i < MaxStandardArgs; i++ {
			if cmd[5+i] != UnusedSlot {
				argSlots = append(argSlots, cmd[5+i])
			}
		}
	}

	returnSlot = cmd[11]
	address = common.BytesToAddress(cmd[12:32])

	return
}

// SlotIndex represents a state slot index with optional dynamic flag.
type SlotIndex uint8

// NewSlotIndex creates a slot index, optionally marking it as dynamic.
func NewSlotIndex(index uint8, isDynamic bool) SlotIndex {
	if isDynamic {
		return SlotIndex(index | DynamicSlotFlag)
	}
	return SlotIndex(index)
}

// Index returns the raw slot index (without dynamic flag).
func (s SlotIndex) Index() uint8 {
	return uint8(s) & ^uint8(DynamicSlotFlag)
}

// IsDynamic returns true if this slot contains a dynamic type.
func (s SlotIndex) IsDynamic() bool {
	return uint8(s)&DynamicSlotFlag != 0
}

// Byte returns the slot as a byte for command encoding.
func (s SlotIndex) Byte() uint8 {
	return uint8(s)
}
