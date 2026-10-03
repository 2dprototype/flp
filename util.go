package flp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// Ptr returns a pointer to a copy of v. Used for optional fields.
func Ptr[T any](v T) *T { return &v }

// pythonRound implements Python's round-half-to-even for floats.
func pythonRound(x float64) float64 {
	floor := math.Floor(x)
	frac := x - floor
	if frac < 0.5 {
		return floor
	}
	if frac > 0.5 {
		return floor + 1
	}
	if math.Mod(floor, 2) == 0 {
		return floor
	}
	return floor + 1
}

// hex2 formats a byte as two lowercase hex digits.
func hex2(b int) string { return fmt.Sprintf("%02x", b) }

// hexUpper2 formats an opcode as two uppercase hex digits.
func hexUpper2(b int) string { return fmt.Sprintf("%02X", b) }

// utf16LEToString decodes UTF-16LE bytes tolerantly (unpaired surrogates become U+FFFD).
func utf16LEToString(b []byte) string {
	n := len(b) / 2
	units := make([]uint16, n)
	for i := 0; i < n; i++ {
		units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

// stringToUTF16LE encodes a string as UTF-16LE bytes (no terminator).
func stringToUTF16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u&0xff), byte(u>>8))
	}
	return out
}

func sortedKeysInt[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// timeValue holds UTC calendar components (JS getUTC* equivalents).
type timeValue struct {
	Year, Month, Day, Hour, Min, Sec, Milli int
}

func toTimeValue(t time.Time) timeValue {
	t = t.UTC()
	return timeValue{Year: t.Year(), Month: int(t.Month()), Day: t.Day(), Hour: t.Hour(), Min: t.Minute(), Sec: t.Second(), Milli: t.Nanosecond() / 1e6}
}

// jsNum formats a float64 the way JavaScript's String(number) does.
func jsNum(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == 0 {
		return "0"
	}
	neg := f < 0
	if neg {
		f = -f
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	ei := strings.IndexByte(s, 'e')
	mant, expStr := s[:ei], s[ei+1:]
	exp, _ := strconv.Atoi(expStr)
	digits := strings.Replace(mant, ".", "", 1)
	n := exp + 1
	k := len(digits)
	var out string
	switch {
	case k <= n && n <= 21:
		out = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		out = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		out = "0." + strings.Repeat("0", -n) + digits
	default:
		e := n - 1
		sign := "+"
		if e < 0 {
			sign = "-"
			e = -e
		}
		if k == 1 {
			out = digits + "e" + sign + strconv.Itoa(e)
		} else {
			out = digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(e)
		}
	}
	if neg {
		out = "-" + out
	}
	return out
}

// jsToFixed mimics JavaScript's Number.prototype.toFixed (round-half-up on the exact value).
func jsToFixed(x float64, digits int) string {
	if math.IsNaN(x) {
		return "NaN"
	}
	if math.IsInf(x, 0) {
		if x < 0 {
			return "-Infinity"
		}
		return "Infinity"
	}
	neg := x < 0
	if neg {
		x = -x
	}
	s := strconv.FormatFloat(x, 'f', 80, 64) // exact decimal expansion
	dot := strings.IndexByte(s, '.')
	intPart, frac := s[:dot], s[dot+1:]
	keep := frac[:digits]
	next := frac[digits]
	num := []byte(intPart + keep)
	if next >= '5' {
		i := len(num) - 1
		for i >= 0 {
			if num[i] == '9' {
				num[i] = '0'
				i--
				continue
			}
			num[i]++
			break
		}
		if i < 0 {
			num = append([]byte{'1'}, num...)
		}
	}
	out := string(num)
	if digits > 0 {
		out = out[:len(out)-digits] + "." + out[len(out)-digits:]
	}
	if neg {
		out = "-" + out
	}
	return out
}

// jsRound mimics Math.round (round half up toward +Infinity).
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// jsonString marshals s like JSON.stringify (no HTML escaping).
func jsonString(s string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(sb.String(), "\n")
}
