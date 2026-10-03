package flp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// BridgeError is a structured bridge-level failure (code + message).
type BridgeError struct {
	Code    string
	Message string
}

func (e *BridgeError) Error() string { return e.Message }

func bridgeErr(code, format string, a ...interface{}) {
	panic(&BridgeError{Code: code, Message: fmt.Sprintf(format, a...)})
}

// Response is the JSON-RPC response envelope.
type Response = map[string]interface{}

func okResp(kind string, result interface{}) Response {
	return Response{"ok": true, "kind": kind, "result": result}
}

func errResp(kind, code, msg string) Response {
	return Response{"ok": false, "kind": kind, "error": code, "message": msg}
}

var readKinds = []string{
	"describe", "get_tempo", "list_channels", "list_mixer", "list_patterns", "list_plugins", "list_arrangements",
	"list_tracks", "list_clips", "find_channel_by_name", "find_insert_by_name", "find_pattern_by_name", "find_plugin_instances",
}

var writeKinds = []string{
	"set_tempo", "set_pattern_name", "set_channel_name", "set_insert_name", "set_time_signature", "set_channel_color",
	"set_insert_color", "set_pattern_color", "set_channel_routing", "set_arrangement_name", "set_track_name", "set_track_color",
	"set_track_grouped", "clone_pattern", "add_clip", "remove_clip", "move_clip", "reorganize_project", "add_pattern_note",
	"set_pattern_notes", "remove_pattern_note", "add_pattern_controller", "set_pattern_controllers", "remove_pattern_controller",
	"create_pattern", "create_channel", "set_native_plugin_param", "set_pattern_length", "transpose_pattern_notes",
	"quantize_pattern_notes", "humanize_velocities", "humanize_timings", "reverse_pattern_notes", "invert_pattern_notes",
	"set_channel_volume", "set_channel_pan", "arrange_song", "instantiate_native_plugin", "set_channel_sample_path",
	"load_factory_preset",
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

var allowedArgs = map[string]map[string]bool{
	"describe": set("path"), "get_tempo": set("path"), "list_channels": set("path"), "list_mixer": set("path"),
	"list_patterns": set("path"), "list_plugins": set("path"), "list_arrangements": set("path"),
	"list_tracks": set("path", "arrangement"), "list_clips": set("path", "arrangement"),
	"find_channel_by_name": set("path", "query", "fuzzy"), "find_insert_by_name": set("path", "query", "fuzzy"),
	"find_pattern_by_name": set("path", "query", "fuzzy"), "find_plugin_instances": set("path", "plugin_name"),
	"set_tempo": set("path", "bpm"), "set_pattern_name": set("path", "iid", "name"), "set_channel_name": set("path", "iid", "name"),
	"set_insert_name": set("path", "index", "name"), "set_time_signature": set("path", "numerator", "denominator"),
	"set_channel_color": set("path", "iid", "color"), "set_insert_color": set("path", "index", "color"),
	"set_pattern_color": set("path", "iid", "color"), "set_channel_routing": set("path", "iid", "target_insert"),
	"set_arrangement_name": set("path", "id", "name"), "set_track_name": set("path", "arrangement", "track", "name"),
	"set_track_color": set("path", "arrangement", "track", "color"), "set_track_grouped": set("path", "arrangement", "track", "grouped"),
	"clone_pattern": set("path", "source_iid", "name"),
	"add_clip":      set("path", "arrangement", "kind", "ref_id", "track_index", "position_ticks", "length_ticks"),
	"remove_clip":   set("path", "arrangement", "track_index", "position_ticks", "ref_id", "kind"),
	"move_clip":     set("path", "arrangement", "track_index", "position_ticks", "ref_id", "kind", "to_track_index", "to_position_ticks"),
	"reorganize_project": set("path", "arrangement", "add_family_separators", "dry_run"),
	"add_pattern_note": set("path", "pattern_id", "position", "channel_iid", "length", "key", "velocity", "pan", "fine_pitch",
		"release", "midi_channel", "mod_x", "mod_y", "group", "flags", "slide"),
	"set_pattern_notes":         set("path", "pattern_id", "notes"),
	"remove_pattern_note":       set("path", "pattern_id", "index"),
	"add_pattern_controller":    set("path", "pattern_id", "position", "channel", "value", "flags"),
	"set_pattern_controllers":   set("path", "pattern_id", "controllers"),
	"remove_pattern_controller": set("path", "pattern_id", "index"),
	"create_pattern":            set("path", "name"),
	"create_channel":            set("path", "name", "kind"),
	"set_native_plugin_param": set("path", "scope", "channel_iid", "insert_index", "slot_index", "param", "band", "field",
		"param_index", "value"),
	"set_pattern_length":        set("path", "pattern_id", "ticks"),
	"transpose_pattern_notes":   set("path", "pattern_id", "semitones", "channel_iid"),
	"quantize_pattern_notes":    set("path", "pattern_id", "grid_ticks", "strength"),
	"humanize_velocities":       set("path", "pattern_id", "range", "seed"),
	"humanize_timings":          set("path", "pattern_id", "range_ticks", "seed"),
	"reverse_pattern_notes":     set("path", "pattern_id"),
	"invert_pattern_notes":      set("path", "pattern_id", "axis_key"),
	"set_channel_volume":        set("path", "iid", "value"),
	"set_channel_pan":           set("path", "iid", "value"),
	"arrange_song":              set("path", "arrangement", "structure", "track_index", "beats_per_bar"),
	"instantiate_native_plugin": set("path", "donor_path", "plugin_name", "insert_index", "slot_marker"),
	"set_channel_sample_path":   set("path", "iid", "sample_path"),
	"load_factory_preset":       set("path", "fst_path", "kind", "name", "insert_index", "slot_marker"),
}

var argAliases = map[string]string{
	"arrangement_id": "arrangement", "arrangementId": "arrangement", "track_id": "track", "trackId": "track",
	"channel_iid": "iid", "channelIid": "iid", "pattern_iid": "iid", "patternIid": "iid",
	"insert_idx": "index", "insertIdx": "index", "insert_index": "index", "insertIndex": "index",
	"arrangement_name": "name", "bpm_value": "bpm",
}

// orderedArgs preserves JSON key order of the request args.
type orderedArgs struct {
	keys []string
	vals map[string]interface{}
}

func parseOrderedObject(raw json.RawMessage) *orderedArgs {
	oa := &orderedArgs{vals: map[string]interface{}{}}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return oa
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return oa
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			break
		}
		key, _ := kt.(string)
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			break
		}
		if _, seen := oa.vals[key]; !seen {
			oa.keys = append(oa.keys, key)
		}
		oa.vals[key] = v
	}
	return oa
}

