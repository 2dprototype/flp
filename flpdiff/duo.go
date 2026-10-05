package flpdiff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/2dprototype/flp/pyflp"
)

// DuoReport describes what the duo parser did with arrangement data.
type DuoReport struct {
	// Source is "flpdiff", "pyflp" or "merged".
	Source string
	// FlpdiffErr / PyflpErr hold the arrangement-parsing failure of each
	// library (nil when it worked).
	FlpdiffErr error
	PyflpErr   error
	Notes      []string
}

func (r *DuoReport) notef(f string, a ...interface{}) {
	r.Notes = append(r.Notes, fmt.Sprintf(f, a...))
}

func (r *DuoReport) String() string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("duo[%s] %s", r.Source, strings.Join(r.Notes, "; "))
}

// ParseFLPFileDuo parses an FLP file with flpdiff and then cross-checks /
// repairs its arrangement data using pyflp. flpdiff stays the source of
// truth for everything except arrangements (so serialization & editing keep
// working); for each arrangement the richer of the two parses wins, and gaps
// (names, tracks, clips, markers) are filled from the other one.
//
// The call only fails when flpdiff itself cannot parse the file.
func ParseFLPFileDuo(data []byte) (*FLPProject, *DuoReport, error) {
	rep := &DuoReport{Source: "flpdiff"}

	proj, err := ParseFLPFile(data)
	if err != nil {
		return nil, rep, err
	}

	pyArrs, perr := parseArrangementsPyflp(data, proj)
	rep.PyflpErr = perr
	if perr != nil {
		rep.notef("pyflp failed: %v", perr)
		return proj, rep, nil
	}
	if len(pyArrs) == 0 {
		rep.notef("pyflp found no arrangements")
		return proj, rep, nil
	}

	merged, changed := mergeArrangements(proj.Arrangements, pyArrs, rep)
	if changed {
		proj.Arrangements = merged
	}
	return proj, rep, nil
}

// ParseArrangementsPyflp exposes the pyflp-based arrangement parse alone.
func ParseArrangementsPyflp(data []byte) ([]Arrangement, error) {
	proj, err := ParseFLPFile(data)
	if err != nil {
		// flpdiff couldn't even give us channel/pattern ids; filter nothing.
		proj = &FLPProject{}
	}
	return parseArrangementsPyflp(data, proj)
}

// parseArrangementsPyflp converts pyflp's arrangement model into flpdiff's.
// It never panics.
func parseArrangementsPyflp(data []byte, ref *FLPProject) (out []Arrangement, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("pyflp panic: %v", r)
		}
	}()

	p, perr := pyflp.ParseBytes(data)
	if perr != nil {
		return nil, perr
	}
	arrs, aerr := p.Arrangements().All()
	if aerr != nil {
		if errors.Is(aerr, pyflp.ErrNoModelsFound) {
			return []Arrangement{}, nil
		}
		return nil, aerr
	}

	// Validity filter identical to flpdiff's keepClip, when we know the ids.
	haveRef := ref != nil && (len(ref.Channels) > 0 || len(ref.Patterns) > 0)
	chIids := map[int]bool{}
	patIDs := map[int]bool{}
	if haveRef {
		for _, c := range ref.Channels {
			chIids[c.Iid] = true
		}
		for _, pt := range ref.Patterns {
			patIDs[pt.ID] = true
		}
	}
	keep := func(c Clip) bool {
		if c.TrackRvidx > playlistMaxTrackIdx {
			return false
		}
		if !haveRef {
			return true
		}
		if c.ItemIndex <= playlistPatternBase {
			return chIids[c.ItemIndex]
		}
		return patIDs[c.ItemIndex-playlistPatternBase]
	}

	for _, a := range arrs {
		arr := Arrangement{
			ID:          int(a.IID()),
			Tracks:      []Track{},
			Clips:       []Clip{},
			TimeMarkers: []TimeMarker{},
		}
		if n, ok := a.Str("name"); ok {
			s := n
			arr.Name = &s
		}

		for ti, t := range a.Tracks() {
			tr := Track{Index: ti}
			if v, ok := t.Int("iid"); ok {
				tr.Iid = Ptr(uint32(v))
			}
			if n, ok := t.Str("name"); ok {
				s := n
				tr.Name = &s
			}
			if cv, ok := t.Get("color"); ok {
				if c, ok := cv.(pyflp.RGBA); ok {
					if b := c.Bytes(); len(b) >= 4 {
						col := unpackRGBA(binary.LittleEndian.Uint32(b))
						tr.Color = &col
					}
				}
			}
			if v, ok := t.Int("icon"); ok {
				tr.Icon = Ptr(uint32(v))
			}
			if v, ok := t.Bool("enabled"); ok {
				tr.Enabled = Ptr(v)
			}
			if v, ok := t.Bool("locked"); ok {
				tr.Locked = Ptr(v)
			}
			if v, ok := t.Bool("grouped"); ok {
				tr.Grouped = Ptr(v)
			}
			arr.Tracks = append(arr.Tracks, tr)

			for _, it := range t.Items() {
				pos, _ := it.Int("position")
				ln, _ := it.Int("length")
				idx, _ := it.Int("item_index")
				rv, _ := it.Int("track_rvidx")
				grp, _ := it.Int("group")
				fl, _ := it.Int("item_flags")
				so, _ := it.Float("start_offset")
				eo, _ := it.Float("end_offset")
				c := Clip{
					Position:    uint32(pos),
					ItemIndex:   int(idx),
					Length:      uint32(ln),
					TrackRvidx:  int(rv),
					Group:       int(grp),
					ItemFlags:   int(fl),
					StartOffset: float32(so),
					EndOffset:   float32(eo),
				}
				if keep(c) {
					arr.Clips = append(arr.Clips, c)
				}
			}
		}
		sort.SliceStable(arr.Clips, func(i, j int) bool {
			if arr.Clips[i].Position != arr.Clips[j].Position {
				return arr.Clips[i].Position < arr.Clips[j].Position
			}
			return arr.Clips[i].TrackRvidx > arr.Clips[j].TrackRvidx
		})

		for _, m := range a.TimeMarkers() {
			pos, _ := m.Int("position")
			typ, _ := m.Int("type")
			tm := TimeMarker{Kind: TimeMarkerMarker, Position: uint32(pos)}
			if typ == int64(pyflp.TimeMarkerTypeSignature) {
				tm.Kind = TimeMarkerSignature
			}
			if n, ok := m.Str("name"); ok {
				s := n
				tm.Name = &s
			}
			if v, ok := m.Int("numerator"); ok {
				tm.Numerator = Ptr(int(v))
			}
			if v, ok := m.Int("denominator"); ok {
				tm.Denominator = Ptr(int(v))
			}
			arr.TimeMarkers = append(arr.TimeMarkers, tm)
		}
		out = append(out, arr)
	}
	return out, nil
}

