package similar

import (
	"context"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
)

// Key is the analysis' section name.
const Key = "harmony"

// Analyzer fingerprints a take's harmony.
type Analyzer struct{}

func (Analyzer) Key() string  { return Key }
func (Analyzer) Version() int { return 1 }

func (Analyzer) Analyze(ctx context.Context, in analyze.Input) (any, error) {
	return Fingerprint(in.Path, in.Info.SampleRate, ctx.Err)
}

// Load reads a take's stored fingerprint. ok is false when it has none yet
// or the analysis failed on it.
func Load(wav string) (p Print, ok bool) {
	var sec struct {
		Error string `json:"error"`
		Data  *Print `json:"data"`
	}
	found, err := audio.ReadAnalysis(wav, Key, &sec)
	if err != nil || !found || sec.Error != "" || sec.Data == nil {
		return Print{}, false
	}
	return *sec.Data, true
}
