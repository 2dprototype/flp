package pyflp

// This file is a tiny re-implementation of the parts of the `construct`
// library which PyFLP relies on. Every event payload is described by a Field;
// Fields parse bytes into plain Go values and build them back.
//
// Value representation:
//
//	integers            int64
//	floats              float64
//	flags / booleans    bool
//	byte blobs          []byte
//	strings             string
//	structs             *Container (ordered, string keyed)
//	arrays / lists      []interface{} or []*Container
//	absent Optional     nil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode/utf16"
)

var errEOF = errors.New("flp: unexpected end of data")
var errStop = errors.New("flp: stop field")

// Ctx carries the keyword arguments PyFLP passes to construct's parse/build.
type Ctx struct {
	Len int  // length of the event payload (StructEventBase)
	New bool // PlaylistEvent: FL Studio 21+ item layout
}

// ---------------------------------------------------------------------------
// Container: ordered string keyed dictionary (construct.Container)
// ---------------------------------------------------------------------------

// Container is an insertion-ordered map holding the fields of a struct.
type Container struct {
	keys []string
	vals map[string]interface{}
}

// NewContainer returns an empty Container.
func NewContainer() *Container {
	return &Container{vals: make(map[string]interface{})}
}

// Has reports whether key exists (even if its value is nil).
func (c *Container) Has(key string) bool {
	_, ok := c.vals[key]
	return ok
}

// Get returns the value stored for key and whether the key exists.
func (c *Container) Get(key string) (interface{}, bool) {
	v, ok := c.vals[key]
	return v, ok
}

// Set stores a value, appending the key if it is new.
func (c *Container) Set(key string, v interface{}) {
	if _, ok := c.vals[key]; !ok {
		c.keys = append(c.keys, key)
	}
	c.vals[key] = v
}

// Keys returns the keys in insertion order.
func (c *Container) Keys() []string {
	out := make([]string, len(c.keys))
	copy(out, c.keys)
	return out
}

// Len returns the number of keys.
func (c *Container) Len() int { return len(c.keys) }

// Int returns the value as an integer; ok is false if it is missing / nil.
func (c *Container) Int(key string) (int64, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return 0, false
	}
	n, err := toInt64(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Float returns the value as a float64.
func (c *Container) Float(key string) (float64, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return 0, false
	}
	f, err := toFloat64(v)
	if err != nil {
		return 0, false
	}
	return f, true
}

// Bool returns the value as a bool.
func (c *Container) Bool(key string) (bool, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return false, false
	}
	b, err := toBool(v)
	if err != nil {
		return false, false
	}
	return b, true
}

// Str returns the value as a string.
func (c *Container) Str(key string) (string, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return "", false
	}
	s, isStr := v.(string)
	return s, isStr
}

// Bytes returns the value as a byte slice.
func (c *Container) Bytes(key string) ([]byte, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return nil, false
	}
	b, isBytes := v.([]byte)
	return b, isBytes
}

// Sub returns the value as a nested Container.
func (c *Container) Sub(key string) (*Container, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return nil, false
	}
	s, isC := v.(*Container)
	return s, isC
}

// List returns the value as a list of Containers.
func (c *Container) List(key string) ([]*Container, bool) {
	v, ok := c.vals[key]
	if !ok || v == nil {
		return nil, false
	}
	l, isL := v.([]*Container)
	return l, isL
}

// ---------------------------------------------------------------------------
// Conversions
// ---------------------------------------------------------------------------

func toInt64(v interface{}) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case nil:
		return 0, errors.New("flp: nil value")
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if f == math.Trunc(f) {
			return int64(f), nil
		}
	}
	return 0, fmt.Errorf("flp: cannot use %T as an integer", v)
}

func toFloat64(v interface{}) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case nil:
		return 0, errors.New("flp: nil value")
	}
	n, err := toInt64(v)
	if err != nil {
		return 0, fmt.Errorf("flp: cannot use %T as a float", v)
	}
	return float64(n), nil
}

