package transcribe

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/audio"
)

func TestVTT(t *testing.T) {
	got := VTT([]Segment{{Start: 1.5, End: 3661.25, Text: " hello "}, {Start: 4, End: 5, Text: "  "}, {Start: 6, End: 7, Text: "again"}})
	want := "WEBVTT\n\n1\n00:00:01.500 --> 01:01:01.250\nhello\n\n2\n00:00:06.000 --> 00:00:07.000\nagain\n\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestEncodeWAV16(t *testing.T) {
	w := EncodeWAV16([]float32{0, 1, -1, 2}, 12000)
	if len(w) != 44+8 || string(w[:4]) != "RIFF" || string(w[8:12]) != "WAVE" {
		t.Fatalf("bad header, len %d", len(w))
	}
	if got := int16(binary.LittleEndian.Uint16(w[46:])); got != 32767 {
		t.Fatalf("1.0 -> %d", got)
	}
	if got := int16(binary.LittleEndian.Uint16(w[48:])); got != -32767 {
		t.Fatalf("-1.0 -> %d", got)
	}
	if got := int16(binary.LittleEndian.Uint16(w[50:])); got != 32767 {
		t.Fatalf("clipping: %d", got)
	}
	if s := wavSeconds(w); s != 4.0/12000 {
		t.Fatalf("wavSeconds %v", s)
	}
}

func TestHTTPTranscribe(t *testing.T) {
	var gotAuth, gotModel, gotFormat string
	var gotFile int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		gotModel, gotFormat = r.FormValue("model"), r.FormValue("response_format")
		f, _, _ := r.FormFile("file")
		b, _ := io.ReadAll(f)
		gotFile = len(b)
		json.NewEncoder(w).Encode(map[string]any{"language": "english", "text": " hi there ",
			"segments": []map[string]any{{"start": 0.5, "end": 2, "text": " hi there "}, {"start": 3, "end": 4, "text": ""}}})
	}))
	defer srv.Close()

	h := &HTTP{URL: srv.URL + "/v1/", APIKey: "sekret"}
	wav := EncodeWAV16(make([]float32, 100), 12000)
	res, err := h.Transcribe(context.Background(), wav, "a.wav")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sekret" || gotModel != "whisper-1" || gotFormat != "verbose_json" || gotFile != len(wav) {
		t.Fatalf("request: %q %q %q %d", gotAuth, gotModel, gotFormat, gotFile)
	}
	if res.Text != "hi there" || len(res.Segments) != 1 || res.Segments[0].Start != 0.5 {
		t.Fatalf("result %+v", res)
	}
}

func TestHTTPErrorsDoNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", 1000), 401)
	}))
	defer srv.Close()
	_, err := (&HTTP{URL: srv.URL, APIKey: "sekret"}).Transcribe(context.Background(), []byte("x"), "a.wav")
	if err == nil || strings.Contains(err.Error(), "sekret") || len(err.Error()) > 500 || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	if _, err := (&HTTP{}).Transcribe(context.Background(), nil, "a"); err == nil {
		t.Fatal("no URL should fail")
	}
}

func TestHTTPTextOnlyAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"text":"just words"}`))
	}))
	defer srv.Close()
	res, err := (&HTTP{URL: srv.URL}).Transcribe(context.Background(), []byte("x"), "a.wav")
	if err != nil || len(res.Segments) != 1 || res.Segments[0].Text != "just words" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestDemo(t *testing.T) {
	w := EncodeWAV16(make([]float32, 25*12000), 12000)
	res, err := Demo{}.Transcribe(context.Background(), w, "a.wav")
	if err != nil || len(res.Segments) != 3 || res.Segments[2].Start != 20 {
		t.Fatalf("%+v %v", res, err)
	}
}

// fake records what it was sent and answers one segment per piece.
type fake struct {
	mu    sync.Mutex
	sizes []float64
	fail  error
}

func (f *fake) Transcribe(ctx context.Context, wav []byte, name string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Result{}, f.fail
	}
	f.sizes = append(f.sizes, wavSeconds(wav))
	return Result{Language: "en", Text: name, Segments: []Segment{{Start: 1, End: 2, Text: name}}}, nil
}

// writeTone writes a mono 12 kHz 32-bit take (the format takes are saved in)
// of the given seconds, a sawtooth at amp.
func writeTone(t *testing.T, seconds int, amp float64) analyze.Input {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "take.wav")
	const rate = 12000
	n := seconds * rate
	pcm := make([]byte, n*4)
	for i := 0; i < n; i++ {
		v := int32(amp * 2147483647 * float64(i%40-20) / 20)
		binary.LittleEndian.PutUint32(pcm[i*4:], uint32(v))
	}
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(pcm)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], rate)
	binary.LittleEndian.PutUint32(h[28:], rate*4)
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 32)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(pcm)))
	if err := os.WriteFile(path, append(h, pcm...), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := audio.ReadWAVInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	return analyze.Input{Dir: dir, Name: "take.wav", Path: path, Info: info}
}

func TestCaptionsChunksAndOffsets(t *testing.T) {
	in := writeTone(t, chunkSeconds+30, 0.3) // two pieces: 600 s and 30 s
	f := &fake{}
	v, err := Captions{T: f}.Analyze(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	d := v.(CaptionsData)
	if len(f.sizes) != 2 || f.sizes[0] != chunkSeconds || f.sizes[1] != 30 {
		t.Fatalf("pieces sent: %v", f.sizes)
	}
	if len(d.Segments) != 2 || d.Segments[0].Start != 1 || d.Segments[1].Start != chunkSeconds+1 {
		t.Fatalf("segments %+v", d.Segments)
	}
	if d.Language != "en" || d.Text != "piece-0.wav piece-1.wav" {
		t.Fatalf("%+v", d)
	}
}

func TestCaptionsSkipsSilence(t *testing.T) {
	in := writeTone(t, 20, 0.0001) // ~ -80 dBFS
	f := &fake{}
	v, err := Captions{T: f}.Analyze(context.Background(), in)
	if err != nil || len(f.sizes) != 0 || len(v.(CaptionsData).Segments) != 0 {
		t.Fatalf("sent %v, %v, %v", f.sizes, v, err)
	}
	// And the empty answer still marshals as [] not null.
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"segments":[]`) {
		t.Fatalf("json %s", b)
	}
}

func TestCaptionsPropagatesErrorsAndCancel(t *testing.T) {
	in := writeTone(t, 5, 0.3)
	if _, err := (Captions{T: &fake{fail: context.DeadlineExceeded}}).Analyze(context.Background(), in); err == nil {
		t.Fatal("service error should fail the analysis")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Captions{T: &fake{}}).Analyze(ctx, in); err == nil {
		t.Fatal("cancelled context should stop")
	}
	if _, err := (Captions{}).Analyze(context.Background(), in); err == nil {
		t.Fatal("no transcriber should fail")
	}
}

func TestKeyVersion(t *testing.T) {
	var a analyze.Analyzer = Captions{}
	if a.Key() != "captions" || a.Version() != 1 {
		t.Fatal("key/version")
	}
}
