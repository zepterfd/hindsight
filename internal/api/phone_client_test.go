package api

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gabeduke/hindsight/internal/phoneclient"
	"github.com/gorilla/websocket"
)

// cutter is a net.Conn that dies after n bytes written, once.
type cutter struct {
	net.Conn
	left atomic.Int64
}

func (c *cutter) Write(p []byte) (int, error) {
	if c.left.Add(-int64(len(p))) < 0 {
		c.Conn.Close()
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}

func feed(n int) <-chan []float32 {
	ch := make(chan []float32)
	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			ch <- phoneChunk(i)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return ch
}

func TestPhoneClientRecordsAndSurvivesALinkThatDrops(t *testing.T) {
	srv, dir := newPhoneServer(t, time.Minute)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/phone"

	var dials atomic.Int32
	d := &websocket.Dialer{NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if dials.Add(1) == 1 {
			cc := &cutter{Conn: c}
			cc.left.Store(120 << 10) // a few chunks, then the WiFi blinks
			return cc, nil
		}
		return c, nil
	}}
	s := &phoneclient.Sender{URL: url, Rate: 48000, Dialer: d, RetryMin: 20 * time.Millisecond, RetryMax: 50 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := s.Run(ctx, feed(12))
	if err != nil {
		t.Fatal(err)
	}
	if dials.Load() < 2 {
		t.Fatalf("the link never dropped (%d dials): the test proves nothing", dials.Load())
	}
	if res.Partial || res.Reason != "stop" || res.Name == "" {
		t.Fatalf("%+v", res)
	}
	checkPhoneTake(t, dir, res.Name, 12) // every chunk, in order, none twice
	waitForPreview(dir, res.Name)
}

func TestPhoneClientDoesNotRetryARefusedRecording(t *testing.T) {
	srv, _ := newPhoneServer(t, time.Minute)
	s := &phoneclient.Sender{URL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/phone", Rate: 48000, ID: "bad"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.Run(ctx, feed(1))
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestPhoneClientStopsWhenTheContextEnds(t *testing.T) {
	s := &phoneclient.Sender{URL: "ws://127.0.0.1:1/api/phone", Rate: 48000, RetryMin: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := s.Run(ctx, make(chan []float32)); err == nil {
		t.Fatal("an unreachable Pi with a deadline should end with an error")
	}
}
