package audio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// The analysis sidecar, <stem>.analysis.json, holds what a program worked
// out about a take's audio, as opposed to what its owner said about it (the
// .meta.json): a transcript, the instruments heard, a harmonic fingerprint.
//
// It is one file with one section per analyser, keyed by the analyser's name,
// so a new analyser adds a key and not another sidecar to takeFiles, the
// startup sweep and the trash. It is kept apart from Meta for the reasons
// the rest of the sidecars are apart: Meta is rewritten by every edit a
// person makes and by Undo, and derived data that a machine can recompute
// must never ride along in that history, or have an analyser's slow
// read-modify-write queue behind a flag edit.
//
// A section is opaque to this package: the analyser that wrote it reads it
// back. Sections are written under the take's lock like every other sidecar
// (LockTake), so an analyser finishing while the take is deleted or trashed
// cannot recreate a sidecar for a take that is gone.

// AnalysisVersion is the file's schema version, not any section's.
const AnalysisVersion = 1

// ErrNewerAnalysis is a sidecar written by a newer build than this one.
var ErrNewerAnalysis = errors.New("analysis sidecar is from a newer version")

type analysisFile struct {
	Version  int                        `json:"version"`
	Sections map[string]json.RawMessage `json:"sections"`
}

// AnalysisPath is the analysis sidecar for a take's wav path. It is
// exported for the packages that write sections; the name is the one
// takeFiles, the trash and the startup sweep agree on.
func AnalysisPath(wav string) string { return strings.TrimSuffix(wav, ".wav") + ".analysis.json" }

func emptyAnalysis() analysisFile {
	return analysisFile{Version: AnalysisVersion, Sections: map[string]json.RawMessage{}}
}

func readAnalysisFile(wav string) (analysisFile, error) {
	b, err := os.ReadFile(AnalysisPath(wav))
	if errors.Is(err, os.ErrNotExist) {
		return emptyAnalysis(), nil
	}
	if err != nil {
		return analysisFile{}, err
	}
	var f analysisFile
	if err := json.Unmarshal(b, &f); err != nil {
		// A torn or hand-edited file is derived data: treat it as empty
		// and let the analysers rebuild it, rather than wedge the take.
		return emptyAnalysis(), nil
	}
	if f.Version > AnalysisVersion {
		return analysisFile{}, ErrNewerAnalysis
	}
	if f.Sections == nil {
		f.Sections = map[string]json.RawMessage{}
	}
	return f, nil
}

// ReadAnalysis decodes one section of a take's analysis into v. It reports
// false, with no error, when the take has no such section. It takes no lock:
// every write is a rename, so a read sees a whole file or the one before.
func ReadAnalysis(wav, key string, v any) (bool, error) {
	f, err := readAnalysisFile(wav)
	if err != nil {
		return false, err
	}
	raw, ok := f.Sections[key]
	if !ok {
		return false, nil
	}
	if v == nil {
		return true, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("analysis section %q: %w", key, err)
	}
	return true, nil
}

// ReadAnalysisSections returns every section of a take's analysis, raw, for
// the endpoint that hands them to the page. A take with none answers an
// empty map.
func ReadAnalysisSections(wav string) (map[string]json.RawMessage, error) {
	f, err := readAnalysisFile(wav)
	if err != nil {
		return nil, err
	}
	return f.Sections, nil
}

// AnalysisKeys lists a take's sections, sorted. A missing or unreadable
// sidecar lists none: this feeds the takes list, which must never fail on
// derived data.
func AnalysisKeys(wav string) []string {
	f, err := readAnalysisFile(wav)
	if err != nil || len(f.Sections) == 0 {
		return nil
	}
	keys := make([]string, 0, len(f.Sections))
	for k := range f.Sections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetAnalysis writes one section of a take's analysis, keeping the others,
// under the take's lock. Callers must not hold the lock themselves (it is
// not reentrant). It writes nothing, and answers ErrTakeGone, if the take's
// WAV is no longer there: an analyser that took minutes over a take must not
// leave its result behind a delete.
func SetAnalysis(wav, key string, v any) error {
	if key == "" {
		return errors.New("analysis section needs a name")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	unlock := LockTake(wav)
	defer unlock()
	if _, err := os.Stat(wav); err != nil {
		return ErrTakeGone
	}
	f, err := readAnalysisFile(wav)
	if err != nil {
		return err
	}
	f.Version = AnalysisVersion
	f.Sections[key] = raw
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(AnalysisPath(wav), b, ".analysis-*.tmp")
}

// ErrTakeGone is a write to a take that was deleted or trashed meanwhile.
var ErrTakeGone = errors.New("take is gone")
