package weiroll

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestCommandEncoderEncode(t *testing.T) {
	encoder := NewCommandEncoder()

	selector := [4]byte{0x12, 0x34, 0x56, 0x78}
	address := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	argSlots := []uint8{0, 1, 2}
	returnSlot := uint8(3)

	cmd := encoder.Encode(selector, FlagCall, argSlots, returnSlot, address)

	t.Run("command size", func(t *testing.T) {
		if len(cmd) != CommandSize {
			t.Errorf("Expected %d bytes, got %d", CommandSize, len(cmd))
		}
	})

	t.Run("selector encoding", func(t *testing.T) {
		if !bytes.Equal(cmd[0:4], selector[:]) {
			t.Error("Selector mismatch")
		}
	})

	t.Run("flags encoding", func(t *testing.T) {
		if cmd[4] != byte(FlagCall) {
			t.Errorf("Expected flag %d, got %d", FlagCall, cmd[4])
		}
	})

	t.Run("argument slots encoding", func(t *testing.T) {
		for i, slot := range argSlots {
			if cmd[5+i] != slot {
				t.Errorf("Arg slot %d: expected %d, got %d", i, slot, cmd[5+i])
			}
		}
		// Remaining slots should be 0xFF
		for i := len(argSlots); i < MaxStandardArgs; i++ {
			if cmd[5+i] != UnusedSlot {
				t.Errorf("Unused slot %d: expected %d, got %d", i, UnusedSlot, cmd[5+i])
			}
		}
	})

	t.Run("return slot encoding", func(t *testing.T) {
		if cmd[11] != returnSlot {
			t.Errorf("Expected return slot %d, got %d", returnSlot, cmd[11])
		}
	})

	t.Run("address encoding", func(t *testing.T) {
		decodedAddr := common.BytesToAddress(cmd[12:32])
		if decodedAddr != address {
			t.Errorf("Address mismatch: expected %s, got %s", address.Hex(), decodedAddr.Hex())
		}
	})
}

func TestCommandEncoderEncodeAllFlags(t *testing.T) {
	encoder := NewCommandEncoder()
	address := common.Address{}
	selector := [4]byte{}

	tests := []struct {
		name  string
		flags CallFlags
	}{
		{"Call", FlagCall},
		{"StaticCall", FlagStaticCall},
		{"CallWithValue", FlagCallWithValue},
		{"ExtendedCommand", FlagExtendedCommand},
		{"TupleReturn", FlagTupleReturn},
		{"Combined flags", FlagCall | FlagExtendedCommand | FlagTupleReturn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := encoder.Encode(selector, tt.flags, nil, NoReturnSlot, address)
			if CallFlags(cmd[4]) != tt.flags {
				t.Errorf("Expected flags %d, got %d", tt.flags, cmd[4])
			}
		})
	}
}

func TestCommandEncoderEncodeExtended(t *testing.T) {
	encoder := NewCommandEncoder()

	selector := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	argSlots := []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9} // 10 args
	returnSlot := uint8(10)

	cmd := encoder.EncodeExtended(selector, FlagCall, argSlots, returnSlot, address)

	t.Run("extended command size", func(t *testing.T) {
		if len(cmd) != ExtendedCommandSize {
			t.Errorf("Expected %d bytes, got %d", ExtendedCommandSize, len(cmd))
		}
	})

	t.Run("extended flag set", func(t *testing.T) {
		if cmd[4]&byte(FlagExtendedCommand) == 0 {
			t.Error("Extended flag not set")
		}
	})

	t.Run("word 1 input bytes are 0xff (VM ignores them)", func(t *testing.T) {
		for i := 0; i < MaxStandardArgs; i++ {
			if cmd[5+i] != UnusedSlot {
				t.Errorf("Word 1 byte %d: expected 0x%02x (unused), got 0x%02x", 5+i, UnusedSlot, cmd[5+i])
			}
		}
	})

	t.Run("all args in word 2", func(t *testing.T) {
		for i := 0; i < len(argSlots); i++ {
			if cmd[32+i] != argSlots[i] {
				t.Errorf("Word 2 arg %d: expected %d, got %d", i, argSlots[i], cmd[32+i])
			}
		}
	})

	t.Run("word 2 padding", func(t *testing.T) {
		for i := len(argSlots); i < 32; i++ {
			if cmd[32+i] != UnusedSlot {
				t.Errorf("Word 2 padding at %d: expected %d, got %d", i, UnusedSlot, cmd[32+i])
			}
		}
	})
}

