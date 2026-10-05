package pyflp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers that build synthetic FLP files
// ---------------------------------------------------------------------------

// u builds a fixed size event; the payload size follows from the ID range.
func u(id EventID, n int64) []byte {
	switch {
	case int(id) >= TEXT:
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(n))
		return data(id, b)
	case int(id) < WORD:
		return []byte{byte(id), byte(n)}
	case int(id) < DWORD:
		b := make([]byte, 3)
		b[0] = byte(id)
		binary.LittleEndian.PutUint16(b[1:], uint16(n))
		return b
	}
	b := make([]byte, 5)
	b[0] = byte(id)
	binary.LittleEndian.PutUint32(b[1:], uint32(n))
	return b
}

// data builds a variable size event.
func data(id EventID, payload []byte) []byte {
	out := []byte{byte(id)}
	out = append(out, encodeVarInt(len(payload))...)
	return append(out, payload...)
}

func ascii(id EventID, s string) []byte { return data(id, append([]byte(s), 0)) }
func utf16Ev(id EventID, s string) []byte { return data(id, encodeUTF16LE(s+"\x00")) }

func concat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func buildFLP(format int16, ppq uint16, events ...[]byte) []byte {
	body := concat(events...)
	var b bytes.Buffer
	b.WriteString("FLhd")
	binary.Write(&b, binary.LittleEndian, uint32(6))
	binary.Write(&b, binary.LittleEndian, format)
	binary.Write(&b, binary.LittleEndian, uint16(2)) // channel count
	binary.Write(&b, binary.LittleEndian, ppq)
	b.WriteString("FLdt")
	binary.Write(&b, binary.LittleEndian, uint32(len(body)))
	b.Write(body)
	return b.Bytes()
}

func notesPayload(notes ...[3]int64) []byte { // position, key, velocity
	var out []byte
	for _, n := range notes {
		item := make([]byte, 24)
		binary.LittleEndian.PutUint32(item[0:], uint32(n[0])) // position
		binary.LittleEndian.PutUint16(item[4:], 0)            // flags
		binary.LittleEndian.PutUint16(item[6:], 0)            // rack channel
		binary.LittleEndian.PutUint32(item[8:], 96)           // length
		binary.LittleEndian.PutUint16(item[12:], uint16(n[1]))
		item[16] = 120 // fine_pitch
		item[21] = byte(n[2]) // velocity
		out = append(out, item...)
	}
	return out
}

func insertFlags(flags uint32) []byte {
	p := make([]byte, 12)
	binary.LittleEndian.PutUint32(p[4:], flags)
	return data(IDInsertFlags, p)
}

