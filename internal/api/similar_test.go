package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gabeduke/hindsight/internal/audio"
	"github.com/gabeduke/hindsight/internal/similar"
)

func putPrint(t *testing.T, wav string, moves [48]float64, changes int) {
	t.Helper()
	p := similar.Print{Key: "C", Changes: changes, Moves: moves}
	p.Profile[0] = 1
	sec := map[string]any{"v": 1, "data": p}
	if err := audio.SetAnalysis(wav, similar.Key, sec); err != nil {
		t.Fatal(err)
	}
}

func TestSimilarAndGroupsReadStoredFingerprints(t *testing.T) {
	r, dir := newTestAPI(t)
	var m1, m2 [48]float64
	m1[7*4], m2[1*4] = 1, 1
	names := []string{"jam_a.wav", "jam_b.wav", "jam_c.wav", "jam_d.wav"}
	for _, n := range names {
		writeRealTake(t, dir, n, 4800)
	}
	putPrint(t, filepath.Join(dir, "jam_a.wav"), m1, 8)
	putPrint(t, filepath.Join(dir, "jam_b.wav"), m1, 8)
	putPrint(t, filepath.Join(dir, "jam_c.wav"), m2, 8)
	// jam_d has no fingerprint yet.

	w := do(t, r, http.MethodGet, "/api/take/similar?file=jam_a.wav")
	var s struct {
		Matches []similar.Match `json:"matches"`
		Pending int             `json:"pending"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(s.Matches) != 1 || s.Matches[0].Name != "jam_b.wav" || s.Pending != 1 {
		t.Errorf("similar = %+v", s)
	}

	w = do(t, r, http.MethodGet, "/api/takes/groups")
	var g struct {
		Groups  []similar.Group `json:"groups"`
		Pending int             `json:"pending"`
	}
	json.Unmarshal(w.Body.Bytes(), &g)
	if len(g.Groups) != 1 || len(g.Groups[0].Members) != 2 || g.Groups[0].Members[0]+g.Groups[0].Members[1] != "jam_a.wavjam_b.wav" && g.Groups[0].Members[0]+g.Groups[0].Members[1] != "jam_b.wavjam_a.wav" {
		t.Errorf("groups = %s", w.Body)
	}
}

func TestSimilarOnATakeNotYetFingerprintedAnswersEmptyNotAnError(t *testing.T) {
	r, dir := newTestAPI(t)
	writeRealTake(t, dir, "jam_a.wav", 4800)
	w := do(t, r, http.MethodGet, "/api/take/similar?file=jam_a.wav")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var s map[string]any
	json.Unmarshal(w.Body.Bytes(), &s)
	if m, _ := s["matches"].([]any); m == nil || len(m) != 0 {
		t.Errorf("matches = %v, want []", s["matches"])
	}
	if w := do(t, r, http.MethodGet, "/api/take/similar?file=jam_none.wav"); w.Code != http.StatusNotFound {
		t.Errorf("missing take = %d", w.Code)
	}
	w = do(t, r, http.MethodGet, "/api/takes/groups")
	var g struct{ Groups []any }
	json.Unmarshal(w.Body.Bytes(), &g)
	if g.Groups == nil {
		t.Errorf("groups = null, want []: %s", w.Body)
	}
}