func toBool(v interface{}) (bool, error) {
	if b, ok := v.(bool); ok {
		return b, nil
	}
	n, err := toInt64(v)
	if err != nil {
		return false, fmt.Errorf("flp: cannot use %T as a bool", v)
	}
	return n != 0, nil
}

// ---------------------------------------------------------------------------
// Reader
// ---------------------------------------------------------------------------

type reader struct {
	b   []byte
	pos int
}

func newReader(b []byte) *reader { return &reader{b: b} }

func (r *reader) remaining() int { return len(r.b) - r.pos }

func (r *reader) take(n int) ([]byte, error) {
	if n < 0 || r.remaining() < n {
		return nil, errEOF
	}
	out := make([]byte, n)
	copy(out, r.b[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}

func (r *reader) rest() []byte {
	out := make([]byte, r.remaining())
	copy(out, r.b[r.pos:])
	r.pos = len(r.b)
	return out
}

// ---------------------------------------------------------------------------
// Field interface
// ---------------------------------------------------------------------------

// Field describes how a value is parsed from / built into bytes.
type Field interface {
	parse(r *reader, c *Ctx, cur *Container) (interface{}, error)
	build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error
}

// NamedField is a named member of a struct.
type NamedField struct {
	Name string
	F    Field
}

// ---------------------------------------------------------------------------
// Integers, floats, flags, bytes
// ---------------------------------------------------------------------------

type intField struct {
	size   int
	signed bool
}

func (f intField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	b, err := r.take(f.size)
	if err != nil {
		return nil, err
	}
	var u uint64
	for i := f.size - 1; i >= 0; i-- {
		u = u<<8 | uint64(b[i])
	}
	if f.signed {
		shift := uint(64 - 8*f.size)
		return int64(u<<shift) >> shift, nil
	}
	return int64(u), nil
}

func (f intField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	n, err := toInt64(v)
	if err != nil {
		return err
	}
	bits := uint(8 * f.size)
	if bits < 64 {
		if f.signed {
			lo := -(int64(1) << (bits - 1))
			hi := (int64(1) << (bits - 1)) - 1
			if n < lo || n > hi {
				return invalidValue("%d does not fit in a signed %d-byte integer", n, f.size)
			}
		} else if n < 0 || n >= int64(1)<<bits {
			return invalidValue("%d does not fit in an unsigned %d-byte integer", n, f.size)
		}
	}
	u := uint64(n)
	for i := 0; i < f.size; i++ {
		w.WriteByte(byte(u >> (8 * uint(i))))
	}
	return nil
}

type floatField struct{ size int }

func (f floatField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	b, err := r.take(f.size)
	if err != nil {
		return nil, err
	}
	if f.size == 4 {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), nil
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
}

func (f floatField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	x, err := toFloat64(v)
	if err != nil {
		return err
	}
	if f.size == 4 {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(float32(x)))
		w.Write(b[:])
		return nil
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(x))
	w.Write(b[:])
	return nil
}

type flagField struct{}

func (f flagField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	b, err := r.take(1)
	if err != nil {
		return nil, err
	}
	return b[0] != 0, nil
}

func (f flagField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	b, err := toBool(v)
	if err != nil {
		return err
	}
	if b {
		w.WriteByte(1)
	} else {
		w.WriteByte(0)
	}
	return nil
}

type bytesField struct{ n int }

func (f bytesField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	return r.take(f.n)
}

func (f bytesField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	b, ok := v.([]byte)
	if !ok {
		return fmt.Errorf("flp: expected []byte, got %T", v)
	}
	if len(b) != f.n {
		return invalidValue("expected %d bytes, got %d", f.n, len(b))
	}
	w.Write(b)
	return nil
}

type greedyBytesField struct{}

func (f greedyBytesField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	return r.rest(), nil
}

func (f greedyBytesField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	if v == nil {
		return nil
	}
	b, ok := v.([]byte)
	if !ok {
		return fmt.Errorf("flp: expected []byte, got %T", v)
	}
	w.Write(b)
	return nil
}

// ---------------------------------------------------------------------------
// Strings
// ---------------------------------------------------------------------------

const (
	encAscii = iota
	encUTF8
	encUTF16LE
)

func decodeLatin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}

