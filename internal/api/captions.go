package api

import (
	"log"
	"net/http"

	"github.com/gabeduke/hindsight/internal/audio"
	"github.com/gabeduke/hindsight/internal/transcribe"
)

// GET /api/take/captions?file= serves a take's captions (the "captions"
// analysis, internal/transcribe) as WebVTT, which a <track> element and most
// editors read: 404 until the analysis has run, or when it failed. The
// words are in the take's analysis sidecar already; this only reshapes them.
func (a *API) handleCaptionsGet(w http.ResponseWriter, r *http.Request) {
	name, wav, ok := a.takeForWrite(w, r)
	if !ok {
		return
	}
	var sec struct {
		Error string                  `json:"error"`
		Data  transcribe.CaptionsData `json:"data"`
	}
	found, err := audio.ReadAnalysis(wav, "captions", &sec)
	if err != nil {
		log.Printf("captions %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not read the captions")
		return
	}
	if !found || sec.Error != "" {
		writeErr(w, http.StatusNotFound, "no captions for this take")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(transcribe.VTT(sec.Data.Segments)))
}