func arrScore(a Arrangement) int {
	return len(a.Clips)*1000 + len(a.Tracks)*10 + len(a.TimeMarkers)
}

func clipKey(c Clip) [4]int {
	return [4]int{int(c.Position), c.ItemIndex, c.TrackRvidx, int(c.Length)}
}

// mergeArrangements picks, per arrangement, the richer parse and fills the
// gaps from the other. Returns changed=false when flpdiff's result was
// already complete.
func mergeArrangements(fd, py []Arrangement, rep *DuoReport) ([]Arrangement, bool) {
	if len(fd) == 0 {
		rep.Source = "pyflp"
		rep.notef("flpdiff found no arrangements; using pyflp (%d)", len(py))
		return py, true
	}

	pyByID := make(map[int]*Arrangement, len(py))
	for i := range py {
		pyByID[py[i].ID] = &py[i]
	}

	changed := false
	used := map[int]bool{}
	out := make([]Arrangement, 0, len(fd))
	for i, a := range fd {
		// Match by ID first, then by position.
		var b *Arrangement
		if m, ok := pyByID[a.ID]; ok {
			b = m
		} else if i < len(py) {
			b = &py[i]
		}
		if b == nil {
			out = append(out, a)
			continue
		}
		used[b.ID] = true

		base, other := a, *b
		fromPy := false
		if arrScore(other) > arrScore(base) {
			base, other = other, base
			fromPy = true
		}
		if fromPy {
			changed = true
			rep.notef("arrangement %d: pyflp richer (clips %d vs %d, tracks %d vs %d)",
				a.ID, len(b.Clips), len(a.Clips), len(b.Tracks), len(a.Tracks))
		}

		// Fill gaps in base from other.
		if base.Name == nil && other.Name != nil {
			base.Name = other.Name
			changed = true
		}
		if len(base.Tracks) < len(other.Tracks) {
			for ti := len(base.Tracks); ti < len(other.Tracks); ti++ {
				base.Tracks = append(base.Tracks, other.Tracks[ti])
			}
			changed = true
		}
		for ti := range base.Tracks {
			if ti >= len(other.Tracks) {
				break
			}
			bt, ot := &base.Tracks[ti], other.Tracks[ti]
			if bt.Name == nil && ot.Name != nil {
				bt.Name = ot.Name
				changed = true
			}
			if bt.Color == nil && ot.Color != nil {
				bt.Color = ot.Color
				changed = true
			}
			if bt.Iid == nil && ot.Iid != nil {
				bt.Iid = ot.Iid
				changed = true
			}
		}
		if len(base.TimeMarkers) == 0 && len(other.TimeMarkers) > 0 {
			base.TimeMarkers = other.TimeMarkers
			changed = true
		}
		// Union of clips the richer parse missed.
		seen := make(map[[4]int]bool, len(base.Clips))
		for _, c := range base.Clips {
			seen[clipKey(c)] = true
		}
		added := 0
		for _, c := range other.Clips {
			if !seen[clipKey(c)] {
				base.Clips = append(base.Clips, c)
				added++
			}
		}
		if added > 0 {
			sort.SliceStable(base.Clips, func(x, y int) bool {
				return base.Clips[x].Position < base.Clips[y].Position
			})
			rep.notef("arrangement %d: +%d clips recovered from %s", a.ID, added,
				map[bool]string{true: "flpdiff", false: "pyflp"}[fromPy])
			changed = true
		}
		out = append(out, base)
	}

	// Arrangements only pyflp saw.
	for i := range py {
		if !used[py[i].ID] && i >= len(fd) {
			out = append(out, py[i])
			rep.notef("arrangement %d only found by pyflp", py[i].ID)
			changed = true
		}
	}

	if changed {
		rep.Source = "merged"
	}
	return out, changed
}
