// Package instrument says what a take sounds like: percussion, bass, chords,
// a lead line, a wash of ambience. It is an analysis (internal/analyze): it
// reads the take, stores its Features and its guesses, and never writes
// anywhere else.
package instrument

import (
	"context"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/dsp"
)

// Key is the analysis' section name in a take's analysis sidecar.
const Key = "instruments"

// Result is the section's data.
type Result struct {
	Model    string   `json:"model"`
	Guesses  []Guess  `json:"guesses"`
	Features Features `json:"features"`
}

// Analyzer is the analysis. The zero value uses Rules.
type Analyzer struct {
	Classifier Classifier
}

func (Analyzer) Key() string { return Key }

// Version is bumped when the thresholds or the features change, so every
// take is read again.
func (Analyzer) Version() int { return 1 }

func (a Analyzer) Analyze(ctx context.Context, in analyze.Input) (any, error) {
	c := a.Classifier
	if c == nil {
		c = Rules{}
	}
	st, err := dsp.NewSTFT(fftSize, fftHop)
	if err != nil {
		return nil, err
	}
	rate, _ := dsp.MonoRate(in.Info.SampleRate)
	ex := newExtractor(rate)
	if _, _, err := dsp.ReadMono(in.Path, func(block []float32) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		st.Push(block, func(mag []float64, _ int64) { ex.frame(mag) })
		return nil
	}); err != nil {
		return nil, err
	}
	f := ex.features()
	return Result{Model: c.Name(), Guesses: c.Classify(f), Features: f}, nil
}
