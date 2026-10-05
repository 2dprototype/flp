package pyflp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var delphiEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

const headerSize = 22 // "FLhd" + size + 6 bytes header + "FLdt" + size

func daysToDuration(days float64) time.Duration {
	return time.Duration(days * 24 * float64(time.Hour))
}

func decodeLicensee(s string) string {
	var out []byte
	for idx, ch := range []rune(s) {
		c1 := int(ch) - 26 + idx
		c2 := int(ch) + 49 + idx
		for _, num := range []int{c1, c2} {
			if num >= 0 && num < 128 && (unicode.IsLetter(rune(num)) || unicode.IsDigit(rune(num))) {
				out = append(out, byte(num))
				break
			}
		}
	}
	return string(out)
}

func encodeLicensee(s string) string {
	var out []byte
	for idx, ch := range []rune(s) {
		c1 := int(ch) + 26 - idx
		c2 := int(ch) - 49 - idx
		for _, cp := range []int{c1, c2} {
			if cp > 0 && cp <= 127 {
				out = append(out, byte(cp))
				break
			}
		}
	}
	return string(out)
}

func maxTempo(v FLVersion) float64 {
	if v.AtLeast(NewFLVersion(1, 4, 2)) && v.Less(NewFLVersion(11)) {
		return 999.0
	}
	return 522.0
}

func projectVersion(m *Model) (FLVersion, error) {
	e, err := m.Events.First(IDProjectFLVersion)
	if err != nil {
		return FLVersion{}, err
	}
	s, _ := e.Str()
	return ParseFLVersion(s)
}

