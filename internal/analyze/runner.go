package analyze

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gabeduke/hindsight/internal/audio"
)

// Runner feeds takes to analysers. The zero value is not usable; fill Dir and
// Analyzers.
type Runner struct {
	Dir       string
	Analyzers []Analyzer

	// Takes lists the takes, newest first as the shelf does. The binary
	// hands in the takes list's cached List, so a pass costs one directory
	// listing, not a read of every take.
	Takes func() ([]audio.Take, error)
	// Idle says whether it is a good moment to spend CPU: no save, no phone
	// recording, nothing playing. Nil means always.
	Idle func() bool
	// Every is the pause between passes. Zero is a minute.
	Every time.Duration
	// Now stands in for the clock in tests.
	Now func() time.Time

	mu      sync.Mutex
	running map[string]bool // "take|key" in flight, so a request and a pass never double up
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Keys lists the analysers' section names, in order.
func (r *Runner) Keys() []string {
	keys := make([]string, 0, len(r.Analyzers))
	for _, a := range r.Analyzers {
		keys = append(keys, a.Key())
	}
	return keys
}

func (r *Runner) find(key string) Analyzer {
	for _, a := range r.Analyzers {
		if a.Key() == key {
			return a
		}
	}
	return nil
}

// ErrUnknown is a section name no analyser answers to.
var ErrUnknown = errors.New("no such analysis")

// ErrBusy is an analysis of that take already under way.
var ErrBusy = errors.New("already analysing")

// Busy reports whether that analysis of that take is under way.
func (r *Runner) Busy(name, key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[filepath.Join(r.Dir, filepath.Base(name))+"|"+key]
}

// Current reports whether the take holds a section from the analyser's own
// version or a newer one. A section that failed counts as current: it is
// tried again only when the analyser's version moves.
func Current(path string, a Analyzer) bool {
	var s struct {
		Version int `json:"v"`
	}
	ok, err := audio.ReadAnalysis(path, a.Key(), &s)
	return err == nil && ok && s.Version >= a.Version()
}

// Analyze runs one analyser on one take now and stores the result, replacing
// any section already there. It is what a person's request calls, and what a
// pass calls for each take it finds missing something. A failure of the
// analyser is stored as the section's Error and returned.
func (r *Runner) Analyze(ctx context.Context, name, key string) (Section, error) {
	a := r.find(key)
	if a == nil {
		return Section{}, ErrUnknown
	}
	path := filepath.Join(r.Dir, filepath.Base(name))
	slot := path + "|" + key
	r.mu.Lock()
	if r.running == nil {
		r.running = map[string]bool{}
	}
	if r.running[slot] {
		r.mu.Unlock()
		return Section{}, ErrBusy
	}
	r.running[slot] = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.running, slot); r.mu.Unlock() }()

	info, err := audio.ReadWAVInfo(path)
	if err != nil {
		return Section{}, err
	}
	in := Input{Dir: r.Dir, Name: filepath.Base(name), Path: path, Info: info, Meta: audio.ReadMeta(path)}
	start := r.now()
	data, aerr := a.Analyze(ctx, in)
	if ctx.Err() != nil {
		// Cancelled mid-way (shutdown, a client that left): leave no
		// section behind, so the next pass starts it over rather than
		// remembering a half-answer as a failure.
		return Section{}, ctx.Err()
	}
	sec := Section{Version: a.Version(), At: start, Duration: r.now().Sub(start).Seconds()}
	if aerr != nil {
		sec.Error = aerr.Error()
	} else {
		sec.Data = data
	}
	if err := audio.SetAnalysis(path, key, sec); err != nil {
		return sec, err
	}
	if aerr != nil {
		return sec, fmt.Errorf("%s on %s: %w", key, name, aerr)
	}
	return sec, nil
}

// Pass analyses what is missing, newest takes first, and returns how many
// sections it wrote. It stops when ctx ends, or the rig stops being idle
// (checked between analyses, so one already running finishes).
func (r *Runner) Pass(ctx context.Context) int {
	if r.Takes == nil || len(r.Analyzers) == 0 {
		return 0
	}
	takes, err := r.Takes()
	if err != nil {
		log.Printf("[!] analyse: listing takes: %v", err)
		return 0
	}
	done := 0
	for _, t := range takes {
		for _, a := range r.Analyzers {
			if ctx.Err() != nil || (r.Idle != nil && !r.Idle()) {
				return done
			}
			path := filepath.Join(r.Dir, t.Name)
			if Current(path, a) || !analysable(t) {
				continue
			}
			if _, err := r.Analyze(ctx, t.Name, a.Key()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, os.ErrNotExist) ||
					errors.Is(err, audio.ErrTakeGone) || errors.Is(err, ErrBusy) {
					continue
				}
				log.Printf("[!] %v", err)
			}
			done++
		}
	}
	return done
}

// analysable is whether a take is complete enough to read: a 32-bit WAV
// with some audio in it. Takes this app did not write are left alone.
func analysable(t audio.Take) bool { return t.Duration > 0 }

// Run passes until ctx ends: one now, then one every Every. It is meant for
// a goroutine of its own.
func (r *Runner) Run(ctx context.Context) {
	every := r.Every
	if every <= 0 {
		every = time.Minute
	}
	for {
		r.Pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
