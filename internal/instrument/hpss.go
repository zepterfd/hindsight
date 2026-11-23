package instrument

import "sort"

// Harmonic/percussive separation by median filtering (Fitzgerald, 2010): a
// held note is a horizontal line in the spectrogram and a hit is a vertical
// one, so the median of a bin along time keeps the notes and the median of a
// frame along frequency keeps the hits. It runs as a stream over a window of
// hpssHalf*2+1 frames, so a fifteen-minute take costs a few dozen frames of
// memory.

const (
	hpssHalf = 8 // frames either side of the centre, and bins either side
)

// hpssFrame is one frame's energy, split into the percussive and the
// harmonic part overall and above highHz (where a kit's hats, snare and
// cymbals live and a bass, a pad or a voice's notes mostly do not).
type hpssFrame struct{ pe, he, peHi, heHi float64 }

const highHz = 1500

type hpss struct {
	bins   int
	binHz  float64
	frames [][]float64 // the last 2*hpssHalf+1 magnitude frames, oldest first
	col    []float64
	row    []float64
}

func newHPSS(bins int, binHz float64) *hpss {
	return &hpss{bins: bins, binHz: binHz, col: make([]float64, 0, 2*hpssHalf+1), row: make([]float64, 0, 2*hpssHalf+1)}
}

// push takes the next frame and, once it holds a full window, answers the
// split of the energy of the window's centre frame.
func (h *hpss) push(mag []float64) (hpssFrame, bool) {
	cp := make([]float64, len(mag))
	copy(cp, mag)
	h.frames = append(h.frames, cp)
	if len(h.frames) > 2*hpssHalf+1 {
		h.frames = h.frames[1:]
	}
	if len(h.frames) < 2*hpssHalf+1 {
		return hpssFrame{}, false
	}
	centre := h.frames[hpssHalf]
	var fr hpssFrame
	hiBin := int(highHz / h.binHz)
	for k := 1; k < h.bins; k++ {
		h.col = h.col[:0]
		for _, f := range h.frames {
			h.col = append(h.col, f[k])
		}
		hm := medianOf(h.col)
		h.row = h.row[:0]
		for j := k - hpssHalf; j <= k+hpssHalf; j++ {
			if j >= 1 && j < h.bins {
				h.row = append(h.row, centre[j])
			}
		}
		pm := medianOf(h.row)
		// A Wiener-style soft mask: the bin's energy is split by the
		// squares of the two medians.
		hh, pp := hm*hm, pm*pm
		den := hh + pp
		e := centre[k] * centre[k]
		if den == 0 {
			continue
		}
		fr.pe += e * pp / den
		fr.he += e * hh / den
		if k >= hiBin {
			fr.peHi += e * pp / den
			fr.heHi += e * hh / den
		}
	}
	return fr, true
}

func medianOf(v []float64) float64 {
	c := make([]float64, len(v))
	copy(c, v)
	sort.Float64s(c)
	return c[len(c)/2]
}
