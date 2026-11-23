package instrument

import (
	"math"
	"sort"

	"github.com/gabeduke/hindsight/internal/dsp"
)

// Features is what the classifier is given: a few numbers about the sound of
// a whole take, each averaged over the frames that are actually playing.
// They are stored beside the verdict, so a better classifier can be checked
// against the same numbers and the thresholds can be tuned from real takes.
type Features struct {
	// PeakDB is the loudest frame's level, in dB below full scale.
	PeakDB float64 `json:"peak_db"`
	// Active is the fraction of the take that is playing (frames within 30
	// dB of the loudest, and above an absolute floor).
	Active float64 `json:"active"`
	// Centroid is the spectral centre of gravity, in Hz.
	Centroid float64 `json:"centroid_hz"`
	// LowRatio is the share of the energy under 250 Hz.
	LowRatio float64 `json:"low_ratio"`
	// Flatness is 0 for a pure tone and 1 for white noise, from 100 Hz to
	// 5 kHz.
	Flatness float64 `json:"flatness"`
	// Peaks is how many separate partials stand out, 80 Hz to 3 kHz: a few
	// for one note, many for a chord.
	Peaks float64 `json:"peaks"`
	// OnsetRate is how often something starts, per second.
	OnsetRate float64 `json:"onsets_per_second"`
	// Percussive is the share of the energy that is a vertical stripe of the
	// spectrogram (a hit: every frequency at once) rather than a horizontal
	// one (a held note), found by median filtering across time and across
	// frequency (harmonic/percussive separation). It is what lets a kit be
	// heard under a bass line, which the other numbers cannot do.
	Percussive float64 `json:"percussive"`
	// PercussiveHigh is the same above 1.5 kHz, weighted by energy, and
	// HighShare is how much of the take's energy is up there. A kit under a
	// bass line shows in these, where the bass hides it in Percussive.
	PercussiveHigh float64 `json:"percussive_high"`
	HighShare      float64 `json:"high_share"`
	// Sustain is how much of a frame's level is still there in the next:
	// near 1 for held notes and pads, low for hits that decay between beats.
	Sustain float64 `json:"sustain"`
}

const (
	fftSize = 1024
	fftHop  = 512
)

// extractor accumulates frames of a take's spectrum.
type extractor struct {
	rate   int
	binHz  float64
	levels []float64 // per frame
	cent   []float64
	low    []float64
	flat   []float64
	peaks  []float64
	flux   []float64
	mags   []float64 // log-compressed previous frame, for the flux
	hpss   *hpss
	perc   []hpssFrame // per frame, aligned to the frame the hpss centres on
	percAt []int       // the frame index each perc entry belongs to
}

func newExtractor(rate int) *extractor {
	return &extractor{rate: rate, binHz: float64(rate) / fftSize, hpss: newHPSS(fftSize/2+1, float64(rate)/fftSize)}
}

func (e *extractor) frame(mag []float64) {
	var energy, wsum, low, geo, arith float64
	nflat := 0
	lo100, hi5k := int(100/e.binHz), int(5000/e.binHz)
	if hi5k >= len(mag) {
		hi5k = len(mag) - 1
	}
	lowEdge := int(250 / e.binHz)
	for k := 1; k < len(mag); k++ {
		m2 := mag[k] * mag[k]
		energy += m2
		wsum += m2 * float64(k) * e.binHz
		if k < lowEdge {
			low += m2
		}
		if k >= lo100 && k <= hi5k {
			geo += math.Log(m2 + 1e-12)
			arith += m2
			nflat++
		}
	}
	level := math.Sqrt(energy) / float64(fftSize) * 2.4 // ~ RMS of the frame, ±1 dB for tones and noise
	e.levels = append(e.levels, level)
	if energy <= 0 {
		e.cent, e.low, e.flat, e.peaks = append(e.cent, 0), append(e.low, 0), append(e.flat, 0), append(e.peaks, 0)
	} else {
		e.cent = append(e.cent, wsum/energy)
		e.low = append(e.low, low/energy)
		f := 0.0
		if nflat > 0 && arith > 0 {
			f = math.Exp(geo/float64(nflat)) / (arith / float64(nflat))
		}
		e.flat = append(e.flat, f)
		e.peaks = append(e.peaks, countPeaks(mag, e.binHz))
	}
	// Spectral flux over a log-compressed spectrum: a new note adds energy
	// at new bins, a held one adds none.
	cur := make([]float64, len(mag))
	for k := range mag {
		cur[k] = math.Log1p(mag[k] * 20)
	}
	var flux float64
	if e.mags != nil {
		for k := range cur {
			if d := cur[k] - e.mags[k]; d > 0 {
				flux += d
			}
		}
	}
	e.flux = append(e.flux, flux)
	e.mags = cur
	if p, ok := e.hpss.push(mag); ok {
		e.perc = append(e.perc, p)
		e.percAt = append(e.percAt, len(e.levels)-1-hpssHalf)
	}
}

