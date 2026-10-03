package flp

import (
	"fmt"
	"strings"
)

// FLPHeader is the parsed "FLhd" block.
type FLPHeader struct {
	Format    int
	NChannels int
	PPQ       int
}

// FLPProject is a parsed project: raw header + events plus the assembled model.
type FLPProject struct {
	Header       FLPHeader
	Events       []FLPEvent
	Metadata     ProjectMetadata
	Channels     []Channel
	Inserts      []MixerInsert
	Patterns     []Pattern
	Arrangements []Arrangement
	InsertRouting []bool
}

var (
	flhdMagic = []byte{0x46, 0x4c, 0x68, 0x64} // "FLhd"
	fldtMagic = []byte{0x46, 0x4c, 0x64, 0x74} // "FLdt"
)

func readMagic(r *byteReader, expected []byte, label string) error {
	start := r.pos
	for i := 0; i < len(expected); i++ {
		b, err := r.readU8()
		if err != nil {
			return wrapParse(label, r, start, label, nil, err)
		}
		if b != int(expected[i]) {
			e := fmt.Errorf("magic mismatch: expected byte 0x%s at position %d, got 0x%s", hex2(int(expected[i])), i, hex2(b))
			return wrapParse(label, r, start, label, nil, e)
		}
	}
	return nil
}

func readHeader(r *byteReader) (FLPHeader, error) {
	start := r.pos
	n, err := r.readU32()
	if err != nil {
		return FLPHeader{}, wrapParse("FLPHeader", r, start, "FLhd", nil, err)
	}
	if n != 6 {
		return FLPHeader{}, wrapParse("FLPHeader", r, start, "FLhd", nil, fmt.Errorf("unexpected FLhd length %d, expected 6", n))
	}
	format, e1 := r.readU16()
	nch, e2 := r.readU16()
	ppq, e3 := r.readU16()
	for _, e := range []error{e1, e2, e3} {
		if e != nil {
			return FLPHeader{}, wrapParse("FLPHeader", r, start, "FLhd", nil, e)
		}
	}
	return FLPHeader{Format: format, NChannels: nch, PPQ: ppq}, nil
}

// ParseFLPFile parses an FLP file from a byte slice.
func ParseFLPFile(data []byte) (*FLPProject, error) {
	r := &byteReader{data: data}
	wrapRoot := func(err error) error {
		return wrapParse("FLPProject", r, 0, "FLPProject", nil, err)
	}
	if err := readMagic(r, flhdMagic, "FLhd"); err != nil {
		return nil, wrapRoot(err)
	}
	header, err := readHeader(r)
	if err != nil {
		return nil, wrapRoot(err)
	}
	if err := readMagic(r, fldtMagic, "FLdt"); err != nil {
		return nil, wrapRoot(err)
	}
	dataLen, err := r.readU32()
	if err != nil {
		return nil, wrapRoot(err)
	}
	dataEnd := r.pos + int(dataLen)

	events := []FLPEvent{}
	eventIndex := 0
	for r.pos < dataEnd {
		idx := eventIndex
		eventIndex++
		ev, err := readEvent(r)
		if err != nil {
			if pe, ok := err.(*FLPParseError); ok {
				i := idx
				err = pe.Extend(nil, &i, []string{fmt.Sprintf("events[%d]", idx)})
			}
			return nil, wrapRoot(err)
		}
		events = append(events, ev)
	}

	metadata := buildMetadata(events)
	channels := buildChannels(events, &metadata)
	inserts := buildMixerInserts(events, &metadata)
	patterns := buildPatterns(events, &metadata)
	arrangements := buildArrangements(events, channels, patterns, &metadata)
	routing := collectInsertRouting(events)
	return &FLPProject{
		Header: header, Events: events, Metadata: metadata, Channels: channels,
		Inserts: inserts, Patterns: patterns, Arrangements: arrangements, InsertRouting: routing,
	}, nil
}

// SerializeFLPProject is the inverse of ParseFLPFile (byte-identical round-trip).
func SerializeFLPProject(p *FLPProject) ([]byte, error) {
	eventsBytes := 0
	for _, ev := range p.Events {
		eventsBytes += eventSize(ev)
	}
	out := make([]byte, 0, 4+4+6+4+4+eventsBytes)
	out = append(out, flhdMagic...)
	out = append(out, 6, 0, 0, 0)
	out = append(out, byte(p.Header.Format), byte(p.Header.Format>>8))
	out = append(out, byte(p.Header.NChannels), byte(p.Header.NChannels>>8))
	out = append(out, byte(p.Header.PPQ), byte(p.Header.PPQ>>8))
	out = append(out, fldtMagic...)
	out = append(out, byte(eventsBytes), byte(eventsBytes>>8), byte(eventsBytes>>16), byte(eventsBytes>>24))
	start := len(out)
	var err error
	for _, ev := range p.Events {
		out, err = appendEvent(out, ev)
		if err != nil {
			return nil, err
		}
	}
	if len(out)-start != eventsBytes {
		return nil, fmt.Errorf("serializer wrote %d bytes, expected %d (%d of events + 18 of headers)", len(out), start+eventsBytes, eventsBytes)
	}
	return out, nil
}

// GetFLVersionBanner returns the "FL Studio …" banner from opcode 0xC0 (FL 25+), or nil.
func GetFLVersionBanner(p *FLPProject) *string {
	for _, e := range p.Events {
		if e.Kind != KindBlob || e.Opcode != 0xc0 {
			continue
		}
		decoded := decodeUtf16LeBytes(e.Payload)
		if strings.HasPrefix(decoded, "FL Studio") {
			return &decoded
		}
	}
	return nil
}

// GetTempo picks tempo from the event stream (0x9C modern, else 0x42 + optional 0x5D).
func GetTempo(p *FLPProject) *float64 {
	for _, e := range p.Events {
		if e.Kind == KindU32 && e.Opcode == 0x9c {
			return Ptr(float64(e.Value) / 1000)
		}
	}
	var coarse *FLPEvent
	for i := range p.Events {
		if p.Events[i].Kind == KindU16 && p.Events[i].Opcode == 0x42 {
			coarse = &p.Events[i]
			break
		}
	}
	if coarse == nil {
		return nil
	}
	tempo := float64(coarse.Value)
	for _, e := range p.Events {
		if e.Kind == KindU16 && e.Opcode == 0x5d {
			tempo += float64(e.Value) / 1000
			break
		}
	}
	return &tempo
}
