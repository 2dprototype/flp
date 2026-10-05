package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time" 
	"sync"
	"math"

	"github.com/2dprototype/flp"
	"github.com/2dprototype/wui"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/wav" 
	"github.com/gopxl/beep/speaker"
)

const (
	synthSR       = beep.SampleRate(44100)
	wavetableSize = 2048 // must be a power of two
	wavetableMask = wavetableSize - 1
)

var speakerOnce sync.Once

func ensureSpeakerInit() {
	speakerOnce.Do(func() {
		speaker.Init(synthSR, synthSR.N(time.Second/30))
	})
}

// globalSpeakerMu serializes every "swap what's playing" operation across
// the whole app, so two code paths can never queue overlapping streams.
var globalSpeakerMu sync.Mutex

// swapSpeakerStream atomically replaces whatever is currently playing.
func swapSpeakerStream(s beep.Streamer) {
	globalSpeakerMu.Lock()
	defer globalSpeakerMu.Unlock()
	speaker.Clear()
	speaker.Play(s)
}

// ───────────── wavetables ─────────────

type wavetableSet struct {
	sine, square, saw, triangle []float64
}

var wavetables = func() *wavetableSet {
	w := &wavetableSet{
		sine:     make([]float64, wavetableSize),
		square:   make([]float64, wavetableSize),
		saw:      make([]float64, wavetableSize),
		triangle: make([]float64, wavetableSize),
	}
	for i := 0; i < wavetableSize; i++ {
		phase := float64(i) / float64(wavetableSize)
		w.sine[i] = math.Sin(2 * math.Pi * phase)
		if phase < 0.5 {
			w.square[i] = 1
		} else {
			w.square[i] = -1
		}
		w.saw[i] = 2*phase - 1
		w.triangle[i] = 1 - 4*math.Abs(phase-0.5)
	}
	return w
}()

func (w *wavetableSet) table(name string) []float64 {
	switch name {
	case "square":
		return w.square
	case "saw":
		return w.saw
	case "triangle":
		return w.triangle
	default:
		return w.sine
	}
}

// ───────────── synth options ─────────────

// SynthOptions is the complete parameter set of the built-in synthesizer.
// It is passed by value and is safe to copy.
type SynthOptions struct {
	// ── Oscillators ──
	Waveform1    string
	Waveform2    string
	Osc2Detune   float64
	Osc2Mix      float64
	SubLevel     float64
	UnisonVoices int
	UnisonDetune float64

	// ── Filter ──
	FilterType     string
	FilterCutoff   float64
	FilterReso     float64
	FilterEnvAmt   float64
	FilterKeyTrack float64

	// ── Filter envelope ──
	FAttack  float64
	FDecay   float64
	FSustain float64
	FRelease float64

	// ── Amp envelope ──
	Attack  float64
	Decay   float64
	Sustain float64
	Release float64

	// ── LFO ──
	LFOShape  string
	LFORate   float64
	LFODepth  float64
	LFOTarget string
	LFOPitch  float64

	// ── Effects ──
	Drive      float64
	DelayTime  float64
	DelayFeed  float64
	DelayMix   float64
	ReverbSize float64
	ReverbMix  float64

	// ── Master ──
	Gain      float64
	VelToAmp  float64
	VelToFilt float64

	// ── Sample playback (one-shots) ──
	SampleAttack  float64 // s, 0 = no fade-in (hard start)
	SampleDecay   float64 // s, time to fall from 1.0 to SampleSustain
	SampleSustain float64 // 0..1, level held during the middle of the sample
	SampleRelease float64 // s, 0 = no fade-out (hard end at sample boundary)
	SampleVolume  float64 // 0..1 (can exceed 1 for boost), global multiplier
	SamplePitch   float64 // semitones, additional offset on top of note-key pitch
}

func defaultSynthOptions() SynthOptions {
	return SynthOptions{
		Waveform1:    "sine",
		Waveform2:    "off",
		Osc2Detune:   0,
		Osc2Mix:      0,
		SubLevel:     0,
		UnisonVoices: 1,
		UnisonDetune: 0,

		FilterType:     "lp",
		FilterCutoff:   18000,
		FilterReso:     0.707,
		FilterEnvAmt:   0,
		FilterKeyTrack: 0,

		FAttack:  0.004,
		FDecay:   0.30,
		FSustain: 1.0,
		FRelease: 0.20,

		Attack:  0.008,
		Decay:   0.20,
		Sustain: 0.85,
		Release: 0.35,

		LFOShape:  "off",
		LFORate:   5,
		LFODepth:  0,
		LFOTarget: "off",
		LFOPitch:  0,

		Drive:      0,
		DelayTime:  0.25,
		DelayFeed:  0,
		DelayMix:   0,
		ReverbSize: 0,
		ReverbMix:  0,

		Gain:      1.0, // Increased for a louder, closer-to-FL master mix
		VelToAmp:  0.7,
		VelToFilt: 0,

		// ── Sample defaults: 0 attack and 0 release for authentic one-shots ──
		SampleAttack:  0.0,
		SampleDecay:   0.0,
		SampleSustain: 1.0,
		SampleRelease: 0.0, // Fixed: set to 0 so drum tails aren't cut/blunted
		SampleVolume:  1.0,
		SamplePitch:   0.0,
	}
}

// ───────────── shared synth config ─────────────

// SynthConfig holds live synth parameters shared between the Visualizer and
// the Settings modal. Safe for concurrent access.
type SynthConfig struct {
	mu   sync.RWMutex
	opts SynthOptions
}

func NewSynthConfig() *SynthConfig {
	return &SynthConfig{opts: defaultSynthOptions()}
}

func (c *SynthConfig) Snapshot() SynthOptions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.opts
}

func (c *SynthConfig) Update(fn func(*SynthOptions)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.opts)
}

func midiKeyToHz(key int) float64 {
	return 440.0 * math.Pow(2.0, float64(key-69)/12.0)
}

// ───────────── sample locator / loader / cache ─────────────

// sampleCache holds decoded mono PCM arrays keyed by absolute file path.
// Populated on demand; never evicted (fine for a preview tool).
type sampleCache struct {
	mu      sync.RWMutex
	samples map[string][]float64
}

var globalSampleCache = &sampleCache{samples: make(map[string][]float64)}

func (c *sampleCache) get(path string) ([]float64, bool) {
	c.mu.RLock()
	s, ok := c.samples[path]
	c.mu.RUnlock()
	return s, ok
}

func (c *sampleCache) put(path string, s []float64) {
	c.mu.Lock()
	c.samples[path] = s
	c.mu.Unlock()
}

// loadSampleCached returns the mono float64 PCM for path, decoding once and
// caching the result. Returns nil if the file can't be decoded.
func loadSampleCached(path string) []float64 {
	if s, ok := globalSampleCache.get(path); ok {
		return s
	}
	s := loadSamplePCM(path)
	if s != nil {
		globalSampleCache.put(path, s)
	}
	return s
}

// loadSamplePCM decodes a WAV file to a mono []float64 at synthSR.
func loadSamplePCM(path string) []float64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(path))
	var streamer beep.StreamSeekCloser
	var format beep.Format
	switch ext {
	case ".wav":
		streamer, format, err = wav.Decode(f)
	default:
		return nil
	}
	if err != nil || streamer == nil {
		return nil
	}
	defer streamer.Close()

	// Decode to mono.
	buf := make([][2]float64, 4096)
	out := make([]float64, 0, streamer.Len())
	for {
		n, ok := streamer.Stream(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				out = append(out, (buf[i][0]+buf[i][1])*0.5)
			}
		}
		if !ok {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}

	// Resample to synthSR if needed (High-Quality Cubic Hermite Interpolation).
	if format.SampleRate != synthSR {
		ratio := float64(format.SampleRate) / float64(synthSR)
		newLen := int(float64(len(out)) / ratio)
		resampled := make([]float64, newLen)
		for i := 0; i < newLen; i++ {
			srcPos := float64(i) * ratio
			i0 := int(srcPos)
			if i0 >= len(out)-1 {
				resampled[i] = out[len(out)-1]
				continue
			}
			frac := srcPos - float64(i0)
			
			y1 := out[i0]
			y2 := out[i0+1]
			y0, y3 := y1, y2
			if i0 > 0 { y0 = out[i0-1] }
			if i0 < len(out)-2 { y3 = out[i0+2] }
			
			a0 := -0.5*y0 + 1.5*y1 - 1.5*y2 + 0.5*y3
			a1 := y0 - 2.5*y1 + 2.0*y2 - 0.5*y3
			a2 := -0.5*y0 + 0.5*y2
			
			resampled[i] = a0*frac*frac*frac + a1*frac*frac + a2*frac + y1
		}
		out = resampled
	}
	return out
}

