package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

// maxCutRanges bounds one request. A person picks a handful of sections by
// hand; a long list is a mistake or a script, and each range costs a fade pair
// and a seek.
const maxCutRanges = 32

// cutRanges is POST /api/cut with `ranges`: the regions, in any order but not
// overlapping, joined in time order into one new take (audio.CutSegments).
// One range is the plain cut, MIDI included; more than one has no MIDI and no
// downbeat, since both are positions on the old timeline.
func (a *API) cutRanges(w http.ResponseWriter, name string, ranges []audio.Range, label string) {
	if len(ranges) > maxCutRanges {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("at most %d ranges", maxCutRanges))
		return
	}
	segs, err := audio.SortedSegments(ranges)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "ranges must not overlap or run backwards")
		return
	}
	if free := audio.EnsureFree(a.cfg.OutputDir, a.cfg.MinFreeGB); free < a.cfg.MinFreeGB {
		writeErr(w, http.StatusInsufficientStorage,
			fmt.Sprintf("low disk: %.2f GB free, need %.2f GB", free, a.cfg.MinFreeGB))
		return
	}
	out, err := audio.CutSegments(a.cfg.OutputDir, audio.SegmentsRequest{
		Source: name, Segments: segs, Label: sanitizeLabel(label),
	}, time.Now())
	switch {
	case errors.Is(err, audio.ErrRange):
		writeErr(w, http.StatusBadRequest, "a range is past the end of the take")
		return
	case errors.Is(err, audio.ErrTooShort):
		writeErr(w, http.StatusBadRequest, "a range is too short to cut")
		return
	case errors.Is(err, audio.ErrBitDepth):
		writeErr(w, http.StatusUnprocessableEntity, "this take is not 32-bit")
		return
	case err != nil:
		log.Printf("cut ranges %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not cut")
		return
	}
	if len(segs) == 1 && a.saver != nil {
		a.saver.CutMIDI(a.cfg.OutputDir, name, out, segs[0].Start, segs[0].End)
	}
	a.background(func() { audio.MakePreview(a.cfg, filepath.Join(a.cfg.OutputDir, out), len(a.cfg.OutMix())) })
	if a.saver != nil {
		a.background(func() { a.saver.Prune(name, out) })
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": out})
}
