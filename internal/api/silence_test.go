package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabeduke/hindsight/internal/audio"
)

// writeGapTake writes an 8 kHz stereo take: 5 s of tone, gap s of near
// silence, 5 s of tone.
func writeGapTake(t *testing.T, dir, name string, gap int) {
	t.Helper()
	const rate = 8000
	var data []int32
	tone := func() {
		for i := 0; i < 5*rate; i++ {
			v := int32(0.3 * math.MaxInt32 * math.Sin(2*math.Pi*220*float64(i)/rate))
			data = append(data, v, v)
		}
	}
	tone()
	for i := 0; i < gap*rate; i++ {
		data = append(data, 0, 0)
	}
	tone()
	if _, err := audio.WriteWAV(filepath.Join(dir, name), data, 2, []int{0, 1}, rate); err != nil {
		t.Fatal(err)
	}
}

func TestSilenceGetReportsTheGapAndWritesNothing(t *testing.T) {
	r, dir := newTestAPI(t)
	writeGapTake(t, dir, "jam_a.wav", 20)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/take/silence?file=jam_a.wav", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var rep audio.SilenceReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Runs) != 1 || rep.RemovedSeconds < 18 {
		t.Errorf("report = %+v", rep)
	}
	if takes, _ := filepath.Glob(filepath.Join(dir, "*.wav")); len(takes) != 1 {
		t.Errorf("a dry run wrote a take: %v", takes)
	}
}

func TestSilenceGetRejectsBadInput(t *testing.T) {
	r, dir := newTestAPI(t)
	writeGapTake(t, dir, "jam_a.wav", 20)
	for _, c := range []struct {
		url  string
		code int
	}{
		{"/api/take/silence?file=jam_a.wav&min_seconds=abc", 400},
		{"/api/take/silence?file=jam_a.wav&threshold_db=12", 400},
		{"/api/take/silence?file=jam_a.wav&keep_seconds=-1", 400},
		{"/api/take/silence?file=nope.wav", 404},
		{"/api/take/silence?file=../x.wav", 400},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, c.url, nil))
		if w.Code != c.code {
			t.Errorf("%s = %d, want %d", c.url, w.Code, c.code)
		}
	}
}

func TestTrimSilenceWritesANewShorterTake(t *testing.T) {
	r, dir := newTestAPI(t)
	writeGapTake(t, dir, "jam_a.wav", 20)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/take/trim-silence?file=jam_a.wav", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var out struct {
		Name   string              `json:"name"`
		Report audio.SilenceReport `json:"report"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	info, err := audio.ReadWAVInfo(filepath.Join(dir, out.Name))
	if err != nil {
		t.Fatal(err)
	}
	if d := info.Duration(); d < 10.8 || d > 11.2 {
		t.Errorf("duration = %v, want about 11", d)
	}
	if src, _ := audio.ReadWAVInfo(filepath.Join(dir, "jam_a.wav")); src.Duration() < 29 {
		t.Error("the source take was changed")
	}
}

func TestTrimSilenceWithNothingToRemoveIs409(t *testing.T) {
	r, dir := newTestAPI(t)
	writeGapTake(t, dir, "jam_a.wav", 2)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/take/trim-silence?file=jam_a.wav", nil))
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestTrimSilenceTakesOptionsFromTheBody(t *testing.T) {
	r, dir := newTestAPI(t)
	writeGapTake(t, dir, "jam_a.wav", 6)
	// The 6 s gap is under the 15 s default but not under 5 s.
	req := httptest.NewRequest(http.MethodPost, "/api/take/trim-silence?file=jam_a.wav",
		strings.NewReader(`{"min_seconds":5,"keep_seconds":0.5}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	bad := httptest.NewRequest(http.MethodPost, "/api/take/trim-silence?file=jam_a.wav", strings.NewReader(`{"min_seconds":`))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, bad)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", w.Code)
	}
}