// resolveSamplePath implements the 3-step lookup chain:
//
//	A) <flp dir>/<basename>
//	B) <flp dir>/Samples/<basename>, <flp dir>/Data/<basename>, and the
//	   relative path as stored (handles "Samples\kick.wav")
//	C) the absolute path as given, after expanding FL tokens
//
// Returns "" if none exist.
func resolveSamplePath(flpPath, samplePath string) string {
	if samplePath == "" {
		return ""
	}
	flpDir := filepath.Dir(flpPath)

	// Normalize separators from the FLP (Windows-style backslashes).
	normalized := strings.ReplaceAll(samplePath, "\\", "/")
	base := filepath.Base(normalized)

	candidates := make([]string, 0, 6)

	// A) FLP root, filename only
	candidates = append(candidates, filepath.Join(flpDir, base))

	// B) Common subfolders + the relative path itself
	candidates = append(candidates, filepath.Join(flpDir, "Samples", base))
	candidates = append(candidates, filepath.Join(flpDir, "Data", base))
	candidates = append(candidates, filepath.Join(flpDir, "Packs", base))
	if normalized != base {
		candidates = append(candidates, filepath.Join(flpDir, filepath.FromSlash(normalized)))
	}

	// C) Absolute path as-is (or FL-token-expanded)
	if filepath.IsAbs(normalized) {
		candidates = append(candidates, filepath.FromSlash(normalized))
	}
	if strings.HasPrefix(samplePath, "%") {
		if expanded := expandFlToken(samplePath); expanded != "" {
			candidates = append(candidates, expanded)
		}
	}

	// D) User-data root (from persistent config)
	if appConfig.FLStudioUserData != "" {
		candidates = append(candidates,
			filepath.Join(appConfig.FLStudioUserData, base))
		if normalized != base {
			candidates = append(candidates,
				filepath.Join(appConfig.FLStudioUserData, filepath.FromSlash(normalized)))
		}
	}

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// expandFlToken handles %FLStudioFactoryData% by scanning the standard
// Image-Line install root for any FL Studio * folder. Windows-only.
func expandFlToken(p string) string {
	// %FLStudioUserData% — from the persistent config.
	const userTok = "%FLStudioUserData%"
	if strings.HasPrefix(p, userTok) {
		if appConfig.FLStudioUserData == "" {
			return ""
		}
		tail := strings.TrimLeft(p[len(userTok):], "/\\")
		candidate := filepath.Join(appConfig.FLStudioUserData, filepath.FromSlash(tail))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		return ""
	}

	// %FLStudioFactoryData% — scan the standard Image-Line install root.
	const factoryTok = "%FLStudioFactoryData%"
	if strings.HasPrefix(p, factoryTok) {
		tail := strings.TrimLeft(p[len(factoryTok):], "/\\")
		root := `C:\Program Files\Image-Line`
		entries, err := os.ReadDir(root)
		if err != nil {
			return ""
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "FL Studio") {
				continue
			}
			candidate := filepath.Join(root, e.Name(), "Data", "Patches", filepath.FromSlash(tail))
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	return ""
}

// isSampleChannel reports whether a channel should be routed to sample
// playback rather than the synth engine.
func isSampleChannel(ch flp.Channel) bool {
	return ch.Kind == flp.ChannelSampler &&
		ch.SamplePath != nil && *ch.SamplePath != ""
}


// ───────────────────────── sample playback ─────────────────────────

// mixSampleEx mixes a mono sample into buf with an ADSR envelope.
func mixSampleEx(buf, sample []float64, startSample, maxSamples int,
	pitchRatio, gain float64, opt SynthOptions, sr float64) {

	if len(sample) < 2 || maxSamples <= 0 || startSample >= len(buf) || pitchRatio <= 0 {
		return
	}
	if gain < 0 {
		gain = 0
	}
	if gain > 4 {
		gain = 4
	}

	sampleLen := len(sample)
	naturalOut := int(float64(sampleLen) / pitchRatio)
	if naturalOut < maxSamples {
		maxSamples = naturalOut
	}
	if startSample+maxSamples > len(buf) {
		maxSamples = len(buf) - startSample
	}
	if maxSamples <= 0 {
		return
	}

	attackSamples := int(opt.SampleAttack * sr)
	decaySamples := int(opt.SampleDecay * sr)
	releaseSamples := int(opt.SampleRelease * sr)
	
	if attackSamples < 0 { attackSamples = 0 }
	if decaySamples < 0 { decaySamples = 0 }
	if releaseSamples < 0 { releaseSamples = 0 }
	if attackSamples > maxSamples { attackSamples = maxSamples }
	if decaySamples > maxSamples-attackSamples { decaySamples = maxSamples - attackSamples }
	if releaseSamples > maxSamples { releaseSamples = maxSamples }

	sustain := opt.SampleSustain
	if sustain < 0 { sustain = 0 }
	if sustain > 1 { sustain = 1 }

	attackEnd := attackSamples
	decayEnd := attackEnd + decaySamples
	releaseStart := maxSamples - releaseSamples
	if releaseStart < decayEnd {
		releaseStart = decayEnd
	}

	// ── Highly optimized fast-path for non-pitched audio clips ──
	fastPath := (pitchRatio == 1.0 && attackSamples == 0 && decaySamples == 0 && releaseSamples == 0 && sustain == 1.0)

	for out := 0; out < maxSamples; out++ {
		var s float64
		if fastPath {
			s = sample[out]
		} else {
			srcPos := float64(out) * pitchRatio
			i0 := int(srcPos)
			if i0 >= sampleLen-1 {
				s = sample[sampleLen-1]
			} else {
				// ── High-Quality Cubic Hermite Interpolation ──
				frac := srcPos - float64(i0)
				y1 := sample[i0]
				y2 := sample[i0+1]
				y0, y3 := y1, y2
				if i0 > 0 { y0 = sample[i0-1] }
				if i0 < sampleLen-2 { y3 = sample[i0+2] }

				a0 := -0.5*y0 + 1.5*y1 - 1.5*y2 + 0.5*y3
				a1 := y0 - 2.5*y1 + 2.0*y2 - 0.5*y3
				a2 := -0.5*y0 + 0.5*y2

				s = a0*frac*frac*frac + a1*frac*frac + a2*frac + y1
			}
		}

		env := 1.0
		if !fastPath {
			if attackSamples > 0 && out < attackEnd {
				env = float64(out) / float64(attackSamples)
			} else if out >= attackEnd && out < releaseStart {
				if decaySamples > 0 && out < decayEnd {
					t := float64(out-attackEnd) / float64(decaySamples)
					env = 1.0 - (1.0-sustain)*t
				} else {
					env = sustain
				}
			} else if releaseSamples > 0 && out >= releaseStart {
				t := float64(out-releaseStart) / float64(releaseSamples)
				if t > 1 { t = 1 }
				env = sustain * (1.0 - t)
			} else if releaseSamples == 0 && out >= releaseStart {
				// Prevent ADSR from muting output if there's no release phase
				env = sustain
			}
		}

		buf[startSample+out] += s * env * gain
	}
}

// mixSample is the pattern-note entry point. It now respects the piano roll 
// note length, correctly truncating playback or applying the release tail 
// when the note ends.
func mixSample(buf []float64, sample []float64, n flp.Note, spt, sr float64,
	opt SynthOptions, channelGain float64) {

	if len(sample) < 2 {
		return
	}
	startSample := int(float64(n.Position) * spt * sr)
	if startSample >= len(buf) {
		return
	}
	pitchRatio := math.Pow(2.0, (float64(n.Key-60)+opt.SamplePitch)/12.0)
	if pitchRatio <= 0 {
		return
	}
	vel := float64(n.Velocity) / 127.0
	if vel <= 0 {
		vel = 1
	}
	gain := opt.SampleVolume * channelGain * vel
	naturalOut := int(float64(len(sample)) / pitchRatio)

	// Calculate exact note duration in samples based on piano roll length
	noteSamples := int(float64(n.Length) * spt * sr)
	releaseSamples := int(opt.SampleRelease * sr)
	
	// Total playback length is the held note duration + the release tail
	maxSamples := noteSamples + releaseSamples

	// Cap playback at the sample's natural length to prevent out-of-bounds reading
	if maxSamples > naturalOut {
		maxSamples = naturalOut
	}

	mixSampleEx(buf, sample, startSample, maxSamples, pitchRatio, gain, opt, sr)
}

// ───────────────────────── pattern / arrangement renderer ─────────────────────────

// renderPatternWithClips is the real rendering pipeline. It mixes:
//
//  1. every note in pat (sampler notes → sample playback, the rest → synth)
//  2. every extraChannelClip (playlist audio clips), truncated to the
//     clip's playlist length
//
// into a single buffer, then applies drive → delay → reverb → master gain.
//
// renderPattern is a thin wrapper that passes no extra clips; renderArrangement
// uses it to feed in the arrangement's channel clips.
func renderPatternWithClips(pat flp.Pattern, ppq int, bpm float64, opt SynthOptions,
	channels []flp.Channel, flpPath string, extraChannelClips []flp.Clip) []float64 {

	if ppq <= 0 {
		ppq = 96
	}
	if bpm <= 0 {
		bpm = 120
	}
	tps := bpm / 60.0 * float64(ppq)
	spt := 1.0 / tps
	sr := float64(synthSR)

	// ── Channel lookup + per-channel sample/gain pre-resolution (once) ──
	chByIid := make(map[int]flp.Channel, len(channels))
	for _, ch := range channels {
		chByIid[ch.Iid] = ch
	}
	chanSamples := make(map[int][]float64)
	chanGains := make(map[int]float64, len(channels))
	haveSamples := false
	for iid, ch := range chByIid {
		// Per-channel gain: FL stores volume in a 0..12800 range where
		// 12800 is the setter's "1.0" (see mutations_transform.go).
		g := 1.0
		if ch.Levels != nil {
			g = float64(ch.Levels.Volume) / 12800.0
			if g < 0 {
				g = 0
			}
			if g > 2 {
				g = 2
			}
		}
		chanGains[iid] = g

		if !isSampleChannel(ch) || flpPath == "" {
			continue
		}
		resolved := resolveSamplePath(flpPath, *ch.SamplePath)
		if resolved == "" {
			continue
		}
		if smp := loadSampleCached(resolved); len(smp) > 0 {
			chanSamples[iid] = smp
			haveSamples = true
		}
	}

	// ── Buffer length: max of pattern-note ends and clip ends + tail ──
	var maxTick uint32
	for _, n := range pat.Notes {
		if e := n.Position + n.Length; e > maxTick {
			maxTick = e
		}
	}
	for _, cl := range extraChannelClips {
		if e := cl.Position + cl.Length; e > maxTick {
			maxTick = e
		}
	}
	filterActive := opt.FilterType != "off" && opt.FilterType != ""
	tail := opt.Release
	if filterActive && opt.FRelease > tail {
		tail = opt.FRelease
	}
	if opt.DelayMix > 0.01 {
		tail += opt.DelayTime * 4
	}
	if opt.ReverbMix > 0.01 {
		tail += 2.0
	}
	if haveSamples || len(extraChannelClips) > 0 {
		tail += 2.0
	}
	totalSec := float64(maxTick)*spt + tail + 0.1
	if totalSec < 0.2 {
		totalSec = 0.2
	}
	buf := make([]float64, int(totalSec*sr))
	if len(pat.Notes) == 0 && len(extraChannelClips) == 0 {
		return buf
	}

	// ── Waveform tables (nil = noise) ──
	var table1, table2 []float64
	if opt.Waveform1 != "noise" {
		table1 = wavetables.table(opt.Waveform1)
	}
	osc2Active := opt.Waveform2 != "off" && opt.Waveform2 != "" && opt.Osc2Mix > 0.001
	if osc2Active && opt.Waveform2 != "noise" {
		table2 = wavetables.table(opt.Waveform2)
	}
	subTable := wavetables.sine

	unisonVoices := opt.UnisonVoices
	if unisonVoices < 1 {
		unisonVoices = 1
	}
	if unisonVoices > 7 {
		unisonVoices = 7
	}
	unisonAmp := 1.0
	if unisonVoices > 1 {
		unisonAmp = 1.0 / math.Sqrt(float64(unisonVoices))
	}
	osc1Mix := 1.0 - opt.Osc2Mix

	lfoActive := (opt.LFOShape == "sine" || opt.LFOShape == "tri" ||
		opt.LFOShape == "square" || opt.LFOShape == "saw") &&
		(opt.LFOTarget == "amp" || opt.LFOTarget == "filter")

	renderVoice := func(n flp.Note) {
		if n.Length == 0 {
			return
		}
		startSample := int(float64(n.Position) * spt * sr)
		if startSample >= len(buf) {
			return
		}
		noteSamples := maxInt(1, int(float64(n.Length)*spt*sr))
		noteEnd := startSample + noteSamples
		if noteEnd > len(buf) {
			noteEnd = len(buf)
		}

		relSec := opt.Release
		if filterActive && opt.FRelease > relSec {
			relSec = opt.FRelease
		}
		relSamples := int(relSec*sr) + 64
		releaseEnd := noteEnd + relSamples
		if releaseEnd > len(buf) {
			releaseEnd = len(buf)
		}

		ampEnv := newADSR(opt.Attack, opt.Decay, opt.Sustain, opt.Release, sr)
		var filtEnv *adsr
		if filterActive {
			filtEnv = newADSR(opt.FAttack, opt.FDecay, opt.FSustain, opt.FRelease, sr)
		}

		vel := float64(n.Velocity) / 127.0
		if vel <= 0 {
			vel = 1
		}
		velAmp := (1 - opt.VelToAmp) + opt.VelToAmp*vel
		velFilt := 1.0 + opt.VelToFilt*(vel-0.5)*2

		baseHz := midiKeyToHz(n.Key)
		subIncr := baseHz * 0.5 * float64(wavetableSize) / sr
		osc2Incr := baseHz * math.Pow(2, opt.Osc2Detune/12.0) * float64(wavetableSize) / sr

		unisonPhases := make([]float64, unisonVoices)
		unisonIncrs := make([]float64, unisonVoices)
		for u := 0; u < unisonVoices; u++ {
			var cents float64
			if unisonVoices > 1 {
				t := float64(u)/float64(unisonVoices-1)*2 - 1
				cents = t * opt.UnisonDetune
			}
			unisonIncrs[u] = baseHz * math.Pow(2, cents/1200.0) * float64(wavetableSize) / sr
		}

		var subPhase, osc2Phase float64
		var filt biquad
		filtCountdown := 0
		lfoIncr := opt.LFORate / sr
		lfoPhase := 0.0
		noiseState := uint32(n.Position)*2654435761 + uint32(n.Key)*40503 + 1

		for i := startSample; i < releaseEnd; i++ {
			if i == noteEnd {
				ampEnv.releaseFrom(opt.Release, sr)
				if filtEnv != nil {
					filtEnv.releaseFrom(opt.FRelease, sr)
				}
			}

			ampVal := ampEnv.tick()
			var filtVal float64
			if filtEnv != nil {
				filtVal = filtEnv.tick()
			}
			if ampEnv.done() {
				break
			}

			var osc float64
			if table1 != nil {
				var sum float64
				for u := 0; u < unisonVoices; u++ {
					idx := int(unisonPhases[u]) & wavetableMask
					sum += table1[idx]
					unisonPhases[u] += unisonIncrs[u]
					if unisonPhases[u] >= wavetableSize {
						unisonPhases[u] -= wavetableSize
					}
				}
				osc = sum * unisonAmp
			} else {
				noiseState = noiseState*1664525 + 1013904223
				osc = float64(int32(noiseState)) / float64(1<<31)
			}

			if osc2Active {
				var v2 float64
				if table2 != nil {
					v2 = table2[int(osc2Phase)&wavetableMask]
				} else {
					noiseState = noiseState*1664525 + 1013904223
					v2 = float64(int32(noiseState)) / float64(1<<31)
				}
				osc = osc*osc1Mix + v2*opt.Osc2Mix
				osc2Phase += osc2Incr
				if osc2Phase >= wavetableSize {
					osc2Phase -= wavetableSize
				}
			}

			if opt.SubLevel > 0.001 {
				osc += subTable[int(subPhase)&wavetableMask] * opt.SubLevel
				subPhase += subIncr
				if subPhase >= wavetableSize {
					subPhase -= wavetableSize
				}
			}

			osc *= ampVal * velAmp

			var lfoVal float64
			if lfoActive {
				switch opt.LFOShape {
				case "sine":
					lfoVal = math.Sin(2 * math.Pi * lfoPhase)
				case "tri":
					lfoVal = 4*math.Abs(lfoPhase-0.5) - 1
				case "square":
					if lfoPhase < 0.5 {
						lfoVal = 1
					} else {
						lfoVal = -1
					}
				case "saw":
					lfoVal = 2*lfoPhase - 1
				}
				lfoVal *= opt.LFODepth
				if opt.LFOTarget == "amp" {
					osc *= 1 + lfoVal
					if osc < 0 {
						osc = 0
					}
				}
				lfoPhase += lfoIncr
				if lfoPhase >= 1 {
					lfoPhase -= 1
				}
			}

			if filterActive {
				if filtCountdown <= 0 {
					fc := opt.FilterCutoff
					if filtEnv != nil {
						fc *= math.Pow(2, opt.FilterEnvAmt*filtVal)
					}
					if opt.FilterKeyTrack > 0.001 {
						semitones := float64(n.Key - 60)
						fc *= math.Pow(2, semitones*opt.FilterKeyTrack/12.0)
					}
					if lfoActive && opt.LFOTarget == "filter" {
						fc *= math.Pow(2, lfoVal*3)
					}
					fc *= velFilt
					filt.set(opt.FilterType, fc, opt.FilterReso, sr)
					filtCountdown = 32
				}
				filtCountdown--
				osc = filt.process(osc)
			}
			
			buf[i] += osc
		}
	}

	// ── 1) Pattern notes ──
	for _, n := range pat.Notes {
		if smp, ok := chanSamples[n.ChannelIid]; ok {
			mixSample(buf, smp, n, spt, sr, opt, chanGains[n.ChannelIid])
			continue
		}
		renderVoice(n)
	}

	// ── 2) Extra channel clips (playlist audio / sampler) ──
	// Each clip plays its sample from cl.Position for at most cl.Length ticks.
	// That's the "sample length as the FLP data says" behaviour.
	for _, cl := range extraChannelClips {
		smp, ok := chanSamples[cl.ItemIndex]
		if !ok || len(smp) < 2 {
			continue
		}
		startSample := int(float64(cl.Position) * spt * sr)
		if startSample >= len(buf) {
			continue
		}
		clipSamples := int(float64(cl.Length) * spt * sr)
		if clipSamples <= 0 {
			continue
		}
		// Audio clips play at original pitch (Key=60), so the pitch ratio
		// comes solely from the user's global SamplePitch.
		pitchRatio := math.Pow(2.0, opt.SamplePitch/12.0)
		if pitchRatio <= 0 {
			continue
		}
		gain := opt.SampleVolume * chanGains[cl.ItemIndex] // clips are unity velocity
		mixSampleEx(buf, smp, startSample, clipSamples, pitchRatio, gain, opt, sr)
	}

	// ── 3) Master chain: drive → delay → reverb → gain ──
	if opt.Drive > 0.001 {
		d := 1.0 + opt.Drive*15
		for i, v := range buf {
			buf[i] = math.Tanh(v * d)
		}
	}
	if opt.DelayMix > 0.001 && opt.DelayTime > 0.001 {
		dl := newDelayLine(int(opt.DelayTime*sr) + 4)
		ds := opt.DelayTime * sr
		fb := opt.DelayFeed
		if fb > 0.9 {
			fb = 0.9
		}
		for i := range buf {
			buf[i] = dl.process(buf[i], ds, fb, opt.DelayMix)
		}
	}
	if opt.ReverbMix > 0.001 {
		rv := newReverb()
		for i := range buf {
			buf[i] = rv.process(buf[i], opt.ReverbSize, opt.ReverbMix)
		}
	}
	g := opt.Gain
	if g <= 0 {
		g = 1
	}
	for i, v := range buf {
		buf[i] = math.Tanh(v * g)
	}
	return buf
}

// renderPattern is the pattern-only entry point used by PlayPattern,
// previewBuf and any other caller that doesn't have channel clips to feed.
func renderPattern(pat flp.Pattern, ppq int, bpm float64, opt SynthOptions,
	channels []flp.Channel, flpPath string) []float64 {
	return renderPatternWithClips(pat, ppq, bpm, opt, channels, flpPath, nil)
}

// renderArrangement flattens an arrangement into a single buffer.
//
//   - Pattern clips are expanded into absolute-time notes (with looping when
//     the clip is longer than the source pattern), and rendered through the
//     normal pattern pipeline.
//   - Channel clips (audio / sampler placed directly on the playlist) are
//     collected separately and mixed in with their clip length as a hard cap,
//     so a 1-beat clip can't bleed its whole sample across 4 beats.
//   - Automation clips are ignored.
//
// All voices — pattern notes, pattern sampler notes and channel clips — go
// through the same master chain (drive, delay, reverb, gain) afterwards.
func renderArrangement(arr flp.Arrangement, patterns []flp.Pattern, ppq int,
	bpm float64, opt SynthOptions, channels []flp.Channel, flpPath string) []float64 {

	patByID := make(map[int]flp.Pattern, len(patterns))
	for _, p := range patterns {
		patByID[p.ID] = p
	}
	chByIid := make(map[int]flp.Channel, len(channels))
	for _, ch := range channels {
		chByIid[ch.Iid] = ch
	}

	// Rough capacity estimate.
	totalNotes := 0
	for _, cl := range arr.Clips {
		if cl.ItemIndex > 20480 {
			if pat, ok := patByID[cl.ItemIndex-20480]; ok {
				totalNotes += len(pat.Notes) * 2
			}
		}
	}
	if totalNotes < 64 {
		totalNotes = 64
	}

	synth := flp.Pattern{
		Notes: make([]flp.Note, 0, totalNotes),
	}
	channelClips := make([]flp.Clip, 0, len(arr.Clips))

	for _, cl := range arr.Clips {
		// ── Channel clip → collect for the clip-aware mixer ──
		if cl.ItemIndex <= 20480 {
			ch, ok := chByIid[cl.ItemIndex]
			if !ok || !isSampleChannel(ch) {
				continue // automation or instrument channel clip
			}
			if ch.SamplePath == nil || *ch.SamplePath == "" {
				continue
			}
			if resolveSamplePath(flpPath, *ch.SamplePath) == "" {
				continue
			}
			if cl.Length == 0 {
				continue
			}
			channelClips = append(channelClips, cl)
			continue
		}

		// ── Pattern clip → expand into absolute-time notes (with looping) ──
		patID := cl.ItemIndex - 20480
		pat, ok := patByID[patID]
		if !ok || len(pat.Notes) == 0 {
			continue
		}

patLen := uint32(0)
		if pat.Length != nil && *pat.Length > 0 {
			patLen = *pat.Length
		} else {
			for _, n := range pat.Notes {
				if e := n.Position + n.Length; e > patLen {
					patLen = e
				}
			}
		}
		if patLen == 0 {
			continue
		}

		numRepeats := (cl.Length + patLen - 1) / patLen
		if numRepeats == 0 {
			numRepeats = 1
		}
		clipEnd := cl.Position + cl.Length

		for rep := uint32(0); rep < numRepeats; rep++ {
			repOffset := rep * patLen
			for _, n := range pat.Notes {
				if n.Length == 0 {
					continue
				}
				absStart := cl.Position + repOffset + n.Position
				if absStart >= clipEnd {
					continue
				}
				absEnd := absStart + n.Length
				if absEnd > clipEnd {
					absEnd = clipEnd
				}
				if absEnd <= absStart {
					continue
				}
				nn := n
				nn.Position = absStart
				nn.Length = absEnd - absStart
				synth.Notes = append(synth.Notes, nn)
			}
		}
	}

	return renderPatternWithClips(synth, ppq, bpm, opt, channels, flpPath, channelClips)
}

// previewBuf renders a short two-note demo (C4, then G4) with the given
// settings, truncated to durationSec.
func previewBuf(cfg SynthOptions, durationSec float64) []float64 {
	pat := flp.Pattern{
		Notes: []flp.Note{
			{Position: 0, Length: 48, Key: 60, Velocity: 100, ChannelIid: 0},
			{Position: 48, Length: 96, Key: 67, Velocity: 110, ChannelIid: 0},
		},
	}
	buf := renderPattern(pat, 96, 120, cfg, nil, "")
	n := int(durationSec * float64(synthSR))
	if n > 0 && len(buf) > n {
		buf = buf[:n]
	}
	return buf
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ───────────── envelope ─────────────

type adsrPhase int

const (
	adsrAttack adsrPhase = iota
	adsrDecay
	adsrSustain
	adsrRelease
	adsrDone
)

// adsr is a per-sample linear envelope. Release time is set at release time
// (so a note released early still takes the same wall-clock time to fade).
type adsr struct {
	phase      adsrPhase
	value      float64
	attackInc  float64
	decayInc   float64
	sustain    float64
	releaseInc float64
}

func newADSR(a, d, s, r, sr float64) *adsr {
	if a < 0.0005 {
		a = 0.0005
	}
	if d < 0.0005 {
		d = 0.0005
	}
	if r < 0 {
		r = 0
	}
	return &adsr{
		phase:      adsrAttack,
		attackInc:  1.0 / (a * sr),
		decayInc:   (1.0 - s) / (d * sr),
		sustain:    s,
		releaseInc: 0, // set on release
	}
}

func (e *adsr) releaseFrom(seconds, sr float64) {
	if e.phase == adsrDone || e.phase == adsrRelease {
		return
	}
	if seconds <= 0 {
		e.value = 0
		e.phase = adsrDone
		return
	}
	e.releaseInc = e.value / (seconds * sr)
	e.phase = adsrRelease
}

func (e *adsr) tick() float64 {
	switch e.phase {
	case adsrAttack:
		e.value += e.attackInc
		if e.value >= 1 {
			e.value = 1
			e.phase = adsrDecay
		}
	case adsrDecay:
		e.value -= e.decayInc
		if e.value <= e.sustain {
			e.value = e.sustain
			e.phase = adsrSustain
		}
	case adsrRelease:
		e.value -= e.releaseInc
		if e.value <= 0 {
			e.value = 0
			e.phase = adsrDone
		}
	}
	return e.value
}


func (e *adsr) done() bool { return e.phase == adsrDone }

// ───────────── biquad filter (RBJ cookbook, TDF-II) ─────────────

type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func (f *biquad) reset() { f.x1, f.x2, f.y1, f.y2 = 0, 0, 0, 0 }

func (f *biquad) process(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// set computes coefficients. kind is "lp" | "hp" | "bp" | "notch".
func (f *biquad) set(kind string, fc, q, sr float64) {
	if fc < 20 {
		fc = 20
	}
	if fc > sr*0.48 {
		fc = sr * 0.48
	}
	if q < 0.05 {
		q = 0.05
	}
	w0 := 2 * math.Pi * fc / sr
	cosW := math.Cos(w0)
	sinW := math.Sin(w0)
	alpha := sinW / (2 * q)

	var b0, b1, b2, a0, a1, a2 float64
	switch kind {
	case "hp":
		b0 = (1 + cosW) / 2
		b1 = -(1 + cosW)
		b2 = (1 + cosW) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW
		a2 = 1 - alpha
	case "bp":
		b0 = alpha
		b1 = 0
		b2 = -alpha
		a0 = 1 + alpha
		a1 = -2 * cosW
		a2 = 1 - alpha
	case "notch":
		b0 = 1
		b1 = -2 * cosW
		b2 = 1
		a0 = 1 + alpha
		a1 = -2 * cosW
		a2 = 1 - alpha
	default: // lp
		b0 = (1 - cosW) / 2
		b1 = 1 - cosW
		b2 = (1 - cosW) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW
		a2 = 1 - alpha
	}
	inv := 1 / a0
	f.b0, f.b1, f.b2 = b0*inv, b1*inv, b2*inv
	f.a1, f.a2 = a1*inv, a2*inv
}

// ───────────── effects ─────────────

// delayLine is a single-tap feedback delay with dry/wet mix.
type delayLine struct {
	buf  []float64
	pos  int
	size int
}

func newDelayLine(n int) *delayLine {
	if n < 2 {
		n = 2
	}
	return &delayLine{buf: make([]float64, n), size: n}
}

func (d *delayLine) process(x, delaySamples, feedback, mix float64) float64 {
	ds := int(delaySamples)
	if ds < 1 {
		ds = 1
	}
	if ds >= d.size {
		ds = d.size - 1
	}
	readPos := d.pos - ds
	if readPos < 0 {
		readPos += d.size
	}
	delayed := d.buf[readPos]
	d.buf[d.pos] = x + delayed*feedback
	d.pos++
	if d.pos >= d.size {
		d.pos = 0
	}
	return x*(1-mix) + delayed*mix
}

// comb + allpass are the building blocks of a Schroeder / Freeverb reverb.
type comb struct {
	buf         []float64
	pos         int
	size        int
	filterStore float64
}

func newComb(n int) *comb { return &comb{buf: make([]float64, n), size: n} }

func (c *comb) process(x, feedback, damp float64) float64 {
	out := c.buf[c.pos]
	c.filterStore = out*(1-damp) + c.filterStore*damp
	c.buf[c.pos] = x + c.filterStore*feedback
	c.pos++
	if c.pos >= c.size {
		c.pos = 0
	}
	return out
}

type allpass struct {
	buf  []float64
	pos  int
	size int
}

func newAllpass(n int) *allpass { return &allpass{buf: make([]float64, n), size: n} }

func (a *allpass) process(x, feedback float64) float64 {
	bufout := a.buf[a.pos]
	out := -x + bufout
	a.buf[a.pos] = x + bufout*feedback
	a.pos++
	if a.pos >= a.size {
		a.pos = 0
	}
	return out
}

// reverb is a mono 8-comb, 4-allpass Freeverb (tunings for 44.1 kHz).
type reverb struct {
	combs     [8]*comb
	allpasses [4]*allpass
}

func newReverb() *reverb {
	return &reverb{
		combs: [8]*comb{
			newComb(1116), newComb(1188), newComb(1277), newComb(1356),
			newComb(1422), newComb(1491), newComb(1557), newComb(1617),
		},
		allpasses: [4]*allpass{
			newAllpass(556), newAllpass(441), newAllpass(341), newAllpass(225),
		},
	}
}

func (r *reverb) process(x, size, mix float64) float64 {
	if mix < 0.001 {
		return x
	}
	feedback := 0.70 + size*0.28
	damp := 0.20

	var wet float64
	for _, c := range r.combs {
		wet += c.process(x, feedback, damp)
	}
	wet *= 0.125
	for _, a := range r.allpasses {
		wet = a.process(wet, 0.5)
	}
	return x*(1-mix) + wet*mix
}

// ───────────── streamer ─────────────

// positionStreamer plays a mono buffer and reports its position in samples.
// It is safe to call Position() concurrently with Stream().
type positionStreamer struct {
	buf   []float64
	pos   int
	mu    sync.Mutex
	ended bool
	onEnd func()
}

func (s *positionStreamer) Stream(samples [][2]float64) (int, bool) {
	s.mu.Lock()
	n := 0
	for i := range samples {
		if s.pos >= len(s.buf) {
			break
		}
		v := s.buf[s.pos]
		samples[i][0] = v
		samples[i][1] = v
		s.pos++
		n++
	}
	done := s.pos >= len(s.buf)
	fireEnd := done && !s.ended
	if fireEnd {
		s.ended = true
	}
	cb := s.onEnd
	s.mu.Unlock()

	if fireEnd && cb != nil {
		go cb()
	}
	if n == 0 && done {
		return 0, false
	}
	return n, true
}

func (s *positionStreamer) Err() error { return nil }

func (s *positionStreamer) Position() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pos
}

// SeekTo moves the playback head to pos (clamped to [0, len(buf)]).
// Safe to call concurrently with Stream().
func (s *positionStreamer) SeekTo(pos int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pos < 0 {
		pos = 0
	}
	if pos > len(s.buf) {
		pos = len(s.buf)
	}
	s.pos = pos
	s.ended = false
}

// ───────────── player ─────────────

type MIDIPlayer struct {
	mu         sync.Mutex
	playMu     sync.Mutex
	stream     *positionStreamer
	playing    bool
	reachedEnd bool
	ppq        int
	bpm        float64
}

func NewMIDIPlayer() *MIDIPlayer { return &MIDIPlayer{} }

// PlayPattern renders pat and starts playing it through the speaker.
// Convenience wrapper around PlayBuffer for one-shot pattern playback.
func (p *MIDIPlayer) PlayPattern(pat flp.Pattern, ppq int, bpm float64,
	opt SynthOptions, startTick uint32, channels []flp.Channel, flpPath string) {
	buf := renderPattern(pat, ppq, bpm, opt, channels, flpPath)
	p.PlayBuffer(buf, ppq, bpm, startTick)
}

// PlayBuffer starts playing a pre-rendered PCM buffer. This is the fast path
// for arrangement playback where the buffer is cached across plays.
func (p *MIDIPlayer) PlayBuffer(buf []float64, ppq int, bpm float64, startTick uint32) {
	p.playMu.Lock()
	defer p.playMu.Unlock()

	ensureSpeakerInit()

	stream := &positionStreamer{buf: buf}

	if bpm > 0 && ppq > 0 {
		tps := bpm / 60.0 * float64(ppq)
		if tps > 0 {
			pos := int(float64(startTick) / tps * float64(synthSR))
			if pos < 0 || pos > len(buf) {
				pos = 0
			}
			stream.pos = pos
		}
	}

	stream.onEnd = func() {
		p.mu.Lock()
		if p.stream == stream {
			p.playing = false
			p.stream = nil
			p.reachedEnd = true
		}
		p.mu.Unlock()
	}

	p.mu.Lock()
	p.stream = stream
	p.playing = true
	p.reachedEnd = false
	p.ppq = ppq
	p.bpm = bpm
	p.mu.Unlock()

	swapSpeakerStream(stream)
}

func (p *MIDIPlayer) Stop() {
	p.mu.Lock()
	playing := p.playing
	p.playing = false
	p.stream = nil
	p.mu.Unlock()
	if playing {
		globalSpeakerMu.Lock()
		speaker.Clear()
		globalSpeakerMu.Unlock()
	}
}

func (p *MIDIPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// TickPosition returns the current playback position in pattern ticks.
func (p *MIDIPlayer) TickPosition() uint32 {
	p.mu.Lock()
	s := p.stream
	ppq := p.ppq
	bpm := p.bpm
	p.mu.Unlock()
	if s == nil {
		return 0
	}
	tps := bpm / 60.0 * float64(ppq)
	if tps <= 0 {
		return 0
	}
	return uint32(float64(s.Position()) / float64(synthSR) * tps)
}

// SeekToTick moves the playback head to the given tick.
func (p *MIDIPlayer) SeekToTick(tick uint32) {
	p.mu.Lock()
	s := p.stream
	ppq := p.ppq
	bpm := p.bpm
	p.mu.Unlock()
	if s == nil {
		return
	}
	tps := bpm / 60.0 * float64(ppq)
	if tps <= 0 {
		return
	}
	sample := int(float64(tick) / tps * float64(synthSR))
	s.SeekTo(sample)
}

// ReachedEnd reports whether the last playback stopped by itself.
func (p *MIDIPlayer) ReachedEnd() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reachedEnd
}

// ───────────────────────── constants & state ─────────────────────────

const (
	defaultW = 700
	defaultH = 540
	version  = "v0.0.2"

	vizHeaderH    = 44
	scrollbarSize = 12
	rulerH        = 24
	trackHdrW     = 160
	pianoKeysW    = 60

	basePxPerTick = 0.15
	baseTrackH    = 24
	baseKeyH      = 12

	margin      = 20
	rowGap      = 10
	editH       = 28
	labelH      = 22
	btnH        = 30
	closeBtnW   = 100
	fileLabelW  = 80
	fileBtnW    = 120
	rowInnerGap = 8
	sidebarW    = 220
)

// Clean Light Palette
var (
	colBG       = wui.RGB(248, 249, 251)
	colSidebar  = wui.RGB(238, 240, 244)
	colHeader   = wui.RGB(226, 229, 235)
	colHeaderFG = wui.RGB(28, 32, 40)
	colHeaderDi = wui.RGB(108, 116, 128)
	colCanvasBG = wui.RGB(252, 253, 255)
	colTrackBG  = wui.RGB(255, 255, 255)
	colTrackAlt = wui.RGB(246, 248, 251)
	colClipBG   = wui.RGB(90, 140, 210)
	colClipEdge = wui.RGB(60, 100, 160)
	colText     = wui.RGB(40, 44, 52)
	colTextDim  = wui.RGB(120, 128, 140)
	colTrack    = wui.RGB(232, 235, 240)
	colThumb    = wui.RGB(160, 168, 180)
	colThumbHi  = wui.RGB(130, 138, 150)
	colGrid     = wui.RGB(216, 220, 226)
	colGridBeat = wui.RGB(233, 236, 241)
	colSep      = wui.RGB(210, 215, 222)
)

var (
	fontNormal *wui.Font
	fontBold   *wui.Font
	fontTitle  *wui.Font
	fontLarge  *wui.Font
)

func makeFont(name string, px int, bold bool) *wui.Font {
	f, err := wui.NewFont(wui.FontDesc{Name: name, Height: -px, Bold: bold})
	if err != nil {
		return nil
	}
	return f
}

func init() {
	fontNormal = makeFont("Segoe UI", 13, false)
	fontBold = makeFont("Segoe UI", 13, true)
	fontTitle = makeFont("Segoe UI", 16, true)
	fontLarge = makeFont("Segoe UI", 24, true)
}

// AppState manages the globally loaded project to avoid redundant file prompts.
type AppState struct {
	Project  *flp.FLPProject
	Original *flp.FLPProject
	Path     string
	SaveAs   string
	OnUpdate func()
}

var app AppState

var globalSynth = NewSynthConfig()

// ───────────── persistent app config ─────────────

// PresetEntry is one named SynthOptions preset in the preset store.
type PresetEntry struct {
	Name    string       `json:"name"`
	Options SynthOptions `json:"options"`
}

// AppConfig is stored as <exe-basename>.json next to the executable.
// Version tracks schema changes so future migrations can be applied.
type AppConfig struct {
	Version          int               `json:"version"`
	FLStudioUserData string            `json:"fl_studio_user_data"`
	Synth            *SynthOptions     `json:"synth,omitempty"`
	Presets          []PresetEntry     `json:"presets,omitempty"`
	// PatternPresets maps a pattern ID (decimal string) to a preset name.
	// Absence of a key means "use the current live synth options".
	PatternPresets map[string]string `json:"pattern_presets,omitempty"`
}

const appConfigVersion = 4

var appConfig AppConfig

// configFilePath returns <exe-dir>/<exe-basename>.json
func configFilePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "flp-tool.json"
	}
	base := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	return filepath.Join(filepath.Dir(exe), base+".json")
}

// loadAppConfig reads the config file, or creates one with sensible defaults,
// then applies the persisted SynthOptions to globalSynth.
func loadAppConfig() {
	// Defaults first.
	if home, herr := os.UserHomeDir(); herr == nil {
		appConfig.FLStudioUserData = filepath.Join(home,
			"Documents", "Image-Line", "FL Studio")
	}
	def := defaultSynthOptions()
	appConfig.Synth = &def
	appConfig.Version = appConfigVersion

	p := configFilePath()
	data, err := os.ReadFile(p)
	if err != nil {
		// First run — write the default config.
		saveAppConfig()
	} else {
		_ = json.Unmarshal(data, &appConfig)
		// Guard against a nil Synth in a malformed file.
		if appConfig.Synth == nil {
			d := defaultSynthOptions()
			appConfig.Synth = &d
		}
		appConfig.Version = appConfigVersion
	}
	// Ensure maps/slices we mutate later are non-nil.
	if appConfig.PatternPresets == nil {
		appConfig.PatternPresets = map[string]string{}
	}

	// Apply persisted synth parameters to the live config.
	restored := *appConfig.Synth
	globalSynth.Update(func(o *SynthOptions) { *o = restored })
}

// saveAppConfig writes the config file. Errors are silently ignored —
// config is a convenience, not critical.
func saveAppConfig() {
	appConfig.Version = appConfigVersion
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(configFilePath(), data, 0o644)
}

// persistSynth snapshots the live synth parameters into the config and
// writes the file. Called when the Synth Settings modal closes.
func persistSynth() {
	snapshot := globalSynth.Snapshot()
	appConfig.Synth = &snapshot
	saveAppConfig()
}

// ───────────── preset store helpers ─────────────

// presetNames returns the ordered list of preset names (may be empty).
func presetNames() []string {
	out := make([]string, len(appConfig.Presets))
	for i, p := range appConfig.Presets {
		out[i] = p.Name
	}
	return out
}

// findPresetIndex returns the index of the named preset, or -1.
func findPresetIndex(name string) int {
	for i, p := range appConfig.Presets {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// upsertPreset adds a new preset or overwrites the existing one with that name.
func upsertPreset(name string, opts SynthOptions) {
	i := findPresetIndex(name)
	if i >= 0 {
		appConfig.Presets[i].Options = opts
	} else {
		appConfig.Presets = append(appConfig.Presets, PresetEntry{Name: name, Options: opts})
	}
	saveAppConfig()
}

// deletePreset removes a preset and clears any pattern assignments to it.
func deletePreset(name string) {
	i := findPresetIndex(name)
	if i < 0 {
		return
	}
	appConfig.Presets = append(appConfig.Presets[:i], appConfig.Presets[i+1:]...)
	for k, v := range appConfig.PatternPresets {
		if v == name {
			delete(appConfig.PatternPresets, k)
		}
	}
	saveAppConfig()
}

// renamePreset renames an existing preset and updates pattern assignments.
// Returns false on invalid input or name collision.
func renamePreset(oldName, newName string) bool {
	if newName == "" || oldName == newName {
		return false
	}
	i := findPresetIndex(oldName)
	if i < 0 {
		return false
	}
	if findPresetIndex(newName) >= 0 {
		return false
	}
	appConfig.Presets[i].Name = newName
	for k, v := range appConfig.PatternPresets {
		if v == oldName {
			appConfig.PatternPresets[k] = newName
		}
	}
	saveAppConfig()
	return true
}

// getPatternPresetOptions returns the preset assigned to the given pattern ID,
// and true if one exists and resolves to a valid preset.
func getPatternPresetOptions(patternID int) (SynthOptions, bool) {
	if appConfig.PatternPresets == nil {
		return SynthOptions{}, false
	}
	name, ok := appConfig.PatternPresets[strconv.Itoa(patternID)]
	if !ok || name == "" {
		return SynthOptions{}, false
	}
	i := findPresetIndex(name)
	if i < 0 {
		return SynthOptions{}, false
	}
	return appConfig.Presets[i].Options, true
}

// setPatternPreset assigns (or clears, when name == "") the preset for a pattern.
func setPatternPreset(patternID int, name string) {
	if appConfig.PatternPresets == nil {
		appConfig.PatternPresets = map[string]string{}
	}
	key := strconv.Itoa(patternID)
	if name == "" {
		delete(appConfig.PatternPresets, key)
	} else {
		appConfig.PatternPresets[key] = name
	}
	saveAppConfig()
}

// getPatternPresetName returns the preset name assigned to a pattern, or "".
func getPatternPresetName(patternID int) string {
	if appConfig.PatternPresets == nil {
		return ""
	}
	return appConfig.PatternPresets[strconv.Itoa(patternID)]
}

// ───────────────────────── text & ui helpers ─────────────────────────

func setText(t *wui.TextEdit, text string) {
	t.SetText(strings.ReplaceAll(text, "\n", "\r\n"))
}

func parseFloat(s string) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return v
	}
	return 0
}

func parseInt(s string) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return 0
}

