package instrument

import (
	"context"
	"math"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
)

const sr = 48000

// synth makes the sounds the classifier is meant to tell apart. They are
// crude on purpose: if a rule only works on a sound this easy it works on
// nothing, and if it works only on these it is not yet worth shipping, which
// is why the thresholds are called a proof of concept.
type synth struct{ rng *rand.Rand }

func newSynth() *synth { return &synth{rng: rand.New(rand.NewSource(1))} }

// note adds a harmonic tone with a decay envelope.
func note(buf []float64, at int, hz float64, dur float64, amp, decay float64, harmonics int) {
	n := int(dur * sr)
	for i := 0; i < n && at+i < len(buf); i++ {
		t := float64(i) / sr
		env := math.Exp(-decay * t)
		// a 5 ms attack so there is no click
		if t < 0.005 {
			env *= t / 0.005
		}
		var v float64
		for h := 1; h <= harmonics; h++ {
			if hz*float64(h) > 5000 {
				break
			}
			v += math.Sin(2*math.Pi*hz*float64(h)*t) / float64(h)
		}
		buf[at+i] += amp * env * v
	}
}

func (s *synth) noiseBurst(buf []float64, at int, dur, amp, decay float64) {
	n := int(dur * sr)
	var prev float64
	for i := 0; i < n && at+i < len(buf); i++ {
		w := s.rng.Float64()*2 - 1
		hp := w - prev // a crude high-pass: hats and snares, not rumble
		prev = w
		buf[at+i] += amp * math.Exp(-decay*float64(i)/sr) * hp * 0.5
	}
}

func seconds(n float64) []float64 { return make([]float64, int(n*sr)) }

func (s *synth) kit(secs float64) []float64 {
	b := seconds(secs)
	for beat := 0.0; beat < secs; beat += 0.5 {
		at := int(beat * sr)
		note(b, at, 55, 0.25, 0.8, 18, 1)               // kick
		s.noiseBurst(b, at+int(0.25*sr), 0.05, 0.4, 60) // hat
		if int(beat/0.5)%2 == 1 {
			s.noiseBurst(b, at, 0.15, 0.7, 25) // snare on the off beats
		}
	}
	return b
}

func (s *synth) bassLine(secs float64) []float64 {
	b := seconds(secs)
	notes := []float64{55, 55, 65.4, 49}
	for i, at := 0, 0.0; at < secs; i, at = i+1, at+0.5 {
		note(b, int(at*sr), notes[i%len(notes)], 0.5, 0.6, 1.5, 8)
	}
	return b
}

func (s *synth) chords(secs float64) []float64 {
	b := seconds(secs)
	for _, root := range []float64{261.6, 220, 174.6, 196} {
		_ = root
	}
	roots := [][3]float64{{261.6, 329.6, 392}, {220, 261.6, 329.6}, {174.6, 220, 261.6}, {196, 246.9, 293.7}}
	for i, at := 0, 0.0; at < secs; i, at = i+1, at+1.5 {
		for _, hz := range roots[i%len(roots)] {
			note(b, int(at*sr), hz, 1.6, 0.25, 0.3, 6)
		}
	}
	return b
}

func (s *synth) lead(secs float64) []float64 {
	b := seconds(secs)
	melody := []float64{440, 494, 523, 587, 523, 494, 440, 392}
	for i, at := 0, 0.0; at < secs; i, at = i+1, at+0.3 {
		note(b, int(at*sr), melody[i%len(melody)], 0.3, 0.5, 4, 5)
	}
	return b
}

func (s *synth) wash(secs float64) []float64 {
	b := seconds(secs)
	var lp float64
	for i := range b {
		lp += 0.15 * ((s.rng.Float64()*2 - 1) - lp) // low-passed noise: rain, a room
		b[i] = 0.3 * lp
	}
	return b
}

func writeMono(t *testing.T, sig []float64) string {
	t.Helper()
	data := make([]int32, len(sig)*2)
	for i, v := range sig {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		s := int32(v * 0.7 * math.MaxInt32)
		data[2*i], data[2*i+1] = s, s
	}
	p := filepath.Join(t.TempDir(), "take.wav")
	if _, err := audio.WriteWAV(p, data, 2, []int{0, 1}, sr); err != nil {
		t.Fatal(err)
	}
	return p
}

