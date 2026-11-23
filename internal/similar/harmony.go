// Package similar finds takes that play the same chord progression, even in
// another key, and groups them. It is an analysis (internal/analyze): each
// take is reduced once to a small harmonic fingerprint, kept in its analysis
// sidecar, and takes are compared by fingerprint, never by audio.
package similar

import (
	"math"
	"sort"

	"github.com/gabeduke/hindsight/internal/dsp"
)

const (
	fftSize = 4096
	fftHop  = 2048
	minHz   = 82.0 // E2: below it a bin is wider than a semitone
	maxHz   = 2000.0
	// maxChords bounds a stored fingerprint however long the take.
	maxChords = 400
)

var noteNames = [12]string{"C", "C#", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}

// chordName names chord id c: 0-11 major on that root, 12-23 minor.
func chordName(c int) string {
	if c < 12 {
		return noteNames[c]
	}
	return noteNames[c-12] + "m"
}

// chroma accumulates one frame's spectrum into twelve pitch classes.
func chroma(mag []float64, binHz float64, out *[12]float64) {
	*out = [12]float64{}
	for k := int(minHz / binHz); k < len(mag); k++ {
		f := float64(k) * binHz
		if f > maxHz {
			break
		}
		midi := 12*math.Log2(f/440) + 69
		pc := int(math.Round(midi))
		// A bin well away from any semitone is leakage, not a note.
		if math.Abs(midi-float64(pc)) > 0.4 {
			continue
		}
		out[((pc%12)+12)%12] += math.Sqrt(mag[k])
	}
}

func normalise(v *[12]float64) float64 {
	var s float64
	for _, x := range v {
		s += x * x
	}
	s = math.Sqrt(s)
	if s > 0 {
		for i := range v {
			v[i] /= s
		}
	}
	return s
}

// templates are the 24 triads, unit length.
var templates = func() (t [24][12]float64) {
	for root := 0; root < 12; root++ {
		for q, third := range []int{4, 3} {
			c := &t[q*12+root]
			c[root] = 1
			c[(root+third)%12] = 0.85
			c[(root+7)%12] = 0.9
			normalise(c)
		}
	}
	return
}()

// bestChord is the triad a normalised chroma most resembles, and how well.
func bestChord(c *[12]float64) (id int, score float64) {
	for i := range templates {
		var d float64
		for j := range c {
			d += c[j] * templates[i][j]
		}
		if d > score {
			id, score = i, d
		}
	}
	return
}