func fmtFloatShort(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func newLabel(text string, x, y, w, h int) *wui.Label {
	l := wui.NewLabel()
	l.SetText(text)
	l.SetBounds(x, y, w, h)
	if fontNormal != nil {
		l.SetFont(fontNormal)
	}
	return l
}

func newBtn(text string, x, y, w, h int, onClick func()) *wui.Button {
	b := wui.NewButton()
	b.SetText(text)
	b.SetBounds(x, y, w, h)
	if onClick != nil {
		b.SetOnClick(onClick)
	}
	if fontNormal != nil {
		b.SetFont(fontNormal)
	}
	return b
}

func newEdit(x, y, w, h int) *wui.EditLine {
	e := wui.NewEditLine()
	e.SetBounds(x, y, w, h)
	if fontNormal != nil {
		e.SetFont(fontNormal)
	}
	return e
}

func newCombo(items []string, x, y, w, h int) *wui.ComboBox {
	c := wui.NewComboBox()
	c.SetItems(items)
	c.SetSelectedIndex(0)
	c.SetBounds(x, y, w, h)
	if fontNormal != nil {
		c.SetFont(fontNormal)
	}
	return c
}

func newOutput(x, y, w, h int) *wui.TextEdit {
	t := wui.NewTextEdit()
	t.SetBounds(x, y, w, h)
	t.SetReadOnly(true)
	t.SetWordWrap(false)
	if fontNormal != nil {
		t.SetFont(fontNormal)
	}
	return t
}

func applyLayout(w *wui.Window, fn func(iw, ih int)) {
	doLayout := func() {
		iw, ih := w.InnerWidth(), w.InnerHeight()
		if iw <= 0 {
			iw = defaultW
		}
		if ih <= 0 {
			ih = defaultH
		}
		fn(iw, ih)
	}
	w.SetOnResize(doLayout)
	w.SetOnShow(doLayout)
	doLayout()
}

func browseFLP(parent *wui.Window, title string) string {
	dlg := wui.NewFileOpenDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("FL Studio project (*.flp, *.fst)", "flp", "fst")
	dlg.AddFilter("All files", "*.*")
	if ok, path := dlg.ExecuteSingleSelection(parent); ok {
		return path
	}
	return ""
}

func browseSave(parent *wui.Window, title string) string {
	dlg := wui.NewFileSaveDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("FL Studio project (*.flp)", "flp")
	if ok, path := dlg.Execute(parent); ok {
		return path
	}
	return ""
}

func browseSaveMIDI(parent *wui.Window, title string) string {
	dlg := wui.NewFileSaveDialog()
	dlg.SetTitle(title)
	dlg.AddFilter("MIDI file (*.mid)", "mid")
	if ok, path := dlg.Execute(parent); ok {
		return path
	}
	return ""
}

func exportPatternMIDI(p *flp.FLPProject, patternIdx int, path string) error {
	if p == nil {
		return fmt.Errorf("no project loaded")
	}
	if patternIdx < 0 || patternIdx >= len(p.Patterns) {
		return fmt.Errorf("pattern index out of range")
	}
	pat := p.Patterns[patternIdx]

	ppq := uint16(96)
	if p.Header.PPQ > 0 {
		ppq = uint16(p.Header.PPQ)
	}
	clock := smf.MetricTicks(ppq)

	channelMap := make(map[int]uint8)
	nextCh := uint8(0)
	for _, n := range pat.Notes {
		if _, ok := channelMap[n.ChannelIid]; !ok {
			channelMap[n.ChannelIid] = nextCh
			nextCh = (nextCh + 1) % 16
		}
	}

	type midiEvent struct {
		tick int64
		msg  midi.Message
	}
	events := make([]midiEvent, 0, len(pat.Notes)*2)

	for _, n := range pat.Notes {
		ch := channelMap[n.ChannelIid]
		vel := uint8(n.Velocity)
		if vel == 0 {
			vel = 100
		}
		events = append(events, midiEvent{tick: int64(n.Position), msg: midi.NoteOn(ch, uint8(n.Key), vel)})
		events = append(events, midiEvent{tick: int64(n.Position + n.Length), msg: midi.NoteOff(ch, uint8(n.Key))})
	}

	sort.Slice(events, func(i, j int) bool { return events[i].tick < events[j].tick })

	var tr smf.Track
	var lastTick int64
	for _, ev := range events {
		delta := ev.tick - lastTick
		if delta < 0 {
			delta = 0
		}
		tr.Add(uint32(delta), ev.msg)
		lastTick = ev.tick
	}
	tr.Close(0)

	s := smf.New()
	s.TimeFormat = clock
	s.Add(tr)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = s.WriteTo(f)
	return err
}

func loadProject(path string) (*flp.FLPProject, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return flp.ParseFLPFile(buf)
}

func formatErr(err error) string {
	if pe, ok := err.(*flp.FLPParseError); ok {
		return "Parse error:\n" + pe.Error()
	}
	return "Error: " + err.Error()
}

// ───────────────────────── timestamps ─────────────────────────

// timestampRows returns [label, value] pairs for every timestamp we can
// recover: FL Studio's embedded creation time and time-spent counter, plus
// the on-disk modification time and file size.
func timestampRows(p *flp.FLPProject, path string) [][2]string {
	rows := [][2]string{}

	if p != nil && p.Metadata.CreatedOn != nil {
		rows = append(rows, [2]string{
			"Created (in FL Studio)",
			p.Metadata.CreatedOn.Local().Format("2006-01-02 15:04:05"),
		})
	}
	if p != nil && p.Metadata.TimeSpentSeconds != nil {
		d := time.Duration(*p.Metadata.TimeSpentSeconds * float64(time.Second))
		rows = append(rows, [2]string{
			"Time spent (FL Studio)",
			formatDuration(d),
		})
	}
	if path != "" {
		if st, err := os.Stat(path); err == nil {
			rows = append(rows, [2]string{
				"File modified",
				st.ModTime().Local().Format("2006-01-02 15:04:05"),
			})
			rows = append(rows, [2]string{
				"File size",
				formatBytes(st.Size()),
			})
		} else {
			rows = append(rows, [2]string{"File", "stat failed: " + err.Error()})
		}
	}
	return rows
}

// timestampsText renders the timestamp rows as a plain multi-line string,
// suitable for a text box.
func timestampsText(p *flp.FLPProject, path string) string {
	rows := timestampRows(p, path)
	if len(rows) == 0 {
		return "(no timestamps available)"
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%-26s %s\n", r[0]+":", r[1])
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func marshalIndentedJSON(v interface{}) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

const maxOutBytes = 2 * 1024 * 1024

func trimForUI(s string) string {
	if len(s) <= maxOutBytes {
		return s
	}
	return s[:maxOutBytes] + "\n\n[... output truncated ...]"
}

// ───────────────────────── main window ─────────────────────────

func main() {
	loadAppConfig()
	
	// CLI: flp-gui <path.flp>  → preload that project.
	if len(os.Args) > 1 {
		path := os.Args[1]
		if path == "-h" || path == "--help" {
			fmt.Fprintf(os.Stderr, "Usage: %s [path/to/project.flp]\n", filepath.Base(os.Args[0]))
			os.Exit(0)
		}
		p, err := loadProject(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "flp-gui: failed to load %s: %v\n", path, err)
			// Fall through and show the window anyway so the user can pick a
			// different file via the GUI.
		} else {
			app.Project = p
			app.Original = p
			app.Path = path
		}
	}
	newMainWindow().Show()
}

func newMainWindow() *wui.Window {
	w := wui.NewWindow()
	w.SetTitle("FLP (FL Studio Project) Tool")
	w.SetSize(defaultW, defaultH)
	w.SetCenterOnShow(true)
	w.SetResizable(true)
	w.SetBackground(colBG)
	w.SetHasMinButton(true)
	w.SetHasMaxButton(true)

	// Sidebar paint box
	sbPaint := wui.NewPaintBox()
	w.Add(sbPaint)

	title := wui.NewLabel()
	title.SetText("FLP TOOL")
	if fontTitle != nil {
		title.SetFont(fontTitle)
	}
	w.Add(title)

	verLabel := newLabel(version, 0, 0, 100, labelH)
	w.Add(verLabel)

	// Dashboard components
	dashPaint := wui.NewPaintBox()
	w.Add(dashPaint)

	btnLoad := newBtn("Load Project...", 0, 0, 140, 40, nil)
	w.Add(btnLoad)

	tools := []struct {
		label string
		open  func(*wui.Window)
	}{
		{"Visualizer", openVisualizerTool},
		{"Edit Project", openEditTool},
		{"Diff Two Projects", openDiffTool},
		{"Inspect JSON/Canon", openInspectTool},
		{"Channel Browser", openChannelTool},
		{"Pattern Browser", openPatternTool},
		{"Mixer Browser", openMixerTool},
		{"Asset Viewer", openAssetTool},
		{"Synthesizer", openSynthSettingsTool},
		{"Git Integration", openGitTool},
		{"About", openAboutTool},
	}

	btns := make([]*wui.Button, len(tools))
	for i, t := range tools {
		openFn := t.open
		b := newBtn(t.label, 0, 0, 100, 36, func() { openFn(w) })
		w.Add(b)
		btns[i] = b
	}

	app.OnUpdate = func() {
		dashPaint.Paint()
		for _, b := range btns {
			// Require project for most tools, except the project-independent ones.
			switch b.Text() {
			case "Diff Two Projects", "About", "Git Integration", "Synthesizer":
				b.SetEnabled(true)
			default:
				b.SetEnabled(app.Project != nil)
			}
		}
	}

	btnLoad.SetOnClick(func() {
		if path := browseFLP(w, "Select FL Studio project"); path != "" {
			p, err := loadProject(path)
			if err != nil {
				wui.MessageBoxError("Load Error", err.Error())
				return
			}
			app.Project = p
			app.Original = p
			app.Path = path
			app.SaveAs = ""
			app.OnUpdate()
		}
	})

	sbPaint.SetOnPaint(func(c *wui.Canvas) {
		iw, ih := c.Size()
		c.FillRect(0, 0, iw, ih, colSidebar)
		c.Line(iw-1, 0, iw-1, ih, colSep)
	})

	dashPaint.SetOnPaint(func(c *wui.Canvas) {
		iw, ih := c.Size()
		c.FillRect(0, 0, iw, ih, colBG)

		if app.Project == nil {
			c.TextOut(40, ih/2-20, "No Project Loaded.", colTextDim)
			return
		}

		c.TextOut(40, 40, filepath.Base(app.Path), colHeaderFG)

		tempo := 120.0
		if t := flp.GetTempo(app.Project); t != nil {
			tempo = *t
		}

		stats := []string{
			fmt.Sprintf("Tempo: %.2f BPM", tempo),
			fmt.Sprintf("PPQ: %d", app.Project.Header.PPQ),
			fmt.Sprintf("Channels: %d", len(app.Project.Channels)),
			fmt.Sprintf("Patterns: %d", len(app.Project.Patterns)),
			fmt.Sprintf("Mixer Inserts: %d", len(app.Project.Inserts)),
			fmt.Sprintf("Arrangements: %d", len(app.Project.Arrangements)),
		}

		y := 90
		for _, stat := range stats {
			c.TextOut(40, y, stat, colText)
			y += 26
		}

		y += 10
		c.TextOut(40, y, "Timestamps", colTextDim)
		y += 24

		for _, row := range timestampRows(app.Project, app.Path) {
			c.TextOut(40, y, row[0]+":", colTextDim)
			c.TextOut(240, y, row[1], colText)
			y += 22
		}
	})

	applyLayout(w, func(iw, ih int) {
		sbPaint.SetBounds(0, 0, sidebarW, ih)
		title.SetBounds(margin, 20, sidebarW-2*margin, 30)
		verLabel.SetBounds(margin, ih-30, sidebarW-2*margin, labelH)

		dashPaint.SetBounds(sidebarW, 0, iw-sidebarW, ih)

		btnLoad.SetBounds(sidebarW+40, ih-80, 160, 40)

		y := 56
		for _, b := range btns {
			b.SetBounds(margin, y, sidebarW-2*margin, 32)
			y += 36
		}
	})

	app.OnUpdate()
	return w
}

func newModal(title string) *wui.Window {
	w := wui.NewWindow()
	w.SetTitle(title)
	w.SetSize(defaultW, defaultH)
	w.SetCenterOnShow(true)
	w.SetResizable(true)
	w.SetBackground(colBG)
	w.SetHasMinButton(true)
	w.SetHasMaxButton(true)
	return w
}

func showModal(w *wui.Window) {
	if err := w.ShowModal(); err != nil {
		fmt.Fprintln(os.Stderr, "flp-gui:", err)
	}
}

// ───────────────────────── 1. Diff ─────────────────────────

func openDiffTool(_ *wui.Window) {
	w := newModal("Diff Two Projects")

	lblA := newLabel("File A (Current):", 0, 0, 120, labelH)
	w.Add(lblA)
	pathA := "No project loaded"
	if app.Path != "" {
		pathA = app.Path
	}
	edA := newEdit(0, 0, 100, editH)
	edA.SetText(pathA)
	edA.SetReadOnly(true)
	w.Add(edA)

	lblB := newLabel("File B:", 0, 0, 120, labelH)
	w.Add(lblB)
	edB := newEdit(0, 0, 100, editH)
	w.Add(edB)
	btnBrowse := newBtn("Browse...", 0, 0, 90, editH, nil)
	w.Add(btnBrowse)

	btnDiff := newBtn("Diff", 0, 0, 100, btnH, nil)
	w.Add(btnDiff)
	btnVerbose := newBtn("Verbose", 0, 0, 100, btnH, nil)
	w.Add(btnVerbose)

	out := newOutput(0, 0, 100, 100)
	setText(out, "Select File B to compare against the currently loaded project.")
	w.Add(out)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		y := margin
		lblA.SetBounds(margin, y+3, 120, labelH)
		edA.SetBounds(margin+120, y, iw-120-2*margin, editH)
		y += editH + rowGap

		lblB.SetBounds(margin, y+3, 120, labelH)
		edW := iw - 120 - 90 - 2*margin - rowInnerGap
		if edW < 100 {
			edW = 100
		}
		edB.SetBounds(margin+120, y, edW, editH)
		btnBrowse.SetBounds(margin+120+edW+rowInnerGap, y, 90, editH)
		y += editH + rowGap + 6

		btnDiff.SetBounds(margin, y, 100, btnH)
		btnVerbose.SetBounds(margin+108, y, 100, btnH)
		y += btnH + rowGap + 6

		bottomH := btnH + margin
		outH := ih - y - bottomH
		if outH < 80 {
			outH = 80
		}
		out.SetBounds(margin, y, iw-2*margin, outH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	btnBrowse.SetOnClick(func() {
		if p := browseFLP(w, "Select Project B"); p != "" {
			edB.SetText(p)
		}
	})

	run := func(verbose bool) {
		if app.Project == nil {
			setText(out, "Error: File A (Main Project) is not loaded.")
			return
		}
		b := edB.Text()
		if b == "" {
			setText(out, "Error: File B must be selected.")
			return
		}
		pb, err := loadProject(b)
		if err != nil {
			setText(out, formatErr(err))
			return
		}
		res := flp.CompareProjects(app.Project, pb)
		t := fmt.Sprintf("%s vs %s", filepath.Base(app.Path), filepath.Base(b))
		body := flp.RenderSummary(res, flp.RenderSummaryOptions{Title: t, Verbose: verbose})
		if !flp.DiffSummaryHasChanges(res.Summary) {
			body += "\n\nNo differences. The two projects are identical."
		}
		setText(out, trimForUI(body))
	}
	btnDiff.SetOnClick(func() { run(false) })
	btnVerbose.SetOnClick(func() { run(true) })

	showModal(w)
}

// ───────────────────────── 2. Edit & Save ─────────────────────────

type propField struct {
	label *wui.Label
	edit  *wui.EditLine
}

type categoryDef struct {
	label string
	list  func(*flp.FLPProject) []string
	read  func(*flp.FLPProject, int) []string
	props []string
	apply func(*flp.FLPProject, int, []string) (*flp.FLPProject, error)
	msg   func(int, []string) string
}

func colorStrings(c *flp.RGBA) []string {
	if c == nil {
		return []string{"", "", "", ""}
	}
	return []string{strconv.Itoa(c.R), strconv.Itoa(c.G), strconv.Itoa(c.B), strconv.Itoa(c.A)}
}

func parseColorStrings(vals []string) flp.MutRGBA {
	get := func(i int) float64 {
		if i < 0 || i >= len(vals) {
			return 0
		}
		return parseFloat(vals[i])
	}
	return flp.MutRGBA{R: get(0), G: get(1), B: get(2), A: get(3)}
}

// clampF restricts v to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// allEmpty reports whether every string is whitespace-only.
func allEmpty(vals []string) bool {
	for _, s := range vals {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// validIntString returns the decimal representation of v if v is within
// [lo, hi], or "" if it's outside the range the corresponding setter accepts.
func validIntString(v, lo, hi int) string {
	if v < lo || v > hi {
		return ""
	}
	return strconv.Itoa(v)
}

func arrTrackAt(p *flp.FLPProject, i int) (int, int, bool) {
	n := 0
	for ai, a := range p.Arrangements {
		for ti := range a.Tracks {
			if n == i {
				return ai, ti, true
			}
			n++
		}
	}
	return 0, 0, false
}

var editCategories = []categoryDef{
	{
		label: "Tempo",
		list:  func(*flp.FLPProject) []string { return []string{"(project tempo)"} },
		read: func(p *flp.FLPProject, _ int) []string {
			t := flp.GetTempo(p)
			if t == nil {
				return []string{"120"}
			}
			return []string{fmtFloatShort(*t)}
		},
		props: []string{"BPM"},
		apply: func(p *flp.FLPProject, _ int, v []string) (*flp.FLPProject, error) {
			return flp.SetTempo(p, parseFloat(v[0]))
		},
		msg: func(_ int, v []string) string { return "Tempo → " + v[0] + " BPM" },
	},
	{
		label: "Time signature",
		list:  func(*flp.FLPProject) []string { return []string{"(project signature)"} },
		read: func(p *flp.FLPProject, _ int) []string {
			num, den := "4", "4"
			if p.Metadata.TimeSignatureNumerator != nil {
				num = strconv.Itoa(*p.Metadata.TimeSignatureNumerator)
			}
			if p.Metadata.TimeSignatureDenominator != nil {
				den = strconv.Itoa(*p.Metadata.TimeSignatureDenominator)
			}
			return []string{num, den}
		},
		props: []string{"Numerator", "Denominator"},
		apply: func(p *flp.FLPProject, _ int, v []string) (*flp.FLPProject, error) {
			return flp.SetTimeSignature(p, parseInt(v[0]), parseInt(v[1]))
		},
		msg: func(_ int, v []string) string {
			return "Time signature → " + v[0] + "/" + v[1]
		},
	},
	{
		label: "Channels",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Channels))
			for i, c := range p.Channels {
				n := fmt.Sprintf("#%d", c.Iid)
				if c.Name != nil && *c.Name != "" {
					n = *c.Name
				}
				out[i] = fmt.Sprintf("%d   %s   [%s]", c.Iid, n, c.Kind)
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Channels) {
				return make([]string, 8)
			}
			c := p.Channels[i]

			name := ""
			if c.Name != nil {
				name = *c.Name
			}

			// Volume/Pan: leave blank if the raw FL value is outside the
			// range the setters accept, so we don't push garbage into them.
			vol, pan := "", ""
			if c.Levels != nil {
				rawVol := float64(c.Levels.Volume)
				rawPan := float64(c.Levels.Pan)
				if rawVol >= 0 && rawVol <= 12800 {
					vol = fmtFloatShort(rawVol / 12800.0)
				}
				if rawPan >= -6400 && rawPan <= 6400 {
					pan = fmtFloatShort(rawPan / 6400.0)
				}
			}

			// Routing: only emit if it's in the setter's accepted range.
			ti := ""
			if c.TargetInsert != nil {
				ti = validIntString(*c.TargetInsert, -1, 127)
			}

			col := colorStrings(c.Color)
			return []string{name, vol, pan, ti, col[0], col[1], col[2], col[3]}
		},
		props: []string{
			"Name", "Volume (0..1)", "Pan (-1..+1)", "Target insert",
			"Color.R", "Color.G", "Color.B", "Color.A",
		},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Channels) {
				return nil, fmt.Errorf("channel index out of range")
			}
			iid := p.Channels[i].Iid
			cur := p
			var err error

			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetChannelName(cur, iid, v[0]); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[1]) != "" {
				if cur, err = flp.SetChannelVolume(cur, iid, parseFloat(v[1])); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[2]) != "" {
				if cur, err = flp.SetChannelPan(cur, iid, parseFloat(v[2])); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[3]) != "" {
				if cur, err = flp.SetChannelRouting(cur, iid, parseInt(v[3])); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[4:]) {
				if cur, err = flp.SetChannelColor(cur, iid, parseColorStrings(v[4:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Channel index %d updated", i)
		},
	},
	{
		label: "Patterns",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Patterns))
			for i, pt := range p.Patterns {
				n := fmt.Sprintf("#%d", pt.ID)
				if pt.Name != nil && *pt.Name != "" {
					n = *pt.Name
				}
				out[i] = fmt.Sprintf("%d   %s   (%d notes)", pt.ID, n, len(pt.Notes))
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Patterns) {
				return make([]string, 6)
			}
			pt := p.Patterns[i]
			name := ""
			if pt.Name != nil {
				name = *pt.Name
			}
			length := ""
			if pt.Length != nil {
				length = strconv.Itoa(int(*pt.Length))
			}
			col := colorStrings(pt.Color)
			return []string{name, length, col[0], col[1], col[2], col[3]}
		},
		props: []string{"Name", "Length (ticks)", "Color.R", "Color.G", "Color.B", "Color.A"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Patterns) {
				return nil, fmt.Errorf("pattern index out of range")
			}
			pid := p.Patterns[i].ID
			cur := p
			var err error
			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetPatternName(cur, pid, v[0]); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[1]) != "" {
				if cur, err = flp.SetPatternLength(cur, pid, int64(parseInt(v[1]))); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[2:]) {
				if cur, err = flp.SetPatternColor(cur, pid, parseColorStrings(v[2:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Pattern index %d updated", i)
		},
	},
	{
		label: "Mixer inserts",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Inserts))
			for i, ins := range p.Inserts {
				n := "(unnamed)"
				if ins.Name != nil && *ins.Name != "" {
					n = *ins.Name
				}
				out[i] = fmt.Sprintf("%d   %s   (%d slots)", ins.Index, n, len(ins.Slots))
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Inserts) {
				return make([]string, 5)
			}
			ins := p.Inserts[i]
			name := ""
			if ins.Name != nil {
				name = *ins.Name
			}
			col := colorStrings(ins.Color)
			return []string{name, col[0], col[1], col[2], col[3]}
		},
		props: []string{"Name", "Color.R", "Color.G", "Color.B", "Color.A"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Inserts) {
				return nil, fmt.Errorf("insert index out of range")
			}
			idx := p.Inserts[i].Index
			cur := p
			var err error

			// Only write the name if the user actually typed something, so an
			// empty field doesn't insert a spurious empty-name event.
			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetInsertName(cur, idx, v[0]); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[1:]) {
				if cur, err = flp.SetInsertColor(cur, idx, parseColorStrings(v[1:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Insert index %d updated", i)
		},
	},
	{
		label: "Arrangements",
		list: func(p *flp.FLPProject) []string {
			out := make([]string, len(p.Arrangements))
			for i, a := range p.Arrangements {
				n := fmt.Sprintf("#%d", a.ID)
				if a.Name != nil && *a.Name != "" {
					n = *a.Name
				}
				out[i] = fmt.Sprintf("%d   %s   (%d tracks)", a.ID, n, len(a.Tracks))
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			if i < 0 || i >= len(p.Arrangements) {
				return []string{""}
			}
			if n := p.Arrangements[i].Name; n != nil {
				return []string{*n}
			}
			return []string{""}
		},
		props: []string{"Name"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			if i < 0 || i >= len(p.Arrangements) {
				return nil, fmt.Errorf("arrangement index out of range")
			}
			if strings.TrimSpace(v[0]) == "" {
				return nil, fmt.Errorf("name cannot be empty")
			}
			return flp.SetArrangementName(p, p.Arrangements[i].ID, v[0])
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Arrangement index %d renamed", i)
		},
	},
	{
		label: "Tracks",
		list: func(p *flp.FLPProject) []string {
			out := []string{}
			for _, a := range p.Arrangements {
				for ti, t := range a.Tracks {
					name := ""
					if t.Name != nil {
						name = *t.Name
					}
					label := fmt.Sprintf("Arr %d / T%-2d", a.ID, ti)
					if name != "" {
						label += "   " + name
					}
					out = append(out, label)
				}
			}
			return out
		},
		read: func(p *flp.FLPProject, i int) []string {
			ai, ti, ok := arrTrackAt(p, i)
			if !ok {
				return make([]string, 6)
			}
			t := p.Arrangements[ai].Tracks[ti]
			name := ""
			if t.Name != nil {
				name = *t.Name
			}
			grouped := ""
			if t.Grouped != nil {
				if *t.Grouped {
					grouped = "1"
				} else {
					grouped = "0"
				}
			}
			col := colorStrings(t.Color)
			return []string{name, grouped, col[0], col[1], col[2], col[3]}
		},
		props: []string{"Name", "Grouped (0/1)", "Color.R", "Color.G", "Color.B", "Color.A"},
		apply: func(p *flp.FLPProject, i int, v []string) (*flp.FLPProject, error) {
			ai, ti, ok := arrTrackAt(p, i)
			if !ok {
				return nil, fmt.Errorf("track index out of range")
			}
			arrID := p.Arrangements[ai].ID
			cur := p
			var err error
			if strings.TrimSpace(v[0]) != "" {
				if cur, err = flp.SetTrackName(cur, arrID, ti, v[0]); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(v[1]) != "" {
				grouped := parseInt(v[1]) != 0
				if cur, err = flp.SetTrackGrouped(cur, arrID, ti, grouped); err != nil {
					return nil, err
				}
			}
			if !allEmpty(v[2:]) {
				if cur, err = flp.SetTrackColor(cur, arrID, ti, parseColorStrings(v[2:])); err != nil {
					return nil, err
				}
			}
			return cur, err
		},
		msg: func(i int, _ []string) string {
			return fmt.Sprintf("Track index %d updated", i)
		},
	},
}

func openEditTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Edit Project", "Please load a project first.")
		return
	}

	w := newModal("Edit Project - " + filepath.Base(app.Path))

	lblCatH := newLabel("Category", 0, 0, 100, 16)
	w.Add(lblCatH)
	lblItemH := newLabel("Item", 0, 0, 100, 16)
	w.Add(lblItemH)
	lblPropH := newLabel("Properties", 0, 0, 100, 16)
	w.Add(lblPropH)

	catList := wui.NewStringList()
	if fontNormal != nil {
		catList.SetFont(fontNormal)
	}
	w.Add(catList)

	itemList := wui.NewStringList()
	if fontNormal != nil {
		itemList.SetFont(fontNormal)
	}
	w.Add(itemList)

	const maxFields = 8
	fields := make([]propField, maxFields)
	for i := range fields {
		lbl := newLabel("", 0, 0, 100, labelH)
		ed := newEdit(0, 0, 100, editH)
		w.Add(lbl)
		w.Add(ed)
		fields[i] = propField{lbl, ed}
	}

	status := newLabel("Editing active project memory.", 0, 0, 100, labelH)
	w.Add(status)
	btnRevert := newBtn("Revert to Disk", 0, 0, 130, btnH, nil)
	w.Add(btnRevert)
	btnApply := newBtn("Apply Change", 0, 0, 130, btnH, nil)
	w.Add(btnApply)
	btnSave := newBtn("Save FLP...", 0, 0, 110, btnH, nil)
	w.Add(btnSave)
	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		y := margin
		headerY := y
		y += 18

		bottomH := btnH + 2*margin
		contentBottom := ih - bottomH
		contentH := contentBottom - y
		if contentH < 80 {
			contentH = 80
		}

		const gap = 6
		availW := contentW - 2*gap
		catW := availW * 24 / 100
		itemW := availW * 30 / 100
		propW := availW - catW - itemW

		catX := margin
		itemX := catX + catW + gap
		propX := itemX + itemW + gap

		lblCatH.SetBounds(catX, headerY, catW, 16)
		lblItemH.SetBounds(itemX, headerY, itemW, 16)
		lblPropH.SetBounds(propX, headerY, propW, 16)

		catList.SetBounds(catX, y, catW, contentH)
		itemList.SetBounds(itemX, y, itemW, contentH)

		fieldH := contentH / maxFields
		if fieldH < 26 {
			fieldH = 26
		}
		if fieldH > 40 {
			fieldH = 40
		}
		const propLabelW = 100
		for i, f := range fields {
			fy := y + i*fieldH
			f.label.SetBounds(propX, fy+4, propLabelW, fieldH-8)
			f.edit.SetBounds(propX+propLabelW+4, fy+2, propW-propLabelW-4, fieldH-4)
		}

		barY := ih - margin - btnH

		btnSave.SetBounds(margin, barY, 110, btnH)
		btnClose.SetBounds(iw-margin-closeBtnW, barY, closeBtnW, btnH)
		btnApply.SetBounds(iw-margin-closeBtnW-rowInnerGap-130, barY, 130, btnH)
		btnRevert.SetBounds(iw-margin-closeBtnW-rowInnerGap-130-rowInnerGap-130, barY, 130, btnH)

		statusX := margin + 110 + rowInnerGap
		statusW := iw - statusX - (2*130 + closeBtnW + 3*rowInnerGap + margin)
		if statusW < 60 {
			statusW = 60
		}
		status.SetBounds(statusX, barY+4, statusW, labelH)
	})

	catIndex := 0
	itemIndex := 0

	clearFields := func() {
		for i := range fields {
			fields[i].label.SetText("")
			fields[i].edit.SetText("")
		}
	}

	populateFields := func(cat int, idx int) {
		clearFields()
		if app.Project == nil || cat < 0 || cat >= len(editCategories) {
			return
		}
		vals := editCategories[cat].read(app.Project, idx)
		for i, p := range editCategories[cat].props {
			if i >= len(fields) {
				break
			}
			fields[i].label.SetText(p)
			if i < len(vals) {
				fields[i].edit.SetText(vals[i])
			}
		}
	}

	readFields := func(cat int) []string {
		if cat < 0 || cat >= len(editCategories) {
			return nil
		}
		out := make([]string, len(editCategories[cat].props))
		for i := range out {
			if i < len(fields) {
				out[i] = fields[i].edit.Text()
			}
		}
		return out
	}

	refreshItems := func() {
		if app.Project == nil || catIndex < 0 || catIndex >= len(editCategories) {
			itemList.SetItems([]string{})
			clearFields()
			return
		}
		cat := editCategories[catIndex]
		items := cat.list(app.Project)
		itemList.SetItems(items)
		keep := itemIndex
		if keep < 0 || keep >= len(items) {
			keep = 0
		}
		if len(items) > 0 {
			itemList.SetSelectedIndex(keep)
			itemIndex = keep
			populateFields(catIndex, keep)
		} else {
			itemIndex = -1
			clearFields()
		}
	}

	catLabels := make([]string, len(editCategories))
	for i, c := range editCategories {
		catLabels[i] = c.label
	}
	catList.SetItems(catLabels)
	catList.SetSelectedIndex(0)
	catList.SetOnChange(func(i int) {
		if i < 0 {
			return
		}
		catIndex = i
		itemIndex = 0
		refreshItems()
	})

	itemList.SetOnChange(func(i int) {
		if i < 0 || catIndex < 0 || catIndex >= len(editCategories) {
			return
		}
		itemIndex = i
		populateFields(catIndex, i)
	})

	btnApply.SetOnClick(func() {
		if app.Project == nil || catIndex < 0 || catIndex >= len(editCategories) || itemIndex < 0 {
			return
		}
		cat := editCategories[catIndex]
		vals := readFields(catIndex)
		next, err := cat.apply(app.Project, itemIndex, vals)
		if err != nil {
			status.SetText("Apply failed: " + err.Error())
			fmt.Fprintf(os.Stderr, "[edit] apply failed (%s, item=%d, vals=%q): %v\n",
				cat.label, itemIndex, vals, err)
			return
		}
		app.Project = next
		app.OnUpdate()
		status.SetText(cat.msg(itemIndex, vals))
		refreshItems()
	})

	btnRevert.SetOnClick(func() {
		if app.Original == nil {
			return
		}
		app.Project = app.Original
		app.OnUpdate()
		status.SetText("Reverted memory to last saved disk state.")
		refreshItems()
	})

	btnSave.SetOnClick(func() {
		path := browseSave(w, "Save Project")
		if path == "" {
			return
		}
		data, err := flp.SerializeFLPProject(app.Project)
		if err != nil {
			status.SetText("Serialize error: " + err.Error())
			return
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			status.SetText("Write error: " + err.Error())
			return
		}
		app.Original = app.Project
		app.Path = path
		app.OnUpdate()
		status.SetText(fmt.Sprintf("Saved to %s", filepath.Base(path)))
	})

	refreshItems()
	showModal(w)
}

