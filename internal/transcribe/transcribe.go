// Package transcribe turns speech in a take into captions: words with the time
// they were said. It is the "speech to text" half of the captions analysis
// (internal/analyze); the other half is the Analyzer in this package, which
// cuts a take into pieces a service will accept and puts the words back on
// the take's own timeline.
//
// The service is behind one interface, Transcriber, so where the words come
// from is a decision of the person who set the Pi up and not of the code:
// an OpenAI-compatible HTTP endpoint (HTTP: OpenAI's own, or a Whisper server
// on the same tailnet, which keeps the audio at home), or Demo, which says
// something made up so the feature can be seen without a service.
package transcribe

import (
	"context"
	"fmt"
	"strings"
)

// Segment is some words and when they were said, in seconds from the start of
// what was transcribed.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Result is what a Transcriber heard.
type Result struct {
	Language string    `json:"language,omitempty"`
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
}

// Transcriber turns one piece of audio into words. wav is a complete WAV
// file (the Analyzer makes 16-bit mono); name is a file name for services
// that want one. It returns when ctx is cancelled.
type Transcriber interface {
	Transcribe(ctx context.Context, wav []byte, name string) (Result, error)
}

// Shift moves every segment `by` seconds later: the piece's words onto the
// take's timeline.
func (r Result) Shift(by float64) Result {
	out := Result{Language: r.Language, Text: r.Text, Segments: make([]Segment, len(r.Segments))}
	for i, s := range r.Segments {
		out.Segments[i] = Segment{Start: s.Start + by, End: s.End + by, Text: s.Text}
	}
	return out
}

// VTT renders segments as WebVTT, which a <track> element and most editors
// read: the captions for a clip.
func VTT(segs []Segment) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	n := 0
	for _, s := range segs {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, stamp(s.Start), stamp(maxF(s.End, s.Start+0.001)), text)
	}
	return b.String()
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// stamp is 00:01:02.345.
func stamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	ms := int64(sec*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