func TestCommandEncoderEncodeCommand(t *testing.T) {
	encoder := NewCommandEncoder()
	address := common.Address{}
	selector := [4]byte{}

	t.Run("standard command for <= 6 args", func(t *testing.T) {
		argSlots := []uint8{0, 1, 2, 3, 4, 5}
		cmd, err := encoder.EncodeCommand(selector, FlagCall, argSlots, NoReturnSlot, address)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(cmd) != CommandSize {
			t.Errorf("Expected %d bytes for standard command, got %d", CommandSize, len(cmd))
		}
	})

	t.Run("extended command for > 6 args", func(t *testing.T) {
		argSlots := make([]uint8, 10)
		cmd, err := encoder.EncodeCommand(selector, FlagCall, argSlots, NoReturnSlot, address)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(cmd) != ExtendedCommandSize {
			t.Errorf("Expected %d bytes for extended command, got %d", ExtendedCommandSize, len(cmd))
		}
	})

	t.Run("error for too many args", func(t *testing.T) {
		argSlots := make([]uint8, MaxExtendedArgs+1)
		_, err := encoder.EncodeCommand(selector, FlagCall, argSlots, NoReturnSlot, address)

		if err != ErrTooManyArguments {
			t.Errorf("Expected ErrTooManyArguments, got %v", err)
		}
	})

	t.Run("max extended args succeeds", func(t *testing.T) {
		argSlots := make([]uint8, MaxExtendedArgs)
		_, err := encoder.EncodeCommand(selector, FlagCall, argSlots, NoReturnSlot, address)

		if err != nil {
			t.Errorf("Max extended args should succeed, got error: %v", err)
		}
	})
}

func TestDecodeCommand(t *testing.T) {
	encoder := NewCommandEncoder()

	t.Run("decode standard command", func(t *testing.T) {
		originalSelector := [4]byte{0x12, 0x34, 0x56, 0x78}
		originalAddress := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
		originalArgSlots := []uint8{0, 1, 2}
		originalReturnSlot := uint8(3)

		cmd := encoder.Encode(originalSelector, FlagCall, originalArgSlots, originalReturnSlot, originalAddress)

		selector, flags, argSlots, returnSlot, address, err := DecodeCommand(cmd)

		if err != nil {
			t.Fatalf("DecodeCommand failed: %v", err)
		}

		if selector != originalSelector {
			t.Error("Selector mismatch")
		}

		if flags.CallType() != FlagCall {
			t.Errorf("Expected Call flag, got %v", flags)
		}

		if len(argSlots) != len(originalArgSlots) {
			t.Errorf("Expected %d args, got %d", len(originalArgSlots), len(argSlots))
		}

		for i, slot := range originalArgSlots {
			if argSlots[i] != slot {
				t.Errorf("Arg slot %d mismatch", i)
			}
		}

		if returnSlot != originalReturnSlot {
			t.Errorf("Expected return slot %d, got %d", originalReturnSlot, returnSlot)
		}

		if address != originalAddress {
			t.Error("Address mismatch")
		}
	})

	t.Run("decode extended command", func(t *testing.T) {
		originalSelector := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
		originalAddress := common.HexToAddress("0x1111111111111111111111111111111111111111")
		originalArgSlots := []uint8{0, 1, 2, 3, 4, 5, 6, 7}
		originalReturnSlot := uint8(8)

		cmd := encoder.EncodeExtended(originalSelector, FlagCall, originalArgSlots, originalReturnSlot, originalAddress)

		selector, flags, argSlots, returnSlot, address, err := DecodeCommand(cmd)

		if err != nil {
			t.Fatalf("DecodeCommand failed: %v", err)
		}

		if selector != originalSelector {
			t.Error("Selector mismatch")
		}

		if !flags.IsExtended() {
			t.Error("Extended flag should be set")
		}

		if len(argSlots) != len(originalArgSlots) {
			t.Errorf("Expected %d args, got %d", len(originalArgSlots), len(argSlots))
		}

		if returnSlot != originalReturnSlot {
			t.Errorf("Expected return slot %d, got %d", originalReturnSlot, returnSlot)
		}

		if address != originalAddress {
			t.Error("Address mismatch")
		}
	})

	t.Run("decode too short command", func(t *testing.T) {
		_, _, _, _, _, err := DecodeCommand([]byte{0x01, 0x02, 0x03})
		if err == nil {
			t.Error("Expected error for short command")
		}
	})
}