// ───────────────────────── 3. Inspect ─────────────────────────

func openInspectTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Inspect Project", "Please load a project first.")
		return
	}

	w := newModal("Inspect Project")

	lblFmt := newLabel("Format:", 0, 0, 60, labelH)
	w.Add(lblFmt)
	cmb := newCombo([]string{"text", "canonical", "json"}, 0, 0, 140, editH)
	w.Add(cmb)

	btnShow := newBtn("Refresh", 0, 0, 100, btnH, nil)
	w.Add(btnShow)

	out := newOutput(0, 0, 100, 100)
	w.Add(out)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		y := margin
		lblFmt.SetBounds(margin, y+3, 60, labelH)
		cmb.SetBounds(margin+60, y, 140, editH)
		btnShow.SetBounds(margin+60+140+rowGap, y-1, 100, btnH)
		y += editH + rowGap + 6

		bottomH := btnH + margin
		outH := ih - y - bottomH
		if outH < 80 {
			outH = 80
		}
		out.SetBounds(margin, y, iw-2*margin, outH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	updateOutput := func() {
		if app.Project == nil {
			setText(out, "No project loaded.")
			return
		}
		switch cmb.Items()[cmb.SelectedIndex()] {
		case "canonical":
			setText(out, trimForUI(flp.RenderCanonical(app.Project)))
		case "json":
			s, jerr := marshalIndentedJSON(flp.ToFlpInfoJson(app.Project))
			if jerr != nil {
				setText(out, formatErr(jerr))
				return
			}
			setText(out, trimForUI(s))
		default:
			header := timestampsText(app.Project, app.Path) + "\n\n"
			setText(out, header+flp.RenderInfo(app.Project, app.Path))
		}
	}
	btnShow.SetOnClick(updateOutput)
	cmb.SetOnChange(func(i int) { updateOutput() })

	updateOutput()
	showModal(w)
}

// ───────────────────────── 4. Visualizer ─────────────────────────

type vizState struct {
	project *flp.FLPProject
	source  string
	view    string

	scrollX, scrollY   int
	contentW, contentH int
	vpW, vpH           int
	pbW, pbH           int

	zoomX, zoomY float64

	dragMode         int
	dragStartX       int
	dragStartY       int
	dragStartScrollX int
	dragStartScrollY int

	patternIdx     int
	arrangementIdx int

	player     *MIDIPlayer
	playTimer  *wui.Timer
	manualTick uint32
	synth      *SynthConfig

	arrCache arrangementCache
}

// scrubToTick updates the playhead position (and seeks the audio if playing).
func (s *vizState) scrubToTick(tick uint32) {
	if s.player != nil && s.player.IsPlaying() {
		s.player.SeekToTick(tick)
	}
	s.manualTick = tick
}

// currentTick returns the tick at which the playhead should be drawn.
func (s *vizState) currentTick() uint32 {
	if s.player != nil && s.player.IsPlaying() {
		return s.player.TickPosition()
	}
	return s.manualTick
}


func openVisualizerTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Visualizer", "Please load a project first.")
		return
	}

	w := newModal("Visualizer")
	w.SetBackground(colCanvasBG)

	// ── Async rendering state ──
	type renderState struct {
		active bool
		start  time.Time
		gen    int // bumped on every new request / cancellation
	}
	rs := &renderState{}

	type renderResult struct {
		buf       []float64
		ppq       int
		bpm       float64
		startTick uint32
		opts      SynthOptions
		gen       int
	}
	renderDoneCh := make(chan renderResult, 1)

	// ── Widgets ──
	lblView := newLabel("View:", 0, 0, 40, labelH)
	w.Add(lblView)
	cmb := newCombo([]string{"arrangement", "pianoroll", "channels", "patterns", "mixer"}, 0, 0, 110, editH)
	w.Add(cmb)

	lblPat := newLabel("Pattern:", 0, 0, 56, labelH)
	w.Add(lblPat)
	patCmb := newCombo([]string{}, 0, 0, 160, editH)
	w.Add(patCmb)

	lblPreset := newLabel("Preset:", 0, 0, 44, labelH)
	w.Add(lblPreset)
	presetCmb := newCombo([]string{"(default)"}, 0, 0, 220, editH)
	w.Add(presetCmb)

	lblZoom := newLabel("Zoom:", 0, 0, 44, labelH)
	w.Add(lblZoom)
	btnZoomOut := newBtn("−", 0, 0, 28, editH, nil)
	w.Add(btnZoomOut)
	btnZoomIn := newBtn("+", 0, 0, 28, editH, nil)
	w.Add(btnZoomIn)
	btnFit := newBtn("Fit", 0, 0, 40, editH, nil)
	w.Add(btnFit)

	btnExportMIDI := newBtn("Export MIDI", 0, 0, 100, editH, nil)
	w.Add(btnExportMIDI)

	btnPlay := newBtn("Play", 0, 0, 70, editH, nil)
	w.Add(btnPlay)

	// ── Tiny progress bar directly under the Play button ──
	progressPb := wui.NewPaintBox()
	progressPb.SetVisible(false)
	w.Add(progressPb)
	progressPb.SetOnPaint(func(c *wui.Canvas) {
		cw, ch := c.Size()
		if cw < 4 || ch < 2 {
			return
		}
		c.FillRect(0, 0, cw, ch, wui.RGB(222, 226, 232))
		if !rs.active {
			return
		}
		// Marquee animation (indeterminate).
		period := 1.0
		phase := math.Mod(time.Since(rs.start).Seconds(), period) / period
		chunkW := cw / 2
		if chunkW < 16 {
			chunkW = 16
		}
		x := int(phase*float64(cw+chunkW)) - chunkW
		xs, xe := x, x+chunkW
		if xs < 0 {
			xs = 0
		}
		if xe > cw {
			xe = cw
		}
		if xe > xs {
			c.FillRect(xs, 0, xe-xs, ch, wui.RGB(70, 130, 220))
		}
	})

	pb := wui.NewPaintBox()
	w.Add(pb)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	s := &vizState{
		project:        app.Project,
		source:         app.Path,
		view:           "arrangement",
		zoomX:          1,
		zoomY:          1,
		patternIdx:     0,
		arrangementIdx: 0,
		player:         NewMIDIPlayer(),
		synth:          globalSynth,
	}

	// ── Helpers ──
	updatePlayBtn := func() {
		canPlay := (s.view == "arrangement" || s.view == "pianoroll") && !rs.active
		btnPlay.SetEnabled(canPlay)
	}

	refreshSelection := func() {
		if s.view == "arrangement" {
			lblPat.SetText("Arr.:")
			items := make([]string, len(s.project.Arrangements))
			for i, a := range s.project.Arrangements {
				n := fmt.Sprintf("#%d", a.ID)
				if a.Name != nil && *a.Name != "" {
					n = *a.Name
				}
				items[i] = fmt.Sprintf("%d: %s", i, n)
			}
			if len(items) == 0 {
				items = []string{"(no arrangements)"}
			}
			patCmb.SetItems(items)
			if s.arrangementIdx >= 0 && s.arrangementIdx < len(items) {
				patCmb.SetSelectedIndex(s.arrangementIdx)
			} else {
				s.arrangementIdx = 0
				patCmb.SetSelectedIndex(0)
			}
		} else {
			lblPat.SetText("Pattern:")
			items := make([]string, len(s.project.Patterns))
			for i, pt := range s.project.Patterns {
				n := fmt.Sprintf("#%d", pt.ID)
				if pt.Name != nil && *pt.Name != "" {
					n = *pt.Name
				}
				items[i] = fmt.Sprintf("%d: %s", i, n)
			}
			if len(items) == 0 {
				items = []string{"(no patterns)"}
			}
			patCmb.SetItems(items)
			if s.patternIdx >= 0 && s.patternIdx < len(items) {
				patCmb.SetSelectedIndex(s.patternIdx)
			} else {
				s.patternIdx = 0
				patCmb.SetSelectedIndex(0)
			}
		}
	}

	refreshPresetCombo := func() {
		if s.view != "pianoroll" || s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
			lblPreset.SetVisible(false)
			presetCmb.SetVisible(false)
			return
		}
		lblPreset.SetVisible(true)
		presetCmb.SetVisible(true)

		pid := s.project.Patterns[s.patternIdx].ID
		cur := getPatternPresetName(pid)

		items := []string{"(default)"}
		items = append(items, presetNames()...)
		presetCmb.SetItems(items)
		idx := 0
		if cur != "" {
			if i := indexOf(items, cur); i >= 0 {
				idx = i
			}
		}
		presetCmb.SetSelectedIndex(idx)
	}

	// ── Layout: 2 compact rows, then the paint box ──
	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		rightEdge := iw - margin
		y := margin

		// ── Row 1: View | Pattern | Preset (fills the remaining width) ──
		x := margin
		lblView.SetBounds(x, y+3, 40, labelH)
		x += 40
		cmb.SetBounds(x, y, 110, editH)
		x += 110 + 8

		lblPat.SetBounds(x, y+3, 56, labelH)
		x += 56
		patCmb.SetBounds(x, y, 160, editH)
		x += 160 + 8

		lblPreset.SetBounds(x, y+3, 44, labelH)
		x += 44
		presetW := rightEdge - x
		if presetW < 100 {
			presetW = 100
		}
		presetCmb.SetBounds(x, y, presetW, editH)

		y += editH + 6

		// ── Row 2: Zoom (left) — Play + Export (right) ──
		x = margin
		lblZoom.SetBounds(x, y+3, 44, labelH)
		x += 44
		btnZoomOut.SetBounds(x, y, 28, editH)
		x += 28 + 4
		btnZoomIn.SetBounds(x, y, 28, editH)
		x += 28 + 4
		btnFit.SetBounds(x, y, 40, editH)

		r := rightEdge
		btnExportMIDI.SetBounds(r-100, y, 100, editH)
		r -= 100 + 8
		playX := r - 70
		btnPlay.SetBounds(playX, y, 70, editH)

		// Tiny progress bar directly under the Play button.
		progressPb.SetBounds(playX, y+editH+2, 70, 4)

		y += editH + 10 // extra 6px so the progress bar has room

		// ── Paint box ──
		bottomH := btnH + margin
		pbH := ih - y - bottomH
		if pbH < 80 {
			pbH = 80
		}
		pb.SetBounds(margin, y, contentW, pbH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	pb.SetOnPaint(func(c *wui.Canvas) {
		s.pbW, s.pbH = c.Size()
		renderViz(c, s)
	})

	// ── Playback / rendering timer ──
	s.playTimer = w.AddTimer(16, func() {
		// Render in progress — animate bar, poll for completion.
		if rs.active {
			select {
			case rr := <-renderDoneCh:
				if rr.gen != rs.gen {
					// Stale render (view changed, new play queued) — discard.
					rs.active = false
					progressPb.SetVisible(false)
					updatePlayBtn()
					btnPlay.SetText("Play")
					return
				}
				rs.active = false
				progressPb.SetVisible(false)
				updatePlayBtn()
				btnPlay.SetText("Stop")

				// Cache the arrangement buffer for instant replays.
				if s.view == "arrangement" {
					s.arrCache = arrangementCache{
						projectPtr: s.project,
						arrIdx:     s.arrangementIdx,
						opts:       rr.opts,
						buf:        rr.buf,
						valid:      true,
					}
				}

				if len(rr.buf) > 0 {
					s.player.PlayBuffer(rr.buf, rr.ppq, rr.bpm, rr.startTick)
				} else {
					btnPlay.SetText("Play")
					s.playTimer.Stop()
				}
			default:
				progressPb.Paint()
			}
			return
		}

		// Normal playback update.
		if s.player == nil || !s.player.IsPlaying() {
			btnPlay.SetText("Play")
			s.playTimer.Stop()
			if s.player != nil && s.player.ReachedEnd() {
				s.manualTick = 0
			}
			pb.Paint()
			return
		}
		pb.Paint()
	})
	s.playTimer.Stop()

	stopPlayback := func() {
		if s.player != nil && s.player.IsPlaying() {
			s.manualTick = s.player.TickPosition()
			s.player.Stop()
			btnPlay.SetText("Play")
			s.playTimer.Stop()
			pb.Paint()
		}
	}

	// ── View change ──
	cmb.SetOnChange(func(_ int) {
		stopPlayback()
		s.view = cmb.Items()[cmb.SelectedIndex()]
		rs.gen++ // invalidate any pending render
		s.scrollX, s.scrollY = 0, 0
		refreshSelection()
		if s.view == "pianoroll" {
			centerPianoRoll(s)
		}
		refreshPresetCombo()
		updatePlayBtn()
		pb.Paint()
	})

	// ── Pattern / arrangement selection change ──
	patCmb.SetOnChange(func(i int) {
		if i < 0 {
			return
		}
		stopPlayback()
		if s.view == "arrangement" {
			s.arrangementIdx = i
			s.manualTick = 0
		} else {
			s.patternIdx = i
			s.manualTick = 0
			if s.view == "pianoroll" {
				centerPianoRoll(s)
				s.scrollX = 0
			}
		}
		refreshPresetCombo()
		pb.Paint()
	})

	// ── Preset combo ──
	presetCmb.SetOnChange(func(i int) {
		if s.view != "pianoroll" {
			return
		}
		if s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
			return
		}
		items := presetCmb.Items()
		if i < 0 || i >= len(items) {
			return
		}
		name := items[i]
		pid := s.project.Patterns[s.patternIdx].ID
		if name == "(default)" {
			setPatternPreset(pid, "")
		} else {
			setPatternPreset(pid, name)
		}
	})

	btnZoomIn.SetOnClick(func() {
		if s.zoomX < 8 {
			s.zoomX *= 1.25
		}
		clampScroll(s)
		pb.Paint()
	})
	btnZoomOut.SetOnClick(func() {
		if s.zoomX > 0.15 {
			s.zoomX /= 1.25
		}
		clampScroll(s)
		pb.Paint()
	})
	btnFit.SetOnClick(func() {
		s.zoomX, s.zoomY = 1, 1
		s.scrollX, s.scrollY = 0, 0
		pb.Paint()
	})

	// ── Play button ──
	btnPlay.SetOnClick(func() {
		if rs.active {
			return // already rendering
		}
		if s.player.IsPlaying() {
			stopPlayback()
			return
		}
		if s.view != "arrangement" && s.view != "pianoroll" {
			return // play disabled in other views
		}

		bpm := 120.0
		if t := flp.GetTempo(s.project); t != nil {
			bpm = *t
		}
		ppq := s.project.Header.PPQ
		if ppq <= 0 {
			ppq = 96
		}

		startTick := s.manualTick
		opts := s.synth.Snapshot()

		// ── Arrangement ──
		if s.view == "arrangement" {
			if s.arrangementIdx < 0 || s.arrangementIdx >= len(s.project.Arrangements) {
				return
			}
			// Cache hit → play immediately, no rendering needed.
			if s.arrCache.matches(s.project, s.arrangementIdx, opts) {
				buf := s.arrCache.buf
				if len(buf) == 0 {
					return
				}
				s.player.PlayBuffer(buf, ppq, bpm, startTick)
				btnPlay.SetText("Stop")
				s.playTimer.Start()
				return
			}

			arr := s.project.Arrangements[s.arrangementIdx]
			patterns := s.project.Patterns
			channels := s.project.Channels
			src := s.source

			rs.active = true
			rs.start = time.Now()
			rs.gen++
			myGen := rs.gen

			progressPb.SetVisible(true)
			btnPlay.SetText("...")
			updatePlayBtn()
			progressPb.Paint()
			s.playTimer.Start()

			go func() {
				buf := renderArrangement(arr, patterns, ppq, bpm, opts, channels, src)
				select {
				case renderDoneCh <- renderResult{
					buf: buf, ppq: ppq, bpm: bpm, startTick: startTick, opts: opts, gen: myGen,
				}:
				default:
					// A newer render has superseded this one.
				}
			}()
			return
		}

		// ── Piano roll ──
		if s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
			return
		}
		pat := s.project.Patterns[s.patternIdx]
		if po, ok := getPatternPresetOptions(pat.ID); ok {
			opts = po
		}
		channels := s.project.Channels
		src := s.source

		rs.active = true
		rs.start = time.Now()
		rs.gen++
		myGen := rs.gen

		progressPb.SetVisible(true)
		btnPlay.SetText("...")
		updatePlayBtn()
		progressPb.Paint()
		s.playTimer.Start()

		go func() {
			buf := renderPattern(pat, ppq, bpm, opts, channels, src)
			select {
			case renderDoneCh <- renderResult{
				buf: buf, ppq: ppq, bpm: bpm, startTick: startTick, opts: opts, gen: myGen,
			}:
			default:
			}
		}()
	})

	w.SetOnMouseWheel(func(x, y int, delta float64) {
		ticks := int(delta / 120)
		if ticks == 0 && delta != 0 {
			if delta > 0 {
				ticks = 1
			} else {
				ticks = -1
			}
		}
		s.scrollY -= ticks * 60
		clampScroll(s)
		pb.Paint()
	})

	w.SetOnKeyDown(func(key int) {
		step := 40
		switch key {
		case wui.KeyUp:
			s.scrollY -= step
		case wui.KeyDown:
			s.scrollY += step
		case wui.KeyLeft:
			s.scrollX -= step
		case wui.KeyRight:
			s.scrollX += step
		case wui.KeyPrior:
			s.scrollY -= s.vpH
		case wui.KeyNext:
			s.scrollY += s.vpH
		case wui.KeyHome:
			s.scrollY = 0
			s.scrollX = 0
		case wui.KeyEnd:
			s.scrollY = 1 << 30
		case wui.KeyAdd, wui.KeyOEMPlus:
			if s.zoomX < 8 {
				s.zoomX *= 1.25
			}
			clampScroll(s)
		case wui.KeySubtract, wui.KeyOEMMinus:
			if s.zoomX > 0.15 {
				s.zoomX /= 1.25
			}
			clampScroll(s)
		case wui.KeySpace:
			btnPlay.OnClick()()
		default:
			return
		}
		clampScroll(s)
		pb.Paint()
	})

	w.SetOnMouseDown(func(_ wui.MouseButton, x, y int) {
		px, py := pb.Position()
		lx, ly := x-px, y-py
		if handleScrollClick(s, lx, ly) {
			pb.Paint()
			return
		}
		if handleTimelineScrub(s, lx, ly) {
			pb.Paint()
		}
	})

	w.SetOnMouseUp(func(_ wui.MouseButton, x, y int) {
		s.dragMode = 0
	})

	pb.SetOnMouseMove(func(x, y int) {
		if s.dragMode == 0 {
			return
		}
		if s.dragMode == 3 {
			if handleTimelineScrub(s, x, y) {
				pb.Paint()
			}
			return
		}
		if handleScrollDrag(s, x, y) {
			pb.Paint()
		}
	})

	btnExportMIDI.SetOnClick(func() {
		if s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
			return
		}
		if path := browseSaveMIDI(w, "Export pattern as MIDI"); path != "" {
			if err := exportPatternMIDI(s.project, s.patternIdx, path); err != nil {
				wui.MessageBoxError("Export MIDI", "Failed to export:\n"+err.Error())
			} else {
				wui.MessageBoxInfo("Export MIDI", "Exported pattern to:\n"+path)
			}
		}
	})

	// Populate combos and button state for the initial view.
	refreshSelection()
	refreshPresetCombo()
	updatePlayBtn()

	defer stopPlayback()
	showModal(w)
}

// arrangementCache holds a fully-rendered arrangement buffer. When the
// project pointer, arrangement index and synth options are all unchanged
// since the last render, the cached buffer is reused so re-clicking Play is
// instantaneous even on large arrangements.
type arrangementCache struct {
	projectPtr *flp.FLPProject
	arrIdx     int
	opts       SynthOptions
	buf        []float64
	valid      bool
}

func (c *arrangementCache) matches(project *flp.FLPProject, arrIdx int, opts SynthOptions) bool {
	return c.valid &&
		c.projectPtr == project &&
		c.arrIdx == arrIdx &&
		c.opts == opts
}

// handleTimelineScrub converts a mouse coordinate inside the timeline view
// (pianoroll or arrangement) into a tick position and updates the playhead.
// The user can click anywhere in the ruler or the content area to reposition
// the playhead; holding the mouse down and dragging keeps updating it.
//
//   - pianoroll   → x < pianoKeysW  is the piano keyboard; anything right is the grid
//   - arrangement → x < trackHdrW   is the track header column; anything right is the grid
//
// Returns true if the click landed inside the scrub area.
func handleTimelineScrub(s *vizState, lx, ly int) bool {
	var leftW int
	switch s.view {
	case "pianoroll":
		leftW = pianoKeysW
	case "arrangement":
		leftW = trackHdrW
	default:
		return false
	}
	if lx < leftW || lx >= s.pbW-scrollbarSize {
		return false
	}
	// Allow seeking from the ruler down (skips only the top header bar
	// and the scrollbars).
	if ly < vizHeaderH || ly >= s.pbH-scrollbarSize {
		return false
	}
	pxPerTick := basePxPerTick * s.zoomX
	tick := float64(lx-leftW+s.scrollX) / pxPerTick
	if tick < 0 {
		tick = 0
	}
	s.scrubToTick(uint32(tick))
	s.dragMode = 3
	return true
}

func centerPianoRoll(s *vizState) {
	if s.project == nil || s.patternIdx < 0 || s.patternIdx >= len(s.project.Patterns) {
		return
	}
	pat := s.project.Patterns[s.patternIdx]
	keyH := int(baseKeyH * s.zoomY)
	if keyH < 4 {
		keyH = 4
	}
	if len(pat.Notes) == 0 {
		s.scrollY = 60 * keyH
	} else {
		minK, maxK := 127, 0
		for _, n := range pat.Notes {
			k := int(n.Key)
			if k < minK {
				minK = k
			}
			if k > maxK {
				maxK = k
			}
		}
		centerK := (minK + maxK) / 2
		yOfKey := (127 - centerK) * keyH
		s.scrollY = yOfKey - (s.vpH-rulerH)/2
	}
	clampScroll(s)
}

func clampScroll(s *vizState) {
	maxY := s.contentH - s.vpH
	if maxY < 0 {
		maxY = 0
	}
	if s.scrollY < 0 {
		s.scrollY = 0
	}
	if s.scrollY > maxY {
		s.scrollY = maxY
	}
	maxX := s.contentW - s.vpW
	if maxX < 0 {
		maxX = 0
	}
	if s.scrollX < 0 {
		s.scrollX = 0
	}
	if s.scrollX > maxX {
		s.scrollX = maxX
	}
}

func vThumb(s *vizState) (pos, size int) {
	trackH := s.pbH - vizHeaderH - scrollbarSize
	if s.contentH <= s.vpH || trackH <= 0 {
		return 0, trackH
	}
	thumbH := s.vpH * trackH / s.contentH
	if thumbH < 24 {
		thumbH = 24
	}
	if thumbH > trackH {
		thumbH = trackH
	}
	maxScroll := s.contentH - s.vpH
	thumbY := 0
	if maxScroll > 0 {
		thumbY = (trackH - thumbH) * s.scrollY / maxScroll
	}
	return thumbY, thumbH
}

func hThumb(s *vizState) (pos, size int) {
	trackW := s.pbW - scrollbarSize
	if s.contentW <= s.vpW || trackW <= 0 {
		return 0, trackW
	}
	thumbW := s.vpW * trackW / s.contentW
	if thumbW < 24 {
		thumbW = 24
	}
	if thumbW > trackW {
		thumbW = trackW
	}
	maxScroll := s.contentW - s.vpW
	thumbX := 0
	if maxScroll > 0 {
		thumbX = (trackW - thumbW) * s.scrollX / maxScroll
	}
	return thumbX, thumbW
}

func handleScrollClick(s *vizState, lx, ly int) bool {
	vx := s.pbW - scrollbarSize
	if lx >= vx && lx < s.pbW && ly >= vizHeaderH && ly < s.pbH-scrollbarSize {
		pos, size := vThumb(s)
		relY := ly - vizHeaderH
		if relY >= pos && relY < pos+size {
			s.dragMode = 1
			s.dragStartY = ly
			s.dragStartScrollY = s.scrollY
		} else if s.contentH > s.vpH {
			if relY < pos {
				s.scrollY -= s.vpH
			} else {
				s.scrollY += s.vpH
			}
			clampScroll(s)
		}
		return true
	}
	hy := s.pbH - scrollbarSize
	if ly >= hy && ly < s.pbH && lx >= 0 && lx < s.pbW-scrollbarSize {
		pos, size := hThumb(s)
		if lx >= pos && lx < pos+size {
			s.dragMode = 2
			s.dragStartX = lx
			s.dragStartScrollX = s.scrollX
		} else if s.contentW > s.vpW {
			if lx < pos {
				s.scrollX -= s.vpW
			} else {
				s.scrollX += s.vpW
			}
			clampScroll(s)
		}
		return true
	}
	return false
}

func handleScrollDrag(s *vizState, lx, ly int) bool {
	switch s.dragMode {
	case 1:
		_, size := vThumb(s)
		delta := ly - s.dragStartY
		trackH := s.pbH - vizHeaderH - scrollbarSize
		trackRange := trackH - size
		maxScroll := s.contentH - s.vpH
		if trackRange > 0 && maxScroll > 0 {
			s.scrollY = s.dragStartScrollY + delta*maxScroll/trackRange
		}
		clampScroll(s)
		return true
	case 2:
		_, size := hThumb(s)
		delta := lx - s.dragStartX
		trackW := s.pbW - scrollbarSize
		trackRange := trackW - size
		maxScroll := s.contentW - s.vpW
		if trackRange > 0 && maxScroll > 0 {
			s.scrollX = s.dragStartScrollX + delta*maxScroll/trackRange
		}
		clampScroll(s)
		return true
	}
	return false
}

func measureContent(s *vizState) (int, int) {
	p := s.project
	const rowH = 24
	switch s.view {
	case "channels":
		return s.vpW, len(p.Channels)*rowH + 40
	case "patterns":
		return s.vpW, len(p.Patterns)*rowH + 40
	case "mixer":
		maxSlots := 0
		for _, ins := range p.Inserts {
			if len(ins.Slots) > maxSlots {
				maxSlots = len(ins.Slots)
			}
		}
		w := 380 + maxSlots*24 + 40
		if w < s.vpW {
			w = s.vpW
		}
		return w, len(p.Inserts)*rowH + 40
	case "pianoroll":
		keyH := int(baseKeyH * s.zoomY)
		if keyH < 4 {
			keyH = 4
		}
		h := rulerH + 128*keyH + 20
		w := s.vpW
		if s.patternIdx >= 0 && s.patternIdx < len(p.Patterns) {
			pat := p.Patterns[s.patternIdx]
			maxTick := uint32(0)
			for _, n := range pat.Notes {
				if e := n.Position + n.Length; e > maxTick {
					maxTick = e
				}
			}
			if maxTick < 384 {
				maxTick = 384
			}
			w = pianoKeysW + int(float64(maxTick)*basePxPerTick*s.zoomX) + 60
			if w < s.vpW {
				w = s.vpW
			}
		}
		return w, h
	default:
		ppq := 96
		if p.Header.PPQ > 0 {
			ppq = int(p.Header.PPQ)
		}
		barTicks := float64(ppq) * 4.0
		maxEnd := uint32(0)
		for _, a := range p.Arrangements {
			for _, cl := range a.Clips {
				if e := cl.Position + cl.Length; e > maxEnd {
					maxEnd = e
				}
			}
		}
		minTicks := uint32(barTicks * 8)
		if maxEnd < minTicks {
			maxEnd = minTicks
		}
		w := trackHdrW + int(float64(maxEnd)*basePxPerTick*s.zoomX) + 60
		if w < s.vpW {
			w = s.vpW
		}

		trackH := int(baseTrackH * s.zoomY)
		if trackH < 8 {
			trackH = 8
		}
		h := rulerH
		for _, a := range p.Arrangements {
			rows := len(a.Tracks)
			if rows == 0 {
				for _, cl := range a.Clips {
					if idx := 499 - cl.TrackRvidx; idx+1 > rows {
						rows = idx + 1
					}
				}
			}
			h += 22 + rows*trackH + 10
		}
		if h < s.vpH {
			h = s.vpH
		}
		return w, h
	}
}

func renderViz(c *wui.Canvas, s *vizState) {
	w, h := c.Size()
	c.FillRect(0, 0, w, h, colCanvasBG)

	c.FillRect(0, 0, w, vizHeaderH, colHeader)
	c.TextOut(14, 8, filepath.Base(s.source), colHeaderFG)
	c.TextOut(w-140, 26, fmt.Sprintf("Zoom %.0f%%", s.zoomX*100), colHeaderDi)

	vpW, vpH := w-scrollbarSize, h-vizHeaderH-scrollbarSize
	s.vpW, s.vpH = vpW, vpH
	s.contentW, s.contentH = measureContent(s)
	clampScroll(s)

	c.PushDrawRegion(0, vizHeaderH, vpW, vpH)
	switch s.view {
	case "channels":
		drawChannelsView(c, s)
	case "patterns":
		drawPatternsView(c, s)
	case "mixer":
		drawMixerView(c, s)
	case "pianoroll":
		drawPianoRoll(c, s)
	default:
		drawArrangementView(c, s)
	}
	c.PopDrawRegion()

	drawScrollbars(c, w, h, s)
}

func drawScrollbars(c *wui.Canvas, w, h int, s *vizState) {
	trackX := w - scrollbarSize
	trackY := vizHeaderH
	trackH := h - vizHeaderH - scrollbarSize
	c.FillRect(trackX, trackY, scrollbarSize, trackH, colTrack)
	if s.contentH > s.vpH {
		pos, size := vThumb(s)
		c.FillRect(trackX+2, trackY+pos, scrollbarSize-4, size, colThumb)
	}

	hTrackY := h - scrollbarSize
	hTrackW := w - scrollbarSize
	c.FillRect(0, hTrackY, hTrackW, scrollbarSize, colTrack)
	if s.contentW > s.vpW {
		pos, size := hThumb(s)
		c.FillRect(pos+2, hTrackY+2, size-4, scrollbarSize-4, colThumb)
	}

	c.FillRect(trackX, hTrackY, scrollbarSize, scrollbarSize, colHeader)
}

func drawArrangementView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	ppq := 96
	if p.Header.PPQ > 0 {
		ppq = int(p.Header.PPQ)
	}
	barTicks := float64(ppq) * 4.0
	beatTicks := float64(ppq)
	pxPerTick := basePxPerTick * s.zoomX
	trackH := int(baseTrackH * s.zoomY)
	if trackH < 8 {
		trackH = 8
	}

	startTick := float64(s.scrollX) / pxPerTick
	endTick := float64(s.scrollX+s.vpW) / pxPerTick
	startBar := int(startTick/barTicks) - 1
	endBar := int(endTick/barTicks) + 1
	startBeat := int(startTick/beatTicks) - 1
	endBeat := int(endTick/beatTicks) + 1

	// Canvas background
	c.FillRect(trackHdrW, contentY+rulerH, s.vpW-trackHdrW, s.vpH-rulerH, colCanvasBG)

	// Ruler
	c.FillRect(trackHdrW, contentY, s.vpW-trackHdrW, rulerH, colHeader)
	for bar := startBar; bar <= endBar; bar++ {
		bx := trackHdrW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < trackHdrW || bx > s.vpW {
			continue
		}
		c.Line(bx, contentY+rulerH-6, bx, contentY+rulerH, colTextDim)
		c.TextOut(bx+3, contentY+5, fmt.Sprintf("%d", bar+1), colHeaderDi)
	}
	c.FillRect(0, contentY, trackHdrW, rulerH, colSidebar)
	c.Line(0, contentY+rulerH-1, trackHdrW, contentY+rulerH-1, colSep)

	cy := contentY + rulerH - s.scrollY
	for _, a := range p.Arrangements {
		if cy+22 > contentY+rulerH && cy < contentY+s.vpH {
			c.FillRect(0, cy, trackHdrW, 22, colSidebar)
			arrName := fmt.Sprintf("#%d", a.ID)
			if a.Name != nil && *a.Name != "" {
				arrName = *a.Name
			}
			c.TextOut(8, cy+4, "Arrangement "+arrName, colHeaderFG)
			c.FillRect(trackHdrW, cy, s.vpW-trackHdrW, 22, colSidebar)
			c.Line(0, cy+21, s.vpW, cy+21, colSep)
		}
		cy += 22

		rows := len(a.Tracks)
		if rows == 0 {
			for _, cl := range a.Clips {
				if idx := 499 - cl.TrackRvidx; idx+1 > rows {
					rows = idx + 1
				}
			}
		}

		byTrack := map[int][]flp.Clip{}
		for _, cl := range a.Clips {
			byTrack[499-cl.TrackRvidx] = append(byTrack[499-cl.TrackRvidx], cl)
		}

		for t := 0; t < rows; t++ {
			visible := cy+trackH > contentY+rulerH && cy < contentY+s.vpH
			if visible {
				// Track header background
				hdrBG := wui.RGB(238, 240, 244)
				if t%2 == 1 {
					hdrBG = wui.RGB(232, 235, 240)
				}
				c.FillRect(0, cy, trackHdrW, trackH, hdrBG)
				if t < len(a.Tracks) {
					if tc, ok := rgbaToWuiColor(a.Tracks[t].Color); ok {
						c.FillRect(0, cy, 4, trackH, tc)
					}
				}
				label := fmt.Sprintf("Track %d", t+1)
				if t < len(a.Tracks) && a.Tracks[t].Name != nil && *a.Tracks[t].Name != "" {
					label = *a.Tracks[t].Name
				}
				c.TextOut(10, cy+(trackH-14)/2, truncate(label, 20), colText)
				c.Line(0, cy+trackH-1, trackHdrW, cy+trackH-1, colSep)

				// Track row background
				bg := colTrackBG
				if t%2 == 1 {
					bg = colTrackAlt
				}
				c.FillRect(trackHdrW, cy, s.vpW-trackHdrW, trackH, bg)

				// Beat grid drawn on top of the row
				for beat := startBeat; beat <= endBeat; beat++ {
					bx := trackHdrW + int(float64(beat)*beatTicks*pxPerTick) - s.scrollX
					if bx < trackHdrW || bx > s.vpW {
						continue
					}
					col := colGridBeat
					if beat%4 == 0 {
						col = colGrid
					}
					c.Line(bx, cy, bx, cy+trackH, col)
				}
				c.Line(trackHdrW, cy+trackH-1, s.vpW, cy+trackH-1, colGrid)

				// Clips
				for _, cl := range byTrack[t] {
					cx := trackHdrW + int(float64(cl.Position)*pxPerTick) - s.scrollX
					cw := int(float64(cl.Length) * pxPerTick)
					if cw < 3 {
						cw = 3
					}
					if cx+cw < trackHdrW || cx > s.vpW {
						continue
					}
					if cx < trackHdrW {
						cw -= trackHdrW - cx
						cx = trackHdrW
					}
					if cx+cw > s.vpW {
						cw = s.vpW - cx
					}

					c.FillRect(cx, cy+2, cw, trackH-4, clipColorFor(p, cl))
					c.DrawRect(cx, cy+2, cw, trackH-4, colClipEdge)
					if lbl := clipLabel(p, cl); lbl != "" && cw > 30 {
						c.TextOut(cx+4, cy+(trackH-14)/2, truncate(lbl, cw/7), wui.RGB(255, 255, 255))
					}
				}
			}
			cy += trackH
		}
		cy += 10
	}

	// ── playback cursor ────────────────────────────────────────────
	// Always drawn: shows the scrub position when idle, live position when
	// playing. The user can click/drag anywhere in the ruler or content
	// area to reposition — see handleTimelineScrub.
	var pos uint32
	if s.player != nil && s.player.IsPlaying() {
		pos = s.player.TickPosition()
	} else {
		pos = s.manualTick
	}
	cx := trackHdrW + int(float64(pos)*pxPerTick) - s.scrollX
	if cx >= trackHdrW && cx <= s.vpW {
		cursorCol := wui.RGB(220, 60, 60)
		// Full-height line down the timeline.
		c.Line(cx, contentY, cx, contentY+s.vpH, cursorCol)
		// Small square cap in the ruler so it's easy to spot and grab.
		c.FillRect(cx-4, contentY+rulerH-9, 9, 9, cursorCol)
		c.Line(cx-4, contentY+rulerH, cx+5, contentY+rulerH, colSep)
	}
}