func encodeLatin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			out = append(out, byte(r))
		} else {
			out = append(out, '?')
		}
	}
	return out
}

func decodeUTF16LE(b []byte) string {
	n := len(b) / 2
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

func encodeUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(out[i*2:], c)
	}
	return out
}

type greedyStringField struct{ enc int }

func (f greedyStringField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	b := r.rest()
	switch f.enc {
	case encUTF16LE:
		return decodeUTF16LE(b), nil
	case encUTF8:
		return string(b), nil
	}
	return decodeLatin1(b), nil
}

func (f greedyStringField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("flp: expected string, got %T", v)
	}
	switch f.enc {
	case encUTF16LE:
		w.Write(encodeUTF16LE(s))
	case encUTF8:
		w.WriteString(s)
	default:
		w.Write(encodeLatin1(s))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Combinators
// ---------------------------------------------------------------------------

type optionalField struct{ sub Field }

func (f optionalField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	save := r.pos
	v, err := f.sub.parse(r, c, cur)
	if err != nil {
		r.pos = save
		return nil, nil
	}
	return v, nil
}

func (f optionalField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	if v == nil {
		return nil
	}
	return f.sub.build(w, v, c, cur)
}

type ifField struct {
	cond func(c *Ctx, cur *Container) bool
	sub  Field
}

func (f ifField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	if f.cond(c, cur) {
		return f.sub.parse(r, c, cur)
	}
	return nil, nil
}

func (f ifField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	if !f.cond(c, cur) {
		return nil
	}
	if v == nil {
		return invalidValue("conditional field has no value")
	}
	return f.sub.build(w, v, c, cur)
}

type adaptField struct {
	sub Field
	dec func(v interface{}) (interface{}, error)
	enc func(v interface{}) (interface{}, error)
}

func (f adaptField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	v, err := f.sub.parse(r, c, cur)
	if err != nil {
		return nil, err
	}
	return f.dec(v)
}

func (f adaptField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	e, err := f.enc(v)
	if err != nil {
		return err
	}
	return f.sub.build(w, e, c, cur)
}

type arrayField struct {
	n   int
	sub Field
}

func (f arrayField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	out := make([]interface{}, 0, f.n)
	for i := 0; i < f.n; i++ {
		v, err := f.sub.parse(r, c, cur)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (f arrayField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	l, ok := v.([]interface{})
	if !ok {
		return fmt.Errorf("flp: expected []interface{}, got %T", v)
	}
	if len(l) != f.n {
		return invalidValue("expected %d items, got %d", f.n, len(l))
	}
	for _, e := range l {
		if err := f.sub.build(w, e, c, cur); err != nil {
			return err
		}
	}
	return nil
}

type structField struct{ fields []NamedField }

func (f structField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	out := NewContainer()
	for _, nf := range f.fields {
		v, err := nf.F.parse(r, c, out)
		if err != nil {
			return nil, err
		}
		if nf.Name != "" {
			out.Set(nf.Name, v)
		}
	}
	return out, nil
}

func (f structField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	cont, ok := v.(*Container)
	if !ok {
		return fmt.Errorf("flp: expected *Container, got %T", v)
	}
	for _, nf := range f.fields {
		var val interface{}
		if nf.Name != "" {
			val, _ = cont.Get(nf.Name)
		}
		if err := nf.F.build(w, val, c, cont); err != nil {
			return err
		}
	}
	return nil
}

// greedyRangeField parses `sub` repeatedly until it fails. A failed attempt
// does not consume any bytes (like construct.GreedyRange).
type greedyRangeField struct{ sub Field }

func (f greedyRangeField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	var out []*Container
	for {
		save := r.pos
		v, err := f.sub.parse(r, c, cur)
		if err != nil {
			if !errors.Is(err, errStop) {
				r.pos = save
			}
			break
		}
		cont, _ := v.(*Container)
		out = append(out, cont)
		if r.pos == save {
			break // guard against zero-width items
		}
	}
	if out == nil {
		out = []*Container{}
	}
	return out, nil
}

func (f greedyRangeField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	l, ok := v.([]*Container)
	if !ok {
		return fmt.Errorf("flp: expected []*Container, got %T", v)
	}
	for _, e := range l {
		if err := f.sub.build(w, e, c, cur); err != nil {
			if errors.Is(err, errStop) {
				return nil
			}
			return err
		}
	}
	return nil
}

// greedyFlagsField is GreedyRange(Flag): a list of booleans.
type greedyFlagsField struct{}

func (f greedyFlagsField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	b := r.rest()
	out := make([]bool, len(b))
	for i, x := range b {
		out[i] = x != 0
	}
	return out, nil
}

func (f greedyFlagsField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	l, ok := v.([]bool)
	if !ok {
		return fmt.Errorf("flp: expected []bool, got %T", v)
	}
	for _, x := range l {
		if x {
			w.WriteByte(1)
		} else {
			w.WriteByte(0)
		}
	}
	return nil
}

// prefixedField: a length prefix followed by a sub-field parsed from exactly
// that many bytes (construct.Prefixed).
type prefixedField struct {
	lenF Field
	sub  Field
}

func (f prefixedField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	lv, err := f.lenF.parse(r, c, cur)
	if err != nil {
		return nil, err
	}
	n, err := toInt64(lv)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > int64(r.remaining()) {
		return nil, errEOF
	}
	data, err := r.take(int(n))
	if err != nil {
		return nil, err
	}
	return f.sub.parse(newReader(data), c, cur)
}

func (f prefixedField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	var tmp bytes.Buffer
	if err := f.sub.build(&tmp, v, c, cur); err != nil {
		return err
	}
	if err := f.lenF.build(w, int64(tmp.Len()), c, cur); err != nil {
		return err
	}
	w.Write(tmp.Bytes())
	return nil
}

// switchField picks a sub-field based on the integer value of a sibling key.
type switchField struct {
	key   string
	cases map[int64]Field
	def   Field
}

func (f switchField) pick(cur *Container) (Field, error) {
	if cur == nil {
		return f.def, nil
	}
	kv, ok := cur.Get(f.key)
	if !ok {
		return nil, fmt.Errorf("flp: switch key %q not found", f.key)
	}
	k, err := toInt64(kv)
	if err != nil {
		return nil, err
	}
	if sub, found := f.cases[k]; found {
		return sub, nil
	}
	return f.def, nil
}

func (f switchField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	sub, err := f.pick(cur)
	if err != nil {
		return nil, err
	}
	return sub.parse(r, c, cur)
}

func (f switchField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	sub, err := f.pick(cur)
	if err != nil {
		return err
	}
	return sub.build(w, v, c, cur)
}

// varIntField is the little-endian base-128 variable length integer used for
// the length of text and data events.
type varIntField struct{}

func (f varIntField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	var n int64
	var shift uint
	for {
		b, err := r.take(1)
		if err != nil {
			return nil, err
		}
		n |= int64(b[0]&0x7f) << shift
		shift += 7
		if b[0]&0x80 == 0 {
			break
		}
		if shift > 63 {
			return nil, errors.New("flp: varint too long")
		}
	}
	return n, nil
}

func (f varIntField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	n, err := toInt64(v)
	if err != nil {
		return err
	}
	if n < 0 {
		return invalidValue("varint cannot be negative")
	}
	w.Write(encodeVarInt(int(n)))
	return nil
}

func encodeVarInt(n int) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			out = append(out, b|0x80)
		} else {
			out = append(out, b)
			break
		}
	}
	return out
}