func TestCallFlagsMethods(t *testing.T) {
	t.Run("CallType", func(t *testing.T) {
		tests := []struct {
			flags    CallFlags
			expected CallFlags
		}{
			{FlagCall, FlagCall},
			{FlagStaticCall, FlagStaticCall},
			{FlagCallWithValue, FlagCallWithValue},
			{FlagCall | FlagExtendedCommand, FlagCall},
			{FlagStaticCall | FlagTupleReturn, FlagStaticCall},
		}

		for _, tt := range tests {
			if tt.flags.CallType() != tt.expected {
				t.Errorf("CallType of %d: expected %d, got %d", tt.flags, tt.expected, tt.flags.CallType())
			}
		}
	})

	t.Run("IsExtended", func(t *testing.T) {
		tests := []struct {
			flags    CallFlags
			expected bool
		}{
			{FlagCall, false},
			{FlagExtendedCommand, true},
			{FlagCall | FlagExtendedCommand, true},
			{FlagTupleReturn, false},
		}

		for _, tt := range tests {
			if tt.flags.IsExtended() != tt.expected {
				t.Errorf("IsExtended of %d: expected %v", tt.flags, tt.expected)
			}
		}
	})

	t.Run("HasTupleReturn", func(t *testing.T) {
		tests := []struct {
			flags    CallFlags
			expected bool
		}{
			{FlagCall, false},
			{FlagTupleReturn, true},
			{FlagCall | FlagTupleReturn, true},
			{FlagExtendedCommand, false},
		}

		for _, tt := range tests {
			if tt.flags.HasTupleReturn() != tt.expected {
				t.Errorf("HasTupleReturn of %d: expected %v", tt.flags, tt.expected)
			}
		}
	})
}

func TestSlotIndex(t *testing.T) {
	t.Run("non-dynamic slot", func(t *testing.T) {
		slot := NewSlotIndex(5, false)

		if slot.Index() != 5 {
			t.Errorf("Expected index 5, got %d", slot.Index())
		}
		if slot.IsDynamic() {
			t.Error("Expected non-dynamic slot")
		}
		if slot.Byte() != 5 {
			t.Errorf("Expected byte 5, got %d", slot.Byte())
		}
	})

	t.Run("dynamic slot", func(t *testing.T) {
		slot := NewSlotIndex(5, true)

		if slot.Index() != 5 {
			t.Errorf("Expected index 5, got %d", slot.Index())
		}
		if !slot.IsDynamic() {
			t.Error("Expected dynamic slot")
		}
		if slot.Byte() != (5 | DynamicSlotFlag) {
			t.Errorf("Expected byte %d, got %d", 5|DynamicSlotFlag, slot.Byte())
		}
	})

	t.Run("boundary values", func(t *testing.T) {
		// Test with max slot index (127)
		slot := NewSlotIndex(MaxStateSlots, false)
		if slot.Index() != MaxStateSlots {
			t.Errorf("Expected index %d, got %d", MaxStateSlots, slot.Index())
		}

		// Test dynamic at max
		dynSlot := NewSlotIndex(MaxStateSlots, true)
		if dynSlot.Index() != MaxStateSlots {
			t.Errorf("Expected index %d, got %d", MaxStateSlots, dynSlot.Index())
		}
		if !dynSlot.IsDynamic() {
			t.Error("Expected dynamic slot")
		}
	})

	t.Run("zero index", func(t *testing.T) {
		slot := NewSlotIndex(0, false)
		if slot.Index() != 0 {
			t.Errorf("Expected index 0, got %d", slot.Index())
		}

		dynSlot := NewSlotIndex(0, true)
		if dynSlot.Index() != 0 {
			t.Errorf("Expected index 0, got %d", dynSlot.Index())
		}
		if dynSlot.Byte() != DynamicSlotFlag {
			t.Errorf("Expected byte %d, got %d", DynamicSlotFlag, dynSlot.Byte())
		}
	})
}

