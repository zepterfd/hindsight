package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

// The silence endpoints (docs/architecture/takes.md, *Removing silence*):
//
//	GET  /api/take/silence?file=[&min_seconds=&keep_seconds=&threshold_db=]
//	     where the take's long silences are, and how much shorter it would be.
//	     Writes nothing.
//	POST /api/take/trim-silence?file=   body {min_seconds, keep_seconds,
//	     threshold_db} all optional: write the take again without them, as a
//	     new take; the source is never changed. 200 {name, report}; 409 when
//	     there is nothing to remove.

func silenceOptions(min, keep, thr float64) (audio.SilenceOptions, error) {
	if min < 0 || keep < 0 || thr > 0 {
		return audio.SilenceOptions{}, errors.New("min_seconds and keep_seconds must not be negative, and threshold_db must be 0 (find it) or below")
	}
	return audio.SilenceOptions{MinSeconds: min, KeepSeconds: keep, ThresholdDB: thr}, nil
}

func (a *API) handleSilenceGet(w http.ResponseWriter, r *http.Request) {
	name, wav, ok := a.takeForWrite(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var v [3]float64
	for i, k := range []string{"min_seconds", "keep_seconds", "threshold_db"} {
		if s := q.Get(k); s != "" {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				writeErr(w, http.StatusBadRequest, k+" is not a number")
				return
			}
			v[i] = f
		}
	}
	opt, err := silenceOptions(v[0], v[1], v[2])
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rep, err := audio.FindSilence(wav, opt)
	switch {
	case errors.Is(err, audio.ErrBitDepth):
		writeErr(w, http.StatusUnprocessableEntity, "this take is not 32-bit")
		return
	case err != nil:
		log.Printf("silence %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not read the take")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, rep)
}

func (a *API) handleTrimSilence(w http.ResponseWriter, r *http.Request) {
	name, _, ok := a.takeForWrite(w, r)
	if !ok {
		return
	}
	var body struct {
		MinSeconds  float64 `json:"min_seconds"`
		KeepSeconds float64 `json:"keep_seconds"`
		ThresholdDB float64 `json:"threshold_db"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	opt, err := silenceOptions(body.MinSeconds, body.KeepSeconds, body.ThresholdDB)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if free := audio.EnsureFree(a.cfg.OutputDir, a.cfg.MinFreeGB); free < a.cfg.MinFreeGB {
		writeErr(w, http.StatusInsufficientStorage,
			fmt.Sprintf("low disk: %.2f GB free, need %.2f GB", free, a.cfg.MinFreeGB))
		return
	}

	out, rep, err := audio.TrimSilence(a.cfg.OutputDir, name, opt, time.Now())
	switch {
	case errors.Is(err, audio.ErrNoSilence):
		writeErr(w, http.StatusConflict, "no silence to remove")
		return
	case errors.Is(err, audio.ErrBitDepth):
		writeErr(w, http.StatusUnprocessableEntity, "this take is not 32-bit")
		return
	case err != nil:
		log.Printf("trim-silence %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not remove the silence")
		return
	}
	// Same as a cut: the preview never blocks or fails the request, and the
	// new take counts against MAX_SAVES -- but the source it came from is
	// kept, as the owner is on its page.
	a.background(func() { audio.MakePreview(a.cfg, filepath.Join(a.cfg.OutputDir, out), len(a.cfg.OutMix())) })
	if a.saver != nil {
		a.background(func() { a.saver.Prune(name, out) })
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": out, "report": rep})
}
