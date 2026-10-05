// package pyflp is a Go port of PyFLP, an unofficial parser for FL Studio
// project (.flp) files.
//
// Load a project file:
//
//	project, err := flp.Parse("/path/to/project.flp")
//
// Save the project:
//
//	err = flp.Save(project, "/path/to/save.flp")
//
// This is a derivative work of PyFLP (C) demberto, licensed under the GNU
// General Public License v3 or later.
package pyflp

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FLVersion is the version of FL Studio which saved a project. Build is only
// meaningful when HasBuild is true (older versions had no build number).
type FLVersion struct {
	Major    int
	Minor    int
	Patch    int
	Build    int
	HasBuild bool
}

// NewFLVersion builds a version from up to four numbers: major, minor,
// patch and build. Missing trailing parts default to zero (build to none).
func NewFLVersion(major int, rest ...int) FLVersion {
	v := FLVersion{Major: major}
	if len(rest) > 0 {
		v.Minor = rest[0]
	}
	if len(rest) > 1 {
		v.Patch = rest[1]
	}
	if len(rest) > 2 {
		v.Build = rest[2]
		v.HasBuild = true
	}
	return v
}

// ParseFLVersion parses strings like "20.9.2.2963" or "11.5.0".
func ParseFLVersion(s string) (FLVersion, error) {
	s = strings.TrimRight(s, "\x00")
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return FLVersion{}, fmt.Errorf("flp: invalid version %q: %w", s, err)
		}
		nums = append(nums, n)
	}
	if len(nums) == 0 || len(nums) > 4 {
		return FLVersion{}, fmt.Errorf("flp: invalid version %q", s)
	}
	return NewFLVersion(nums[0], nums[1:]...), nil
}

// Parts returns the numeric components present in the version.
func (v FLVersion) Parts() []int {
	p := []int{v.Major, v.Minor, v.Patch}
	if v.HasBuild {
		p = append(p, v.Build)
	}
	return p
}

func (v FLVersion) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.HasBuild {
		s += "." + strconv.Itoa(v.Build)
	}
	return s
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Compare returns -1, 0 or 1. A version without a build number sorts before
// one with a build number when everything else is equal.
func (v FLVersion) Compare(o FLVersion) int {
	if c := cmpInt(v.Major, o.Major); c != 0 {
		return c
	}
	if c := cmpInt(v.Minor, o.Minor); c != 0 {
		return c
	}
	if c := cmpInt(v.Patch, o.Patch); c != 0 {
		return c
	}
	switch {
	case !v.HasBuild && !o.HasBuild:
		return 0
	case !v.HasBuild:
		return -1
	case !o.HasBuild:
		return 1
	}
	return cmpInt(v.Build, o.Build)
}

// Less reports whether v is older than o.
func (v FLVersion) Less(o FLVersion) bool { return v.Compare(o) < 0 }

// AtLeast reports whether v is the same as, or newer than, o.
func (v FLVersion) AtLeast(o FLVersion) bool { return v.Compare(o) >= 0 }

// MusicalTime is a position expressed in bars, beats and ticks.
//
// 1 bar == 16 beats == 768 (internal), 1 beat == 240 ticks == 48 (internal),
// 5 ticks == 1 (internal representation).
type MusicalTime struct {
	Bars  int
	Beats int
	Ticks int
}

func (t MusicalTime) String() string {
	return fmt.Sprintf("%d:%d:%d", t.Bars, t.Beats, t.Ticks)
}

// RGBA is a colour with every component normalised to the range 0.0-1.0.
type RGBA struct {
	Red, Green, Blue, Alpha float64
}

// RGBAFromBytes converts 4 bytes (R, G, B, A) into an RGBA.
func RGBAFromBytes(b []byte) RGBA {
	var c [4]float64
	for i := 0; i < 4 && i < len(b); i++ {
		c[i] = float64(b[i]) / 255
	}
	return RGBA{c[0], c[1], c[2], c[3]}
}

func toByte(f float64) byte {
	r := math.Round(f * 255)
	if r < 0 {
		r = 0
	}
	if r > 255 {
		r = 255
	}
	return byte(r)
}

// Bytes converts the colour back into 4 bytes (R, G, B, A).
func (c RGBA) Bytes() []byte {
	return []byte{toByte(c.Red), toByte(c.Green), toByte(c.Blue), toByte(c.Alpha)}
}

// Hex returns the colour as #RRGGBBAA.
func (c RGBA) Hex() string {
	b := c.Bytes()
	return fmt.Sprintf("#%02x%02x%02x%02x", b[0], b[1], b[2], b[3])
}

func (c RGBA) String() string { return c.Hex() }

// ParseRGBA parses #RRGGBB or #RRGGBBAA (the leading # is optional).
func ParseRGBA(s string) (RGBA, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 && len(s) != 8 {
		return RGBA{}, fmt.Errorf("flp: invalid colour %q (want #RRGGBB or #RRGGBBAA)", s)
	}
	if len(s) == 6 {
		s += "ff"
	}
	b := make([]byte, 4)
	for i := 0; i < 4; i++ {
		n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return RGBA{}, fmt.Errorf("flp: invalid colour %q: %w", s, err)
		}
		b[i] = byte(n)
	}
	return RGBAFromBytes(b), nil
}