func sample(t *testing.T) []byte {
	t.Helper()
	return buildFLP(0, 96,
		ascii(IDProjectFLVersion, "20.9.2.2963"),
		u(IDProjectTempo, 140000),
		utf16Ev(IDProjectTitle, "My Song"),
		utf16Ev(IDProjectArtists, "Someone"),
		// channel rack
		utf16Ev(IDDisplayGroupName, "Generators"),
		u(IDChannelNew, 0),
		u(IDChannelType, ChannelTypeSampler),
		u(IDChannelGroupNum, 0),
		utf16Ev(IDPluginName, "Kick"),
		utf16Ev(IDPluginInternalName, ""),
		utf16Ev(IDChannelSamplePath, "C:\\kick.wav"),
		u(IDChannelVolByte, 100),
		u(IDChannelNew, 1),
		u(IDChannelType, ChannelTypeInstrument),
		u(IDChannelGroupNum, 0),
		utf16Ev(IDPluginName, "Lead"),
		utf16Ev(IDPluginInternalName, "BooBass"),
		data(IDPluginData, concat(u(DWORD, 100)[1:], u(DWORD, 200)[1:], u(DWORD, 300)[1:])),
		// patterns
		u(IDPatternNew, 1),
		utf16Ev(IDPatternName, "Drums"),
		data(IDPatternNotes, notesPayload([3]int64{0, 60, 100}, [3]int64{96, 64, 90})),
		// mixer
		u(IDMixerAPDC, 1),
		u(IDInsertOutput, 0),
		insertFlags(0x8),
		utf16Ev(IDInsertName, "Master"),
		u(IDInsertOutput, 0),
		insertFlags(0x8),
		utf16Ev(IDInsertName, "Bass"),
		u(IDInsertOutput, 0),
		// arrangements
		u(IDArrangementNew, 0),
		utf16Ev(IDArrangementName, "Arrangement"),
		u(IDArrangementsCurrent, 0),
	)
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestParseHeaderErrors(t *testing.T) {
	good := sample(t)
	cases := map[string]func([]byte) []byte{
		"magic":  func(b []byte) []byte { b = append([]byte{}, b...); b[0] = 'X'; return b },
		"size":   func(b []byte) []byte { b = append([]byte{}, b...); b[4] = 7; return b },
		"format": func(b []byte) []byte { b = append([]byte{}, b...); b[8] = 0x77; return b },
		"ppq":    func(b []byte) []byte { b = append([]byte{}, b...); b[12] = 5; return b },
		"data":   func(b []byte) []byte { b = append([]byte{}, b...); b[14] = 'X'; return b },
		"length": func(b []byte) []byte { return append(append([]byte{}, b...), 0) },
		"short":  func(b []byte) []byte { return b[:10] },
	}
	for name, mut := range cases {
		_, err := ParseBytes(mut(good))
		if !errors.Is(err, ErrHeaderCorrupted) {
			t.Errorf("%s: want ErrHeaderCorrupted, got %v", name, err)
		}
	}
}

func TestRoundtripIsByteIdentical(t *testing.T) {
	orig := sample(t)
	p, err := ParseBytes(orig)
	if err != nil {
		t.Fatal(err)
	}
	out, err := SaveBytes(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, out) {
		t.Fatalf("round trip changed the file (%d -> %d bytes)", len(orig), len(out))
	}
}

func TestProjectProperties(t *testing.T) {
	p, err := ParseBytes(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := p.Version()
	if v.String() != "20.9.2.2963" {
		t.Errorf("version = %s", v)
	}
	if tempo, ok := p.Float("tempo"); !ok || tempo != 140 {
		t.Errorf("tempo = %v %v", tempo, ok)
	}
	if title, _ := p.Str("title"); title != "My Song" {
		t.Errorf("title = %q", title)
	}
	if p.PPQ() != 96 || p.Format() != FormatProject {
		t.Errorf("ppq/format wrong: %d %v", p.PPQ(), p.Format())
	}
}

func TestSetAndSave(t *testing.T) {
	p, _ := ParseBytes(sample(t))
	if err := p.SetPath("tempo", "128.5"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPath("title", "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPath("tempo", "5"); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("tempo below minimum should fail, got %v", err)
	}
	out, _ := SaveBytes(p)
	q, err := ParseBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if tempo, _ := q.Float("tempo"); tempo != 128.5 {
		t.Errorf("tempo = %v", tempo)
	}
	if title, _ := q.Str("title"); title != "Renamed" {
		t.Errorf("title = %q", title)
	}
}

func TestChannels(t *testing.T) {
	p, _ := ParseBytes(sample(t))
	rack := p.Channels()
	chs, err := rack.All()
	if err != nil || len(chs) != 2 {
		t.Fatalf("channels = %d, err %v", len(chs), err)
	}
	if chs[0].Class != ClassSampler || chs[1].Class != ClassInstrument {
		t.Errorf("classes = %v %v", chs[0].Class, chs[1].Class)
	}
	if chs[0].DisplayName() != "Kick" {
		t.Errorf("display name = %q", chs[0].DisplayName())
	}
	if g := chs[0].Group(); g == nil {
		t.Error("group missing")
	} else if n, _ := g.Str("name"); n != "Generators" {
		t.Errorf("group = %q", n)
	}
	if v, ok := chs[0].Int("volume"); !ok || v != 100 {
		t.Errorf("volume = %v %v", v, ok)
	}
	if err := p.SetPath("channels[Kick].volume", "50"); err != nil {
		t.Fatal(err)
	}
	if v, _ := p.Get("channels[0].volume", false); v != int64(50) {
		t.Errorf("volume after set = %v", v)
	}
	pl := chs[1].Plugin()
	if pl == nil || pl.Kind != "BooBass" {
		t.Fatalf("plugin = %v", pl)
	}
	if v, _ := pl.Int("mid"); v != 200 {
		t.Errorf("BooBass mid = %d", v)
	}
	if err := p.SetPath("channels[Lead].plugin.high", "999"); err != nil {
		t.Fatal(err)
	}
	if v, _ := p.Get("channels[1].plugin.high", false); v != int64(999) {
		t.Errorf("BooBass high = %v", v)
	}
	if _, err := rack.ByName("Nope"); !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("want ErrChannelNotFound, got %v", err)
	}
}

func TestPatternsAndNotes(t *testing.T) {
	p, _ := ParseBytes(sample(t))
	pats := p.Patterns()
	n, err := pats.Len()
	if err != nil || n != 1 {
		t.Fatalf("patterns = %d %v", n, err)
	}
	pt, err := pats.ByName("Drums")
	if err != nil {
		t.Fatal(err)
	}
	notes := pt.Notes()
	if len(notes) != 2 {
		t.Fatalf("notes = %d", len(notes))
	}
	if k, _ := notes[0].Str("key"); k != "C5" {
		t.Errorf("key = %q", k)
	}
	if err := p.SetPath("patterns[1].notes[1].velocity", "42"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPath("patterns[1].notes[0].key", "A#3"); err != nil {
		t.Fatal(err)
	}
	out, _ := SaveBytes(p)
	q, _ := ParseBytes(out)
	qn, _ := q.Patterns().ByIID(1)
	if v, _ := qn.Notes()[1].Int("velocity"); v != 42 {
		t.Errorf("velocity = %d", v)
	}
	if k, _ := qn.Notes()[0].Str("key"); k != "A#3" {
		t.Errorf("key = %q", k)
	}
}

func TestMixer(t *testing.T) {
	p, _ := ParseBytes(sample(t))
	if on, _ := p.Mixer().Bool("apdc"); !on {
		t.Error("apdc should be on")
	}
	if n, err := p.Mixer().Len(); err != nil || n != 2 {
		t.Errorf("inserts = %d %v", n, err)
	}
}

func TestEventTree(t *testing.T) {
	var evs []*Event
	for _, id := range []EventID{IDProjectTitle, IDProjectGenre, IDProjectUrl} {
		e, err := NewEvent(id, UnicodeEvent, encodeUTF16LE("x\x00"))
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, e)
	}
	root := NewRootTree(evs)
	sub := root.SubtreeIDs(IDProjectGenre, IDProjectUrl)
	ne, _ := NewEvent(IDProjectArtists, UnicodeEvent, encodeUTF16LE("y\x00"))
	sub.Insert(0, ne)
	if root.Len() != 4 || sub.Len() != 3 {
		t.Fatalf("lens root=%d sub=%d", root.Len(), sub.Len())
	}
	if root.Events()[1] != ne {
		t.Error("new event should sit before Genre in the root tree")
	}
	if _, err := sub.Pop(IDProjectGenre, 0); err != nil {
		t.Fatal(err)
	}
	if root.Len() != 3 || root.Contains(IDProjectGenre) {
		t.Error("pop should remove from parents too")
	}
	if _, err := root.First(IDProjectGenre); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("want ErrEventNotFound, got %v", err)
	}
}

func TestEventValidation(t *testing.T) {
	if _, err := NewEvent(IDProjectTempo, U8Event, []byte{1}); !errors.Is(err, ErrEventIDOutOfRange) {
		t.Errorf("want ErrEventIDOutOfRange, got %v", err)
	}
	if _, err := NewEvent(IDProjectTempo, U32Event, []byte{1, 2}); !errors.Is(err, ErrInvalidEventChunkSize) {
		t.Errorf("want ErrInvalidEventChunkSize, got %v", err)
	}
}

func TestCodecs(t *testing.T) {
	// VarInt
	for _, n := range []int{0, 1, 127, 128, 300, 16384, 1 << 21} {
		v, err := (varIntField{}).parse(newReader(encodeVarInt(n)), nil, nil)
		if err != nil || v.(int64) != int64(n) {
			t.Errorf("varint %d -> %v %v", n, v, err)
		}
	}
	// MusicalTime
	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, 768+48*2+3)
	v, _ := linearMusical.parse(newReader(raw), nil, nil)
	if mt := v.(MusicalTime); mt != (MusicalTime{1, 2, 15}) {
		t.Errorf("musical time = %v", mt)
	}
	var buf bytes.Buffer
	if err := linearMusical.build(&buf, v, nil, nil); err != nil || !bytes.Equal(buf.Bytes(), raw) {
		t.Errorf("musical time build = %x %v", buf.Bytes(), err)
	}
	// RGBA
	c, err := ParseRGBA("#ff8000")
	if err != nil || c.Hex() != "#ff8000ff" {
		t.Errorf("colour = %v %v", c, err)
	}
}

func TestVersionOrdering(t *testing.T) {
	a, _ := ParseFLVersion("20.9.2.2963")
	b, _ := ParseFLVersion("12.9.1")
	if !b.Less(a) || a.Less(b) {
		t.Error("version ordering wrong")
	}
	if _, err := ParseFLVersion("a.b"); err == nil {
		t.Error("invalid version accepted")
	}
}

func TestEveryIDIsRegistered(t *testing.T) {
	seen := map[EventID]bool{}
	for _, id := range AllIDs() {
		if seen[id] {
			t.Errorf("duplicate id %d", id)
		}
		seen[id] = true
		if d := DeclaredType(id); d != nil && !d.allows(id) {
			t.Errorf("%s declares type %s which does not allow it", IDName(id), d.Name)
		}
	}
	if len(seen) < 100 {
		t.Errorf("only %d ids registered", len(seen))
	}
}
