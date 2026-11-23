package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
)

// The analysis endpoints (docs/architecture/takes.md, *Analysis*):
//
//	GET  /api/take/analysis?file=        every section a take holds, and which
//	                                     analyses this Pi can run
//	POST /api/take/analyze?file=&kind=   run one now, in the background (202)
//
// Which analyses exist is the binary's business (cmd/hindsight lists its
// analysers); this file knows only the runner, so a new analyser adds no
// route here.

// SetAnalyzer gives the API the runner that does a take's analyses, and the
// context its work ends with: a request's own would stop a transcription
// when the page that asked closed.
func (a *API) SetAnalyzer(ctx context.Context, r *analyze.Runner) {
	a.analyzer = r
	a.analyzeCtx = ctx
}

type analysisResponse struct {
	// Available is every analysis this Pi can run, by section name.
	Available []string `json:"available"`
	// Sections is what the take holds: each analyser's own wrapped result.
	Sections map[string]json.RawMessage `json:"sections"`
}

func (a *API) handleAnalysisGet(w http.ResponseWriter, r *http.Request) {
	name, wav, ok := a.takeForWrite(w, r)
	if !ok {
		return
	}
	secs, err := audio.ReadAnalysisSections(wav)
	if err != nil {
		log.Printf("analysis %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not read the analysis")
		return
	}
	resp := analysisResponse{Available: []string{}, Sections: secs}
	if a.analyzer != nil {
		resp.Available = a.analyzer.Keys()
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleAnalyzePost(w http.ResponseWriter, r *http.Request) {
	name, _, ok := a.takeForWrite(w, r)
	if !ok {
		return
	}
	if a.analyzer == nil || len(a.analyzer.Analyzers) == 0 {
		writeErr(w, http.StatusNotImplemented, "no analyses are set up on this Pi")
		return
	}
	kind := r.URL.Query().Get("kind")
	if !hasKey(a.analyzer.Keys(), kind) {
		writeErr(w, http.StatusNotFound, "no such analysis")
		return
	}
	// Claimed here, not in the goroutine, so a second press answers 409 at
	// once instead of 202 and a quiet no-op.
	if a.analyzer.Busy(name, kind) {
		writeErr(w, http.StatusConflict, "already analysing this take")
		return
	}
	a.background(func() {
		if _, err := a.analyzer.Analyze(a.analyzeCtx, name, kind); err != nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, analyze.ErrBusy) && !errors.Is(err, audio.ErrTakeGone) {
			log.Printf("analyse %s %s: %v", kind, filepath.Base(name), err)
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "kind": kind})
}

func hasKey(keys []string, k string) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}
