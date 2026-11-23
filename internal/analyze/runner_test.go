package analyze

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

type fake struct {
	key     string
	version int
	calls   atomic.Int32
	err     error
	block   chan struct{}
}

func (f *fake) Key() string  { return f.key }
func (f *fake) Version() int { return f.version }
func (f *fake) Analyze(ctx context.Context, in Input) (any, error) {
	f.calls.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return map[string]any{"name": in.Name, "seconds": in.Info.Duration()}, nil
}

func newDir(t *testing.T, names ...string) (string, func() ([]audio.Take, error)) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if _, err := audio.WriteWAV(filepath.Join(dir, n), make([]int32, 4800*2), 2, []int{0, 1}, 48000); err != nil {
			t.Fatal(err)
		}
	}
	l := audio.NewTakeList(dir)
	return dir, func() ([]audio.Take, error) { ts, _, err := l.List(); return ts, err }
}

func TestPassWritesEachMissingSectionOnce(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav", "jam_b.wav")
	f := &fake{key: "words", version: 1}
	r := &Runner{Dir: dir, Analyzers: []Analyzer{f}, Takes: takes}

	if n := r.Pass(context.Background()); n != 2 {
		t.Fatalf("first pass did %d, want 2", n)
	}
	if n := r.Pass(context.Background()); n != 0 {
		t.Fatalf("second pass did %d, want 0: nothing was missing", n)
	}
	var s Section
	if ok, err := audio.ReadAnalysis(filepath.Join(dir, "jam_a.wav"), "words", &s); !ok || err != nil || s.Version != 1 || s.Data == nil {
		t.Errorf("section = %+v, %v, %v", s, ok, err)
	}
}

func TestANewerAnalyzerVersionRedoesTheSection(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav")
	f := &fake{key: "words", version: 1}
	r := &Runner{Dir: dir, Analyzers: []Analyzer{f}, Takes: takes}
	r.Pass(context.Background())
	f.version = 2
	if n := r.Pass(context.Background()); n != 1 {
		t.Errorf("after a version bump the pass did %d, want 1", n)
	}
	if f.calls.Load() != 2 {
		t.Errorf("calls = %d", f.calls.Load())
	}
}

func TestAFailureIsRememberedUntilTheVersionMoves(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav")
	f := &fake{key: "words", version: 1, err: errors.New("no model")}
	r := &Runner{Dir: dir, Analyzers: []Analyzer{f}, Takes: takes}
	r.Pass(context.Background())
	r.Pass(context.Background())
	if f.calls.Load() != 1 {
		t.Errorf("a failing analyser ran %d times; it must not be retried on every pass", f.calls.Load())
	}
	var s Section
	audio.ReadAnalysis(filepath.Join(dir, "jam_a.wav"), "words", &s)
	if s.Error != "no model" || s.Data != nil {
		t.Errorf("section = %+v, want the error stored and no data", s)
	}
}

func TestPassWaitsForAnIdleRig(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav")
	f := &fake{key: "words", version: 1}
	idle := false
	r := &Runner{Dir: dir, Analyzers: []Analyzer{f}, Takes: takes, Idle: func() bool { return idle }}
	if n := r.Pass(context.Background()); n != 0 || f.calls.Load() != 0 {
		t.Fatalf("ran %d while the rig was busy", n)
	}
	idle = true
	if n := r.Pass(context.Background()); n != 1 {
		t.Fatalf("did %d once idle, want 1", n)
	}
}

func TestACancelledAnalysisLeavesNothingBehind(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav")
	f := &fake{key: "words", version: 1, block: make(chan struct{})}
	r := &Runner{Dir: dir, Analyzers: []Analyzer{f}, Takes: takes}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Pass(ctx); close(done) }()
	for f.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if keys := audio.AnalysisKeys(filepath.Join(dir, "jam_a.wav")); keys != nil {
		t.Errorf("a cancelled run stored %v: it would never be retried", keys)
	}
}

func TestOneAnalysisOfATakeAtATime(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav")
	f := &fake{key: "words", version: 1, block: make(chan struct{})}
	r := &Runner{Dir: dir, Analyzers: []Analyzer{f}, Takes: takes}
	started := make(chan struct{})
	go func() { close(started); r.Analyze(context.Background(), "jam_a.wav", "words") }()
	<-started
	for f.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if _, err := r.Analyze(context.Background(), "jam_a.wav", "words"); !errors.Is(err, ErrBusy) {
		t.Errorf("second request = %v, want ErrBusy", err)
	}
	close(f.block)
}

func TestAnalyzeByNameRefusesStrangers(t *testing.T) {
	dir, takes := newDir(t, "jam_a.wav")
	r := &Runner{Dir: dir, Analyzers: []Analyzer{&fake{key: "words", version: 1}}, Takes: takes}
	if _, err := r.Analyze(context.Background(), "jam_a.wav", "nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("err = %v, want ErrUnknown", err)
	}
	// A path in the name is flattened, never followed out of the takes folder.
	if _, err := r.Analyze(context.Background(), "../../etc/passwd", "words"); err == nil {
		t.Error("analysed a path outside the takes folder")
	}
}

func TestKeys(t *testing.T) {
	r := &Runner{Analyzers: []Analyzer{&fake{key: "a"}, &fake{key: "b"}}}
	if got := r.Keys(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("keys = %v", got)
	}
}