func normaliseArgs(kind string, oa *orderedArgs) map[string]interface{} {
	if oa == nil {
		return map[string]interface{}{}
	}
	allowed, ok := allowedArgs[kind]
	if !ok {
		return oa.vals
	}
	out := map[string]interface{}{}
	unknown := []string{}
	for _, key := range oa.keys {
		canonical := key
		if !allowed[key] {
			if a, ok := argAliases[key]; ok {
				canonical = a
			}
		}
		if !allowed[canonical] {
			unknown = append(unknown, key)
			continue
		}
		if _, dup := out[canonical]; dup {
			bridgeErr("INVALID_ARGS", "args.%s supplied twice (also via alias '%s')", canonical, key)
		}
		out[canonical] = oa.vals[key]
	}
	if len(unknown) > 0 {
		names := make([]string, 0, len(allowed))
		for k := range allowed {
			names = append(names, k)
		}
		sort.Strings(names)
		bridgeErr("INVALID_ARGS", "unknown args for %s: %s; allowed: %s", kind, strings.Join(unknown, ", "), strings.Join(names, ", "))
	}
	return out
}

// ───────────── JS-ish coercions ─────────────

// toNumber mimics JS Number(v) for JSON-decoded values.
func toNumber(v interface{}) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		t := strings.TrimSpace(x)
		if t == "" {
			return 0
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return math.NaN()
		}
		return f
	}
	return math.NaN()
}

func getNum(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return math.NaN()
	}
	return toNumber(v)
}