var projectSpecs = []*PropSpec{
	evProp("artists", KString, "Authors / artists info. embedded in exported WAV & MP3. New in FL Studio v5.0.", IDProjectArtists),
	customProp("channel_count", KInt, "Number of channels in the rack. For Patcher presets, the total number of plugins used inside it.",
		func(m *Model) (interface{}, bool) { v, ok := m.kw["channel_count"]; return v, ok },
		func(m *Model, v interface{}) error {
			n, _ := v.(int64)
			if n < 0 {
				return invalidValue("channel count cannot be less than zero")
			}
			m.kw["channel_count"] = n
			return nil
		}),
	customProp("comments", KString, "Comments / project description / summary (very old versions stored RTF).",
		func(m *Model) (interface{}, bool) {
			if e := m.event(IDProjectComments, IDProjectRTFComments); e != nil {
				return e.Value(), true
			}
			return nil, false
		},
		func(m *Model, v interface{}) error { return m.setEventValue(v, IDProjectComments, IDProjectRTFComments) }),
	readOnly(customProp("created_on", KDateTime, "The local date and time on which this project was created.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDProjectTimestamp)
			if e == nil {
				return nil, false
			}
			c, _ := e.Container()
			days, ok := c.Float("created_on")
			if !ok {
				return nil, false
			}
			return delphiEpoch.Add(daysToDuration(days)), true
		}, nil)),
	withEnum(customProp("format", KEnum, "Internal format marker used by FL Studio to distinguish between types.",
		func(m *Model) (interface{}, bool) { v, ok := m.kw["format"]; return v, ok },
		func(m *Model, v interface{}) error { m.kw["format"] = v; return nil }), FileFormatEnum),
	customProp("data_path", KString, "The absolute path used by FL to store all your renders. New in FL Studio v9.0.0.",
		func(m *Model) (interface{}, bool) {
			if e := m.event(IDProjectDataPath); e != nil {
				return e.Value(), true
			}
			return nil, false
		},
		func(m *Model, v interface{}) error {
			s, _ := v.(string)
			if s == "." {
				s = ""
			}
			return m.setEventValue(s, IDProjectDataPath)
		}),
	evProp("genre", KString, "Genre of the song embedded in exported WAV & MP3. New in FL Studio v5.0.", IDProjectGenre),
	evProp("licensed", KBool, "Whether the project was last saved with a licensed copy of FL Studio.", IDProjectLicensed),
	customProp("licensee", KString, "The license holder's username who last saved the project (empty for trial). New in FL Studio v1.3.9.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDProjectLicensee)
			if e == nil {
				return nil, false
			}
			s, _ := e.Str()
			return decodeLicensee(s), true
		},
		func(m *Model, v interface{}) error {
			s, _ := v.(string)
			return m.setEventValue(encodeLicensee(s), IDProjectLicensee)
		}),
	evProp("looped", KBool, "Whether a portion of the playlist is selected.", IDProjectLoopActive),
	evProp("main_pitch", KInt, "Master pitch (in cents). Min = -1200. Max = +1200. Defaults to 0.", IDProjectPitch),
	evProp("main_volume", KInt, "Changed in FL Studio v1.7.6: can be up to 125% (+5.6dB) now.", IDProjectVolume),
	withEnum(evProp("pan_law", KEnum, "Whether a circular or a triangular pan law is used for the project.", IDProjectPanLaw), PanLawEnum),
	customProp("ppq", KInt, "Pulses per quarter (timebase). Do not change; it affects all length, position and offset calculations.",
		func(m *Model) (interface{}, bool) { v, ok := m.kw["ppq"]; return v, ok },
		func(m *Model, v interface{}) error {
			n, _ := v.(int64)
			if !validPPQ(int(n)) {
				return invalidValue("expected one of %v; got %d instead", ValidPPQs, n)
			}
			m.kw["ppq"] = n
			return nil
		}),
	evProp("show_info", KBool, "Whether to show a banner while the project is loading inside FL Studio.", IDProjectShowInfo),
	customProp("tempo", KFloat, "Tempo at the current position of the playhead (in BPM).",
		func(m *Model) (interface{}, bool) {
			if e := m.event(IDProjectTempo); e != nil {
				n, _ := e.Int()
				return float64(n) / 1000, true
			}
			coarse := m.event(IDProjectTempoCoarse)
			if coarse == nil {
				return nil, false
			}
			n, _ := coarse.Int()
			t := float64(n)
			if fine := m.event(IDProjectTempoFine); fine != nil {
				f, _ := fine.Int()
				t += float64(f) / 1000
			}
			return t, true
		},
		func(m *Model, v interface{}) error {
			if !m.Events.ContainsAny(IDProjectTempo, IDProjectTempoCoarse, IDProjectTempoFine) {
				return cannotSet(IDProjectTempo, IDProjectTempoCoarse, IDProjectTempoFine)
			}
			value, _ := v.(float64)
			ver, err := projectVersion(m)
			if err != nil {
				return err
			}
			mx := maxTempo(ver)
			if value != math.Trunc(value) && ver.Less(NewFLVersion(3, 4, 0)) {
				return invalidValue("fine tuned (fractional) tempo is not supported before FL Studio 3.4.0; use a whole number")
			}
			if value > mx || value < MinTempo {
				return invalidValue("invalid tempo %v; expected %v-%v", value, MinTempo, mx)
			}
			if e := m.event(IDProjectTempo); e != nil {
				if err := e.SetValue(int64(value * 1000)); err != nil {
					return err
				}
			}
			if e := m.event(IDProjectTempoFine); e != nil {
				if err := e.SetValue(int64((value - math.Floor(value)) * 1000)); err != nil {
					return err
				}
			}
			if e := m.event(IDProjectTempoCoarse); e != nil {
				if err := e.SetValue(int64(math.Floor(value))); err != nil {
					return err
				}
			}
			return nil
		}),
	readOnly(customProp("time_spent", KDuration, "Time spent on the project since its creation.",
		func(m *Model) (interface{}, bool) {
			e := m.event(IDProjectTimestamp)
			if e == nil {
				return nil, false
			}
			c, _ := e.Container()
			days, ok := c.Float("time_spent")
			if !ok {
				return nil, false
			}
			return daysToDuration(days), true
		}, nil)),
	evProp("title", KString, "Name of the song / project.", IDProjectTitle),
	evProp("url", KString, "Web link of the project info.", IDProjectUrl),
	customProp("version", KVersion, "The version of FL Studio which was used to save the file.",
		func(m *Model) (interface{}, bool) {
			v, err := projectVersion(m)
			if err != nil {
				return nil, false
			}
			return v, true
		},
		func(m *Model, v interface{}) error {
			ver, _ := v.(FLVersion)
			e := m.event(IDProjectFLVersion)
			if e == nil {
				return cannotSet(IDProjectFLVersion)
			}
			if ver.Major == 0 && ver.Minor == 0 && ver.Patch == 0 && !ver.HasBuild {
				return invalidValue("expected format: major.minor.patch.build?")
			}
			parts := ver.Parts()
			strs := make([]string, len(parts))
			for i, p := range parts {
				strs[i] = strconv.Itoa(p)
			}
			if err := e.SetValue(strings.Join(strs, ".")); err != nil {
				return err
			}
			if ver.HasBuild {
				if b := m.event(IDProjectFLBuild); b != nil {
					return b.SetValue(int64(ver.Build))
				}
			}
			return nil
		}),
}

