package phoneclient

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Live serves a tap's audio to whoever connects, for the phone relay
// (web/static/lib/phone/relay.js): a WebSocket that says
// {"type":"hello","rate":N} and then sends the audio as binary interleaved
// stereo float32, live. It keeps nothing: audio with no client connected is
// dropped, and a new client replaces the old. The Pi's take is the record;
// this is the first hop to it.
//
// It listens, which the tap otherwise never does (docs/plan/inline-tap.md:
// outbound only). It is for the relay mode, on the room's network, and sends
// nothing but sound; any origin may connect because it carries no secret and
// accepts no input.
type Live struct {
	Rate int

	mu   sync.Mutex
	conn *websocket.Conn
}

var liveUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// Handler is the WebSocket endpoint.
func (l *Live) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := liveUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hello, _ := json.Marshal(map[string]any{"type": "hello", "rate": l.Rate})
		if c.WriteMessage(websocket.TextMessage, hello) != nil {
			c.Close()
			return
		}
		l.mu.Lock()
		old := l.conn
		l.conn = c
		l.mu.Unlock()
		if old != nil {
			old.Close()
		}
		// Nothing is read from a client, but reading is how a close is noticed.
		go func() {
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					l.drop(c)
					return
				}
			}
		}()
	})
}

func (l *Live) drop(c *websocket.Conn) {
	l.mu.Lock()
	if l.conn == c {
		l.conn = nil
	}
	l.mu.Unlock()
	c.Close()
}

// Pump sends every chunk to the connected client until chunks closes or ctx
// ends. A slow client is dropped rather than waited for: live audio that
// arrives late is worse than a gap, and the gap is the Uploader's to report.
func (l *Live) Pump(ctx context.Context, chunks <-chan []float32) {
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-chunks:
			if !ok {
				return
			}
			l.mu.Lock()
			conn := l.conn
			l.mu.Unlock()
			if conn == nil {
				continue
			}
			b := make([]byte, 4*len(c))
			for i, s := range c {
				binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(s))
			}
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
				l.drop(conn)
			}
		}
	}
}

// Serve listens on addr and pumps chunks to the client, until ctx ends.
func (l *Live) Serve(ctx context.Context, addr string, chunks <-chan []float32) error {
	mux := http.NewServeMux()
	mux.Handle("/stream", l.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { l.Pump(ctx, chunks); cancel() }()
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