// stopIfField aborts the enclosing GreedyRange when cond is true.
type stopIfField struct{ cond func(cur *Container) bool }

func (f stopIfField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	if f.cond(cur) {
		return nil, errStop
	}
	return nil, nil
}

func (f stopIfField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	if f.cond(cur) {
		return errStop
	}
	return nil
}

// paddedUTF16Field is construct.PaddedString(length*2, "utf-16-le") where the
// length (in characters) is stored in a sibling key.
type paddedUTF16Field struct{ lengthKey string }

func (f paddedUTF16Field) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	lv, ok := cur.Get(f.lengthKey)
	if !ok {
		return nil, fmt.Errorf("flp: length key %q not found", f.lengthKey)
	}
	n, err := toInt64(lv)
	if err != nil {
		return nil, err
	}
	if n < 0 || n*2 > int64(r.remaining()) {
		return nil, errEOF
	}
	data, err := r.take(int(n * 2))
	if err != nil {
		return nil, err
	}
	for len(data) >= 2 && data[len(data)-1] == 0 && data[len(data)-2] == 0 {
		data = data[:len(data)-2]
	}
	return decodeUTF16LE(data), nil
}

func (f paddedUTF16Field) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("flp: expected string, got %T", v)
	}
	lv, _ := cur.Get(f.lengthKey)
	n, err := toInt64(lv)
	if err != nil {
		return err
	}
	data := encodeUTF16LE(s)
	if int64(len(data)) > n*2 {
		return invalidValue("string is longer than its stored length of %d characters", n)
	}
	w.Write(data)
	w.Write(make([]byte, int(n*2)-len(data)))
	return nil
}

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