// Project represents an FL Studio project.
type Project struct{ *Model }

func newProject(events *EventTree, channelCount, ppq int, format FileFormat) *Project {
	m := newModel("Project", events, projectSpecs)
	m.kw["channel_count"] = int64(channelCount)
	m.kw["ppq"] = int64(ppq)
	m.kw["format"] = int64(format)
	return &Project{m}
}

// Format returns the file format marker.
func (p *Project) Format() FileFormat { v, _ := p.Int("format"); return FileFormat(v) }

// PPQ returns the timebase.
func (p *Project) PPQ() int { v, _ := p.Int("ppq"); return int(v) }

// ChannelCount returns the channel count stored in the header.
func (p *Project) ChannelCount() int { v, _ := p.Int("channel_count"); return int(v) }

// Version returns the version of FL Studio that saved the project.
func (p *Project) Version() (FLVersion, error) { return projectVersion(p.Model) }

// AllEvents returns all events in file order.
func (p *Project) AllEvents() []*Event { return p.Events.Root().Events() }

func (p *Project) String() string {
	v, err := p.Version()
	if err != nil {
		return fmt.Sprintf("FL Studio (unknown version) %s", p.Format())
	}
	return fmt.Sprintf("FL Studio v%s %s", v, p.Format())
}

func (p *Project) versionOrZero() FLVersion { v, _ := p.Version(); return v }

// Arrangements provides the arrangements and related properties.
func (p *Project) Arrangements() *Arrangements {
	arrNew := false
	view := p.Events.Subtree(func(e *Event) Select {
		if e.id == IDArrangementNew {
			arrNew = true
		}
		// Prevents accidentally passing on Pattern's timemarkers.
		if GroupOf(e.id) == GroupTimeMarker && arrNew {
			return Include
		}
		if InGroups(e.id, GroupArrangement, GroupArrangements, GroupTrack) {
			return Include
		}
		return Skip
	})
	m := newModel("Arrangements", view, nil)
	m.kw["channels"] = p.Channels()
	m.kw["patterns"] = p.Patterns()
	m.kw["version"] = p.versionOrZero()
	return &Arrangements{m}
}

// Channels provides the channels and the channel rack properties.
func (p *Project) Channels() *ChannelRack {
	view := p.Events.Subtree(func(e *Event) Select {
		if e.id == IDInsertFlags {
			return Cut
		}
		if InGroups(e.id, GroupChannel, GroupDisplayGroup, GroupPlugin, GroupRack) {
			return Include
		}
		return Skip
	})
	m := newModel("ChannelRack", view, rackSpecs)
	m.kw["channel_count"] = p.ChannelCount()
	return &ChannelRack{m}
}

// Mixer provides the inserts and mixer related properties.
func (p *Project) Mixer() *Mixer {
	began := false
	view := p.Events.Subtree(func(e *Event) Select {
		if InGroups(e.id, GroupMixer, GroupInsert, GroupSlot) {
			began = true
			return Include
		}
		if began && GroupOf(e.id) == GroupPlugin {
			return Include
		}
		return Skip
	})
	m := newModel("Mixer", view, mixerSpecs)
	m.kw["version"] = p.versionOrZero()
	return &Mixer{m}
}

// Patterns provides the patterns and related properties.
func (p *Project) Patterns() *Patterns {
	arrNew := false
	view := p.Events.Subtree(func(e *Event) Select {
		if e.id == IDArrangementNew {
			arrNew = true
		} else if GroupOf(e.id) == GroupTimeMarker && !arrNew {
			return Include
		} else if InGroups(e.id, GroupPattern, GroupPatterns) {
			return Include
		}
		return Skip
	})
	return &Patterns{newModel("Patterns", view, patternsSpecs)}
}

// RemoteControllers returns the remote (internal) controllers.
func (p *Project) RemoteControllers() []*Model { return RemoteControllers(p.Events) }

// ---------------------------------------------------------------------------
// Parse / Save
// ---------------------------------------------------------------------------