func drawPianoRoll(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	if s.patternIdx < 0 || s.patternIdx >= len(p.Patterns) {
		return
	}
	pat := p.Patterns[s.patternIdx]

	ppq := 96
	if p.Header.PPQ > 0 {
		ppq = int(p.Header.PPQ)
	}
	barTicks := float64(ppq) * 4.0
	beatTicks := float64(ppq)
	pxPerTick := basePxPerTick * s.zoomX
	keyH := int(baseKeyH * s.zoomY)
	if keyH < 4 {
		keyH = 4
	}

	gridTop := contentY + rulerH
	gridBottom := contentY + s.vpH

	startTick := float64(s.scrollX) / pxPerTick
	endTick := float64(s.scrollX+s.vpW) / pxPerTick
	startBar := int(startTick/barTicks) - 1
	endBar := int(endTick/barTicks) + 1

	// Ruler
	c.FillRect(pianoKeysW, contentY, s.vpW-pianoKeysW, rulerH, colHeader)
	for bar := startBar; bar <= endBar; bar++ {
		bx := pianoKeysW + int(float64(bar)*barTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		c.Line(bx, contentY+rulerH-6, bx, contentY+rulerH, colTextDim)
		c.TextOut(bx+3, contentY+5, fmt.Sprintf("%d", bar+1), colHeaderDi)
	}
	c.FillRect(0, contentY, pianoKeysW, rulerH, colSidebar)
	c.Line(0, contentY+rulerH-1, s.vpW, contentY+rulerH-1, colSep)

	// Keys + row backgrounds
	for k := 127; k >= 0; k-- {
		y := gridTop + (127-k)*keyH - s.scrollY
		if y+keyH < gridTop || y > gridBottom {
			continue
		}

		black := isBlackKey(k)

		var keyCol wui.Color
		var rowBg wui.Color
		if black {
			keyCol = wui.RGB(60, 62, 70)
			rowBg = wui.RGB(242, 244, 248)
		} else {
			keyCol = wui.RGB(255, 255, 255)
			rowBg = wui.RGB(255, 255, 255)
			if k%12 == 0 {
				rowBg = wui.RGB(235, 240, 248)
			}
		}
		c.FillRect(0, y, pianoKeysW, keyH, keyCol)
		c.Line(0, y+keyH-1, pianoKeysW, y+keyH-1, wui.RGB(180, 184, 190))
		c.FillRect(pianoKeysW, y, s.vpW-pianoKeysW, keyH, rowBg)

		if k%12 == 0 {
			c.TextOut(6, y+1, fmt.Sprintf("C%d", k/12-1), colTextDim)
		}
	}

	// Grid
	startBeat := int(startTick/beatTicks) - 1
	endBeat := int(endTick/beatTicks) + 1
	for beat := startBeat; beat <= endBeat; beat++ {
		bx := pianoKeysW + int(float64(beat)*beatTicks*pxPerTick) - s.scrollX
		if bx < pianoKeysW || bx > s.vpW {
			continue
		}
		col := colGridBeat
		if beat%4 == 0 {
			col = colGrid
		}
		c.Line(bx, gridTop, bx, gridBottom, col)
	}

	// Notes
	for _, n := range pat.Notes {
		y := gridTop + (127-int(n.Key))*keyH - s.scrollY
		if y+keyH < gridTop || y > gridBottom {
			continue
		}
		x := pianoKeysW + int(float64(n.Position)*pxPerTick) - s.scrollX
		nw := int(float64(n.Length) * pxPerTick)
		if nw < 2 {
			nw = 2
		}
		if x+nw < pianoKeysW || x > s.vpW {
			continue
		}
		if x < pianoKeysW {
			nw -= pianoKeysW - x
			x = pianoKeysW
		}
		if x+nw > s.vpW {
			nw = s.vpW - x
		}

		col := noteColor(p, n.ChannelIid)
		c.FillRect(x, y+1, nw, keyH-2, col)
		c.DrawRect(x, y+1, nw, keyH-2, colClipEdge)
	}

	// ── playback cursor ────────────────────────────────────────────
	// Always drawn: shows the scrub position when idle, live position when
	// playing. The user can click/drag anywhere in the grid to reposition.
	var pos uint32
	if s.player != nil && s.player.IsPlaying() {
		pos = s.player.TickPosition()
	} else {
		pos = s.manualTick
	}
	cx := pianoKeysW + int(float64(pos)*pxPerTick) - s.scrollX
	if cx >= pianoKeysW && cx <= s.vpW {
		cursorCol := wui.RGB(220, 60, 60)
		c.Line(cx, gridTop, cx, gridBottom, cursorCol)
		// Triangular cap in the ruler so it's easy to spot and grab.
		c.FillRect(cx-4, contentY+rulerH-9, 9, 9, cursorCol)
		c.Line(cx-4, contentY+rulerH, cx+5, contentY+rulerH, colSep)
	}
}

func drawChannelsView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	const rowH = 24

	cy := contentY + 10 - s.scrollY
	if cy+24 > contentY && cy < contentY+s.vpH {
		c.TextOut(14, cy, fmt.Sprintf("%d channels", len(p.Channels)), colText)
	}
	cy += 26

	for _, ch := range p.Channels {
		if cy+rowH > contentY && cy < contentY+s.vpH {
			kind := string(ch.Kind)
			bg := colTrackBG
			c.FillRect(0, cy, s.vpW, rowH, bg)
			c.FillRect(0, cy, 5, rowH, channelColorFor(p, ch.Iid))
			c.Line(0, cy+rowH-1, s.vpW, cy+rowH-1, colGrid)

			name := fmt.Sprintf("#%d", ch.Iid)
			if ch.Name != nil && *ch.Name != "" {
				name = *ch.Name
			}
			c.TextOut(12, cy+4, truncate(name, 32), colText)
			c.TextOut(260, cy+4, kind, colTextDim)
			if ch.Plugin != nil {
				plug := ch.Plugin.InternalName
				if ch.Plugin.Name != nil && *ch.Plugin.Name != "" {
					plug = *ch.Plugin.Name
				}
				c.TextOut(380, cy+4, truncate(plug, 40), colTextDim)
			}
		}
		cy += rowH
	}
}

func drawPatternsView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	const rowH = 24

	cy := contentY + 10 - s.scrollY
	if cy+24 > contentY && cy < contentY+s.vpH {
		c.TextOut(14, cy, fmt.Sprintf("%d patterns", len(p.Patterns)), colText)
	}
	cy += 26

	for _, pt := range p.Patterns {
		if cy+rowH > contentY && cy < contentY+s.vpH {
			pc := patternColorFor(p, pt.ID)
			c.FillRect(0, cy, s.vpW, rowH, colTrackBG)
			c.FillRect(0, cy, 5, rowH, pc)
			c.Line(0, cy+rowH-1, s.vpW, cy+rowH-1, colGrid)

			name := fmt.Sprintf("#%d", pt.ID)
			if pt.Name != nil && *pt.Name != "" {
				name = *pt.Name
			}
			c.TextOut(12, cy+4, truncate(name, 30), colText)

			info := fmt.Sprintf("%d notes", len(pt.Notes))
			if n := len(pt.Controllers); n > 0 {
				info += fmt.Sprintf(", %d keyframes", n)
			}
			c.TextOut(300, cy+4, info, colTextDim)

			if len(pt.Notes) > 0 {
				bars := 80
				density := make([]int, bars)
				maxTick := uint32(1)
				for _, n := range pt.Notes {
					if n.Position+n.Length > maxTick {
						maxTick = n.Position + n.Length
					}
				}
				for _, n := range pt.Notes {
					i := int(uint64(n.Position) * uint64(bars) / uint64(maxTick))
					if i >= bars {
						i = bars - 1
					}
					density[i]++
				}
				maxD := 1
				for _, d := range density {
					if d > maxD {
						maxD = d
					}
				}
				bx := 480
				for i, d := range density {
					if d == 0 {
						continue
					}
					hh := (rowH - 8) * d / maxD
					if hh < 1 {
						hh = 1
					}
					c.FillRect(bx+i*3, cy+rowH-4-hh, 2, hh, pc)
				}
			}
		}
		cy += rowH
	}
}

func drawMixerView(c *wui.Canvas, s *vizState) {
	p := s.project
	contentY := vizHeaderH
	const rowH = 26

	offX := -s.scrollX

	cy := contentY + 10 - s.scrollY
	if cy+24 > contentY && cy < contentY+s.vpH {
		c.TextOut(offX+14, cy, fmt.Sprintf("%d mixer inserts", len(p.Inserts)), colText)
	}
	cy += 26

	for _, ins := range p.Inserts {
		if cy+rowH > contentY && cy < contentY+s.vpH {
			c.FillRect(offX, cy, s.contentW, rowH, colTrackBG)
			c.FillRect(offX, cy, 5, rowH, insertColorFor(p, ins.Index))
			c.Line(offX, cy+rowH-1, s.contentW, cy+rowH-1, colGrid)

			name := "(unnamed)"
			if ins.Name != nil && *ins.Name != "" {
				name = *ins.Name
			}
			c.TextOut(offX+12, cy+4, fmt.Sprintf("Insert %d", ins.Index), colTextDim)
			c.TextOut(offX+110, cy+4, truncate(name, 30), colText)

			sx := offX + 380
			for _, slot := range ins.Slots {
				label := "-"
				if slot.HasPlugin != nil && *slot.HasPlugin {
					switch {
					case slot.PluginVstName != nil:
						label = truncate(*slot.PluginVstName, 3)
					case slot.PluginName != nil:
						label = truncate(*slot.PluginName, 3)
					case slot.InternalName != nil:
						label = truncate(*slot.InternalName, 3)
					default:
						label = "*"
					}
				}
				bg := colTrackAlt
				if slot.HasPlugin != nil && *slot.HasPlugin {
					bg = insColor(ins.Index + slot.Index + 1)
				}
				c.FillRect(sx, cy+6, 22, rowH-12, bg)
				c.TextOut(sx+3, cy+6, label, colHeaderFG)
				sx += 24
			}
		}
		cy += rowH
	}
}

// ───────────────────────── viz helpers ─────────────────────────

func truncate(s string, n int) string {
	if n < 3 || len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func rgbaToWuiColor(c *flp.RGBA) (wui.Color, bool) {
	if c == nil {
		return wui.RGB(0, 0, 0), false
	}
	return wui.RGB(uint8(c.R), uint8(c.G), uint8(c.B)), true
}

func isBlackKey(k int) bool {
	switch k % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

func channelColor(kind string) wui.Color {
	switch kind {
	case "sampler":
		return wui.RGB(80, 160, 220)
	case "instrument":
		return wui.RGB(220, 140, 80)
	case "layer":
		return wui.RGB(180, 120, 200)
	case "automation":
		return wui.RGB(120, 200, 140)
	}
	return wui.RGB(140, 140, 140)
}

func patternColor(id int) wui.Color {
	h := float64(((id*137)%360)+360) / 60.0
	s := 0.65
	v := 0.90
	i := int(h)
	f := h - float64(i)
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return wui.RGB(uint8(r*255), uint8(g*255), uint8(b*255))
}

func insColor(idx int) wui.Color { return patternColor(idx + 7) }

func channelColorFor(p *flp.FLPProject, iid int) wui.Color {
	for _, ch := range p.Channels {
		if ch.Iid == iid {
			if col, ok := rgbaToWuiColor(ch.Color); ok {
				return col
			}
			return channelColor(string(ch.Kind))
		}
	}
	return channelColor("instrument")
}

func patternColorFor(p *flp.FLPProject, patternID int) wui.Color {
	for _, pt := range p.Patterns {
		if pt.ID == patternID {
			if col, ok := rgbaToWuiColor(pt.Color); ok {
				return col
			}
			break
		}
	}
	return patternColor(patternID)
}

func insertColorFor(p *flp.FLPProject, index int) wui.Color {
	for _, ins := range p.Inserts {
		if ins.Index == index {
			if col, ok := rgbaToWuiColor(ins.Color); ok {
				return col
			}
			break
		}
	}
	return insColor(index)
}

func noteColor(p *flp.FLPProject, channelIid int) wui.Color {
	for _, ch := range p.Channels {
		if ch.Iid == channelIid {
			if col, ok := rgbaToWuiColor(ch.Color); ok {
				return col
			}
			return channelColor(string(ch.Kind))
		}
	}
	return wui.RGB(150, 150, 150)
}

func clipColorFor(p *flp.FLPProject, cl flp.Clip) wui.Color {
	if cl.ItemIndex > 20480 {
		return patternColorFor(p, cl.ItemIndex-20480)
	}
	return channelColorFor(p, cl.ItemIndex)
}

func clipLabel(p *flp.FLPProject, cl flp.Clip) string {
	if cl.ItemIndex > 20480 {
		pid := cl.ItemIndex - 20480
		for _, pt := range p.Patterns {
			if pt.ID == pid {
				if pt.Name != nil && *pt.Name != "" {
					return *pt.Name
				}
				return fmt.Sprintf("Pattern %d", pid)
			}
		}
		return fmt.Sprintf("Pattern %d", pid)
	}
	for _, ch := range p.Channels {
		if ch.Iid == cl.ItemIndex {
			if ch.Name != nil && *ch.Name != "" {
				return *ch.Name
			}
			break
		}
	}
	return fmt.Sprintf("Clip %d", cl.ItemIndex)
}

// ───────────────────────── 5/6/7. Browsers ─────────────────────────

type browserRow struct {
	label   string
	details func() string
}

func openBrowser(name string, rows func(*flp.FLPProject) []browserRow) {
	if app.Project == nil {
		wui.MessageBoxInfo(name, "Please load a project first.")
		return
	}
	w := newModal(name)

	list := wui.NewStringList()
	if fontNormal != nil {
		list.SetFont(fontNormal)
	}
	w.Add(list)

	det := newOutput(0, 0, 100, 100)
	w.Add(det)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		y := margin
		bottomH := btnH + margin
		contentH := ih - y - bottomH
		if contentH < 80 {
			contentH = 80
		}

		listW := 280
		if listW > (iw-2*margin)/2 {
			listW = (iw - 2*margin) / 2
		}
		if listW < 150 {
			listW = 150
		}

		list.SetBounds(margin, y, listW, contentH)
		det.SetBounds(margin+listW+rowGap, y, iw-2*margin-listW-rowGap, contentH)
		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	data := rows(app.Project)
	items := make([]string, len(data))
	for i, r := range data {
		items[i] = r.label
	}
	list.SetItems(items)

	if len(items) > 0 {
		list.SetSelectedIndex(0)
		setText(det, data[0].details())
	} else {
		setText(det, "(empty)")
	}

	list.SetOnChange(func(i int) {
		if i >= 0 && i < len(data) {
			setText(det, data[i].details())
		}
	})

	showModal(w)
}

func openChannelTool(_ *wui.Window) {
	openBrowser("Channel Browser", func(p *flp.FLPProject) []browserRow {
		out := make([]browserRow, 0, len(p.Channels))
		for _, ch := range p.Channels {
			c := ch
			name := fmt.Sprintf("#%d", c.Iid)
			if c.Name != nil && *c.Name != "" {
				name = *c.Name
			}
			out = append(out, browserRow{
				label: name + " [" + string(c.Kind) + "]",
				details: func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Channel #%d\n", c.Iid)
					fmt.Fprintf(&b, "Kind: %s\n", c.Kind)
					if c.Name != nil && *c.Name != "" {
						fmt.Fprintf(&b, "Name: %s\n", *c.Name)
					}

					// ── Sample resolution ──
					if c.SamplePath != nil && *c.SamplePath != "" {
						raw := *c.SamplePath
						fmt.Fprintf(&b, "\nSample (raw): %s\n", raw)
						if app.Path != "" {
							if resolved := resolveSamplePath(app.Path, raw); resolved != "" {
								fmt.Fprintf(&b, "Resolved:    %s\n", resolved)
								if st, err := os.Stat(resolved); err == nil {
									fmt.Fprintf(&b, "Status:      found (%s)\n", formatBytes(st.Size()))
								}
								if pcm := loadSampleCached(resolved); len(pcm) > 0 {
									dur := float64(len(pcm)) / float64(synthSR)
									fmt.Fprintf(&b, "Duration:    %.3f s\n", dur)
									fmt.Fprintf(&b, "PCM samples: %d\n", len(pcm))
								}
							} else {
								fmt.Fprintf(&b, "Resolved:    (not found)\n")
								fmt.Fprintf(&b, "Status:      missing — checked FLP root, Samples/, Data/, Packs/, and absolute path\n")
							}
						}
					}

					if c.Plugin != nil {
						fmt.Fprintf(&b, "Plugin: %s\n", c.Plugin.InternalName)
						if c.Plugin.Name != nil && *c.Plugin.Name != "" {
							fmt.Fprintf(&b, "  Name:   %s\n", *c.Plugin.Name)
						}
						if c.Plugin.Vendor != nil && *c.Plugin.Vendor != "" {
							fmt.Fprintf(&b, "  Vendor: %s\n", *c.Plugin.Vendor)
						}
					}
					if c.Levels != nil {
						fmt.Fprintf(&b, "Levels: pan=%d volume=%d\n", c.Levels.Pan, c.Levels.Volume)
					}
					if c.TargetInsert != nil {
						fmt.Fprintf(&b, "Routed to insert: %d\n", *c.TargetInsert)
					}
					if c.Color != nil {
						fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n", c.Color.R, c.Color.G, c.Color.B, c.Color.A)
					}
					if c.AutomationTarget != nil {
						fmt.Fprintf(&b, "Automation target: %s\n", c.AutomationTarget.Kind)
					}
					if n := len(c.AutomationPoints); n > 0 {
						fmt.Fprintf(&b, "Automation points: %d\n", n)
					}
					return b.String()
				},
			})
		}
		return out
	})
}

func openPatternTool(_ *wui.Window) {
	openBrowser("Pattern Browser", func(p *flp.FLPProject) []browserRow {
		out := make([]browserRow, 0, len(p.Patterns))
		for _, pt := range p.Patterns {
			pat := pt
			name := fmt.Sprintf("#%d", pat.ID)
			if pat.Name != nil && *pat.Name != "" {
				name = *pat.Name
			}
			out = append(out, browserRow{
				label: fmt.Sprintf("%s  (%d notes)", name, len(pat.Notes)),
				details: func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Pattern #%d\n", pat.ID)
					if pat.Name != nil && *pat.Name != "" {
						fmt.Fprintf(&b, "Name: %s\n", *pat.Name)
					}
					if pat.Length != nil {
						fmt.Fprintf(&b, "Length: %d ticks\n", *pat.Length)
					}
					if pat.Looped != nil {
						fmt.Fprintf(&b, "Looped: %v\n", *pat.Looped)
					}
					fmt.Fprintf(&b, "Notes: %d\n", len(pat.Notes))
					fmt.Fprintf(&b, "Controllers: %d\n", len(pat.Controllers))
					if len(pat.Notes) > 0 {
						b.WriteString("\nFirst notes:\n")
						n := len(pat.Notes)
						if n > 20 {
							n = 20
						}
						for i := 0; i < n; i++ {
							note := pat.Notes[i]
							fmt.Fprintf(&b, "  pos=%-6d key=%-3d len=%-6d ch=%d vel=%d\n",
								note.Position, note.Key, note.Length, note.ChannelIid, note.Velocity)
						}
						if len(pat.Notes) > n {
							fmt.Fprintf(&b, "  ... and %d more\n", len(pat.Notes)-n)
						}
					}
					return b.String()
				},
			})
		}
		return out
	})
}

func openMixerTool(_ *wui.Window) {
	openBrowser("Mixer Browser", func(p *flp.FLPProject) []browserRow {
		out := make([]browserRow, 0, len(p.Inserts))
		for _, ins := range p.Inserts {
			i := ins
			name := "(unnamed)"
			if i.Name != nil && *i.Name != "" {
				name = *i.Name
			}
			out = append(out, browserRow{
				label: fmt.Sprintf("Insert %d  %s  (%d slots)", i.Index, name, len(i.Slots)),
				details: func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Insert %d\n", i.Index)
					if i.Name != nil && *i.Name != "" {
						fmt.Fprintf(&b, "Name: %s\n", *i.Name)
					}
					if i.Flags != nil {
						fmt.Fprintf(&b, "Flags: enabled=%v locked=%v solo=%v\n",
							i.Flags.Enabled, i.Flags.Locked, i.Flags.Solo)
					}
					if i.Pan != nil {
						fmt.Fprintf(&b, "Pan: %d\n", *i.Pan)
					}
					if i.Volume != nil {
						fmt.Fprintf(&b, "Volume: %d\n", *i.Volume)
					}
					if i.Color != nil {
						fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n", i.Color.R, i.Color.G, i.Color.B, i.Color.A)
					}
					fmt.Fprintf(&b, "Slots: %d\n", len(i.Slots))
					for _, sl := range i.Slots {
						lbl := "(empty)"
						if sl.HasPlugin != nil && *sl.HasPlugin {
							switch {
							case sl.PluginVstName != nil:
								lbl = *sl.PluginVstName
							case sl.PluginName != nil:
								lbl = *sl.PluginName
							case sl.InternalName != nil:
								lbl = *sl.InternalName
							default:
								lbl = "(unknown)"
							}
						}
						fmt.Fprintf(&b, "  Slot %d: %s\n", sl.Index, lbl)
					}
					return b.String()
				},
			})
		}
		return out
	})
}

// ───────────────────────── 8. Git ─────────────────────────

func openGitTool(_ *wui.Window) {
	w := newModal("Git Integration")

	b1 := newBtn("Setup (local)", 0, 0, 150, btnH, nil)
	w.Add(b1)
	b2 := newBtn("Setup (global)", 0, 0, 150, btnH, nil)
	w.Add(b2)
	b3 := newBtn("Setup (textconv)", 0, 0, 150, btnH, nil)
	w.Add(b3)
	b4 := newBtn("Verify", 0, 0, 122, btnH, nil)
	w.Add(b4)

	out := newOutput(0, 0, 100, 100)
	setText(out, "Choose an action. Setup writes git config for the current repository.")
	w.Add(out)

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		y := margin
		bw := (contentW - 3*rowInnerGap) / 4
		if bw < 90 {
			bw = 90
		}
		b1.SetBounds(margin, y, bw, btnH)
		b2.SetBounds(margin+bw+rowInnerGap, y, bw, btnH)
		b3.SetBounds(margin+2*(bw+rowInnerGap), y, bw, btnH)
		b4.SetBounds(margin+3*(bw+rowInnerGap), y, bw, btnH)
		y += btnH + rowGap + 6

		bottomH := btnH + margin
		outH := ih - y - bottomH
		if outH < 80 {
			outH = 80
		}
		out.SetBounds(margin, y, contentW, outH)

		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	runSetup := func(o flp.SetupOptions) {
		res, err := flp.SetupGit(o)
		if err != nil {
			setText(out, "Error: "+err.Error())
			return
		}
		setText(out, flp.RenderSetupRecap(res))
	}
	b1.SetOnClick(func() { runSetup(flp.SetupOptions{}) })
	b2.SetOnClick(func() { runSetup(flp.SetupOptions{Scope: flp.ScopeGlobal}) })
	b3.SetOnClick(func() { runSetup(flp.SetupOptions{Mode: flp.ModeTextconv}) })
	b4.SetOnClick(func() { setText(out, flp.RenderVerifyReport(flp.VerifyGit(""))) })

	showModal(w)
}

