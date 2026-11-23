// Package phoneclient is the Go end of the Pi's /api/phone recording
// protocol (internal/api/phone.go), for a sender that is not a phone's
// browser: an inline tap, a script, a second Pi. It is the same protocol as
// web/static/lib/phone/uploader.js and keeps its promises:
//
//   - audio goes up in numbered chunks (stereo float32) and stays queued
//     here until the Pi acks it;
//   - when the connection drops, it reconnects with the same recording id,
//     the Pi says which chunk it wants next, and everything from there is
//     sent again, so a WiFi blink loses nothing;
//   - Run returns when the Pi has saved the take, with its name.
//
// The queue is bounded: a link that stays down longer than MaxQueue seconds
// of audio loses the oldest chunks (the Pi starts the take from the oldest
// one held, `first`, rather than wait for audio that is gone).
package phoneclient

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Result is how a recording ended.
type Result struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Partial bool    `json:"partial"`
	Reason  string  `json:"reason"`
}

// Sender streams one recording.
type Sender struct {
	URL  string // ws://pi/api/phone or wss://…
	Rate int    // sample rate of the chunks
	ID   string // the recording's id; made if empty. Reuse it to resume a recording

	// MaxQueueChunks is how many unacked chunks are kept (default 6000: ten
	// minutes of tenth-of-a-second chunks).
	MaxQueueChunks int
	RetryMin       time.Duration // default 500 ms
	RetryMax       time.Duration // default 5 s
	// Silent is how long a connection may hear nothing while chunks are
	// outstanding before it is replaced (default 10 s).
	Silent time.Duration
	Dialer *websocket.Dialer
	// OnState hears 'connecting', 'recording', 'reconnecting', 'saving'.
	OnState func(state string)

	mu    sync.Mutex
	queue []chunk
	seq   uint32 // the next chunk number
	done  bool   // the source has ended; stopAt is final
}

type chunk struct {
	seq uint32
	msg []byte
}

// NewID makes a recording id the Pi accepts.
func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Encode is one binary message: uint32 LE chunk number, then float32 LE samples.
func Encode(seq uint32, samples []float32) []byte {
	b := make([]byte, 4+4*len(samples))
	binary.LittleEndian.PutUint32(b, seq)
	for i, s := range samples {
		binary.LittleEndian.PutUint32(b[4+4*i:], math.Float32bits(s))
	}
	return b
}

func (s *Sender) defaults() {
	if s.ID == "" {
		s.ID = NewID()
	}
	if s.MaxQueueChunks == 0 {
		s.MaxQueueChunks = 6000
	}
	if s.RetryMin == 0 {
		s.RetryMin = 500 * time.Millisecond
	}
	if s.RetryMax == 0 {
		s.RetryMax = 5 * time.Second
	}
	if s.Silent == 0 {
		s.Silent = 10 * time.Second
	}
	if s.Dialer == nil {
		s.Dialer = websocket.DefaultDialer
	}
}

func (s *Sender) state(st string) {
	if s.OnState != nil {
		s.OnState(st)
	}
}

// push queues one chunk of interleaved stereo samples.
func (s *Sender) push(samples []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, chunk{seq: s.seq, msg: Encode(s.seq, samples)})
	s.seq++
	if over := len(s.queue) - s.MaxQueueChunks; over > 0 {
		s.queue = append([]chunk(nil), s.queue[over:]...)
	}
}

func (s *Sender) ack(next uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := 0
	for i < len(s.queue) && s.queue[i].seq < next {
		i++
	}
	s.queue = s.queue[i:]
}

func (s *Sender) snapshot() (first uint32, total uint32, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	first = s.seq
	if len(s.queue) > 0 {
		first = s.queue[0].seq
	}
	return first, s.seq, s.done
}

