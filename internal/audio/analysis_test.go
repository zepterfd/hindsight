package audio

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type testSection struct {
	Words []string `json:"words"`
	N     int      `json:"n"`
}

func writeEmptyTake(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if _, err := WriteWAV(p, make([]int32, 4800*2), 2, []int{0, 1}, 48000); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnalysisSectionsAreKeptApart(t *testing.T) {
	wav := writeEmptyTake(t, t.TempDir(), "jam_a.wav")
	if err := SetAnalysis(wav, "captions", testSection{Words: []string{"hello"}, N: 1}); err != nil {
		t.Fatal(err)
	}
	if err := SetAnalysis(wav, "instruments", testSection{N: 2}); err != nil {
		t.Fatal(err)
	}
	var c, i testSection
	if ok, err := ReadAnalysis(wav, "captions", &c); !ok || err != nil || c.N != 1 || c.Words[0] != "hello" {
		t.Errorf("captions = %+v, %v, %v", c, ok, err)
	}
	if ok, err := ReadAnalysis(wav, "instruments", &i); !ok || err != nil || i.N != 2 {
		t.Errorf("instruments = %+v, %v, %v; writing a second section must keep the first", i, ok, err)
	}
	if got, want := AnalysisKeys(wav), []string{"captions", "instruments"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	// Rewriting a section replaces only it.
	if err := SetAnalysis(wav, "captions", testSection{N: 9}); err != nil {
		t.Fatal(err)
	}
	ReadAnalysis(wav, "captions", &c)
	if c.N != 9 || len(c.Words) != 0 {
		t.Errorf("captions after rewrite = %+v", c)
	}
}

func TestAnalysisMissingIsNotAnError(t *testing.T) {
	wav := writeEmptyTake(t, t.TempDir(), "jam_a.wav")
	var s testSection
	if ok, err := ReadAnalysis(wav, "captions", &s); ok || err != nil {
		t.Errorf("a take with no analysis read as %v, %v", ok, err)
	}
	if keys := AnalysisKeys(wav); keys != nil {
		t.Errorf("keys = %v, want none", keys)
	}
}

func TestAnalysisTornFileReadsAsEmptyAndHeals(t *testing.T) {
	wav := writeEmptyTake(t, t.TempDir(), "jam_a.wav")
	if err := os.WriteFile(AnalysisPath(wav), []byte(`{"version":1,"sections":{"caption`), 0o644); err != nil {
		t.Fatal(err)
	}
	if keys := AnalysisKeys(wav); keys != nil {
		t.Errorf("torn file listed %v", keys)
	}
	if err := SetAnalysis(wav, "captions", testSection{N: 3}); err != nil {
		t.Fatalf("a torn sidecar must not block the next write: %v", err)
	}
	var s testSection
	if ok, _ := ReadAnalysis(wav, "captions", &s); !ok || s.N != 3 {
		t.Errorf("after healing: %+v %v", s, ok)
	}
}

func TestAnalysisFromANewerBuildIsNotOverwritten(t *testing.T) {
	wav := writeEmptyTake(t, t.TempDir(), "jam_a.wav")
	if err := os.WriteFile(AnalysisPath(wav), []byte(`{"version":99,"sections":{"x":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetAnalysis(wav, "captions", testSection{}); !errors.Is(err, ErrNewerAnalysis) {
		t.Errorf("err = %v, want ErrNewerAnalysis", err)
	}
	b, _ := os.ReadFile(AnalysisPath(wav))
	if string(b) != `{"version":99,"sections":{"x":1}}` {
		t.Errorf("a newer file was rewritten: %s", b)
	}
}

func TestAnalysisNeverOutlivesItsTake(t *testing.T) {
	dir := t.TempDir()
	wav := writeEmptyTake(t, dir, "jam_a.wav")
	if err := os.Remove(wav); err != nil {
		t.Fatal(err)
	}
	if err := SetAnalysis(wav, "captions", testSection{}); !errors.Is(err, ErrTakeGone) {
		t.Errorf("err = %v, want ErrTakeGone", err)
	}
	if _, err := os.Stat(AnalysisPath(wav)); err == nil {
		t.Error("a deleted take got an analysis sidecar")
	}
}

func TestAnalysisTravelsWithTheTake(t *testing.T) {
	dir := t.TempDir()
	wav := writeEmptyTake(t, dir, "jam_a.wav")
	if err := SetAnalysis(wav, "captions", testSection{N: 1}); err != nil {
		t.Fatal(err)
	}
	if err := TrashTake(dir, "jam_a.wav", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(AnalysisPath(wav)); err == nil {
		t.Error("the analysis stayed behind when the take was trashed")
	}
	if _, err := os.Stat(filepath.Join(TrashDir(dir), "jam_a", "jam_a.analysis.json")); err != nil {
		t.Errorf("the analysis is not in the trash with its take: %v", err)
	}
	if stem, ok := sidecarStem("jam_a.analysis.json"); !ok || stem != "jam_a" {
		t.Errorf("sidecarStem = %q, %v", stem, ok)
	}
	if stem, ok := internalSidecarStem("jam_a.analysis.json"); !ok || stem != "jam_a" {
		t.Errorf("internalSidecarStem = %q, %v", stem, ok)
	}
}

func TestTakeListSeesAnAnalysisFinish(t *testing.T) {
	dir := t.TempDir()
	wav := writeEmptyTake(t, dir, "jam_a.wav")
	l := NewTakeList(dir)
	_, before, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetAnalysis(wav, "captions", testSection{N: 1}); err != nil {
		t.Fatal(err)
	}
	takes, after, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("the list's ETag did not move when an analysis was written, so no page would refetch")
	}
	if len(takes) != 1 || !reflect.DeepEqual(takes[0].Analysis, []string{"captions"}) {
		t.Errorf("take.Analysis = %v", takes[0].Analysis)
	}
}