// Krumhansl-Kessler key profiles.
var (
	majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// keyOf names the key a pitch-class profile fits best, "C", "Am".
func keyOf(p [12]float64) string {
	best, bestR, bestMinor := 0, -2.0, false
	for tonic := 0; tonic < 12; tonic++ {
		for _, minor := range []bool{false, true} {
			prof := &majorProfile
			if minor {
				prof = &minorProfile
			}
			var rot [12]float64
			for i := range rot {
				rot[i] = prof[(i-tonic+12)%12]
			}
			if r := pearson(p[:], rot[:]); r > bestR {
				best, bestR, bestMinor = tonic, r, minor
			}
		}
	}
	if bestMinor {
		return noteNames[best] + "m"
	}
	return noteNames[best]
}

// keyFromChords names the key whose own chords (I ii iii IV V vi for a major
// key, i III iv v VI VII for a minor one) the take spends longest on, with
// the tonic chord and the dominant counting extra. I-V-vi-IV fits C and Am
// equally well; the tonic chord's extra weight is what chooses, and a person
// can rename it, so this is a label and not a fact.
func keyFromChords(w []float64) string {
	bestScore, best := -1.0, ""
	for t := 0; t < 12; t++ {
		maj := w[t]*2 + w[(t+7)%12]*1.2 + w[(t+5)%12] + w[12+(t+2)%12] + w[12+(t+4)%12] + w[12+(t+9)%12]
		min := w[12+t]*2 + w[12+(t+7)%12]*0.6 + w[12+(t+5)%12] + w[(t+3)%12] + w[(t+8)%12] + w[(t+10)%12]
		if maj > bestScore {
			bestScore, best = maj, noteNames[t]
		}
		if min > bestScore {
			bestScore, best = min, noteNames[t]+"m"
		}
	}
	return best
}

func pearson(a, b []float64) float64 {
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= float64(len(a))
	mb /= float64(len(b))
	var num, da, db float64
	for i := range a {
		num += (a[i] - ma) * (b[i] - mb)
		da += (a[i] - ma) * (a[i] - ma)
		db += (b[i] - mb) * (b[i] - mb)
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}

// Seg is a chord and when it began, in seconds.
type Seg struct {
	At    float64 `json:"at"`
	Chord string  `json:"chord"`
}

// Print is a take's harmonic fingerprint.
type Print struct {
	// Key is the best-fitting key, "Am".
	Key string `json:"key"`
	// Profile is the take's mean pitch-class energy, unit length, C first.
	Profile [12]float64 `json:"profile"`
	// Moves is the progression, independent of key: how often one chord
	// followed another, by the root's step up in semitones (12) and the two
	// chords' qualities (4), as shares of all changes.
	Moves [48]float64 `json:"moves"`
	// Chords is the chord sequence, as far as maxChords.
	Chords []Seg `json:"chords"`
	// Changes is how many chord changes Moves was made from. A fingerprint
	// of fewer than three is a drone or a single chord, and matches nothing
	// but itself.
	Changes int `json:"changes"`
	// Seconds is the take's length.
	Seconds float64 `json:"seconds"`
}

// builder turns a stream of spectra into a Print.
type builder struct {
	binHz    float64
	hopSecs  float64
	profile  [12]float64
	ids      []int // chord per frame, -1 for none
	energies []float64
	c        [12]float64
}

func newBuilder(rate int) *builder {
	return &builder{binHz: float64(rate) / fftSize, hopSecs: float64(fftHop) / float64(rate)}
}

func (b *builder) frame(mag []float64) {
	chroma(mag, b.binHz, &b.c)
	e := normalise(&b.c)
	b.energies = append(b.energies, e)
	id, score := bestChord(&b.c)
	if score < 0.7 {
		id = -1
	}
	b.ids = append(b.ids, id)
	if id >= 0 {
		for i := range b.c {
			b.profile[i] += b.c[i] * e
		}
	}
}

func (b *builder) print(seconds float64) Print {
	p := Print{Seconds: seconds}
	if len(b.ids) == 0 {
		return p
	}
	// A frame quieter than a tenth of the loudest is not a chord.
	var top float64
	for _, e := range b.energies {
		top = math.Max(top, e)
	}
	ids := make([]int, len(b.ids))
	for i, id := range b.ids {
		ids[i] = id
		if b.energies[i] < top*0.1 {
			ids[i] = -1
		}
	}
	ids = modeFilter(ids, 2)
	// Runs of the same chord, ignoring runs shorter than three frames
	// (a passing chord or a mis-hear), then merged again.
	type run struct{ id, n, at int }
	var runs []run
	for i, id := range ids {
		if len(runs) > 0 && runs[len(runs)-1].id == id {
			runs[len(runs)-1].n++
		} else {
			runs = append(runs, run{id, 1, i})
		}
	}
	var kept []run
	for _, r := range runs {
		if r.id < 0 || r.n < 3 {
			continue
		}
		if len(kept) > 0 && kept[len(kept)-1].id == r.id {
			kept[len(kept)-1].n += r.n
			continue
		}
		kept = append(kept, r)
	}
	var moves [48]float64
	for i := 1; i < len(kept); i++ {
		a, c := kept[i-1].id, kept[i].id
		step := ((c % 12) - (a % 12) + 12) % 12
		moves[step*4+(a/12)*2+(c/12)]++
		p.Changes++
	}
	if p.Changes > 0 {
		for i := range moves {
			moves[i] /= float64(p.Changes)
		}
	}
	p.Moves = moves
	for _, r := range kept {
		if len(p.Chords) >= maxChords {
			break
		}
		p.Chords = append(p.Chords, Seg{At: math.Round(float64(r.at)*b.hopSecs*100) / 100, Chord: chordName(r.id)})
	}
	p.Profile = b.profile
	hasProfile := normalise(&p.Profile) > 0
	if len(kept) > 0 {
		weights := make([]float64, 24)
		for _, r := range kept {
			weights[r.id] += float64(r.n)
		}
		p.Key = keyFromChords(weights)
	} else if hasProfile {
		p.Key = keyOf(p.Profile)
	}
	return p
}

// modeFilter replaces each value with the most common one within r frames
// either side.
func modeFilter(ids []int, r int) []int {
	out := make([]int, len(ids))
	for i := range ids {
		count := map[int]int{}
		for j := i - r; j <= i+r; j++ {
			if j >= 0 && j < len(ids) {
				count[ids[j]]++
			}
		}
		keys := make([]int, 0, len(count))
		for k := range count {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		best, bn := ids[i], 0
		for _, k := range keys {
			if count[k] > bn {
				best, bn = k, count[k]
			}
		}
		out[i] = best
	}
	return out
}

// Fingerprint reads a take and describes its harmony.
func Fingerprint(path string, sampleRate int, check func() error) (Print, error) {
	rate, _ := dsp.MonoRate(sampleRate)
	st, err := dsp.NewSTFT(fftSize, fftHop)
	if err != nil {
		return Print{}, err
	}
	b := newBuilder(rate)
	info, _, err := dsp.ReadMono(path, func(block []float32) error {
		if check != nil {
			if err := check(); err != nil {
				return err
			}
		}
		st.Push(block, func(mag []float64, _ int64) { b.frame(mag) })
		return nil
	})
	if err != nil {
		return Print{}, err
	}
	return b.print(info.Duration()), nil
}
