package flpdiff

import "fmt"

// EventKind identifies the payload encoding of an FLP event.
type EventKind string

const (
	KindU8   EventKind = "u8"
	KindU16  EventKind = "u16"
	KindU32  EventKind = "u32"
	KindBlob EventKind = "blob"
)

// FLPEvent is one FLP TLV event. Value is set for u8/u16/u32; Payload for blob.
type FLPEvent struct {
	Kind    EventKind
	Opcode  int
	Value   uint32
	Payload []byte
}

// fl25Byte3Opcodes lists opcodes with a fixed 3-byte payload (no size prefix).
var fl25Byte3Opcodes = map[int]bool{0xac: true}

// readEvent reads one TLV event.
//
//	0x00-0x3F → 1-byte payload
//	0x40-0x7F → 2-byte payload (uint16 LE)
//	0x80-0xBF → 4-byte payload (uint32 LE)
//	0xC0-0xFF → varint-prefixed N-byte payload
func readEvent(r *byteReader) (FLPEvent, error) {
	evStart := r.pos
	opcode, err := r.readU8()
	if err != nil {
		return FLPEvent{}, wrapParse("FLPEvent", r, evStart, "FLPEvent", nil, err)
	}
	payStart := r.pos
	op := opcode
	frag := "0x" + hexUpper2(opcode)
	fail := func(e error) (FLPEvent, error) {
		inner := wrapParse("FLPEvent.payload", r, payStart, frag, &op, e)
		return FLPEvent{}, wrapParse("FLPEvent", r, evStart, "FLPEvent", nil, inner)
	}
	if fl25Byte3Opcodes[opcode] {
		b, err := r.readBytes(3)
		if err != nil {
			return fail(err)
		}
		return FLPEvent{Kind: KindBlob, Opcode: opcode, Payload: b}, nil
	}
	if opcode >= 0xc0 {
		n, err := readVarInt(r)
		if err != nil {
			return fail(err)
		}
		b, err := r.readBytes(int(n))
		if err != nil {
			return fail(err)
		}
		return FLPEvent{Kind: KindBlob, Opcode: opcode, Payload: b}, nil
	}
	if opcode < 0x40 {
		v, err := r.readU8()
		if err != nil {
			return fail(err)
		}
		return FLPEvent{Kind: KindU8, Opcode: opcode, Value: uint32(v)}, nil
	}
	if opcode < 0x80 {
		v, err := r.readU16()
		if err != nil {
			return fail(err)
		}
		return FLPEvent{Kind: KindU16, Opcode: opcode, Value: uint32(v)}, nil
	}
	v, err := r.readU32()
	if err != nil {
		return fail(err)
	}
	return FLPEvent{Kind: KindU32, Opcode: opcode, Value: v}, nil
}

// appendEvent serialises an event, appending to out.
func appendEvent(out []byte, ev FLPEvent) ([]byte, error) {
	out = append(out, byte(ev.Opcode))
	switch ev.Kind {
	case KindU8:
		return append(out, byte(ev.Value)), nil
	case KindU16:
		return append(out, byte(ev.Value), byte(ev.Value>>8)), nil
	case KindU32:
		return append(out, byte(ev.Value), byte(ev.Value>>8), byte(ev.Value>>16), byte(ev.Value>>24)), nil
	case KindBlob:
		if fl25Byte3Opcodes[ev.Opcode] {
			if len(ev.Payload) != 3 {
				return nil, fmt.Errorf("byte3 opcode 0x%x expects 3-byte payload, got %d", ev.Opcode, len(ev.Payload))
			}
			return append(out, ev.Payload...), nil
		}
		out = writeVarInt(out, uint32(len(ev.Payload)))
		return append(out, ev.Payload...), nil
	}
	return nil, fmt.Errorf("unknown event kind %q", ev.Kind)
}

// eventSize returns the on-disk size of an event.
func eventSize(ev FLPEvent) int {
	switch ev.Kind {
	case KindU8:
		return 2
	case KindU16:
		return 3
	case KindU32:
		return 5
	default:
		if fl25Byte3Opcodes[ev.Opcode] {
			return 4
		}
		return measureVarInt(uint32(len(ev.Payload))) + 1 + len(ev.Payload)
	}
}