var (
	fU8    Field = intField{1, false}
	fI8    Field = intField{1, true}
	fU16   Field = intField{2, false}
	fI16   Field = intField{2, true}
	fU32   Field = intField{4, false}
	fI32   Field = intField{4, true}
	fU64   Field = intField{8, false}
	fF32   Field = floatField{4}
	fF64   Field = floatField{8}
	fFlag  Field = flagField{}
	fGreed Field = greedyBytesField{}
)

func fBytes(n int) Field         { return bytesField{n} }
func opt(f Field) Field          { return optionalField{f} }
func st(fs ...NamedField) Field  { return structField{fs} }
func nf(name string, f Field) NamedField { return NamedField{name, f} }

func ifLen(n int, f Field) Field {
	return ifField{cond: func(c *Ctx, cur *Container) bool { return c.Len == n }, sub: f}
}

func arr(n int, f Field) Field { return arrayField{n, f} }

// pairOf is Int16ul[2] / Int32ul[2] converted to a [2]int64 (a "tuple").
func pairOf(f Field) Field {
	return adaptField{
		sub: arr(2, f),
		dec: func(v interface{}) (interface{}, error) {
			l, ok := v.([]interface{})
			if !ok || len(l) != 2 {
				return nil, errors.New("flp: expected a 2-item array")
			}
			a, err := toInt64(l[0])
			if err != nil {
				return nil, err
			}
			b, err := toInt64(l[1])
			if err != nil {
				return nil, err
			}
			return [2]int64{a, b}, nil
		},
		enc: func(v interface{}) (interface{}, error) {
			p, err := toPair(v)
			if err != nil {
				return nil, err
			}
			return []interface{}{p[0], p[1]}, nil
		},
	}
}

func toPair(v interface{}) ([2]int64, error) {
	switch p := v.(type) {
	case [2]int64:
		return p, nil
	case [2]int:
		return [2]int64{int64(p[0]), int64(p[1])}, nil
	case []int:
		if len(p) == 2 {
			return [2]int64{int64(p[0]), int64(p[1])}, nil
		}
	case []interface{}:
		if len(p) == 2 {
			a, err1 := toInt64(p[0])
			b, err2 := toInt64(p[1])
			if err1 == nil && err2 == nil {
				return [2]int64{a, b}, nil
			}
		}
	}
	return [2]int64{}, fmt.Errorf("flp: cannot use %T as an integer pair", v)
}

// ---------------------------------------------------------------------------
// Adapters (pyflp._adapters)
// ---------------------------------------------------------------------------

