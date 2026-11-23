package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
	"github.com/gabeduke/hindsight/internal/config"
	"github.com/gorilla/mux"
)

type wordsAnalyzer struct{ gate chan struct{} }

func (wordsAnalyzer) Key() string  { return "words" }
func (wordsAnalyzer) Version() int { return 1 }
func (w wordsAnalyzer) Analyze(ctx context.Context, in analyze.Input) (any, error) {
	if w.gate != nil {
		select {
		case <-w.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]string{"take": in.Name}, nil
}

func newAnalysisAPI(t *testing.T, an analyze.Analyzer) (*mux.Router, string, *API) {
	t.Helper()
	dir := t.TempDir()
	a := New(&config.Config{OutputDir: dir}, nil, nil, nil, nil)
	t.Cleanup(a.WaitBackground)
	if an != nil {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		lister := audio.NewTakeList(dir)
		a.SetAnalyzer(ctx, &analyze.Runner{Dir: dir, Analyzers: []analyze.Analyzer{an},
			Takes: func() ([]audio.Take, error) { ts, _, err := lister.List(); return ts, err }})
	}
	r := mux.NewRouter()
	a.SetupRoutes(r)
	return r, dir, a
}

func TestAnalysisGetListsWhatThePiCanRunAndWhatTheTakeHolds(t *testing.T) {
	r, dir, a := newAnalysisAPI(t, wordsAnalyzer{})
	writeRealTake(t, dir, "jam_a.wav", 4800)

	w := do(t, r, http.MethodGet, "/api/take/analysis?file=jam_a.wav")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var resp analysisResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Available) != 1 || resp.Available[0] != "words" || len(resp.Sections) != 0 {
		t.Errorf("before: %+v", resp)
	}

	if _, err := a.analyzer.Analyze(context.Background(), "jam_a.wav", "words"); err != nil {
		t.Fatal(err)
	}
	w = do(t, r, http.MethodGet, "/api/take/analysis?file=jam_a.wav")
	resp = analysisResponse{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp.Sections["words"]; !ok {
		t.Errorf("after: %s", w.Body)
	}
}

func TestAnalysisGetWithNoAnalysersSaysSoWithAnEmptyList(t *testing.T) {
	r, dir, _ := newAnalysisAPI(t, nil)
	writeRealTake(t, dir, "jam_a.wav", 4800)
	var resp analysisResponse
	w := do(t, r, http.MethodGet, "/api/take/analysis?file=jam_a.wav")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || resp.Available == nil || len(resp.Available) != 0 {
		t.Errorf("status %d, %s: want an empty available list, not null", w.Code, w.Body)
	}
}

func TestAnalysisRefusesBadTakeNames(t *testing.T) {
	r, _, _ := newAnalysisAPI(t, wordsAnalyzer{})
	for _, url := range []string{
		"/api/take/analysis?file=../x.wav", "/api/take/analysis?file=", "/api/take/analysis?file=jam_missing.wav",
	} {
		if w := do(t, r, http.MethodGet, url); w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
			t.Errorf("%s -> %d", url, w.Code)
		}
	}
}

func TestAnalyzePostStartsOneInTheBackground(t *testing.T) {
	gate := make(chan struct{})
	r, dir, a := newAnalysisAPI(t, wordsAnalyzer{gate: gate})
	wav := writeRealTake(t, dir, "jam_a.wav", 4800)

	if w := do(t, r, http.MethodPost, "/api/take/analyze?file=jam_a.wav&kind=words"); w.Code != http.StatusAccepted {
		t.Fatalf("first = %d: %s", w.Code, w.Body)
	}
	// The same analysis is already under way: refused, not queued behind itself.
	deadline := time.Now().Add(2 * time.Second)
	for !a.analyzer.Busy("jam_a.wav", "words") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if w := do(t, r, http.MethodPost, "/api/take/analyze?file=jam_a.wav&kind=words"); w.Code != http.StatusConflict {
		t.Errorf("second = %d, want 409", w.Code)
	}
	close(gate)
	a.WaitBackground()
	if keys := audio.AnalysisKeys(wav); len(keys) != 1 || keys[0] != "words" {
		t.Errorf("keys = %v; the background run should have stored its section", keys)
	}
}

func TestAnalyzePostRefusals(t *testing.T) {
	r, dir, _ := newAnalysisAPI(t, wordsAnalyzer{})
	writeRealTake(t, dir, "jam_a.wav", 4800)
	if w := do(t, r, http.MethodPost, "/api/take/analyze?file=jam_a.wav&kind=nope"); w.Code != http.StatusNotFound {
		t.Errorf("unknown kind = %d", w.Code)
	}
	r2, dir2, _ := newAnalysisAPI(t, nil)
	writeRealTake(t, dir2, "jam_a.wav", 4800)
	if w := do(t, r2, http.MethodPost, "/api/take/analyze?file=jam_a.wav&kind=words"); w.Code != http.StatusNotImplemented {
		t.Errorf("none set up = %d, want 501", w.Code)
	}
}
