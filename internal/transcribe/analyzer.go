package transcribe

import (
	"context"
	"errors"
	"fmt"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/dsp"
)

// Chunking: services cap an upload (OpenAI's is 25 MB), and a long take
// shouldn't be one request that fails at minute nine. 16-bit mono at 12 kHz
// is about 1.4 MB a minute, so ten-minute pieces are ~14 MB.
const chunkSeconds = 600

// silentDB is the level (dBFS RMS) under which a piece has nobody talking in
// it: it is not sent, which is faster and keeps the service from inventing
// words for room tone, which Whisper-family models are known to do.
const silentDB = -55.0

// Captions is the analyser: the words in a take, with when they were said.
type Captions struct {
	T Transcriber
}

// CaptionsData is what is kept in the sidecar's "captions" section.
type CaptionsData struct {
	Language string    `json:"language,omitempty"`
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
}

func (Captions) Key() string  { return "captions" }
func (Captions) Version() int { return 1 }

// Analyze streams the take down to speech rate (dsp.ReadMono), cuts it into
// ten-minute pieces, sends every piece with sound in it, and puts the words
// back on the take's own timeline.
func (c Captions) Analyze(ctx context.Context, in analyze.Input) (any, error) {
	if c.T == nil {
		return nil, errors.New("captions: no transcriber")
	}
	rate, _ := dsp.MonoRate(in.Info.SampleRate)
	if rate <= 0 {
		return nil, errors.New("captions: the take has no sample rate")
	}
	size := chunkSeconds * rate
	out := CaptionsData{Segments: []Segment{}}
	pending := make([]float32, 0, size)
	piece := 0

	flush := func() error {
		defer func() { piece++; pending = pending[:0] }()
		if len(pending) == 0 || dsp.DB(dsp.RMS(pending)) < silentDB {
			return nil
		}
		res, err := c.T.Transcribe(ctx, EncodeWAV16(pending, rate), fmt.Sprintf("piece-%d.wav", piece))
		if err != nil {
			return err
		}
		res = res.Shift(float64(piece * chunkSeconds))
		if out.Language == "" {
			out.Language = res.Language
		}
		if res.Text != "" {
			if out.Text != "" {
				out.Text += " "
			}
			out.Text += res.Text
		}
		out.Segments = append(out.Segments, res.Segments...)
		return nil
	}

	_, _, err := dsp.ReadMono(in.Path, func(block []float32) error {
		for len(block) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			n := size - len(pending)
			if n > len(block) {
				n = len(block)
			}
			pending = append(pending, block[:n]...)
			block = block[n:]
			if len(pending) == size {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}