// fourByteBool is a 4 byte integer exposed as a bool.
var fourByteBool Field = adaptField{
	sub: fU32,
	dec: func(v interface{}) (interface{}, error) {
		n, err := toInt64(v)
		return n != 0, err
	},
	enc: func(v interface{}) (interface{}, error) {
		b, err := toBool(v)
		if b {
			return int64(1), err
		}
		return int64(0), err
	},
}

// linearMusical converts the internal tick count to MusicalTime.
var linearMusical Field = adaptField{
	sub: fU32,
	dec: func(v interface{}) (interface{}, error) {
		n, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		bars := n / 768
		rem := n % 768
		beats := rem / 48
		rem = rem % 48
		return MusicalTime{Bars: int(bars), Beats: int(beats), Ticks: int(rem * 5)}, nil
	},
	enc: func(v interface{}) (interface{}, error) {
		t, ok := v.(MusicalTime)
		if !ok {
			return nil, fmt.Errorf("flp: expected MusicalTime, got %T", v)
		}
		if t.Ticks%5 != 0 {
			warn("Ticks must be a multiple of 5")
		}
		return int64(t.Bars*768 + t.Beats*48 + t.Ticks/5), nil
	},
}

// log2Field: 2 ** (value / factor).
func log2Field(sub Field, factor int) Field {
	return adaptField{
		sub: sub,
		dec: func(v interface{}) (interface{}, error) {
			n, err := toInt64(v)
			if err != nil {
				return nil, err
			}
			return math.Pow(2, float64(n)/float64(factor)), nil
		},
		enc: func(v interface{}) (interface{}, error) {
			x, err := toFloat64(v)
			if err != nil {
				return nil, err
			}
			if x <= 0 {
				return nil, invalidValue("log2 of %v is undefined", x)
			}
			return int64(float64(factor) * math.Log2(x)), nil
		},
	}
}

// logNormal maps a pair of 16 bit integers to a float between 0.0 and 1.0.
func logNormal(sub Field, lo, hi int64) Field {
	return adaptField{
		sub: sub,
		dec: func(v interface{}) (interface{}, error) {
			p, err := toPair(v)
			if err != nil {
				return nil, err
			}
			if p[0] == 0 {
				return 0.0, nil
			}
			if p[1] != 63 {
				return nil, fmt.Errorf("flp: not a LogNormal, 2nd int must be 63; not %d", p[1])
			}
			x := math.Pow(2, float64(p[0])/4096) / 32768
			return math.Max(math.Min(1.0, x), 0.0), nil
		},
		enc: func(v interface{}) (interface{}, error) {
			x, err := toFloat64(v)
			if err != nil {
				return nil, err
			}
			if x < 0.0 || x > 1.0 {
				return nil, invalidValue("expected a value between 0.0 to 1.0; got %v", x)
			}
			if x == 0 {
				return []interface{}{int64(0), int64(0)}, nil
			}
			n := int64(4096 * (math.Log2(x) + 15))
			if n < lo {
				n = lo
			}
			if n > hi {
				n = hi
			}
			return []interface{}{n, int64(63)}, nil
		},
	}
}

// heightField exposes a float32 track height as a percentage string.
var heightField Field = adaptField{
	sub: fF32,
	dec: func(v interface{}) (interface{}, error) {
		x, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("%d%%", int(math.Trunc(x*100))), nil
	},
	enc: func(v interface{}) (interface{}, error) {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("flp: expected string, got %T", v)
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "%")
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
			return nil, invalidValue("invalid height %q", s)
		}
		return float64(n) / 100, nil
	},
}

// asciiString / unicodeString are text event payloads: a null terminated
// string which is stripped of trailing nulls when parsed.
func nullTerminated(enc int) Field {
	return adaptField{
		sub: greedyStringField{enc},
		dec: func(v interface{}) (interface{}, error) {
			s, _ := v.(string)
			return strings.TrimRight(s, "\x00"), nil
		},
		enc: func(v interface{}) (interface{}, error) {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("flp: expected string, got %T", v)
			}
			return s + "\x00", nil
		},
	}
}

