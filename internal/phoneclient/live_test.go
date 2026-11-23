package phoneclient

import (
	"context"
	"encoding/binary"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dialLive(t *testing.T, srvURL string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srvURL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	return c
}

func TestLiveSaysHelloThenStreamsAndDropsWhenNobodyListens(t *testing.T) {
	l := &Live{Rate: 48000}
	srv := httptest.NewServer(l.Handler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks := make(chan []float32)
	go l.Pump(ctx, chunks)

	// Nobody connected: sent audio is dropped, and does not block the source.
	chunks <- []float32{9, 9}

	c := dialLive(t, srv.URL)
	defer c.Close()
	var hello struct {
		Type string `json:"type"`
		Rate int    `json:"rate"`
	}
	if err := c.ReadJSON(&hello); err != nil || hello.Type != "hello" || hello.Rate != 48000 {
		t.Fatalf("%+v %v", hello, err)
	}
	// The handler registers the client just after writing the hello.
	time.Sleep(50 * time.Millisecond)
	chunks <- []float32{0.5, -0.25}
	mt, b, err := c.ReadMessage()
	if err != nil || mt != websocket.BinaryMessage || len(b) != 8 ||
		math.Float32frombits(binary.LittleEndian.Uint32(b)) != 0.5 || math.Float32frombits(binary.LittleEndian.Uint32(b[4:])) != -0.25 {
		t.Fatalf("%v % x %v", mt, b, err)
	}
}

func TestANewClientReplacesTheOldOne(t *testing.T) {
	l := &Live{Rate: 10}
	srv := httptest.NewServer(l.Handler())
	defer srv.Close()
	a := dialLive(t, srv.URL)
	a.ReadMessage() // hello
	time.Sleep(50 * time.Millisecond)
	b := dialLive(t, srv.URL)
	b.ReadMessage()
	time.Sleep(50 * time.Millisecond)
	if _, _, err := a.ReadMessage(); err == nil {
		t.Fatal("the first client should have been closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan []float32, 1)
	go l.Pump(ctx, ch)
	ch <- []float32{1, 1}
	if _, _, err := b.ReadMessage(); err != nil {
		t.Fatal(err)
	}
}