// countPeaks counts the local maxima of the spectrum between 80 Hz and 3 kHz
// that are within 20 dB of the frame's strongest and stand clear of their
// neighbours, which for a harmonic sound is about its partials.
func countPeaks(mag []float64, binHz float64) float64 {
	lo, hi := int(80/binHz), int(3000/binHz)
	if hi > len(mag)-2 {
		hi = len(mag) - 2
	}
	var top float64
	for k := lo; k <= hi; k++ {
		if mag[k] > top {
			top = mag[k]
		}
	}
	if top <= 0 {
		return 0
	}
	floor := top * 0.1
	n := 0
	for k := lo + 1; k < hi; k++ {
		if mag[k] > floor && mag[k] > mag[k-1] && mag[k] >= mag[k+1] &&
			mag[k] > 2*mag[k-2] && mag[k] > 2*mag[k+2] {
			n++
		}
	}
	return float64(n)
}

func (e *extractor) features() Features {
	var f Features
	n := len(e.levels)
	if n == 0 {
		f.PeakDB = -120
		return f
	}
	var peak float64
	for _, l := range e.levels {
		if l > peak {
			peak = l
		}
	}
	f.PeakDB = dsp.DB(peak)
	gate := math.Max(peak*0.0316, 1e-4) // 30 dB under the loudest, never under -80 dBFS
	var active []int
	for i, l := range e.levels {
		if l >= gate {
			active = append(active, i)
		}
	}
	f.Active = float64(len(active)) / float64(n)
	if len(active) == 0 {
		return f
	}
	mean := func(v []float64) float64 {
		var s float64
		for _, i := range active {
			s += v[i]
		}
		return s / float64(len(active))
	}
	f.Centroid, f.LowRatio, f.Flatness, f.Peaks = mean(e.cent), mean(e.low), mean(e.flat), mean(e.peaks)
	var sum hpssFrame
	for j, at := range e.percAt {
		if at >= 0 && at < n && e.levels[at] >= gate {
			sum.pe += e.perc[j].pe
			sum.he += e.perc[j].he
			sum.peHi += e.perc[j].peHi
			sum.heHi += e.perc[j].heHi
		}
	}
	if t := sum.pe + sum.he; t > 0 {
		f.Percussive = sum.pe / t
		f.HighShare = (sum.peHi + sum.heHi) / t
	}
	if t := sum.peHi + sum.heHi; t > 0 {
		f.PercussiveHigh = sum.peHi / t
	}

	// Sustain: how much of a frame's level the next one still has, over
	// consecutive active frames, clipped so a swell isn't more than held.
	var s float64
	pairs := 0
	for _, i := range active {
		if i+1 < n && e.levels[i+1] >= gate && e.levels[i] > 0 {
			s += math.Min(1, e.levels[i+1]/e.levels[i])
			pairs++
		}
	}
	if pairs > 0 {
		f.Sustain = s / float64(pairs)
	}

	f.OnsetRate = onsetRate(e.flux, e.levels, float64(e.rate)/fftHop)
	return f
}

// onsetRate counts peaks of the flux that stand well above their
// surroundings: more than the local median by twice the median deviation
// and by an absolute margin, and at least 100 ms from the last one.
func onsetRate(flux, levels []float64, framesPerSecond float64) float64 {
	if len(flux) < 4 {
		return 0
	}
	const half = 8 // frames either side, ~0.35 s
	minGap := int(0.1 * framesPerSecond)
	count, last := 0, -minGap
	win := make([]float64, 0, 2*half+1)
	for i := 1; i < len(flux)-1; i++ {
		if !(flux[i] > flux[i-1] && flux[i] >= flux[i+1]) {
			continue
		}
		win = win[:0]
		for j := i - half; j <= i+half; j++ {
			if j >= 0 && j < len(flux) {
				win = append(win, flux[j])
			}
		}
		med := median(win)
		for k := range win {
			win[k] = math.Abs(win[k] - med)
		}
		mad := median(win)
		// An onset is a rise in level, not just a change in the spectrum:
		// steady noise changes its spectrum every frame and starts nothing.
		if flux[i] > med+2*mad+2 && i-last >= minGap && levelRise(levels, i) {
			count++
			last = i
		}
	}
	return float64(count) / (float64(len(flux)) / framesPerSecond)
}

// levelRise says the frame at i is at least 40% louder than the four before.
func levelRise(levels []float64, i int) bool {
	lo := i - 4
	if lo < 0 {
		lo = 0
	}
	if lo == i {
		return false
	}
	var prev float64
	for _, l := range levels[lo:i] {
		prev += l
	}
	prev /= float64(i - lo)
	return levels[i] > 1.4*prev
}

func median(v []float64) float64 {
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	return c[len(c)/2]
}