// getNumDefault is Number(args[key] ?? def).
func getNumDefault(m map[string]interface{}, key string, def float64) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	return toNumber(v)
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func jsStr(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		return jsNum(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func has(m map[string]interface{}, k string) bool { _, ok := m[k]; return ok }

// intFrom converts a validated-finite float to int, failing with the given INVALID_ARGS message if non-integer.
func intFrom(f float64, format string) int {
	if !isInt(f) {
		mutErr("INVALID_ARGS", format, jsNum(f))
	}
	return int(f)
}

// ───────────── arg parsers ─────────────

func parsePlacementArg(args map[string]interface{}) ClipPlacement {
	kind, _ := args["kind"].(string)
	if kind != "pattern" && kind != "channel" {
		mutErr("INVALID_ARGS", "args.kind must be 'pattern' or 'channel'")
	}
	refID, trackIdx, pos, ln := getNum(args, "ref_id"), getNum(args, "track_index"), getNum(args, "position_ticks"), getNum(args, "length_ticks")
	for _, f := range []float64{refID, trackIdx, pos, ln} {
		if !finite(f) {
			mutErr("INVALID_ARGS", "args.ref_id, args.track_index, args.position_ticks, args.length_ticks all required (numbers)")
		}
	}
	return ClipPlacement{Kind: kind, RefID: refID, TrackIndex: trackIdx, PositionTicks: pos, LengthTicks: ln}
}

func parseMatchArg(args map[string]interface{}) ClipMatch {
	t := getNum(args, "track_index")
	if !finite(t) {
		mutErr("INVALID_ARGS", "args.track_index is required (non-negative integer)")
	}
	out := ClipMatch{TrackIndex: t}
	if has(args, "position_ticks") {
		p := getNum(args, "position_ticks")
		if !finite(p) {
			mutErr("INVALID_ARGS", "args.position_ticks must be number when provided")
		}
		out.PositionTicks = &p
	}
	if has(args, "ref_id") {
		r := getNum(args, "ref_id")
		if !finite(r) {
			mutErr("INVALID_ARGS", "args.ref_id must be number when provided")
		}
		out.RefID = &r
	}
	if has(args, "kind") {
		k, _ := args["kind"].(string)
		if k != "pattern" && k != "channel" {
			mutErr("INVALID_ARGS", "args.kind must be 'pattern' or 'channel'")
		}
		out.Kind = k
	}
	return out
}

func numArgFn(args map[string]interface{}) func(k string, required bool, def float64) float64 {
	return func(k string, required bool, def float64) float64 {
		raw, ok := args[k]
		if !ok || raw == nil {
			if required {
				mutErr("INVALID_ARGS", "args.%s is required (number)", k)
			}
			return def
		}
		v := toNumber(raw)
		if !finite(v) {
			mutErr("INVALID_ARGS", "args.%s must be a finite number, got %s", k, jsStr(raw))
		}
		return v
	}
}

func parseNoteArgs(args map[string]interface{}) NoteInput {
	num := numArgFn(args)
	flags := num("flags", false, 0x4000)
	if has(args, "slide") {
		b, ok := args["slide"].(bool)
		if !ok {
			mutErr("INVALID_ARGS", "args.slide must be boolean when provided")
		}
		fi := toInt32(flags)
		if b {
			fi |= 0x08
		} else {
			fi &^= 0x08
		}
		flags = float64(fi)
	}
	return NoteInput{
		Position: num("position", true, 0), ChannelIid: num("channel_iid", true, 0), Length: num("length", true, 0), Key: num("key", true, 0),
		Flags: flags, Group: num("group", false, 0), FinePitch: num("fine_pitch", false, 120), Release: num("release", false, 64),
		MidiChannel: num("midi_channel", false, 0), Pan: num("pan", false, 64), Velocity: num("velocity", false, 100),
		ModX: num("mod_x", false, 128), ModY: num("mod_y", false, 128),
	}
}

func parseControllerArgs(args map[string]interface{}) ControllerInput {
	num := numArgFn(args)
	return ControllerInput{Position: num("position", true, 0), Channel: num("channel", true, 0), Value: num("value", true, 0), Flags: num("flags", false, 0)}
}

func parseRGBAArg(args map[string]interface{}) MutRGBA {
	c, ok := args["color"].(map[string]interface{})
	if !ok {
		mutErr("INVALID_ARGS", "args.color is required (object {r,g,b,a?})")
	}
	r, g, b := getNum(c, "r"), getNum(c, "g"), getNum(c, "b")
	a := 0.0
	if v, present := c["a"]; present {
		a = toNumber(v)
	}
	for _, f := range []float64{r, g, b, a} {
		if !finite(f) {
			mutErr("INVALID_ARGS", "args.color components must be numeric")
		}
	}
	return MutRGBA{r, g, b, a}
}

// ───────────── loading ─────────────

func loadProject(path string) *FLPProject {
	abs, _ := filepath.Abs(path)
	if _, err := os.Stat(abs); err != nil {
		bridgeErr("FILE_NOT_FOUND", "flp file not found: %s", abs)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		bridgeErr("READ_ERROR", "cannot read %s: %s", abs, err.Error())
	}
	p, perr := ParseFLPFile(b)
	if perr != nil {
		if _, ok := perr.(*FLPParseError); ok {
			bridgeErr("PARSE_ERROR", "%s", perr.Error())
		}
		panic(perr)
	}
	return p
}

func resolveFlToken(p string) string {
	const tok = "%FLStudioFactoryData%"
	if strings.HasPrefix(p, tok) {
		tail := strings.TrimLeft(p[len(tok):], "/")
		return filepath.Join("/Applications/FL Studio 2025.app/Contents/Resources/FL", tail)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func writeProject(path string, p *FLPProject) int {
	bytes, err := SerializeFLPProject(p)
	if err != nil {
		panic(err)
	}
	abs, _ := filepath.Abs(path)
	if err := os.WriteFile(abs, bytes, 0o644); err != nil {
		panic(err)
	}
	return len(bytes)
}

// ───────────── write dispatcher ─────────────

func executeWrite(kind string, args map[string]interface{}, path string, project *FLPProject) (resp Response) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *MutationError:
				resp = errResp(kind, e.Code, e.Message)
			case *BridgeError:
				resp = errResp(kind, e.Code, e.Message)
			case error:
				resp = errResp(kind, "UNKNOWN", e.Error())
			default:
				resp = errResp(kind, "UNKNOWN", fmt.Sprint(r))
			}
		}
	}()
	abs, _ := filepath.Abs(path)
	extra := func(p *FLPProject, extraKV map[string]interface{}) Response {
		n := writeProject(path, p)
		res := map[string]interface{}{"path": abs, "bytes_written": n}
		for k, v := range extraKV {
			res[k] = v
		}
		return okResp(kind, res)
	}
	mutated := project

	switch kind {
	case "set_tempo":
		bpm := getNum(args, "bpm")
		if !finite(bpm) {
			mutErr("INVALID_ARGS", "args.bpm is required (number)")
		}
		mutated = must(SetTempo(project, bpm))
	case "set_pattern_name":
		iid := getNum(args, "iid")
		name, isStr := args["name"].(string)
		if !finite(iid) {
			mutErr("INVALID_ARGS", "args.iid is required (positive integer)")
		}
		if !isStr {
			mutErr("INVALID_ARGS", "args.name is required (string)")
		}
		mutated = must(SetPatternName(project, intFrom(iid, "pattern iid must be a positive integer (1-based), got %s"), name))
	case "set_channel_name":
		iid := getNum(args, "iid")
		name, isStr := args["name"].(string)
		if !finite(iid) {
			mutErr("INVALID_ARGS", "args.iid is required (non-negative integer)")
		}
		if !isStr {
			mutErr("INVALID_ARGS", "args.name is required (string)")
		}
		mutated = must(SetChannelName(project, intFrom(iid, "channel iid must be a non-negative integer, got %s"), name))
	case "set_insert_name":
		idx := getNum(args, "index")
		name, isStr := args["name"].(string)
		if !finite(idx) {
			mutErr("INVALID_ARGS", "args.index is required (non-negative integer)")
		}
		if !isStr {
			mutErr("INVALID_ARGS", "args.name is required (string)")
		}
		mutated = must(SetInsertName(project, intFrom(idx, "insert index must be a non-negative integer, got %s"), name))
	case "set_time_signature":
		num, den := getNum(args, "numerator"), getNum(args, "denominator")
		if !finite(num) {
			mutErr("INVALID_ARGS", "args.numerator is required (positive integer)")
		}
		if !finite(den) {
			mutErr("INVALID_ARGS", "args.denominator is required (power-of-2 integer)")
		}
		mutated = must(SetTimeSignature(project,
			intFrom(num, "numerator must be an integer in [1, 255], got %s"),
			intFrom(den, "denominator must be a power of 2 in [1, 64] (1, 2, 4, 8, 16, 32, 64), got %s")))
	case "set_channel_color":
		iid := getNum(args, "iid")
		if !finite(iid) {
			mutErr("INVALID_ARGS", "args.iid is required (non-negative integer)")
		}
		mutated = must(SetChannelColor(project, intFrom(iid, "channel iid must be a non-negative integer, got %s"), parseRGBAArg(args)))
	case "set_insert_color":
		idx := getNum(args, "index")
		if !finite(idx) {
			mutErr("INVALID_ARGS", "args.index is required (non-negative integer)")
		}
		mutated = must(SetInsertColor(project, intFrom(idx, "insert index must be a non-negative integer, got %s"), parseRGBAArg(args)))
	case "set_pattern_color":
		iid := getNum(args, "iid")
		if !finite(iid) {
			mutErr("INVALID_ARGS", "args.iid is required (positive integer)")
		}
		mutated = must(SetPatternColor(project, intFrom(iid, "pattern iid must be a positive integer (1-based), got %s"), parseRGBAArg(args)))
	case "set_channel_routing":
		iid, target := getNum(args, "iid"), getNum(args, "target_insert")
		if !finite(iid) {
			mutErr("INVALID_ARGS", "args.iid is required (non-negative integer)")
		}
		if !finite(target) {
			mutErr("INVALID_ARGS", "args.target_insert is required (-1 or 0..127)")
		}
		mutated = must(SetChannelRouting(project, intFrom(iid, "channel iid must be a non-negative integer, got %s"),
			intFrom(target, "targetInsert must be integer in [-1, 127] (signed int8), got %s")))
	case "set_arrangement_name":
		id := getNumDefault(args, "id", 0)
		name, isStr := args["name"].(string)
		if !finite(id) {
			mutErr("INVALID_ARGS", "args.id must be a non-negative integer")
		}
		if !isStr {
			mutErr("INVALID_ARGS", "args.name is required (string)")
		}
		mutated = must(SetArrangementName(project, intFrom(id, "arrangement id must be a non-negative integer, got %s"), name))
	case "set_track_name":
		arr, trk := getNumDefault(args, "arrangement", 0), getNum(args, "track")
		name, isStr := args["name"].(string)
		if !finite(arr) {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		if !finite(trk) {
			mutErr("INVALID_ARGS", "args.track is required (non-negative integer)")
		}
		if !isStr {
			mutErr("INVALID_ARGS", "args.name is required (string)")
		}
		mutated = must(SetTrackName(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"),
			intFrom(trk, "track index must be a non-negative integer, got %s"), name))
	case "set_track_color":
		arr, trk := getNumDefault(args, "arrangement", 0), getNum(args, "track")
		if !finite(arr) {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		if !finite(trk) {
			mutErr("INVALID_ARGS", "args.track is required (non-negative integer)")
		}
		mutated = must(SetTrackColor(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"),
			intFrom(trk, "track index must be a non-negative integer, got %s"), parseRGBAArg(args)))
	case "add_clip":
		arr := getNumDefault(args, "arrangement", 0)
		if !finite(arr) {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		mutated = must(AddClip(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"), parsePlacementArg(args)))
	case "remove_clip":
		arr := getNumDefault(args, "arrangement", 0)
		if !finite(arr) {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		mutated = must(RemoveClip(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"), parseMatchArg(args)))
	case "move_clip":
		arr := getNumDefault(args, "arrangement", 0)
		if !finite(arr) {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		to := ClipDest{}
		if has(args, "to_track_index") {
			t := getNum(args, "to_track_index")
			if !finite(t) {
				mutErr("INVALID_ARGS", "args.to_track_index must be a number")
			}
			to.TrackIndex = &t
		}
		if has(args, "to_position_ticks") {
			p := getNum(args, "to_position_ticks")
			if !finite(p) {
				mutErr("INVALID_ARGS", "args.to_position_ticks must be a number")
			}
			to.PositionTicks = &p
		}
		mutated = must(MoveClip(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"), parseMatchArg(args), to))
	case "set_track_grouped":
		arr, trk := getNumDefault(args, "arrangement", 0), getNum(args, "track")
		grouped, isBool := args["grouped"].(bool)
		if !finite(arr) {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		if !finite(trk) {
			mutErr("INVALID_ARGS", "args.track is required (non-negative integer)")
		}
		if !isBool {
			mutErr("INVALID_ARGS", "args.grouped is required (boolean)")
		}
		mutated = must(SetTrackGrouped(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"),
			intFrom(trk, "track index must be a non-negative integer, got %s"), grouped))
	case "clone_pattern":
		iid := getNum(args, "source_iid")
		var newName *string
		if v, ok := args["name"]; ok {
			s, isStr := v.(string)
			if !isStr {
				mutErr("INVALID_ARGS", "args.name must be a string when provided")
			}
			newName = &s
		}
		if !finite(iid) {
			mutErr("INVALID_ARGS", "args.source_iid is required (positive integer)")
		}
		mutated = must(ClonePattern(project, intFrom(iid, "pattern iid must be a positive integer (1-based), got %s"), newName))
	case "reorganize_project":
		arr := getNumDefault(args, "arrangement", 0)
		if !finite(arr) || arr < 0 {
			mutErr("INVALID_ARGS", "args.arrangement must be a non-negative integer")
		}
		opts := ReorganizeOptions{}
		a := int(arr)
		opts.ArrangementID = &a
		if v, ok := args["add_family_separators"]; ok {
			b, isBool := v.(bool)
			if !isBool {
				mutErr("INVALID_ARGS", "args.add_family_separators must be boolean when provided")
			}
			opts.AddFamilySeparators = &b
		}
		dryRun := false
		if v, ok := args["dry_run"]; ok {
			b, isBool := v.(bool)
			if !isBool {
				mutErr("INVALID_ARGS", "args.dry_run must be boolean when provided")
			}
			dryRun = b
		}
		if dryRun {
			plan := PlanReorganize(project, opts)
			return okResp(kind, map[string]interface{}{"path": abs, "dry_run": true, "mutations_applied": 0, "plan": plan})
		}
		out, plan, applied, err := ReorganizeProject(project, opts)
		if err != nil {
			panic(err)
		}
		n := writeProject(path, out)
		return okResp(kind, map[string]interface{}{"path": abs, "bytes_written": n, "mutations_applied": applied, "plan": plan})
	case "add_pattern_note":
		pid := getNum(args, "pattern_id")
		if !finite(pid) {
			mutErr("INVALID_ARGS", "args.pattern_id is required (positive integer)")
		}
		mutated = must(AddPatternNote(project, intFrom(pid, "pattern id must be a positive integer (1-based), got %s"), parseNoteArgs(args)))
	case "set_pattern_notes":
		pid := getNum(args, "pattern_id")
		if !finite(pid) {
			mutErr("INVALID_ARGS", "args.pattern_id is required (positive integer)")
		}
		raw, ok := args["notes"].([]interface{})
		if !ok {
			mutErr("INVALID_ARGS", "args.notes is required (array of note objects, possibly empty)")
		}
		notes := make([]NoteInput, len(raw))
		for i, n := range raw {
			m, ok := n.(map[string]interface{})
			if !ok {
				mutErr("INVALID_ARGS", "args.notes[%d] must be an object", i)
			}
			notes[i] = parseNoteArgs(m)
		}
		mutated = must(SetPatternNotes(project, intFrom(pid, "pattern id must be a positive integer (1-based), got %s"), notes))
	case "remove_pattern_note":
		pid, idx := getNum(args, "pattern_id"), getNum(args, "index")
		if !finite(pid) {
			mutErr("INVALID_ARGS", "args.pattern_id is required (positive integer)")
		}
		if !finite(idx) {
			mutErr("INVALID_ARGS", "args.index is required (non-negative integer)")
		}
		mutated = must(RemovePatternNote(project, intFrom(pid, "pattern id must be a positive integer (1-based), got %s"), idx))
	case "add_pattern_controller":
		pid := getNum(args, "pattern_id")
		if !finite(pid) {
			mutErr("INVALID_ARGS", "args.pattern_id is required (positive integer)")
		}
		mutated = must(AddPatternController(project, intFrom(pid, "pattern id must be a positive integer (1-based), got %s"), parseControllerArgs(args)))
	case "set_pattern_controllers":
		pid := getNum(args, "pattern_id")
		if !finite(pid) {
			mutErr("INVALID_ARGS", "args.pattern_id is required (positive integer)")
		}
		raw, ok := args["controllers"].([]interface{})
		if !ok {
			mutErr("INVALID_ARGS", "args.controllers is required (array of controller objects, possibly empty)")
		}
		ctrls := make([]ControllerInput, len(raw))
		for i, c := range raw {
			m, ok := c.(map[string]interface{})
			if !ok {
				mutErr("INVALID_ARGS", "args.controllers[%d] must be an object", i)
			}
			ctrls[i] = parseControllerArgs(m)
		}
		mutated = must(SetPatternControllers(project, intFrom(pid, "pattern id must be a positive integer (1-based), got %s"), ctrls))
	case "remove_pattern_controller":
		pid, idx := getNum(args, "pattern_id"), getNum(args, "index")
		if !finite(pid) {
			mutErr("INVALID_ARGS", "args.pattern_id is required (positive integer)")
		}
		if !finite(idx) {
			mutErr("INVALID_ARGS", "args.index is required (non-negative integer)")
		}
		mutated = must(RemovePatternController(project, intFrom(pid, "pattern id must be a positive integer (1-based), got %s"), idx))
	case "create_pattern":
		name := ""
		if v, ok := args["name"]; ok {
			s, isStr := v.(string)
			if !isStr {
				mutErr("INVALID_ARGS", "args.name must be string when provided")
			}
			name = s
		}
		out, id, err := CreatePattern(project, name)
		if err != nil {
			panic(err)
		}
		return extra(out, map[string]interface{}{"pattern_id": id})
	case "set_native_plugin_param":
		mutated = must(execSetNativePluginParam(project, args))
	case "set_pattern_length":
		pid, ticks := getNum(args, "pattern_id"), getNum(args, "ticks")
		if !isInt(pid) || !isInt(ticks) {
			mutErr("INVALID_ARGS", "pattern_id + ticks required (integers)")
		}
		mutated = must(SetPatternLength(project, int(pid), int64(ticks)))
	case "transpose_pattern_notes":
		pid, semi := getNum(args, "pattern_id"), getNum(args, "semitones")
		var ch *int
		if has(args, "channel_iid") {
			f := getNum(args, "channel_iid")
			v := -1 // non-integer → matches nothing, like NaN !== iid
			if isInt(f) {
				v = int(f)
			}
			ch = &v
		}
		if !isInt(pid) || !isInt(semi) {
			mutErr("INVALID_ARGS", "pattern_id + semitones required (integers)")
		}
		mutated = must(TransposePatternNotes(project, int(pid), int(semi), ch))
	case "quantize_pattern_notes":
		pid, grid := getNum(args, "pattern_id"), getNum(args, "grid_ticks")
		strength := 1.0
		if has(args, "strength") {
			strength = getNum(args, "strength")
		}
		if !isInt(pid) || !isInt(grid) {
			mutErr("INVALID_ARGS", "pattern_id + grid_ticks required (integers)")
		}
		mutated = must(QuantizePatternNotes(project, int(pid), int(grid), strength))
	case "humanize_velocities":
		pid, rng := getNum(args, "pattern_id"), getNum(args, "range")
		var seed *float64
		if has(args, "seed") {
			s := getNum(args, "seed")
			seed = &s
		}
		if !isInt(pid) || !isInt(rng) {
			mutErr("INVALID_ARGS", "pattern_id + range required (integers)")
		}
		mutated = must(HumanizeVelocities(project, int(pid), int(rng), seed))
	case "humanize_timings":
		pid, rng := getNum(args, "pattern_id"), getNum(args, "range_ticks")
		var seed *float64
		if has(args, "seed") {
			s := getNum(args, "seed")
			seed = &s
		}
		if !isInt(pid) || !isInt(rng) {
			mutErr("INVALID_ARGS", "pattern_id + range_ticks required (integers)")
		}
		mutated = must(HumanizeTimings(project, int(pid), int(rng), seed))
	case "reverse_pattern_notes":
		pid := getNum(args, "pattern_id")
		if !isInt(pid) {
			mutErr("INVALID_ARGS", "pattern_id required (integer)")
		}
		mutated = must(ReversePatternNotes(project, int(pid)))
	case "invert_pattern_notes":
		pid := getNum(args, "pattern_id")
		axis := 60.0
		if has(args, "axis_key") {
			axis = getNum(args, "axis_key")
		}
		if !isInt(pid) {
			mutErr("INVALID_ARGS", "pattern_id required (integer)")
		}
		if !isInt(axis) {
			mutErr("INVALID_ARGS", "axisKey must be in [0, 131], got %s", jsNum(axis))
		}
		mutated = must(InvertPatternNotes(project, int(pid), int(axis)))
	case "set_channel_volume":
		iid, v := getNum(args, "iid"), getNum(args, "value")
		if !isInt(iid) || !finite(v) {
			mutErr("INVALID_ARGS", "iid (int) + value (0..1) required")
		}
		mutated = must(SetChannelVolume(project, int(iid), v))
	case "set_channel_pan":
		iid, v := getNum(args, "iid"), getNum(args, "value")
		if !isInt(iid) || !finite(v) {
			mutErr("INVALID_ARGS", "iid (int) + value (-1..+1) required")
		}
		mutated = must(SetChannelPan(project, int(iid), v))
	case "arrange_song":
		arr := getNumDefault(args, "arrangement", 0)
		raw, ok := args["structure"].([]interface{})
		if !ok {
			mutErr("INVALID_ARGS", "args.structure must be an array")
		}
		structure := make([]SongSection, len(raw))
		for i, s := range raw {
			obj, ok := s.(map[string]interface{})
			if !ok {
				panic(fmt.Errorf("Cannot read properties of %s (reading 'pattern_id')", jsStr(s)))
			}
			pid, bars := getNum(obj, "pattern_id"), getNum(obj, "bars")
			if !isInt(pid) || !isInt(bars) {
				mutErr("INVALID_ARGS", "structure[%d].pattern_id + bars required (integers)", i)
			}
			sec := SongSection{PatternID: int(pid), Bars: int(bars)}
			if has(obj, "position_ticks") {
				p := getNum(obj, "position_ticks")
				sec.PositionTicks = &p
			}
			structure[i] = sec
		}
		trackIndex := 0.0
		if has(args, "track_index") {
			trackIndex = getNum(args, "track_index")
		}
		bpb := 4.0
		if has(args, "beats_per_bar") {
			bpb = getNum(args, "beats_per_bar")
		}
		mutated = must(ArrangeSong(project, intFrom(arr, "arrangement id must be a non-negative integer, got %s"), structure,
			ArrangeSongOptions{TrackIndex: &trackIndex, BeatsPerBar: &bpb}))
	case "set_channel_sample_path":
		iid := getNum(args, "iid")
		sp, isStr := args["sample_path"].(string)
		if !isInt(iid) || !isStr {
			mutErr("INVALID_ARGS", "iid (int) + sample_path (string, FL token form) required")
		}
		mutated = must(SetChannelSamplePath(project, int(iid), sp))
	case "load_factory_preset":
		return execLoadFactoryPreset(kind, args, path, project, abs)
	case "instantiate_native_plugin":
		donorPath := ""
		if v, ok := args["donor_path"]; ok && v != nil {
			donorPath = jsStr(v)
		}
		pluginName := ""
		if v, ok := args["plugin_name"]; ok && v != nil {
			pluginName = jsStr(v)
		}
		insertIdx := getNumDefault(args, "insert_index", 0)
		slotMarker := getNumDefault(args, "slot_marker", 0)
		if donorPath == "" || pluginName == "" {
			mutErr("INVALID_ARGS", "args.donor_path + args.plugin_name required")
		}
		db, err := os.ReadFile(donorPathAbs(donorPath))
		if err != nil {
			panic(err)
		}
		donor, perr := ParseFLPFile(db)
		if perr != nil {
			panic(perr)
		}
		out, slot, err := InstantiateNativePlugin(project, donor, pluginName, int(insertIdx), int(slotMarker))
		if err != nil {
			panic(err)
		}
		return extra(out, map[string]interface{}{"fl_ipc_slot_index": slot})
	case "create_channel":
		name := ""
		if v, ok := args["name"]; ok {
			s, isStr := v.(string)
			if !isStr {
				mutErr("INVALID_ARGS", "args.name must be string when provided")
			}
			name = s
		}
		kindArg := ""
		if v, ok := args["kind"]; ok {
			s, _ := v.(string)
			if s != "sampler" && s != "instrument" && s != "automation" && s != "layer" {
				mutErr("INVALID_ARGS", "args.kind must be one of: sampler, instrument, automation, layer")
			}
			kindArg = s
		}
		out, iid, err := CreateChannel(project, name, ChannelKindInput(kindArg))
		if err != nil {
			panic(err)
		}
		return extra(out, map[string]interface{}{"channel_iid": iid})
	default:
		return errResp(kind, "UNKNOWN", "write dispatcher fell through for kind="+kind)
	}
	return extra(mutated, nil)
}

func donorPathAbs(p string) string {
	abs, _ := filepath.Abs(p)
	return abs
}

func execSetNativePluginParam(project *FLPProject, args map[string]interface{}) (*FLPProject, error) {
	var scope PluginScope
	switch args["scope"] {
	case "channel":
		cid := getNum(args, "channel_iid")
		if !finite(cid) {
			mutErr("INVALID_ARGS", "args.channel_iid required when scope='channel'")
		}
		scope = PluginScope{Kind: "channel", ChannelIid: cid}
	case "mixer_slot":
		ii, si := getNum(args, "insert_index"), getNum(args, "slot_index")
		if !finite(ii) || !finite(si) {
			mutErr("INVALID_ARGS", "args.insert_index + args.slot_index required when scope='mixer_slot'")
		}
		scope = PluginScope{Kind: "mixer_slot", InsertIndex: ii, SlotIndex: si}
	default:
		mutErr("INVALID_ARGS", "args.scope must be 'channel' or 'mixer_slot'")
	}
	var param PluginParamRef
	switch args["param"] {
	case "main_level":
		param = PluginParamRef{Kind: "main_level"}
	case "band":
		band := getNum(args, "band")
		field, _ := args["field"].(string)
		if !finite(band) {
			mutErr("INVALID_ARGS", "args.band required (1..7) when param='band'")
		}
		if field != "level" && field != "freq" && field != "width" {
			mutErr("INVALID_ARGS", "args.field must be 'level' | 'freq' | 'width' when param='band'")
		}
		param = PluginParamRef{Kind: "band", Band: band, Field: field}
	case "param":
		idx := getNum(args, "param_index")
		if !isInt(idx) || idx < 0 {
			mutErr("INVALID_ARGS", "args.param_index required (non-negative integer) when param='param'")
		}
		param = PluginParamRef{Kind: "param", Index: idx}
	default:
		mutErr("INVALID_ARGS", "args.param must be 'main_level' | 'band' | 'param'")
	}
	value := getNum(args, "value")
	if !finite(value) {
		mutErr("INVALID_ARGS", "args.value required (normalized 0..1)")
	}
	return SetNativePluginParam(project, scope, param, value)
}

func execLoadFactoryPreset(kind string, args map[string]interface{}, path string, project *FLPProject, abs string) Response {
	fstArg := ""
	if v, ok := args["fst_path"]; ok && v != nil {
		fstArg = jsStr(v)
	}
	presetKind := "generator"
	if v, ok := args["kind"]; ok && v != nil {
		presetKind = jsStr(v)
	}
	if fstArg == "" {
		mutErr("INVALID_ARGS", "args.fst_path required")
	}
	if presetKind != "generator" && presetKind != "effect" {
		mutErr("INVALID_ARGS", "args.kind must be 'generator' or 'effect'")
	}
	resolved := resolveFlToken(fstArg)
	db, err := os.ReadFile(resolved)
	if err != nil {
		mutErr("PRESET_FILE_NOT_FOUND", "cannot read .fst at %s: %s", resolved, err.Error())
	}
	donor, perr := ParseFLPFile(db)
	if perr != nil {
		panic(perr)
	}
	if presetKind == "generator" {
		var name *string
		if v, ok := args["name"]; ok {
			s := jsStr(v)
			name = &s
		}
		out, iid, e := LoadFactoryGeneratorPreset(project, donor, name)
		if e != nil {
			panic(e)
		}
		n := writeProject(path, out)
		return okResp(kind, map[string]interface{}{"path": abs, "bytes_written": n, "channel_iid": iid})
	}
	insertIdx := getNumDefault(args, "insert_index", -1)
	slotMarker := getNumDefault(args, "slot_marker", -1)
	if !isInt(insertIdx) || insertIdx < 0 || !isInt(slotMarker) || slotMarker < 0 {
		mutErr("INVALID_ARGS", "args.insert_index + args.slot_marker required (non-negative ints) for kind='effect'")
	}
	out, slot, e := LoadFactoryEffectPreset(project, donor, int(insertIdx), int(slotMarker))
	if e != nil {
		panic(e)
	}
	n := writeProject(path, out)
	return okResp(kind, map[string]interface{}{"path": abs, "bytes_written": n, "fl_ipc_slot_index": slot})
}

// ───────────── read dispatcher ─────────────

func matchesName(name *string, query string, fuzzy bool) bool {
	if name == nil || *name == "" || query == "" {
		return false
	}
	if fuzzy {
		return strings.Contains(strings.ToLower(*name), strings.ToLower(query))
	}
	return *name == query
}

func nullable(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func boolPtrIface(p *bool) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok && v != nil {
		return jsStr(v)
	}
	return ""
}

func fuzzyArg(args map[string]interface{}) bool {
	if v, ok := args["fuzzy"]; ok && v != nil {
		if b, isBool := v.(bool); isBool && !b {
			return false
		}
	}
	return true
}

func arrangementIndexArg(args map[string]interface{}, n int) (int, *Response, string) {
	f := getNumDefault(args, "arrangement", 0)
	if !isInt(f) || f < 0 || f >= float64(n) {
		return 0, nil, fmt.Sprintf("args.arrangement out of range; project has %d arrangement(s)", n)
	}
	return int(f), nil, ""
}

func executeRead(kind string, args map[string]interface{}, project *FLPProject) Response {
	switch kind {
	case "describe":
		return okResp(kind, ToFlpInfoJson(project))
	case "get_tempo":
		return okResp(kind, map[string]interface{}{"tempo_bpm": GetTempo(project)})
	case "list_channels":
		return okResp(kind, BuildProjectSummary(project).Channels)
	case "list_mixer":
		return okResp(kind, BuildProjectSummary(project).Inserts)
	case "list_patterns":
		return okResp(kind, BuildProjectSummary(project).Patterns)
	case "list_plugins":
		plugins := []map[string]interface{}{}
		for _, ch := range project.Channels {
			if ch.Plugin != nil {
				name := ch.Plugin.InternalName
				if ch.Plugin.Name != nil {
					name = *ch.Plugin.Name
				}
				plugins = append(plugins, map[string]interface{}{"scope": "channel", "channel_index": ch.Iid, "name": name, "vendor": nullable(ch.Plugin.Vendor)})
			}
		}
		summary := BuildProjectSummary(project)
		for _, ins := range summary.Inserts {
			for _, slot := range ins.Slots {
				if slot.Plugin != nil {
					plugins = append(plugins, map[string]interface{}{"scope": "mixer", "insert_index": ins.Index, "slot_index": slot.Index,
						"name": slot.Plugin.Name, "vendor": nullable(slot.Plugin.Vendor)})
				}
			}
		}
		return okResp(kind, plugins)
	case "list_arrangements":
		out := []map[string]interface{}{}
		for _, a := range project.Arrangements {
			out = append(out, map[string]interface{}{"id": a.ID, "name": nullable(a.Name), "track_count": len(a.Tracks),
				"clip_count": len(a.Clips), "timemarker_count": len(a.TimeMarkers)})
		}
		return okResp(kind, out)
	case "list_tracks":
		idx, _, msg := arrangementIndexArg(args, len(project.Arrangements))
		if msg != "" {
			return errResp(kind, "INVALID_ARGS", msg)
		}
		all := project.Arrangements[idx].Tracks
		parent := map[int]int{}
		lastParent := 0
		for _, t := range all {
			if (t.Grouped == nil || !*t.Grouped) || t.Index == 0 {
				lastParent = t.Index
			}
			parent[t.Index] = lastParent
		}
		tracks := []map[string]interface{}{}
		for _, t := range all {
			named := t.Name != nil || (t.Locked != nil && *t.Locked) || (t.Enabled != nil && !*t.Enabled) || (t.Grouped != nil && *t.Grouped)
			if !named {
				continue
			}
			var iid interface{}
			if t.Iid != nil {
				iid = *t.Iid
			}
			var color interface{}
			if t.Color != nil {
				color = *rgbaPtrOut(t.Color)
			}
			var height interface{}
			if t.Height != nil {
				height = float64(*t.Height)
			}
			grouped := t.Grouped != nil && *t.Grouped
			var parentIdx interface{}
			if p := parent[t.Index]; p != t.Index {
				parentIdx = p
			}
			row := map[string]interface{}{"index": t.Index, "name": nullable(t.Name), "color": color, "enabled": boolPtrIface(t.Enabled),
				"locked": boolPtrIface(t.Locked), "height": height, "grouped": grouped, "parent_index": parentIdx}
			if iid != nil {
				row["iid"] = iid
			}
			tracks = append(tracks, row)
		}
		return okResp(kind, map[string]interface{}{"arrangement": idx, "total_tracks": len(all), "tracks": tracks})
	case "find_channel_by_name":
		q, fz := argString(args, "query"), fuzzyArg(args)
		out := []map[string]interface{}{}
		for _, c := range project.Channels {
			if matchesName(c.Name, q, fz) {
				out = append(out, map[string]interface{}{"iid": c.Iid, "name": nullable(c.Name), "kind": string(c.Kind)})
			}
		}
		return okResp(kind, out)
	case "find_insert_by_name":
		q, fz := argString(args, "query"), fuzzyArg(args)
		out := []map[string]interface{}{}
		for _, i := range project.Inserts {
			if matchesName(i.Name, q, fz) {
				out = append(out, map[string]interface{}{"index": i.Index, "name": nullable(i.Name)})
			}
		}
		return okResp(kind, out)
	case "find_pattern_by_name":
		q, fz := argString(args, "query"), fuzzyArg(args)
		out := []map[string]interface{}{}
		for _, p := range project.Patterns {
			if matchesName(p.Name, q, fz) {
				out = append(out, map[string]interface{}{"id": p.ID, "name": nullable(p.Name), "notes": len(p.Notes)})
			}
		}
		return okResp(kind, out)
	case "find_plugin_instances":
		target := strings.ToLower(argString(args, "plugin_name"))
		out := []map[string]interface{}{}
		for _, ch := range project.Channels {
			if ch.Plugin == nil {
				continue
			}
			n := ch.Plugin.InternalName
			if ch.Plugin.Name != nil {
				n = *ch.Plugin.Name
			}
			if n != "" && strings.Contains(strings.ToLower(n), target) {
				out = append(out, map[string]interface{}{"scope": "channel", "channel_index": ch.Iid, "name": n})
			}
		}
		for _, ins := range BuildProjectSummary(project).Inserts {
			for _, slot := range ins.Slots {
				var n string
				if slot.Plugin != nil {
					n = slot.Plugin.Name
				} else if slot.PluginName != nil {
					n = *slot.PluginName
				}
				if n != "" && strings.Contains(strings.ToLower(n), target) {
					out = append(out, map[string]interface{}{"scope": "mixer", "insert_index": ins.Index, "slot_index": slot.Index, "name": n})
				}
			}
		}
		return okResp(kind, out)
	case "list_clips":
		idx, _, msg := arrangementIndexArg(args, len(project.Arrangements))
		if msg != "" {
			return errResp(kind, "INVALID_ARGS", msg)
		}
		out := []map[string]interface{}{}
		for _, c := range project.Arrangements[idx].Clips {
			isPattern := c.ItemIndex > playlistPatternBase
			k, ref := "channel", c.ItemIndex
			if isPattern {
				k, ref = "pattern", c.ItemIndex-playlistPatternBase
			}
			out = append(out, map[string]interface{}{"position_ticks": c.Position, "length_ticks": c.Length, "track_index": clipTrackMax - c.TrackRvidx,
				"kind": k, "ref_id": ref, "group": c.Group, "flags": c.ItemFlags})
		}
		return okResp(kind, out)
	}
	return errResp(kind, "UNKNOWN", "dispatcher fell through for kind="+kind)
}

// Execute runs one bridge request and returns the response envelope.
func Execute(kind string, rawArgs json.RawMessage) (resp Response) {
	if !inList(readKinds, kind) && !inList(writeKinds, kind) {
		all := append(append([]string{}, readKinds...), writeKinds...)
		sort.Strings(all)
		return errResp(kind, "UNSUPPORTED_KIND", fmt.Sprintf("unknown kind %s; supported: %s", jsonString(kind), strings.Join(all, ", ")))
	}
	var args map[string]interface{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if be, ok := r.(*BridgeError); ok {
					resp = errResp(kind, be.Code, be.Message)
					return
				}
				resp = errResp(kind, "UNKNOWN", fmt.Sprint(r))
			}
		}()
		var oa *orderedArgs
		if len(rawArgs) > 0 {
			oa = parseOrderedObject(rawArgs)
		}
		args = normaliseArgs(kind, oa)
	}()
	if resp != nil {
		return resp
	}
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *BridgeError:
				resp = errResp(kind, e.Code, e.Message)
			case error:
				resp = errResp(kind, "UNKNOWN", e.Error())
			default:
				resp = errResp(kind, "UNKNOWN", fmt.Sprint(r))
			}
		}
	}()
	path, _ := args["path"].(string)
	if path == "" {
		bridgeErr("INVALID_ARGS", "args.path is required (non-empty string)")
	}
	project := loadProject(path)
	if inList(writeKinds, kind) {
		return executeWrite(kind, args, path, project)
	}
	return executeRead(kind, args, project)
}

// BridgeMain reads one JSON request from stdin and writes one JSON line to stdout.
func BridgeMain(stdin io.Reader, stdout, stderr io.Writer) int {
	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "bridge: stdin read failed: %v\n", err)
		return 1
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		fmt.Fprintln(stderr, "bridge: empty stdin (expected one JSON object)")
		return 1
	}
	var req struct {
		Kind *string         `json:"kind"`
		Args json.RawMessage `json:"args"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	var top interface{}
	if err := dec.Decode(&top); err != nil {
		writeJSONLine(stdout, errResp("?", "INVALID_REQUEST", err.Error()))
		return 0
	}
	if _, isObj := top.(map[string]interface{}); !isObj {
		writeJSONLine(stdout, errResp("?", "INVALID_REQUEST", "expected object with string `kind`"))
		return 0
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil || req.Kind == nil {
		writeJSONLine(stdout, errResp("?", "INVALID_REQUEST", "expected object with string `kind`"))
		return 0
	}
	writeJSONLine(stdout, Execute(*req.Kind, req.Args))
	return 0
}

func writeJSONLine(w io.Writer, v interface{}) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		buf.Reset()
		_ = enc.Encode(errResp("?", "UNKNOWN", err.Error()))
	}
	_, _ = w.Write(buf.Bytes())
}