// Run sends chunks (interleaved stereo float32, Rate Hz) until the channel is
// closed, then stops the recording and returns what the Pi saved. It returns
// early, with an error, if ctx ends or the Pi refuses the recording; a
// recording that can't start is not retried.
func (s *Sender) Run(ctx context.Context, chunks <-chan []float32) (Result, error) {
	s.defaults()
	if s.Rate <= 0 {
		return Result{}, errors.New("phoneclient: no sample rate")
	}
	wake := make(chan struct{}, 1)
	kick := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go func() {
		for c := range chunks {
			s.push(c)
			kick()
		}
		s.mu.Lock()
		s.done = true
		s.mu.Unlock()
		kick()
	}()

	backoff := s.RetryMin
	s.state("connecting")
	for {
		res, finished, err := s.session(ctx, wake)
		if finished || err != nil && !retryable(err) {
			return res, err
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		s.state("reconnecting")
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
		if backoff *= 2; backoff > s.RetryMax {
			backoff = s.RetryMax
		}
	}
}

// refused is an error the Pi gave for the recording itself.
type refused string

func (r refused) Error() string { return "the Pi refused the recording: " + string(r) }

func retryable(err error) bool {
	var r refused
	return !errors.As(err, &r)
}

type incoming struct {
	Type    string  `json:"type"`
	Next    uint32  `json:"next"`
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Partial bool    `json:"partial"`
	Reason  string  `json:"reason"`
	Error   string  `json:"error"`
}

// session is one connection's life. finished is true when the Pi saved the take.
func (s *Sender) session(ctx context.Context, wake <-chan struct{}) (res Result, finished bool, err error) {
	conn, _, err := s.Dialer.DialContext(ctx, s.URL, nil)
	if err != nil {
		return Result{}, false, err
	}
	defer conn.Close()

	first, _, _ := s.snapshot()
	if err := conn.WriteJSON(map[string]any{"type": "start", "id": s.ID, "rate": s.Rate, "first": first}); err != nil {
		return Result{}, false, err
	}

	msgs := make(chan incoming)
	rerr := make(chan error, 1)
	go func() {
		for {
			var m incoming
			if err := conn.ReadJSON(&m); err != nil {
				rerr <- err
				return
			}
			select {
			case msgs <- m:
			case <-ctx.Done():
				return
			}
		}
	}()

	var ready bool
	var sentTo uint32 // the next chunk number not yet sent on this connection
	var stopSent bool
	lastHeard := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	// send writes every queued chunk this connection hasn't had.
	send := func() error {
		s.mu.Lock()
		var out [][]byte
		for _, c := range s.queue {
			if c.seq >= sentTo {
				out = append(out, c.msg)
				sentTo = c.seq + 1
			}
		}
		s.mu.Unlock()
		for _, m := range out {
			if err := conn.WriteMessage(websocket.BinaryMessage, m); err != nil {
				return err
			}
		}
		_, total, done := s.snapshot()
		if done && !stopSent {
			stopSent = true
			s.state("saving")
			return conn.WriteJSON(map[string]any{"type": "stop", "chunks": total})
		}
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return Result{}, false, ctx.Err()
		case err := <-rerr:
			return Result{}, false, err
		case <-wake:
			if ready {
				if err := send(); err != nil {
					return Result{}, false, err
				}
			}
		case <-tick.C:
			s.mu.Lock()
			waiting := len(s.queue) > 0
			s.mu.Unlock()
			if ready && waiting && time.Since(lastHeard) > s.Silent {
				return Result{}, false, errors.New("the Pi went quiet")
			}
		case m := <-msgs:
			lastHeard = time.Now()
			switch m.Type {
			case "ready":
				ready = true
				s.ack(m.Next)
				sentTo = m.Next // everything still queued goes (again) on this connection
				s.state("recording")
				if err := send(); err != nil {
					return Result{}, false, err
				}
			case "ack":
				s.ack(m.Next)
			case "saved":
				return Result{Name: m.Name, Seconds: m.Seconds, Partial: m.Partial, Reason: m.Reason}, true, nil
			case "error":
				if !ready {
					return Result{}, false, refused(m.Error)
				}
			}
		}
	}
}

// FramesPerChunk is a tenth of a second, the chunk size the page uses.
func FramesPerChunk(rate int) int { return rate / 10 }