// ───────────────────────── 9. About ─────────────────────────

func openAboutTool(_ *wui.Window) {
	w := newModal("About")

	lines := []string{
		"FLP Tool",
		"Version " + version,
		"",
		"A small toolbox for FL Studio (.flp) project files:",
		"semantic diff, inspection, visualisation, editing, git setup.",
		"",
		"Built with the flp library and gonutz/wui.",
	}
	labels := make([]*wui.Label, len(lines))
	for i, l := range lines {
		lbl := wui.NewLabel()
		lbl.SetText(l)
		if i == 0 && fontTitle != nil {
			lbl.SetFont(fontTitle)
		} else if fontNormal != nil {
			lbl.SetFont(fontNormal)
		}
		w.Add(lbl)
		labels[i] = lbl
	}

	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		lineH := 24
		totalH := len(lines) * lineH
		startY := (ih - totalH) / 2
		if startY < margin {
			startY = margin
		}
		for i, lbl := range labels {
			lbl.SetBounds(margin*2, startY+i*lineH, iw-4*margin, lineH)
		}
		btnClose.SetBounds(iw-margin-closeBtnW, ih-margin-btnH, closeBtnW, btnH)
	})

	showModal(w)
}

// ───────────────────────── 10. Asset Viewer ─────────────────────────

func openAssetTool(_ *wui.Window) {
	if app.Project == nil {
		wui.MessageBoxInfo("Asset Viewer", "Please load a project first.")
		return
	}

	p := app.Project
	w := newModal("Asset Viewer - " + filepath.Base(app.Path))

	tree := wui.NewTreeView()
	w.Add(tree)

	lblTitle := wui.NewLabel()
	lblTitle.SetText("Select an item")
	if fontBold != nil {
		lblTitle.SetFont(fontBold)
	}
	w.Add(lblTitle)

	det := newOutput(0, 0, 100, 100)
	setText(det, "Select a node from the tree on the left to see its details.")
	w.Add(det)

	btnPrimary := newBtn("No Action", 0, 0, 160, btnH, nil)
	btnPrimary.SetEnabled(false)
	w.Add(btnPrimary)
	btnCopy := newBtn("Copy Details", 0, 0, 130, btnH, nil)
	w.Add(btnCopy)
	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() { w.Close() })
	w.Add(btnClose)

	applyLayout(w, func(iw, ih int) {
		contentW := iw - 2*margin
		y := margin
		bottomH := btnH + margin
		contentH := ih - y - bottomH
		if contentH < 80 {
			contentH = 80
		}
		treeW := contentW * 36 / 100
		if treeW < 200 {
			treeW = 200
		}
		if treeW > contentW-200 {
			treeW = contentW - 200
		}
		if treeW < 100 {
			treeW = 100
		}
		tree.SetBounds(margin, y, treeW, contentH)

		rightX := margin + treeW + rowGap
		rightW := iw - margin - rightX
		if rightW < 100 {
			rightW = 100
		}
		lblTitle.SetBounds(rightX, y, rightW, 24)
		det.SetBounds(rightX, y+28, rightW, contentH-28)

		barY := ih - margin - btnH
		btnPrimary.SetBounds(margin, barY, 160, btnH)
		btnCopy.SetBounds(margin+160+rowInnerGap, barY, 130, btnH)
		btnClose.SetBounds(iw-margin-closeBtnW, barY, closeBtnW, btnH)
	})

	// ── Node metadata ────────────────────────────────────────────────
	type nodeAction struct {
		label string
		run   func()
	}
	type nodeData struct {
		title  string
		detail string
		action *nodeAction
	}

	data := map[*wui.TreeNode]nodeData{}

	addNode := func(parent *wui.TreeNode, text, title, detail string, act *nodeAction) *wui.TreeNode {
		var n *wui.TreeNode
		if parent == nil {
			n = tree.Add(text)
		} else {
			n = parent.Add(text)
		}
		data[n] = nodeData{title: title, detail: detail, action: act}
		return n
	}

	// ── Project root ─────────────────────────────────────────────────
	projDetail := func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "File: %s\n", app.Path)
		fmt.Fprintf(&b, "PPQ: %d\n", p.Header.PPQ)
		if t := flp.GetTempo(p); t != nil {
			fmt.Fprintf(&b, "Tempo: %.2f BPM\n", *t)
		}
		if p.Metadata.TimeSignatureNumerator != nil && p.Metadata.TimeSignatureDenominator != nil {
			fmt.Fprintf(&b, "Time signature: %d/%d\n",
				*p.Metadata.TimeSignatureNumerator, *p.Metadata.TimeSignatureDenominator)
		}

		// ── timestamps ──
		b.WriteString("\n")
		b.WriteString(timestampsText(p, app.Path))
		b.WriteString("\n")

		fmt.Fprintf(&b, "\nChannels:       %d\n", len(p.Channels))
		fmt.Fprintf(&b, "Patterns:       %d\n", len(p.Patterns))
		fmt.Fprintf(&b, "Mixer inserts:  %d\n", len(p.Inserts))
		fmt.Fprintf(&b, "Arrangements:   %d\n", len(p.Arrangements))
		return b.String()
	}
	rootNode := addNode(nil, "Project", "Project", projDetail(), nil)

	// ── Channels (with sample + plugin sub-nodes) ────────────────────
	chCat := addNode(rootNode, fmt.Sprintf("Channels (%d)", len(p.Channels)),
		"Channels", "All channels in the project.", nil)
	for _, ch := range p.Channels {
		c := ch
		name := fmt.Sprintf("#%d", c.Iid)
		if c.Name != nil && *c.Name != "" {
			name = *c.Name
		}
		cdetail := func() string {
			var b strings.Builder
			fmt.Fprintf(&b, "Channel #%d\n", c.Iid)
			fmt.Fprintf(&b, "Kind: %s\n", c.Kind)
			if c.Name != nil {
				fmt.Fprintf(&b, "Name: %s\n", *c.Name)
			}
			if c.SamplePath != nil && *c.SamplePath != "" {
				fmt.Fprintf(&b, "Sample path: %s\n", *c.SamplePath)
			}
			if c.Plugin != nil {
				fmt.Fprintf(&b, "Plugin internal name: %s\n", c.Plugin.InternalName)
				if c.Plugin.Name != nil {
					fmt.Fprintf(&b, "Plugin name: %s\n", *c.Plugin.Name)
				}
				if c.Plugin.Vendor != nil {
					fmt.Fprintf(&b, "Vendor: %s\n", *c.Plugin.Vendor)
				}
			}
			if c.Levels != nil {
				fmt.Fprintf(&b, "Volume: %d\n", c.Levels.Volume)
				fmt.Fprintf(&b, "Pan: %d\n", c.Levels.Pan)
			}
			if c.TargetInsert != nil {
				fmt.Fprintf(&b, "Routed to insert: %d\n", *c.TargetInsert)
			}
			if c.Color != nil {
				fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n",
					c.Color.R, c.Color.G, c.Color.B, c.Color.A)
			}
			if c.AutomationTarget != nil {
				fmt.Fprintf(&b, "Automation target: %s\n", c.AutomationTarget.Kind)
			}
			if n := len(c.AutomationPoints); n > 0 {
				fmt.Fprintf(&b, "Automation points: %d\n", n)
			}
			return b.String()
		}
		var act *nodeAction
		if c.SamplePath != nil && *c.SamplePath != "" {
			sp := *c.SamplePath
			act = &nodeAction{label: "Copy Sample Path", run: func() {
				wui.SetClipboardText(sp)
			}}
		}
		chNode := addNode(chCat, fmt.Sprintf("%s [%s]", name, c.Kind),
			name, cdetail(), act)

		// Sample sub-node
		if c.SamplePath != nil && *c.SamplePath != "" {
			sp := *c.SamplePath
			sdetail := func() string {
				var b strings.Builder
				fmt.Fprintf(&b, "Sample path:\n%s\n\n", sp)
				fmt.Fprintf(&b, "File:    %s\n", filepath.Base(sp))
				fmt.Fprintf(&b, "Dir:     %s\n", filepath.Dir(sp))
				if st, err := os.Stat(sp); err == nil {
					fmt.Fprintf(&b, "Exists:  yes\n")
					fmt.Fprintf(&b, "Size:    %d bytes\n", st.Size())
					fmt.Fprintf(&b, "ModTime: %s\n", st.ModTime().Format("2006-01-02 15:04:05"))
				} else {
					fmt.Fprintf(&b, "Exists:  no (%v)\n", err)
				}
				return b.String()
			}
			sact := &nodeAction{label: "Copy Sample Path", run: func() {
				wui.SetClipboardText(sp)
			}}
			addNode(chNode, "Sample: "+filepath.Base(sp), "Sample", sdetail(), sact)
		}

		// Plugin sub-node
		if c.Plugin != nil {
			plug := c.Plugin
			pdetail := func() string {
				var b strings.Builder
				fmt.Fprintf(&b, "Internal name: %s\n", plug.InternalName)
				if plug.Name != nil {
					fmt.Fprintf(&b, "Name:          %s\n", *plug.Name)
				}
				if plug.Vendor != nil {
					fmt.Fprintf(&b, "Vendor:        %s\n", *plug.Vendor)
				}
				return b.String()
			}
			addNode(chNode, "Plugin: "+plug.InternalName, "Plugin", pdetail(), nil)
		}
	}

	// ── Patterns ─────────────────────────────────────────────────────
	patCat := addNode(rootNode, fmt.Sprintf("Patterns (%d)", len(p.Patterns)),
		"Patterns", "All patterns in the project.", nil)
	for _, pt := range p.Patterns {
		pat := pt
		name := fmt.Sprintf("#%d", pat.ID)
		if pat.Name != nil && *pat.Name != "" {
			name = *pat.Name
		}
		pdetail := func() string {
			var b strings.Builder
			fmt.Fprintf(&b, "Pattern ID: %d\n", pat.ID)
			if pat.Name != nil {
				fmt.Fprintf(&b, "Name: %s\n", *pat.Name)
			}
			if pat.Length != nil {
				fmt.Fprintf(&b, "Length: %d ticks\n", *pat.Length)
			}
			if pat.Looped != nil {
				fmt.Fprintf(&b, "Looped: %v\n", *pat.Looped)
			}
			fmt.Fprintf(&b, "Notes: %d\n", len(pat.Notes))
			fmt.Fprintf(&b, "Controllers: %d\n", len(pat.Controllers))
			if pat.Color != nil {
				fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n",
					pat.Color.R, pat.Color.G, pat.Color.B, pat.Color.A)
			}
			if len(pat.Notes) > 0 {
				b.WriteString("\nFirst notes:\n")
				n := len(pat.Notes)
				if n > 16 {
					n = 16
				}
				for i := 0; i < n; i++ {
					note := pat.Notes[i]
					fmt.Fprintf(&b, "  pos=%-6d key=%-3d len=%-6d ch=%d vel=%d\n",
						note.Position, note.Key, note.Length, note.ChannelIid, note.Velocity)
				}
				if len(pat.Notes) > n {
					fmt.Fprintf(&b, "  ... and %d more\n", len(pat.Notes)-n)
				}
			}
			return b.String()
		}
		addNode(patCat, fmt.Sprintf("%d: %s (%d notes)", pat.ID, name, len(pat.Notes)),
			name, pdetail(), nil)
	}

	// ── Mixer inserts ────────────────────────────────────────────────
	mixCat := addNode(rootNode, fmt.Sprintf("Mixer Inserts (%d)", len(p.Inserts)),
		"Mixer Inserts", "All mixer inserts and their slots.", nil)
	for _, ins := range p.Inserts {
		i := ins
		name := "(unnamed)"
		if i.Name != nil && *i.Name != "" {
			name = *i.Name
		}
		idetail := func() string {
			var b strings.Builder
			fmt.Fprintf(&b, "Insert index: %d\n", i.Index)
			if i.Name != nil {
				fmt.Fprintf(&b, "Name: %s\n", *i.Name)
			}
			if i.Volume != nil {
				fmt.Fprintf(&b, "Volume: %d\n", *i.Volume)
			}
			if i.Pan != nil {
				fmt.Fprintf(&b, "Pan: %d\n", *i.Pan)
			}
			if i.Flags != nil {
				fmt.Fprintf(&b, "Enabled: %v\n", i.Flags.Enabled)
				fmt.Fprintf(&b, "Locked:  %v\n", i.Flags.Locked)
				fmt.Fprintf(&b, "Solo:    %v\n", i.Flags.Solo)
			}
			if i.Color != nil {
				fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n",
					i.Color.R, i.Color.G, i.Color.B, i.Color.A)
			}
			fmt.Fprintf(&b, "Slots: %d\n", len(i.Slots))
			return b.String()
		}
		insNode := addNode(mixCat, fmt.Sprintf("%d: %s", i.Index, name),
			name, idetail(), nil)
		for _, sl := range i.Slots {
			slot := sl
			lbl := "(empty)"
			if slot.HasPlugin != nil && *slot.HasPlugin {
				switch {
				case slot.PluginVstName != nil:
					lbl = *slot.PluginVstName
				case slot.PluginName != nil:
					lbl = *slot.PluginName
				case slot.InternalName != nil:
					lbl = *slot.InternalName
				default:
					lbl = "(unknown plugin)"
				}
			}
			sdetail := func() string {
				var b strings.Builder
				fmt.Fprintf(&b, "Slot index: %d\n", slot.Index)
				if slot.HasPlugin != nil {
					fmt.Fprintf(&b, "Has plugin: %v\n", *slot.HasPlugin)
				}
				if slot.InternalName != nil {
					fmt.Fprintf(&b, "Internal name: %s\n", *slot.InternalName)
				}
				if slot.PluginName != nil {
					fmt.Fprintf(&b, "Plugin name: %s\n", *slot.PluginName)
				}
				if slot.PluginVstName != nil {
					fmt.Fprintf(&b, "VST name: %s\n", *slot.PluginVstName)
				}
				return b.String()
			}
			addNode(insNode, fmt.Sprintf("Slot %d: %s", slot.Index, lbl),
				"Slot "+lbl, sdetail(), nil)
		}
	}

	// ── Arrangements / tracks / clips ────────────────────────────────
	arrCat := addNode(rootNode, fmt.Sprintf("Arrangements (%d)", len(p.Arrangements)),
		"Arrangements", "All arrangements, tracks and clips.", nil)
	for _, a := range p.Arrangements {
		arr := a
		name := fmt.Sprintf("#%d", arr.ID)
		if arr.Name != nil && *arr.Name != "" {
			name = *arr.Name
		}
		adetail := func() string {
			var b strings.Builder
			fmt.Fprintf(&b, "Arrangement ID: %d\n", arr.ID)
			if arr.Name != nil {
				fmt.Fprintf(&b, "Name: %s\n", *arr.Name)
			}
			fmt.Fprintf(&b, "Tracks: %d\n", len(arr.Tracks))
			fmt.Fprintf(&b, "Clips:  %d\n", len(arr.Clips))
			return b.String()
		}
		arrNode := addNode(arrCat, fmt.Sprintf("%d: %s", arr.ID, name),
			name, adetail(), nil)

		byTrack := map[int][]flp.Clip{}
		for _, cl := range arr.Clips {
			idx := 499 - cl.TrackRvidx
			byTrack[idx] = append(byTrack[idx], cl)
		}

		for ti, tr := range arr.Tracks {
			track := tr
			ti := ti
			tname := fmt.Sprintf("Track %d", ti+1)
			if track.Name != nil && *track.Name != "" {
				tname = *track.Name
			}
			tdetail := func() string {
				var b strings.Builder
				fmt.Fprintf(&b, "Track index: %d\n", ti)
				if track.Name != nil {
					fmt.Fprintf(&b, "Name: %s\n", *track.Name)
				}
				if track.Grouped != nil {
					fmt.Fprintf(&b, "Grouped: %v\n", *track.Grouped)
				}
				if track.Color != nil {
					fmt.Fprintf(&b, "Color: rgba(%d,%d,%d,%d)\n",
						track.Color.R, track.Color.G, track.Color.B, track.Color.A)
				}
				return b.String()
			}
			trNode := addNode(arrNode, tname, tname, tdetail(), nil)

			for _, cl := range byTrack[ti] {
				clip := cl
				clbl := clipLabel(p, clip)
				cdetail := func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "Clip: %s\n", clbl)
					fmt.Fprintf(&b, "Position: %d ticks\n", clip.Position)
					fmt.Fprintf(&b, "Length: %d ticks\n", clip.Length)
					fmt.Fprintf(&b, "Item index: %d\n", clip.ItemIndex)
					fmt.Fprintf(&b, "Track row index: %d\n", 499-clip.TrackRvidx)
					return b.String()
				}
				addNode(trNode, clbl, clbl, cdetail(), nil)
			}
		}
	}

	// ── Selection wiring ─────────────────────────────────────────────
	var current nodeData
	tree.SetOnSelect(func(n *wui.TreeNode) {
		d, ok := data[n]
		if !ok {
			current = nodeData{}
			lblTitle.SetText(n.Text())
			setText(det, "")
			btnPrimary.SetText("No Action")
			btnPrimary.SetEnabled(false)
			return
		}
		current = d
		lblTitle.SetText(d.title)
		setText(det, d.detail)
		if d.action != nil {
			btnPrimary.SetText(d.action.label)
			btnPrimary.SetEnabled(true)
		} else {
			btnPrimary.SetText("No Action")
			btnPrimary.SetEnabled(false)
		}
	})

	btnPrimary.SetOnClick(func() {
		if current.action != nil {
			current.action.run()
		}
	})
	btnCopy.SetOnClick(func() {
		if current.detail != "" {
			wui.SetClipboardText(current.detail)
		}
	})

	showModal(w)
}

// ───────────── synth GUI helpers ─────────────

type ctrlAdder interface {
	Add(c wui.Control)
}

func addSynthHeader(parent ctrlAdder, x, y, w int, text string) {
	lbl := wui.NewLabel()
	lbl.SetText(text)
	lbl.SetBounds(x, y, w, labelH)
	if fontBold != nil {
		lbl.SetFont(fontBold)
	}
	parent.Add(lbl)
}

func addSynthFloat(parent ctrlAdder, x, y, labelW, editW int, label string,
	min, max float64, prec int, initial float64, onChange func(float64)) *wui.FloatUpDown {
	lbl := wui.NewLabel()
	lbl.SetText(label)
	lbl.SetBounds(x, y+4, labelW, labelH)
	if fontNormal != nil {
		lbl.SetFont(fontNormal)
	}
	parent.Add(lbl)

	up := wui.NewFloatUpDown()
	up.SetBounds(x+labelW, y, editW, editH)
	up.SetMinMax(min, max)
	up.SetPrecision(prec)
	up.SetValue(initial)
	up.SetOnValueChange(onChange)
	parent.Add(up)
	return up
}

func addSynthInt(parent ctrlAdder, x, y, labelW, editW int, label string,
	min, max, initial int, onChange func(int)) *wui.IntUpDown {
	lbl := wui.NewLabel()
	lbl.SetText(label)
	lbl.SetBounds(x, y+4, labelW, labelH)
	if fontNormal != nil {
		lbl.SetFont(fontNormal)
	}
	parent.Add(lbl)

	up := wui.NewIntUpDown()
	up.SetBounds(x+labelW, y, editW, editH)
	up.SetMinMax(min, max)
	up.SetValue(initial)
	up.SetOnValueChange(onChange)
	parent.Add(up)
	return up
}

func addSynthCombo(parent ctrlAdder, x, y, labelW, editW int, label string,
	items []string, selected int, onChange func(int)) *wui.ComboBox {
	lbl := wui.NewLabel()
	lbl.SetText(label)
	lbl.SetBounds(x, y+4, labelW, labelH)
	if fontNormal != nil {
		lbl.SetFont(fontNormal)
	}
	parent.Add(lbl)

	cmb := wui.NewComboBox()
	cmb.SetBounds(x+labelW, y, editW, editH)
	cmb.SetItems(items)
	if selected >= 0 && selected < len(items) {
		cmb.SetSelectedIndex(selected)
	} else {
		cmb.SetSelectedIndex(0)
	}
	cmb.SetOnChange(onChange)
	if fontNormal != nil {
		cmb.SetFont(fontNormal)
	}
	parent.Add(cmb)
	return cmb
}

// ───────────────────────── 11. Synthesizer Settings ─────────────────────────

