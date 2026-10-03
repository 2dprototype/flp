package flp

import (
	"fmt"
	"strings"
)

// FLPParseErrorContext carries byte-offset and nesting context for a parse failure.
type FLPParseErrorContext struct {
	SchemaName         string
	ByteOffsetAbsolute int
	Opcode             *int
	EventIndex         *int
	NestingPath        []string
	PrecedingHex       string
}

// FLPParseError is the structured error returned by the FLP parser.
type FLPParseError struct {
	Ctx   FLPParseErrorContext
	Cause error
	msg   string
}

func (e *FLPParseError) Error() string { return e.msg }
func (e *FLPParseError) Unwrap() error { return e.Cause }

// NewFLPParseError builds an error with a formatted message.
func NewFLPParseError(ctx FLPParseErrorContext, cause error) *FLPParseError {
	return &FLPParseError{Ctx: ctx, Cause: cause, msg: formatParseMessage(ctx, cause)}
}

// Extend returns a copy with the extra context merged; extra path fragments are prepended.
func (e *FLPParseError) Extend(opcode *int, eventIndex *int, extraPath []string) *FLPParseError {
	merged := e.Ctx
	if opcode != nil {
		merged.Opcode = opcode
	}
	if eventIndex != nil {
		merged.EventIndex = eventIndex
	}
	if len(extraPath) > 0 {
		np := make([]string, 0, len(extraPath)+len(e.Ctx.NestingPath))
		np = append(np, extraPath...)
		np = append(np, e.Ctx.NestingPath...)
		merged.NestingPath = np
	}
	return NewFLPParseError(merged, e.Cause)
}

func formatParseMessage(ctx FLPParseErrorContext, cause error) string {
	parts := []string{fmt.Sprintf("at byte %d", ctx.ByteOffsetAbsolute)}
	if ctx.EventIndex != nil {
		parts = append(parts, fmt.Sprintf("event #%d", *ctx.EventIndex))
	}
	if ctx.Opcode != nil {
		parts = append(parts, "opcode 0x"+hexUpper2(*ctx.Opcode))
	}
	head := "FLPParseError " + strings.Join(parts, ", ")
	path := ""
	if len(ctx.NestingPath) > 0 {
		path = "\n  path: " + strings.Join(ctx.NestingPath, " › ")
	}
	causeMsg := ""
	if cause != nil {
		causeMsg = "\n  cause: " + cause.Error()
	}
	hex := ""
	if ctx.PrecedingHex != "" {
		hex = "\n  previous bytes (hex): " + ctx.PrecedingHex
	}
	return head + path + causeMsg + hex
}

// byteReader is a little-endian cursor over a byte slice.
type byteReader struct {
	data []byte
	pos  int
}

var errEOF = fmt.Errorf("unexpected end of input")

func (r *byteReader) readU8() (int, error) {
	if r.pos+1 > len(r.data) {
		return 0, errEOF
	}
	v := r.data[r.pos]
	r.pos++
	return int(v), nil
}

func (r *byteReader) readU16() (int, error) {
	if r.pos+2 > len(r.data) {
		return 0, errEOF
	}
	v := uint16(r.data[r.pos]) | uint16(r.data[r.pos+1])<<8
	r.pos += 2
	return int(v), nil
}

func (r *byteReader) readU32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, errEOF
	}
	d := r.data[r.pos:]
	v := uint32(d[0]) | uint32(d[1])<<8 | uint32(d[2])<<16 | uint32(d[3])<<24
	r.pos += 4
	return v, nil
}

func (r *byteReader) readBytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, errEOF
	}
	out := make([]byte, n)
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}

// capturePrecedingHex returns up to `window` bytes before startOffset as hex.
func capturePrecedingHex(data []byte, startOffset int, window int) string {
	from := startOffset - window
	if from < 0 {
		from = 0
	}
	if startOffset > len(data) {
		return "<unavailable>"
	}
	parts := make([]string, 0, startOffset-from)
	for i := from; i < startOffset; i++ {
		parts = append(parts, hex2(int(data[i])))
	}
	return strings.Join(parts, " ")
}

// wrapParse converts a raw error into an FLPParseError carrying context; an
// existing FLPParseError is extended with the path fragment instead.
func wrapParse(schema string, r *byteReader, start int, fragment string, opcode *int, err error) error {
	if err == nil {
		return nil
	}
	if pe, ok := err.(*FLPParseError); ok {
		return pe.Extend(opcode, nil, []string{fragment})
	}
	return NewFLPParseError(FLPParseErrorContext{
		SchemaName:         schema,
		ByteOffsetAbsolute: start,
		Opcode:             opcode,
		NestingPath:        []string{fragment},
		PrecedingHex:       capturePrecedingHex(r.data, start, 16),
	}, err)
}
