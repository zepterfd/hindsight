package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
	"github.com/gabeduke/hindsight/internal/transcribe"
)

func TestCaptionsGetServesWebVTT(t *testing.T) {
	r, dir, _ := newAnalysisAPI(t, nil)
	writeRealTake(t, dir, "jam_a.wav", 4800)
	wav := filepath.Join(dir, "jam_a.wav")

	if w := do(t, r, http.MethodGet, "/api/take/captions?file=jam_a.wav"); w.Code != http.StatusNotFound {
		t.Fatalf("before the analysis: %d", w.Code)
	}

	sec := analyze.Section{Version: 1, At: time.Now(), Data: transcribe.CaptionsData{
		Text: "from the top", Segments: []transcribe.Segment{{Start: 2, End: 4.5, Text: "from the top"}}}}
	if err := audio.SetAnalysis(wav, "captions", sec); err != nil {
		t.Fatal(err)
	}
	w := do(t, r, http.MethodGet, "/api/take/captions?file=jam_a.wav")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/vtt") {
		t.Fatalf("status %d type %q", w.Code, w.Header().Get("Content-Type"))
	}
	want := "WEBVTT\n\n1\n00:00:02.000 --> 00:00:04.500\nfrom the top\n\n"
	if w.Body.String() != want {
		t.Fatalf("body %q", w.Body)
	}

	// A failed analysis is a 404, not an empty file.
	if err := audio.SetAnalysis(wav, "captions", analyze.Section{Version: 1, Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	if w := do(t, r, http.MethodGet, "/api/take/captions?file=jam_a.wav"); w.Code != http.StatusNotFound {
		t.Fatalf("after a failure: %d", w.Code)
	}
}

func TestCaptionsGetRefusesBadNames(t *testing.T) {
	r, _, _ := newAnalysisAPI(t, nil)
	for _, f := range []string{"../x.wav", "", "nope.wav"} {
		w := do(t, r, http.MethodGet, "/api/take/captions?file="+f)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
			t.Errorf("%q: %d", f, w.Code)
		}
	}
}