func TestConstants(t *testing.T) {
	t.Run("command sizes", func(t *testing.T) {
		if CommandSize != 32 {
			t.Errorf("Expected CommandSize=32, got %d", CommandSize)
		}
		if ExtendedCommandSize != 64 {
			t.Errorf("Expected ExtendedCommandSize=64, got %d", ExtendedCommandSize)
		}
	})

	t.Run("argument limits", func(t *testing.T) {
		if MaxStandardArgs != 6 {
			t.Errorf("Expected MaxStandardArgs=6, got %d", MaxStandardArgs)
		}
		if MaxExtendedArgs != 32 {
			t.Errorf("Expected MaxExtendedArgs=32, got %d", MaxExtendedArgs)
		}
	})

	t.Run("slot constants", func(t *testing.T) {
		if MaxStateSlots != 127 {
			t.Errorf("Expected MaxStateSlots=127, got %d", MaxStateSlots)
		}
		if DynamicSlotFlag != 0x80 {
			t.Errorf("Expected DynamicSlotFlag=0x80, got %d", DynamicSlotFlag)
		}
		if StateSlotMarker != 0xFE {
			t.Errorf("Expected StateSlotMarker=0xFE, got %d", StateSlotMarker)
		}
		if NoReturnSlot != 0xFF {
			t.Errorf("Expected NoReturnSlot=0xFF, got %d", NoReturnSlot)
		}
		if UnusedSlot != 0xFF {
			t.Errorf("Expected UnusedSlot=0xFF, got %d", UnusedSlot)
		}
	})

	t.Run("flag values", func(t *testing.T) {
		if FlagCall != 0x01 {
			t.Errorf("Expected FlagCall=0x01, got %d", FlagCall)
		}
		if FlagStaticCall != 0x02 {
			t.Errorf("Expected FlagStaticCall=0x02, got %d", FlagStaticCall)
		}
		if FlagCallWithValue != 0x03 {
			t.Errorf("Expected FlagCallWithValue=0x03, got %d", FlagCallWithValue)
		}
		if FlagCallTypeMask != 0x03 {
			t.Errorf("Expected FlagCallTypeMask=0x03, got %d", FlagCallTypeMask)
		}
		if FlagExtendedCommand != 0x40 {
			t.Errorf("Expected FlagExtendedCommand=0x40, got %d", FlagExtendedCommand)
		}
		if FlagTupleReturn != 0x80 {
			t.Errorf("Expected FlagTupleReturn=0x80, got %d", FlagTupleReturn)
		}
	})
}

func TestNewCommandEncoder(t *testing.T) {
	encoder := NewCommandEncoder()
	if encoder == nil {
		t.Error("Expected non-nil encoder")
	}
}

func TestEncodeEmptyArgs(t *testing.T) {
	encoder := NewCommandEncoder()
	selector := [4]byte{0x12, 0x34, 0x56, 0x78}
	address := common.Address{}

	cmd := encoder.Encode(selector, FlagCall, nil, NoReturnSlot, address)

	// All arg slots should be 0xFF
	for i := 0; i < MaxStandardArgs; i++ {
		if cmd[5+i] != UnusedSlot {
			t.Errorf("Empty arg slot %d should be %d, got %d", i, UnusedSlot, cmd[5+i])
		}
	}
}

func TestEncodeCommandRoundtrip(t *testing.T) {
	encoder := NewCommandEncoder()

	testCases := []struct {
		name       string
		selector   [4]byte
		flags      CallFlags
		argSlots   []uint8
		returnSlot uint8
		address    common.Address
	}{
		{
			name:       "simple call",
			selector:   [4]byte{0x12, 0x34, 0x56, 0x78},
			flags:      FlagCall,
			argSlots:   []uint8{0, 1},
			returnSlot: 2,
			address:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		},
		{
			name:       "no args no return",
			selector:   [4]byte{0xAA, 0xBB, 0xCC, 0xDD},
			flags:      FlagCall,
			argSlots:   nil,
			returnSlot: NoReturnSlot,
			address:    common.Address{},
		},
		{
			name:       "max standard args",
			selector:   [4]byte{0x11, 0x22, 0x33, 0x44},
			flags:      FlagStaticCall,
			argSlots:   []uint8{0, 1, 2, 3, 4, 5},
			returnSlot: 6,
			address:    common.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := encoder.EncodeCommand(tc.selector, tc.flags, tc.argSlots, tc.returnSlot, tc.address)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			selector, flags, argSlots, returnSlot, address, err := DecodeCommand(cmd)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			if selector != tc.selector {
				t.Errorf("Selector mismatch: expected %s, got %s",
					hex.EncodeToString(tc.selector[:]),
					hex.EncodeToString(selector[:]))
			}

			if flags.CallType() != tc.flags.CallType() {
				t.Errorf("Flags mismatch: expected %d, got %d", tc.flags, flags)
			}

			if len(argSlots) != len(tc.argSlots) {
				t.Errorf("Arg slots length mismatch: expected %d, got %d", len(tc.argSlots), len(argSlots))
			}

			if returnSlot != tc.returnSlot {
				t.Errorf("Return slot mismatch: expected %d, got %d", tc.returnSlot, returnSlot)
			}

			if address != tc.address {
				t.Errorf("Address mismatch: expected %s, got %s", tc.address.Hex(), address.Hex())
			}
		})
	}
}