// ---------------------------------------------------------------------------
// Special structures
// ---------------------------------------------------------------------------

// automationPointsField is PrefixedArray(Int32ul, Struct(...)) where every
// point also carries its absolute `position`, the running sum of the
// `_offset` values (the change on the X axis w.r.t. the previous point).
type automationPointsField struct{}

func (f automationPointsField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	cv, err := fU32.parse(r, c, cur)
	if err != nil {
		return nil, err
	}
	count, _ := toInt64(cv)
	if count < 0 || count > int64(r.remaining())/24+1 {
		return nil, errEOF
	}
	out := make([]*Container, 0, int(count))
	position := 0.0
	for i := int64(0); i < count; i++ {
		p := NewContainer()
		off, err := fF64.parse(r, c, p)
		if err != nil {
			return nil, err
		}
		offset, _ := toFloat64(off)
		position += offset
		val, err := fF64.parse(r, c, p)
		if err != nil {
			return nil, err
		}
		tension, err := fF32.parse(r, c, p)
		if err != nil {
			return nil, err
		}
		u1, err := fBytes(4).parse(r, c, p)
		if err != nil {
			return nil, err
		}
		p.Set("_offset", off)
		p.Set("position", position)
		p.Set("value", val)
		p.Set("tension", tension)
		p.Set("_u1", u1)
		out = append(out, p)
	}
	return out, nil
}

func (f automationPointsField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	l, ok := v.([]*Container)
	if !ok {
		return fmt.Errorf("flp: expected []*Container, got %T", v)
	}
	if err := fU32.build(w, int64(len(l)), c, cur); err != nil {
		return err
	}
	for _, p := range l {
		off, _ := p.Get("_offset")
		val, _ := p.Get("value")
		tension, _ := p.Get("tension")
		u1, _ := p.Get("_u1")
		if err := fF64.build(w, off, c, p); err != nil {
			return err
		}
		if err := fF64.build(w, val, c, p); err != nil {
			return err
		}
		if err := fF32.build(w, tension, c, p); err != nil {
			return err
		}
		if err := fBytes(4).build(w, u1, c, p); err != nil {
			return err
		}
	}
	return nil
}

// notebookPagesField is the page list of the NoteBook 2 plugin: pages are
// (index, varint length, UTF-16 text) triplets terminated by an index of -1.
type notebookPagesField struct{}

func (f notebookPagesField) parse(r *reader, c *Ctx, cur *Container) (interface{}, error) {
	out := []*Container{}
	for {
		save := r.pos
		idx, err := fI32.parse(r, c, cur)
		if err != nil {
			r.pos = save
			break
		}
		if n, _ := toInt64(idx); n == -1 {
			break
		}
		p := NewContainer()
		p.Set("index", idx)
		l, err := (varIntField{}).parse(r, c, p)
		if err != nil {
			r.pos = save
			break
		}
		p.Set("length", l)
		s, err := (paddedUTF16Field{"length"}).parse(r, c, p)
		if err != nil {
			r.pos = save
			break
		}
		p.Set("value", s)
		out = append(out, p)
	}
	return out, nil
}

func (f notebookPagesField) build(w *bytes.Buffer, v interface{}, c *Ctx, cur *Container) error {
	l, ok := v.([]*Container)
	if !ok {
		return fmt.Errorf("flp: expected []*Container, got %T", v)
	}
	for _, p := range l {
		idx, _ := p.Get("index")
		value, _ := p.Get("value")
		s, _ := value.(string)
		units := len(encodeUTF16LE(s)) / 2
		length, _ := p.Int("length")
		if int(length) < units {
			length = int64(units)
			p.Set("length", length)
		}
		if err := fI32.build(w, idx, c, p); err != nil {
			return err
		}
		if err := (varIntField{}).build(w, length, c, p); err != nil {
			return err
		}
		if err := (paddedUTF16Field{"length"}).build(w, s, c, p); err != nil {
			return err
		}
	}
	return fI32.build(w, int64(-1), c, cur)
}