func classify(t *testing.T, sig []float64) Result {
	t.Helper()
	p := writeMono(t, sig)
	info, err := audio.ReadWAVInfo(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Analyzer{}.Analyze(context.Background(), analyze.Input{Path: p, Info: info})
	if err != nil {
		t.Fatal(err)
	}
	return out.(Result)
}

func has(r Result, label string) bool {
	for _, g := range r.Guesses {
		if g.Label == label {
			return true
		}
	}
	return false
}

func TestEachSoundGetsItsLabel(t *testing.T) {
	s := newSynth()
	for _, c := range []struct {
		name string
		sig  []float64
		want string
		not  []string
	}{
		{"a kit", s.kit(8), Percussion, []string{Bass, Chords, Lead}},
		{"a bass line", s.bassLine(8), Bass, []string{Percussion, Lead}},
		{"chords", s.chords(9), Chords, []string{Percussion, Bass}},
		{"a lead line", s.lead(8), Lead, []string{Percussion, Bass, Chords}},
		{"a wash of noise", s.wash(8), Ambience, []string{Bass, Chords, Lead}},
	} {
		r := classify(t, c.sig)
		if !has(r, c.want) {
			t.Errorf("%s: guesses %+v, want %q; features %+v", c.name, r.Guesses, c.want, r.Features)
		}
		for _, n := range c.not {
			if has(r, n) {
				t.Errorf("%s: also guessed %q: %+v; features %+v", c.name, n, r.Guesses, r.Features)
			}
		}
	}
}

func TestSilenceIsSilence(t *testing.T) {
	r := classify(t, seconds(4))
	if len(r.Guesses) != 1 || r.Guesses[0].Label != Silence {
		t.Errorf("guesses = %+v", r.Guesses)
	}
	// Hiss at -90 dB is silence too, not "ambience".
	hiss := seconds(4)
	rng := rand.New(rand.NewSource(2))
	for i := range hiss {
		hiss[i] = (rng.Float64()*2 - 1) * 3e-5
	}
	if r := classify(t, hiss); len(r.Guesses) != 1 || r.Guesses[0].Label != Silence {
		t.Errorf("hiss: %+v %+v", r.Guesses, r.Features)
	}
}

func TestAKitWithABassLineIsBoth(t *testing.T) {
	s := newSynth()
	k, b := s.kit(8), s.bassLine(8)
	for i := range k {
		k[i] = 0.7*k[i] + 0.7*b[i]
	}
	r := classify(t, k)
	if !has(r, Percussion) || !has(r, Bass) {
		t.Errorf("guesses = %+v, want both; features %+v", r.Guesses, r.Features)
	}
}

func TestAClassifierCanBeSwappedAndIsNamed(t *testing.T) {
	p := writeMono(t, newSynth().lead(2))
	info, _ := audio.ReadWAVInfo(p)
	out, err := Analyzer{Classifier: fixed{}}.Analyze(context.Background(), analyze.Input{Path: p, Info: info})
	if err != nil {
		t.Fatal(err)
	}
	r := out.(Result)
	if r.Model != "fixed" || len(r.Guesses) != 1 || r.Guesses[0].Label != "theremin" {
		t.Errorf("%+v", r)
	}
	if r.Features.PeakDB == 0 {
		t.Error("features should be stored beside any classifier's verdict")
	}
}

type fixed struct{}

func (fixed) Name() string { return "fixed" }
func (fixed) Classify(Features) []Guess {
	return []Guess{{"theremin", 1}}
}

func TestACancelledReadStops(t *testing.T) {
	p := writeMono(t, newSynth().lead(4))
	info, _ := audio.ReadWAVInfo(p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Analyzer{}).Analyze(ctx, analyze.Input{Path: p, Info: info}); err == nil {
		t.Error("a cancelled analysis returned a result")
	}
}

func TestKeyAndVersion(t *testing.T) {
	var a analyze.Analyzer = Analyzer{}
	if a.Key() != "instruments" || a.Version() < 1 {
		t.Errorf("%s v%d", a.Key(), a.Version())
	}
}
