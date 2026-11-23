package transcribe

import (
	"context"
	"encoding/binary"
	"fmt"
)

// Demo is a Transcriber that makes words up, for seeing captions without a
// service (and for the demo build). It says a line for every ten seconds of
// the piece, so what comes back is as long as the audio is, and it hears the
// piece's own length from its WAV header.
type Demo struct{}

var demoLines = []string{
	"Okay, from the top of the chorus.",
	"Let's try that again, a little slower.",
	"That bridge needs another bar.",
	"Hindsight note: keep this one.",
}

func (Demo) Transcribe(ctx context.Context, wav []byte, name string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	secs := wavSeconds(wav)
	res := Result{Language: "en", Segments: []Segment{}}
	for i, t := 0, 0.0; t < secs; i, t = i+1, t+10 {
		end := t + 4
		if end > secs {
			end = secs
		}
		res.Segments = append(res.Segments, Segment{Start: t, End: end, Text: demoLines[i%len(demoLines)]})
		if res.Text != "" {
			res.Text += " "
		}
		res.Text += demoLines[i%len(demoLines)]
	}
	return res, nil
}

// wavSeconds reads the length of a canonical 44-byte-header PCM WAV, which is
// what EncodeWAV16 writes; anything else is zero seconds.
func wavSeconds(wav []byte) float64 {
	if len(wav) < 44 || string(wav[0:4]) != "RIFF" {
		return 0
	}
	rate := binary.LittleEndian.Uint32(wav[24:28])
	ch := binary.LittleEndian.Uint16(wav[22:24])
	bits := binary.LittleEndian.Uint16(wav[34:36])
	if rate == 0 || ch == 0 || bits == 0 {
		return 0
	}
	return float64(len(wav)-44) / float64(uint32(ch)*uint32(bits/8)*rate)
}

func (Demo) String() string { return fmt.Sprint("demo transcriber") }
