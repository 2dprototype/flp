package pyflp

import (
	"bytes"
	"fmt"
	"strconv"
)

// Event ID ranges. The ID of an event decides the size of its payload:
//
//	0   - 63   one byte
//	64  - 127  two bytes
//	128 - 191  four bytes
//	192 - 207  text (length prefixed with a VarInt)
//	208 - 255  data (length prefixed with a VarInt)
const (
	BYTE  = 0
	WORD  = 64
	DWORD = 128
	TEXT  = 192
	DATA  = 208
)

// EventID is the ID byte of an event.
type EventID uint8

// newTextIDs are IDs >= DATA which still hold (UTF-16) text.
var newTextIDs = []EventID{TEXT + 49, TEXT + 39, TEXT + 47}

func isNewTextID(id EventID) bool {
	for _, n := range newTextIDs {
		if id == n {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ID registry
// ---------------------------------------------------------------------------

var idInfos [256]*idEntry

func init() {
	for i := range idTable {
		e := &idTable[i]
		idInfos[e.ID] = e
	}
}

// IDName returns a readable name such as "ProjectID.Tempo" for known IDs and
// the decimal number for unknown ones.
func IDName(id EventID) string {
	if e := idInfos[id]; e != nil {
		name := e.Name
		if e.Deprecated {
			name = "_" + name
		}
		return groupNames[e.Group] + "." + name
	}
	return strconv.Itoa(int(id))
}

// GroupOf returns the group (PyFLP enum) an ID is a member of.
func GroupOf(id EventID) IDGroup {
	if e := idInfos[id]; e != nil {
		return e.Group
	}
	return GroupNone
}

// InGroups reports whether id is a member of any of the groups.
func InGroups(id EventID, groups ...IDGroup) bool {
	g := GroupOf(id)
	if g == GroupNone {
		return false
	}
	for _, x := range groups {
		if x == g {
			return true
		}
	}
	return false
}

// DeclaredType returns the event type PyFLP associates with id or nil.
func DeclaredType(id EventID) *EventType {
	if e := idInfos[id]; e != nil {
		return e.Type
	}
	return nil
}

// AllIDs returns every known event ID in table order.
func AllIDs() []EventID {
	out := make([]EventID, len(idTable))
	for i, e := range idTable {
		out[i] = e.ID
	}
	return out
}

// ---------------------------------------------------------------------------
// Event types
// ---------------------------------------------------------------------------

// EventKind classifies event types.
type EventKind int

const (
	KindScalar  EventKind = iota // bool / integer / float / colour (IDs 0-191)
	KindString                   // ASCII or UTF-16 text
	KindStruct                   // fixed layout structure (*Container)
	KindList                     // array of structures
	KindUnknown                  // raw bytes
)

func (k EventKind) String() string {
	switch k {
	case KindScalar:
		return "scalar"
	case KindString:
		return "string"
	case KindStruct:
		return "struct"
	case KindList:
		return "list"
	}
	return "unknown"
}

// EventType describes how the payload of an event is interpreted. The
// package level variables (U8Event, TrackEvent, ...) mirror PyFLP's classes.
type EventType struct {
	Name  string
	Kind  EventKind
	codec Field
	// ctx builds the parse keyword arguments from the payload length.
	ctx func(n int) Ctx
	// lo and hi restrict the IDs (when hi != 0); strIDs allows the text IDs.
	lo, hi int
	strIDs bool
	// check is run on the raw payload before parsing (warnings only).
	check func(data []byte)
}

func (t *EventType) String() string { return t.Name }

func (t *EventType) allows(id EventID) bool {
	if t.strIDs {
		return (int(id) >= TEXT && int(id) < DATA) || isNewTextID(id)
	}
	if t.hi != 0 {
		return int(id) >= t.lo && int(id) < t.hi
	}
	return true
}

func (t *EventType) allowedDesc() string {
	if t.strIDs {
		return "192-207 or " + fmt.Sprint(newTextIDs)
	}
	if t.hi != 0 {
		return fmt.Sprintf("%d-%d", t.lo, t.hi-1)
	}
	return "any"
}

// Scalar event types.
var (
	BoolEvent = &EventType{Name: "BoolEvent", Kind: KindScalar, lo: BYTE, hi: WORD, codec: fFlag}
	I8Event   = &EventType{Name: "I8Event", Kind: KindScalar, lo: BYTE, hi: WORD, codec: fI8}
	U8Event   = &EventType{Name: "U8Event", Kind: KindScalar, lo: BYTE, hi: WORD, codec: fU8}

	I16Event = &EventType{Name: "I16Event", Kind: KindScalar, lo: WORD, hi: DWORD, codec: fI16}
	U16Event = &EventType{Name: "U16Event", Kind: KindScalar, lo: WORD, hi: DWORD, codec: fU16}

	F32Event = &EventType{Name: "F32Event", Kind: KindScalar, lo: DWORD, hi: TEXT, codec: fF32}
	I32Event = &EventType{Name: "I32Event", Kind: KindScalar, lo: DWORD, hi: TEXT, codec: fI32}
	U32Event = &EventType{Name: "U32Event", Kind: KindScalar, lo: DWORD, hi: TEXT, codec: fU32}

	// U16TupleEvent stores two 16 bit integers (value type [2]int64).
	U16TupleEvent = &EventType{Name: "U16TupleEvent", Kind: KindScalar, lo: DWORD, hi: TEXT, codec: pairOf(fU16)}

	// ColorEvent stores a 4 byte colour (value type RGBA).
	ColorEvent = &EventType{Name: "ColorEvent", Kind: KindScalar, lo: DWORD, hi: TEXT, codec: colorField}

	// AsciiEvent / UnicodeEvent store text (value type string).
	AsciiEvent   = &EventType{Name: "AsciiEvent", Kind: KindString, strIDs: true, codec: nullTerminated(encAscii)}
	UnicodeEvent = &EventType{Name: "UnicodeEvent", Kind: KindString, strIDs: true, codec: nullTerminated(encUTF16LE)}

	// UnknownDataEvent is used for events whose structure is unknown.
	UnknownDataEvent = &EventType{Name: "UnknownDataEvent", Kind: KindUnknown, codec: fGreed}
)

// NativePluginEvent is the placeholder type for unimplemented native plugins.
var NativePluginEvent = UnknownDataEvent

var colorField Field = adaptField{
	sub: fBytes(4),
	dec: func(v interface{}) (interface{}, error) {
		b, _ := v.([]byte)
		return RGBAFromBytes(b), nil
	},
	enc: func(v interface{}) (interface{}, error) {
		c, ok := v.(RGBA)
		if !ok {
			return nil, fmt.Errorf("flp: expected RGBA, got %T", v)
		}
		return c.Bytes(), nil
	},
}

// ---------------------------------------------------------------------------
// Event
// ---------------------------------------------------------------------------

// Event is a single (ID, payload) pair of an FLP file.
//
// The original payload bytes are retained, so an event which was never
// modified is serialised back byte for byte. Modifying an event rebuilds its
// payload from the decoded value.
type Event struct {
	id    EventID
	typ   *EventType
	value interface{}
	raw   []byte
	ctx   Ctx
}

// NewEvent parses a payload.
//
// Returns an error wrapping ErrEventIDOutOfRange when id is not valid for
// typ and ErrInvalidEventChunkSize if a fixed size event gets a payload of the
// wrong size.
func NewEvent(id EventID, typ *EventType, data []byte) (*Event, error) {
	if typ == nil {
		return nil, fmt.Errorf("%w: nil event type", ErrFLP)
	}
	if !typ.allows(id) {
		return nil, eventIDOutOfRange(id, typ.allowedDesc())
	}
	if int(id) < TEXT {
		want := 4
		if int(id) < WORD {
			want = 1
		} else if int(id) < DWORD {
			want = 2
		}
		if len(data) != want {
			return nil, invalidChunkSize(want, len(data))
		}
	}
	if typ.check != nil {
		typ.check(data)
	}
	var ctx Ctx
	if typ.ctx != nil {
		ctx = typ.ctx(len(data))
	}
	v, err := typ.codec.parse(newReader(data), &ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("flp: cannot parse %s as %s: %w", IDName(id), typ.Name, err)
	}
	raw := make([]byte, len(data))
	copy(raw, data)
	return &Event{id: id, typ: typ, value: v, raw: raw, ctx: ctx}, nil
}

// NewEventFromValue builds an event from a decoded value.
func NewEventFromValue(id EventID, typ *EventType, value interface{}) (*Event, error) {
	if typ == nil {
		return nil, fmt.Errorf("%w: nil event type", ErrFLP)
	}
	if !typ.allows(id) {
		return nil, eventIDOutOfRange(id, typ.allowedDesc())
	}
	e := &Event{id: id, typ: typ, value: value}
	if err := e.Commit(); err != nil {
		return nil, err
	}
	return e, nil
}

// ID returns the event ID.
func (e *Event) ID() EventID { return e.id }

// Type returns the event type.
func (e *Event) Type() *EventType { return e.typ }

// Value returns the decoded payload. Its dynamic type depends on the event
// type (int64, float64, bool, string, RGBA, [2]int64, *Container, ...).
func (e *Event) Value() interface{} { return e.value }

// Data returns a copy of the serialised payload (without ID / length).
func (e *Event) Data() []byte {
	out := make([]byte, len(e.raw))
	copy(out, e.raw)
	return out
}

// Commit rebuilds the payload from the decoded value. It must be called after
// a *Container obtained from Value() was modified in place.
func (e *Event) Commit() error {
	var buf bytes.Buffer
	ctx := e.ctx
	if err := e.typ.codec.build(&buf, e.value, &ctx, nil); err != nil {
		return fmt.Errorf("flp: cannot build %s as %s: %w", IDName(e.id), e.typ.Name, err)
	}
	e.raw = buf.Bytes()
	return nil
}

// SetValue replaces the whole decoded value of the event. On error the event
// is left unchanged.
func (e *Event) SetValue(v interface{}) error {
	var buf bytes.Buffer
	ctx := e.ctx
	if err := e.typ.codec.build(&buf, v, &ctx, nil); err != nil {
		return err
	}
	// Re-parse so that the stored value always has the canonical Go type.
	reparsed, err := e.typ.codec.parse(newReader(buf.Bytes()), &ctx, nil)
	if err != nil {
		return err
	}
	e.value = reparsed
	e.raw = buf.Bytes()
	return nil
}

// Container returns the value of a struct event.
func (e *Event) Container() (*Container, bool) {
	c, ok := e.value.(*Container)
	return c, ok
}

// List returns the value of a list event made of structures.
func (e *Event) List() ([]*Container, bool) {
	l, ok := e.value.([]*Container)
	return l, ok
}

// Field reads a field of a struct event.
func (e *Event) Field(key string) (interface{}, bool) {
	c, ok := e.value.(*Container)
	if !ok {
		return nil, false
	}
	return c.Get(key)
}

// SetField changes a field of a struct event. The key must exist and its
// current value must not be nil (i.e. the field has to be present in the
// file), otherwise an error wrapping ErrPropertyCannotBeSet is returned.
func (e *Event) SetField(key string, v interface{}) error {
	c, ok := e.value.(*Container)
	if !ok {
		return fmt.Errorf("%w: %s is not a struct event", ErrPropertyCannotBeSet, IDName(e.id))
	}
	old, exists := c.Get(key)
	if !exists || old == nil {
		return cannotSet(e.id)
	}
	c.Set(key, v)
	if err := e.Commit(); err != nil {
		c.Set(key, old)
		return err
	}
	return nil
}

// Int returns the value of an integer / bool event.
func (e *Event) Int() (int64, bool) {
	n, err := toInt64(e.value)
	return n, err == nil
}

// Str returns the value of a text event.
func (e *Event) Str() (string, bool) {
	s, ok := e.value.(string)
	return s, ok
}

// Bytes serialises the event: ID, [VarInt length], payload.
func (e *Event) Bytes() []byte {
	out := make([]byte, 0, len(e.raw)+3)
	out = append(out, byte(e.id))
	if int(e.id) >= TEXT {
		out = append(out, encodeVarInt(len(e.raw))...)
	}
	return append(out, e.raw...)
}

// Size is the serialised size of the event in bytes.
func (e *Event) Size() int {
	switch {
	case int(e.id) >= TEXT:
		return len(e.Bytes())
	case int(e.id) >= DWORD:
		return 5
	case int(e.id) >= WORD:
		return 3
	}
	return 2
}

// Equal compares the IDs and serialised payloads of two events.
func (e *Event) Equal(o *Event) bool {
	if e == nil || o == nil {
		return e == o
	}
	return e.id == o.id && bytes.Equal(e.raw, o.raw)
}

func (e *Event) String() string {
	return fmt.Sprintf("<%s(id=%s, value=%v)>", e.typ.Name, IDName(e.id), e.value)
}