// Parse reads an FL Studio project file.
func Parse(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

// ParseReader reads a whole project from r.
func ParseReader(r io.Reader) (*Project, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

// ParseBytes parses the contents of an FLP file.
//
// Errors wrap ErrHeaderCorrupted when the header holds an invalid value and
// ErrVersionNotDetected when the string encoding could not be determined.
func ParseBytes(data []byte) (*Project, error) {
	if len(data) < 14 {
		return nil, headerCorrupted("Couldn't read the header entirely")
	}
	magic := string(data[0:4])
	hdrSize := binary.LittleEndian.Uint32(data[4:8])
	format := int16(binary.LittleEndian.Uint16(data[8:10]))
	channelCount := binary.LittleEndian.Uint16(data[10:12])
	ppq := binary.LittleEndian.Uint16(data[12:14])

	if magic != "FLhd" {
		return nil, headerCorrupted("Unexpected header chunk magic; expected 'FLhd'")
	}
	if hdrSize != 6 {
		return nil, headerCorrupted("Unexpected header chunk size; expected 6")
	}
	ff := FileFormat(format)
	if !ff.Valid() {
		return nil, headerCorrupted("Unsupported project file format")
	}
	if !validPPQ(int(ppq)) {
		return nil, headerCorrupted("Invalid PPQ")
	}
	if len(data) < headerSize || string(data[14:18]) != "FLdt" {
		return nil, headerCorrupted("Unexpected data chunk magic; expected 'FLdt'")
	}
	eventsSize := binary.LittleEndian.Uint32(data[18:22])
	if eventsSize == 0 {
		return nil, headerCorrupted("Data chunk size couldn't be read")
	}
	if len(data) != int(eventsSize)+headerSize {
		return nil, headerCorrupted("Data chunk size corrupted")
	}

	var events []*Event
	var strType *EventType
	plugName := ""
	hasPlugName := false

	r := newReader(data[headerSize:])
	for r.remaining() > 0 {
		idb, _ := r.take(1)
		id := EventID(idb[0])

		var value []byte
		var err error
		switch {
		case int(id) < WORD:
			value, err = r.take(1)
		case int(id) < DWORD:
			value, err = r.take(2)
		case int(id) < TEXT:
			value, err = r.take(4)
		default:
			var sz interface{}
			sz, err = (varIntField{}).parse(r, nil, nil)
			if err == nil {
				n, _ := toInt64(sz)
				if n < 0 || n > int64(r.remaining()) {
					err = errEOF
				} else {
					value, err = r.take(int(n))
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("%w: event %s is truncated: %v", ErrDataCorrupted, IDName(id), err)
		}

		if id == IDProjectFLVersion {
			s := strings.TrimRight(decodeLatin1(value), "\x00")
			parts := strings.Split(s, ".")
			nums := make([]int, 0, len(parts))
			for _, part := range parts {
				n, perr := strconv.Atoi(part)
				if perr != nil {
					return nil, fmt.Errorf("%w: invalid FL Studio version %q", ErrDataCorrupted, s)
				}
				nums = append(nums, n)
			}
			if nums[0] > 11 || (nums[0] == 11 && len(nums) >= 2 && nums[1] >= 5) {
				strType = UnicodeEvent
			} else {
				strType = AsciiEvent
			}
		}

		typ := DeclaredType(id)
		if typ == nil {
			switch {
			case int(id) < WORD:
				typ = U8Event
			case int(id) < DWORD:
				typ = U16Event
			case int(id) < TEXT:
				typ = U32Event
			case int(id) < DATA || isNewTextID(id):
				if strType == nil {
					return nil, ErrVersionNotDetected
				}
				typ = strType
			case id == IDPluginData && hasPlugName:
				typ = EventTypeByInternalName(plugName)
			default:
				typ = UnknownDataEvent
			}
		}

		ev, err := NewEvent(id, typ, value)
		if err != nil {
			return nil, err
		}
		if id == IDPluginInternalName {
			if s, ok := ev.Str(); ok {
				plugName = s
				hasPlugName = true
			}
		}
		events = append(events, ev)
	}

	return newProject(NewRootTree(events), int(channelCount), int(ppq), ff), nil
}

// Save writes a parsed project back to a file.
//
// Always have a backup ready, just in case.
func Save(p *Project, path string) error {
	data, err := SaveBytes(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SaveBytes serialises a project.
func SaveBytes(p *Project) ([]byte, error) {
	var body bytes.Buffer
	for _, e := range p.AllEvents() {
		body.Write(e.Bytes())
	}
	numChannels, err := p.Channels().Len()
	if err != nil {
		numChannels = p.ChannelCount()
	}
	var buf bytes.Buffer
	buf.WriteString("FLhd")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(6))
	_ = binary.Write(&buf, binary.LittleEndian, int16(p.Format()))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(numChannels))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(p.PPQ()))
	buf.WriteString("FLdt")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(body.Len()))
	buf.Write(body.Bytes())
	return buf.Bytes(), nil
}
