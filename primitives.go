package flp

import "fmt"

// readVarInt reads the 7-bit varint used to prefix payload length for opcodes 0xC0-0xFF.
func readVarInt(r *byteReader) (uint32, error) {
	start := r.pos
	var value uint32
	shift := 0
	for {
		if shift > 35 {
			return 0, wrapParse("VarInt", r, start, "VarInt", nil, fmt.Errorf("varint exceeds 5 bytes (> 2^35) — corrupt"))
		}
		b, err := r.readU8()
		if err != nil {
			return 0, wrapParse("VarInt", r, start, "VarInt", nil, err)
		}
		// JS shifts wrap modulo 32; mirror that.
		value |= uint32(b&0x7f) << uint(shift&31)
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	return value, nil
}

// writeVarInt appends the varint encoding of v to out.
func writeVarInt(out []byte, v uint32) []byte {
	for v >= 0x80 {
		out = append(out, byte(v&0x7f)|0x80)
		v >>= 7
	}
	return append(out, byte(v&0x7f))
}

// measureVarInt returns the encoded size of v.
func measureVarInt(v uint32) int {
	n := 1
	for v >= 0x80 {
		n++
		v >>= 7
	}
	return n
}

// decodeUtf16LeBytes decodes a payload as a UTF-16LE null-terminated string.
// Payloads under 2 bytes decode to "" (FL 9 legacy 1-byte placeholders).
func decodeUtf16LeBytes(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	end := len(b)
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			end = i
			break
		}
	}
	return utf16LEToString(b[:end])
}

// decodeUtf8Bytes decodes a payload as a UTF-8 null-terminated string (tolerant).
func decodeUtf8Bytes(b []byte) string {
	end := len(b)
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			end = i
			break
		}
	}
	return decodeUTF8Lossy(b[:end])
}

// decodeUTF8Lossy converts bytes to a string replacing invalid sequences with U+FFFD.
func decodeUTF8Lossy(b []byte) string {
	return string([]rune(string(b)))
}
