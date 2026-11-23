// Package analyze runs analysers over takes in the background and keeps what
// they find in each take's analysis sidecar (audio.SetAnalysis).
//
// An analyser is one idea about a take's audio -- what was said in it, which
// instruments are in it, what it sounds like harmonically -- behind one
// interface, so a new idea is a new package that satisfies Analyzer and a line
// where the binary lists its analysers, and never a change to saving, the
// takes list, the trash or the API. The Runner is the only thing that knows
// when to run them: it finds takes missing a section, or holding one from an
// older version of its analyser, and does them one at a time while the rig is
// idle, so a transcription never competes with the capture path for the Pi's
// CPU.
package analyze

import (
	"context"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

// Analyzer is one kind of analysis.
type Analyzer interface {
	// Key names the analyser's section in the sidecar: lower-case, stable
	// for ever ("captions"), because it is the section's address.
	Key() string
	// Version is bumped when the analyser's output would change for the
	// same audio; a section written by an older version is redone.
	Version() int
	// Analyze reads the take and returns what it found, which must marshal
	// to JSON. It should return when ctx is cancelled, and it must not
	// write anywhere: the Runner stores the result.
	Analyze(ctx context.Context, in Input) (any, error)
}

// Input is a take as an analyser sees it.
type Input struct {
	Dir  string        // the takes folder
	Name string        // the take's file name
	Path string        // Dir joined with Name
	Info audio.WAVInfo // the WAV's header
	Meta audio.Meta    // the owner's sidecar, read when the run began
}

// Section is how every analyser's result is wrapped on disk, so the Runner
// can tell a stale section from a current one without knowing what is in it.
type Section struct {
	Version  int       `json:"v"`
	At       time.Time `json:"at"`
	Duration float64   `json:"took_seconds,omitempty"`
	// Error is set instead of Data when the analyser failed on this take, so
	// the Runner does not try it again every pass; a newer Version tries again.
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}
