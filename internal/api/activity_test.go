package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gabeduke/hindsight/internal/activity"
	"github.com/gabeduke/hindsight/internal/audio"
)

// writeShortTake writes a one-second stereo 8 kHz take: a real WAV, which the
// list reads the header of.
func writeShortTake(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := audio.WriteWAV(filepath.Join(dir, name), make([]int32, 8000*2), 2, []int{0, 1}, 8000); err != nil {
		t.Fatal(err)
	}
}

func getActivity(t *testing.T, r http.Handler, url string, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestActivityOfAnEmptyShelfIsAnEmptyFeedWithAStrip(t *testing.T) {
	r, _ := newTestAPI(t)
	w := getActivity(t, r, "/api/activity?days=7", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var f activity.Feed
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.Cards == nil || len(f.Cards) != 0 || len(f.Days) != 7 {
		t.Errorf("feed = %+v", f)
	}
}

func TestActivityGroupsTheTakesOnTheShelfAndHonoursTheZone(t *testing.T) {
	r, dir := newTestAPI(t)
	for _, n := range []string{"jam_a.wav", "jam_b.wav"} {
		writeShortTake(t, dir, n)
	}
	w := getActivity(t, r, "/api/activity?tz=Pacific/Auckland&per_card=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var f activity.Feed
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.Zone != "Pacific/Auckland" {
		t.Errorf("zone = %q", f.Zone)
	}
	var sess *activity.Card
	for i := range f.Cards {
		if f.Cards[i].Kind == activity.Session {
			sess = &f.Cards[i]
		}
	}
	if sess == nil || sess.Count != 2 || len(sess.Takes) != 1 {
		t.Errorf("one sitting of two takes, one listed: %+v", f.Cards)
	}
	if bad := getActivity(t, r, "/api/activity?tz=Not/AZone", ""); bad.Code != http.StatusOK {
		t.Errorf("an unknown zone falls back to the Pi's: %d", bad.Code)
	}
}

func TestActivityIsA304WhileNothingChanged(t *testing.T) {
	r, dir := newTestAPI(t)
	writeShortTake(t, dir, "jam_a.wav")
	first := getActivity(t, r, "/api/activity", "")
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	if again := getActivity(t, r, "/api/activity", tag); again.Code != http.StatusNotModified {
		t.Errorf("same list, same day: %d", again.Code)
	}
	// A new take moves the list's tag, so the feed's.
	writeShortTake(t, dir, "jam_b.wav")
	if changed := getActivity(t, r, "/api/activity", tag); changed.Code != http.StatusOK {
		t.Errorf("a new take: %d", changed.Code)
	}
	// A different zone is a different feed.
	if other := getActivity(t, r, "/api/activity?tz=Asia/Tokyo", tag); other.Code != http.StatusOK {
		t.Errorf("another zone: %d", other.Code)
	}
}