func openSynthSettingsTool(_ *wui.Window) {
	w := newModal("Synthesizer")
	w.SetOnClose(persistSynth)

	initial := globalSynth.Snapshot()

	// ── Preview cache (invalidated on any change) ──
	var (
		previewBufCache []float64
		previewCfg      SynthOptions
		previewValid    bool
	)
	getPreview := func() []float64 {
		cfg := globalSynth.Snapshot()
		if !previewValid || cfg != previewCfg {
			previewBufCache = previewBuf(cfg, 1.0)
			previewCfg = cfg
			previewValid = true
		}
		return previewBufCache
	}

	// ── Preview paint box ──
	prevPb := wui.NewPaintBox()
	w.Add(prevPb)
	prevPb.SetOnPaint(func(c *wui.Canvas) {
		cw, ch := c.Size()
		c.FillRect(0, 0, cw, ch, wui.RGB(22, 26, 32))
		if cw < 2 || ch < 8 {
			return
		}
		centerY := ch / 2
		c.Line(0, centerY, cw, centerY, wui.RGB(46, 52, 62))
		c.Line(0, centerY-ch/4, cw, centerY-ch/4, wui.RGB(34, 40, 48))
		c.Line(0, centerY+ch/4, cw, centerY+ch/4, wui.RGB(34, 40, 48))

		buf := getPreview()
		if len(buf) == 0 {
			return
		}
		scale := float64(ch) / 2 * 0.92
		samplesPerCol := float64(len(buf)) / float64(cw)
		if samplesPerCol < 1 {
			samplesPerCol = 1
		}
		fill := wui.RGB(70, 180, 110)
		edge := wui.RGB(120, 230, 160)
		for x := 0; x < cw; x++ {
			start := int(float64(x) * samplesPerCol)
			end := int(float64(x+1) * samplesPerCol)
			if start >= len(buf) {
				break
			}
			if end > len(buf) {
				end = len(buf)
			}
			mn, mx := 0.0, 0.0
			for i := start; i < end; i++ {
				v := buf[i]
				if v < mn {
					mn = v
				}
				if v > mx {
					mx = v
				}
			}
			y0 := centerY - int(mx*scale)
			y1 := centerY - int(mn*scale)
			if y1 <= y0 {
				y1 = y0 + 1
			}
			c.Line(x, y0, x, y1, fill)
			c.Line(x, y0, x, y0, edge)
		}
		c.Line(0, ch-1, cw, ch-1, wui.RGB(50, 58, 68))
	})

	// ── Widget set ──
	type widgetSet struct {
		wave1, wave2                  *wui.ComboBox
		osc2Detune, osc2Mix, subLevel *wui.FloatUpDown
		unisonVoices                  *wui.IntUpDown
		unisonDetune                  *wui.FloatUpDown

		filtType                         *wui.ComboBox
		filtCutoff, filtReso             *wui.FloatUpDown
		filtEnvAmt, filtKeyTrack         *wui.FloatUpDown

		aA, aD, aS, aR *wui.FloatUpDown
		fA, fD, fS, fR *wui.FloatUpDown

		lfoShape, lfoTarget         *wui.ComboBox
		lfoRate, lfoDepth, lfoPitch *wui.FloatUpDown

		drive                    *wui.FloatUpDown
		delTime, delFeed, delMix *wui.FloatUpDown
		revSize, revMix          *wui.FloatUpDown

		gain, velAmp, velFilt *wui.FloatUpDown

		sampleAttack, sampleDecay, sampleSustain *wui.FloatUpDown
		sampleRelease, sampleVolume, samplePitch *wui.FloatUpDown
	}
	var ws widgetSet

	sync := func() {
		globalSynth.Update(func(o *SynthOptions) {
			o.Waveform1 = ws.wave1.Items()[ws.wave1.SelectedIndex()]
			o.Waveform2 = ws.wave2.Items()[ws.wave2.SelectedIndex()]
			o.Osc2Detune = ws.osc2Detune.Value()
			o.Osc2Mix = ws.osc2Mix.Value()
			o.SubLevel = ws.subLevel.Value()
			o.UnisonVoices = ws.unisonVoices.Value()
			o.UnisonDetune = ws.unisonDetune.Value()

			o.FilterType = ws.filtType.Items()[ws.filtType.SelectedIndex()]
			o.FilterCutoff = ws.filtCutoff.Value()
			o.FilterReso = ws.filtReso.Value()
			o.FilterEnvAmt = ws.filtEnvAmt.Value()
			o.FilterKeyTrack = ws.filtKeyTrack.Value()

			o.Attack = ws.aA.Value()
			o.Decay = ws.aD.Value()
			o.Sustain = ws.aS.Value()
			o.Release = ws.aR.Value()
			o.FAttack = ws.fA.Value()
			o.FDecay = ws.fD.Value()
			o.FSustain = ws.fS.Value()
			o.FRelease = ws.fR.Value()

			o.LFOShape = ws.lfoShape.Items()[ws.lfoShape.SelectedIndex()]
			o.LFOTarget = ws.lfoTarget.Items()[ws.lfoTarget.SelectedIndex()]
			o.LFORate = ws.lfoRate.Value()
			o.LFODepth = ws.lfoDepth.Value()
			o.LFOPitch = ws.lfoPitch.Value()

			o.Drive = ws.drive.Value()
			o.DelayTime = ws.delTime.Value()
			o.DelayFeed = ws.delFeed.Value()
			o.DelayMix = ws.delMix.Value()
			o.ReverbSize = ws.revSize.Value()
			o.ReverbMix = ws.revMix.Value()

			o.Gain = ws.gain.Value()
			o.VelToAmp = ws.velAmp.Value()
			o.VelToFilt = ws.velFilt.Value()

			// Sample
			o.SampleAttack = ws.sampleAttack.Value()
			o.SampleDecay = ws.sampleDecay.Value()
			o.SampleSustain = ws.sampleSustain.Value()
			o.SampleRelease = ws.sampleRelease.Value()
			o.SampleVolume = ws.sampleVolume.Value()
			o.SamplePitch = ws.samplePitch.Value()
		})
		previewValid = false
		prevPb.Paint()
	}

	// ── Tab control ──
	tabs := wui.NewTabControl()
	w.Add(tabs)
	tabs.AddTab("Oscillator")
	tabs.AddTab("Filter")
	tabs.AddTab("Envelopes")
	tabs.AddTab("LFO")
	tabs.AddTab("FX")
	tabs.AddTab("Sample")

	var panels []*wui.Panel
	for i := 0; i < 6; i++ {
		p := wui.NewPanel()
		p.SetVisible(i == 0)
		w.Add(p)
		panels = append(panels, p)
	}

	// Compact row pitch used inside every panel.
	const rowH = 32

	// ── Oscillator panel: 2 cols × 4 rows ──
	{
		p := panels[0]
		waveItems := []string{"sine", "square", "saw", "triangle", "noise"}
		wave2Items := []string{"off", "sine", "square", "saw", "triangle", "noise"}

		ws.wave1 = addSynthCombo(p, 15, 8, 90, 130, "Wave 1", waveItems,
			indexOf(waveItems, initial.Waveform1), func(int) { sync() })
		ws.wave2 = addSynthCombo(p, 15, 8+rowH, 90, 130, "Wave 2", wave2Items,
			indexOf(wave2Items, initial.Waveform2), func(int) { sync() })
		ws.osc2Detune = addSynthFloat(p, 15, 8+2*rowH, 90, 130, "Detune (st)",
			-24, 24, 2, initial.Osc2Detune, func(float64) { sync() })
		ws.osc2Mix = addSynthFloat(p, 15, 8+3*rowH, 90, 130, "Osc2 mix",
			0, 1, 3, initial.Osc2Mix, func(float64) { sync() })

		ws.subLevel = addSynthFloat(p, 340, 8, 90, 130, "Sub level",
			0, 1, 3, initial.SubLevel, func(float64) { sync() })
		ws.unisonVoices = addSynthInt(p, 340, 8+rowH, 90, 130, "Unison v.",
			1, 7, initial.UnisonVoices, func(int) { sync() })
		ws.unisonDetune = addSynthFloat(p, 340, 8+2*rowH, 90, 130, "U. detune (c)",
			0, 50, 1, initial.UnisonDetune, func(float64) { sync() })
	}

	// ── Filter panel ──
	{
		p := panels[1]
		ftypes := []string{"off", "lp", "hp", "bp", "notch"}

		ws.filtType = addSynthCombo(p, 15, 8, 90, 130, "Type", ftypes,
			indexOf(ftypes, initial.FilterType), func(int) { sync() })
		ws.filtCutoff = addSynthFloat(p, 15, 8+rowH, 90, 130, "Cutoff (Hz)",
			20, 18000, 1, initial.FilterCutoff, func(float64) { sync() })
		ws.filtReso = addSynthFloat(p, 15, 8+2*rowH, 90, 130, "Resonance (Q)",
			0.5, 20, 2, initial.FilterReso, func(float64) { sync() })

		ws.filtEnvAmt = addSynthFloat(p, 340, 8, 90, 130, "Env amt (oct)",
			-4, 4, 2, initial.FilterEnvAmt, func(float64) { sync() })
		ws.filtKeyTrack = addSynthFloat(p, 340, 8+rowH, 90, 130, "Key track",
			0, 1, 3, initial.FilterKeyTrack, func(float64) { sync() })
	}

	// ── Envelopes panel: two 4-knob rows ──
	{
		p := panels[2]

		addSynthHeader(p, 15, 4, 300, "Amplitude envelope")
		ws.aA = addSynthFloat(p, 15, 26, 30, 60, "A", 0.001, 4, 3, initial.Attack, func(float64) { sync() })
		ws.aD = addSynthFloat(p, 130, 26, 30, 60, "D", 0.001, 4, 3, initial.Decay, func(float64) { sync() })
		ws.aS = addSynthFloat(p, 245, 26, 30, 60, "S", 0, 1, 3, initial.Sustain, func(float64) { sync() })
		ws.aR = addSynthFloat(p, 360, 26, 30, 60, "R", 0.001, 4, 3, initial.Release, func(float64) { sync() })

		addSynthHeader(p, 15, 70, 300, "Filter envelope")
		ws.fA = addSynthFloat(p, 15, 92, 30, 60, "A", 0.001, 4, 3, initial.FAttack, func(float64) { sync() })
		ws.fD = addSynthFloat(p, 130, 92, 30, 60, "D", 0.001, 4, 3, initial.FDecay, func(float64) { sync() })
		ws.fS = addSynthFloat(p, 245, 92, 30, 60, "S", 0, 1, 3, initial.FSustain, func(float64) { sync() })
		ws.fR = addSynthFloat(p, 360, 92, 30, 60, "R", 0.001, 4, 3, initial.FRelease, func(float64) { sync() })
	}

	// ── LFO panel ──
	{
		p := panels[3]
		lfoShapes := []string{"off", "sine", "tri", "square", "saw"}
		targets := []string{"off", "amp", "filter"}

		ws.lfoShape = addSynthCombo(p, 15, 8, 90, 130, "Shape", lfoShapes,
			indexOf(lfoShapes, initial.LFOShape), func(int) { sync() })
		ws.lfoRate = addSynthFloat(p, 15, 8+rowH, 90, 130, "Rate (Hz)",
			0.05, 20, 2, initial.LFORate, func(float64) { sync() })
		ws.lfoDepth = addSynthFloat(p, 15, 8+2*rowH, 90, 130, "Depth",
			0, 1, 3, initial.LFODepth, func(float64) { sync() })

		ws.lfoTarget = addSynthCombo(p, 340, 8, 90, 130, "Target", targets,
			indexOf(targets, initial.LFOTarget), func(int) { sync() })
		ws.lfoPitch = addSynthFloat(p, 340, 8+rowH, 90, 130, "Pitch (st)",
			0, 12, 2, initial.LFOPitch, func(float64) { sync() })
	}

	// ── FX panel: drive / delay / reverb stacked ──
	{
		p := panels[4]

		addSynthHeader(p, 15, 4, 200, "Drive")
		ws.drive = addSynthFloat(p, 15, 24, 60, 100, "", 0, 1, 3, initial.Drive, func(float64) { sync() })

		addSynthHeader(p, 15, 58, 300, "Delay")
		ws.delTime = addSynthFloat(p, 15, 78, 55, 70, "Time", 0.01, 1.0, 3, initial.DelayTime, func(float64) { sync() })
		ws.delFeed = addSynthFloat(p, 210, 78, 55, 70, "FB", 0, 0.9, 2, initial.DelayFeed, func(float64) { sync() })
		ws.delMix = addSynthFloat(p, 405, 78, 55, 70, "Mix", 0, 1, 3, initial.DelayMix, func(float64) { sync() })

		addSynthHeader(p, 15, 112, 300, "Reverb")
		ws.revSize = addSynthFloat(p, 15, 132, 55, 70, "Size", 0, 1, 3, initial.ReverbSize, func(float64) { sync() })
		ws.revMix = addSynthFloat(p, 210, 132, 55, 70, "Mix", 0, 1, 3, initial.ReverbMix, func(float64) { sync() })
	}

	// ── Sample panel: envelope | output | user data ──
	{
		p := panels[5]

		// Left column — Sample envelope
		addSynthHeader(p, 15, 4, 180, "Sample envelope")
		ws.sampleAttack = addSynthFloat(p, 15, 26, 45, 65, "A", 0, 0.5, 4, initial.SampleAttack, func(float64) { sync() })
		ws.sampleDecay = addSynthFloat(p, 15, 26+rowH, 45, 65, "D", 0, 0.5, 4, initial.SampleDecay, func(float64) { sync() })
		ws.sampleSustain = addSynthFloat(p, 15, 26+2*rowH, 45, 65, "S", 0, 1, 3, initial.SampleSustain, func(float64) { sync() })
		ws.sampleRelease = addSynthFloat(p, 15, 26+3*rowH, 45, 65, "R", 0, 0.5, 4, initial.SampleRelease, func(float64) { sync() })

		// Middle column — Output
		addSynthHeader(p, 165, 4, 130, "Output")
		ws.sampleVolume = addSynthFloat(p, 165, 26, 45, 65, "Vol", 0, 2, 3, initial.SampleVolume, func(float64) { sync() })
		ws.samplePitch = addSynthFloat(p, 165, 26+rowH, 45, 65, "Pitch", -24, 24, 2, initial.SamplePitch, func(float64) { sync() })

		// Right column — FL Studio user data
		addSynthHeader(p, 315, 4, 330, "FL Studio user data")
		userLbl := newLabel("Path:", 315, 30, 40, labelH)
		p.Add(userLbl)

		userEdit := wui.NewEditLine()
		userEdit.SetBounds(355, 28, 260, editH)
		userEdit.SetText(appConfig.FLStudioUserData)
		userEdit.SetOnTextChange(func() {
			appConfig.FLStudioUserData = userEdit.Text()
			saveAppConfig()
		})
		if fontNormal != nil {
			userEdit.SetFont(fontNormal)
		}
		p.Add(userEdit)

		userBrowse := newBtn("Browse...", 315, 28+rowH, 100, editH, func() {
			dlg := wui.NewFolderSelectDialog()
			dlg.SetTitle("Select FL Studio user data folder")
			if ok, path := dlg.Execute(w); ok && path != "" {
				appConfig.FLStudioUserData = path
				userEdit.SetText(path)
				saveAppConfig()
			}
		})
		p.Add(userBrowse)

		userInfo := newLabel(
			"Used to resolve %FLStudioUserData% paths and\n"+
				"as a fallback root for relative samples.",
			315, 28+2*rowH, 300, 40)
		p.Add(userInfo)
	}

	// ── applyOptsToWidgets pushes a SynthOptions into all widgets. ──
	applyOptsToWidgets := func(o SynthOptions) {
		ws.wave1.SetSelectedIndex(indexOf(ws.wave1.Items(), o.Waveform1))
		ws.wave2.SetSelectedIndex(indexOf(ws.wave2.Items(), o.Waveform2))
		ws.osc2Detune.SetValue(o.Osc2Detune)
		ws.osc2Mix.SetValue(o.Osc2Mix)
		ws.subLevel.SetValue(o.SubLevel)
		ws.unisonVoices.SetValue(o.UnisonVoices)
		ws.unisonDetune.SetValue(o.UnisonDetune)

		ws.filtType.SetSelectedIndex(indexOf(ws.filtType.Items(), o.FilterType))
		ws.filtCutoff.SetValue(o.FilterCutoff)
		ws.filtReso.SetValue(o.FilterReso)
		ws.filtEnvAmt.SetValue(o.FilterEnvAmt)
		ws.filtKeyTrack.SetValue(o.FilterKeyTrack)

		ws.aA.SetValue(o.Attack)
		ws.aD.SetValue(o.Decay)
		ws.aS.SetValue(o.Sustain)
		ws.aR.SetValue(o.Release)
		ws.fA.SetValue(o.FAttack)
		ws.fD.SetValue(o.FDecay)
		ws.fS.SetValue(o.FSustain)
		ws.fR.SetValue(o.FRelease)

		ws.lfoShape.SetSelectedIndex(indexOf(ws.lfoShape.Items(), o.LFOShape))
		ws.lfoRate.SetValue(o.LFORate)
		ws.lfoDepth.SetValue(o.LFODepth)
		ws.lfoTarget.SetSelectedIndex(indexOf(ws.lfoTarget.Items(), o.LFOTarget))
		ws.lfoPitch.SetValue(o.LFOPitch)

		ws.drive.SetValue(o.Drive)
		ws.delTime.SetValue(o.DelayTime)
		ws.delFeed.SetValue(o.DelayFeed)
		ws.delMix.SetValue(o.DelayMix)
		ws.revSize.SetValue(o.ReverbSize)
		ws.revMix.SetValue(o.ReverbMix)

		ws.gain.SetValue(o.Gain)
		ws.velAmp.SetValue(o.VelToAmp)
		ws.velFilt.SetValue(o.VelToFilt)

		ws.sampleAttack.SetValue(o.SampleAttack)
		ws.sampleDecay.SetValue(o.SampleDecay)
		ws.sampleSustain.SetValue(o.SampleSustain)
		ws.sampleRelease.SetValue(o.SampleRelease)
		ws.sampleVolume.SetValue(o.SampleVolume)
		ws.samplePitch.SetValue(o.SamplePitch)

		previewValid = false
		prevPb.Paint()
	}

	// ── Preset store controls (top row) ──
	presetLbl := newLabel("Preset:", 0, 0, 46, labelH)
	w.Add(presetLbl)

	presetCmb := wui.NewComboBox()
	presetCmb.SetBounds(0, 0, 130, editH)
	if fontNormal != nil {
		presetCmb.SetFont(fontNormal)
	}
	w.Add(presetCmb)

	nameLbl := newLabel("Name:", 0, 0, 40, labelH)
	w.Add(nameLbl)

	nameEdit := newEdit(0, 0, 110, editH)
	w.Add(nameEdit)

	btnSaveAs := newBtn("Save As", 0, 0, 72, editH, nil)
	w.Add(btnSaveAs)
	btnRename := newBtn("Rename", 0, 0, 72, editH, nil)
	w.Add(btnRename)
	btnDelete := newBtn("Delete", 0, 0, 68, editH, nil)
	w.Add(btnDelete)
	btnLoad := newBtn("Load", 0, 0, 60, editH, nil)
	w.Add(btnLoad)

	refreshPresetCombo := func(selectName string) {
		names := presetNames()
		if len(names) == 0 {
			presetCmb.SetItems([]string{"(no presets)"})
			presetCmb.SetSelectedIndex(0)
			return
		}
		presetCmb.SetItems(names)
		idx := 0
		if selectName != "" {
			if i := indexOf(names, selectName); i >= 0 {
				idx = i
			}
		}
		presetCmb.SetSelectedIndex(idx)
	}

	selectedPresetName := func() string {
		names := presetNames()
		if len(names) == 0 {
			return ""
		}
		i := presetCmb.SelectedIndex()
		if i < 0 || i >= len(names) {
			return ""
		}
		return names[i]
	}

	refreshPresetCombo("")

	presetCmb.SetOnChange(func(i int) {
		names := presetNames()
		if i < 0 || i >= len(names) {
			nameEdit.SetText("")
			return
		}
		nameEdit.SetText(names[i])
	})

	btnSaveAs.SetOnClick(func() {
		name := strings.TrimSpace(nameEdit.Text())
		if name == "" {
			wui.MessageBoxError("Preset", "Please enter a name in the Name field first.")
			return
		}
		opts := globalSynth.Snapshot()
		upsertPreset(name, opts)
		refreshPresetCombo(name)
	})

	btnLoad.SetOnClick(func() {
		name := strings.TrimSpace(nameEdit.Text())
		if name == "" {
			name = selectedPresetName()
		}
		i := findPresetIndex(name)
		if i < 0 {
			wui.MessageBoxError("Preset", "No preset named '"+name+"'.")
			return
		}
		opts := appConfig.Presets[i].Options
		globalSynth.Update(func(o *SynthOptions) { *o = opts })
		applyOptsToWidgets(opts)
		refreshPresetCombo(name)
	})

	btnRename.SetOnClick(func() {
		oldName := selectedPresetName()
		newName := strings.TrimSpace(nameEdit.Text())
		if oldName == "" {
			wui.MessageBoxError("Preset", "Select a preset to rename.")
			return
		}
		if newName == "" {
			wui.MessageBoxError("Preset", "Enter the new name in the Name field.")
			return
		}
		if oldName == newName {
			return
		}
		if !renamePreset(oldName, newName) {
			wui.MessageBoxError("Preset", "Cannot rename: a preset named '"+newName+"' already exists.")
			return
		}
		refreshPresetCombo(newName)
	})

	btnDelete.SetOnClick(func() {
		name := selectedPresetName()
		if name == "" {
			return
		}
		deletePreset(name)
		refreshPresetCombo("")
		nameEdit.SetText("")
	})

	// ── Master row ──
	masterLbl := newLabel("Master", 0, 0, 60, labelH)
	if fontBold != nil {
		masterLbl.SetFont(fontBold)
	}
	w.Add(masterLbl)

	gainLbl := newLabel("Gain", 0, 0, 36, labelH)
	w.Add(gainLbl)
	ws.gain = wui.NewFloatUpDown()
	ws.gain.SetMinMax(0, 1)
	ws.gain.SetPrecision(3)
	ws.gain.SetValue(initial.Gain)
	ws.gain.SetOnValueChange(func(float64) { sync() })
	w.Add(ws.gain)

	velAmpLbl := newLabel("Vel→Amp", 0, 0, 66, labelH)
	w.Add(velAmpLbl)
	ws.velAmp = wui.NewFloatUpDown()
	ws.velAmp.SetMinMax(0, 1)
	ws.velAmp.SetPrecision(3)
	ws.velAmp.SetValue(initial.VelToAmp)
	ws.velAmp.SetOnValueChange(func(float64) { sync() })
	w.Add(ws.velAmp)

	velFiltLbl := newLabel("Vel→Filter", 0, 0, 76, labelH)
	w.Add(velFiltLbl)
	ws.velFilt = wui.NewFloatUpDown()
	ws.velFilt.SetMinMax(0, 1)
	ws.velFilt.SetPrecision(3)
	ws.velFilt.SetValue(initial.VelToFilt)
	ws.velFilt.SetOnValueChange(func(float64) { sync() })
	w.Add(ws.velFilt)

	// ── Preview label ──
	prevLbl := newLabel("Preview (C4 → G4)", 0, 0, 200, labelH)
	if fontBold != nil {
		prevLbl.SetFont(fontBold)
	}
	w.Add(prevLbl)

	// ── Buttons ──
	btnPreview := newBtn("Preview Note", 0, 0, 120, btnH, nil)
	w.Add(btnPreview)
	btnReset := newBtn("Reset", 0, 0, 80, btnH, nil)
	w.Add(btnReset)
	btnClose := newBtn("Close", 0, 0, closeBtnW, btnH, func() {
		persistSynth()
		w.Close()
	})
	w.Add(btnClose)

	btnPreview.SetOnClick(func() {
		ensureSpeakerInit()
		cfg := globalSynth.Snapshot()
		buf := previewBuf(cfg, 1.2)
		swapSpeakerStream(&positionStreamer{buf: buf})
	})

	btnReset.SetOnClick(func() {
		def := defaultSynthOptions()
		globalSynth.Update(func(o *SynthOptions) { *o = def })
		applyOptsToWidgets(def)
	})

	tabs.SetOnChange(func(idx int) {
		for i, p := range panels {
			p.SetVisible(i == idx)
		}
	})

	// ── Layout ──
	applyLayout(w, func(iw, ih int) {
		const smallGap = 6
		const topMargin = 12
		const botMargin = 12

		// ── Row 1: preset store ──
		y := topMargin
		presetLbl.SetBounds(margin, y+4, 46, labelH)
		presetCmb.SetBounds(margin+46, y, 130, editH)
		nameLbl.SetBounds(margin+46+130+10, y+4, 40, labelH)
		nameEdit.SetBounds(margin+46+130+10+40, y, 110, editH)

		btnX := iw - margin
		btnX -= 60
		btnLoad.SetBounds(btnX, y, 60, editH)
		btnX -= 68 + smallGap
		btnDelete.SetBounds(btnX, y, 68, editH)
		btnX -= 72 + smallGap
		btnRename.SetBounds(btnX, y, 72, editH)
		btnX -= 72 + smallGap
		btnSaveAs.SetBounds(btnX, y, 72, editH)

		y += editH + smallGap

		// ── Row 2: master controls ──
		masterLbl.SetBounds(margin, y+4, 55, labelH)
		gainLbl.SetBounds(margin+55, y+4, 36, labelH)
		ws.gain.SetBounds(margin+91, y, 70, editH)
		velAmpLbl.SetBounds(margin+171, y+4, 66, labelH)
		ws.velAmp.SetBounds(margin+237, y, 70, editH)
		velFiltLbl.SetBounds(margin+317, y+4, 76, labelH)
		ws.velFilt.SetBounds(margin+393, y, 70, editH)

		y += editH + smallGap

		// ── Bottom-anchored: buttons ──
		barY := ih - botMargin - btnH

		// ── Preview label + box fill the space between tabs and bottom bar ──
		const tabH = 200
		tabs.SetBounds(margin, y, iw-2*margin, tabH)
		cx, cy, cw, chh := tabs.ContentBounds()
		for _, p := range panels {
			p.SetBounds(cx+2, cy+2, cw-4, chh-4)
		}
		y += tabH + smallGap

		prevLbl.SetBounds(margin, y, 200, labelH)
		y += labelH + 2

		prevH := barY - y - smallGap
		if prevH < 60 {
			prevH = 60
		}
		prevPb.SetBounds(margin, y, iw-2*margin, prevH)

		btnPreview.SetBounds(margin, barY, 120, btnH)
		btnReset.SetBounds(margin+120+smallGap, barY, 80, btnH)
		btnClose.SetBounds(iw-margin-closeBtnW, barY, closeBtnW, btnH)
	})

	prevPb.Paint()
	showModal(w)
}

// indexOf returns the index of s in list, or 0 if not found.
func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return 0
}
