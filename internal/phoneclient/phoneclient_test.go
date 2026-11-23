package phoneclient

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestEncode(t *testing.T) {
	b := Encode(7, []float32{0.5, -1})
	if len(b) != 12 || binary.LittleEndian.Uint32(b) != 7 || math.Float32frombits(binary.LittleEndian.Uint32(b[8:])) != -1 {
		t.Fatalf("% x", b)
	}
}

func drain(t *testing.T, p PCM, raw []byte) [][]float32 {
	t.Helper()
	ch, _, err := p.Chunks(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]float32
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestPCMChunksMonoS16BecomesStereoTenthSecondChunks(t *testing.T) {
	// 1 kHz rate: a chunk is 100 frames. 250 mono samples: 100, 100, 50.
	raw := make([]byte, 250*2)
	for i := 0; i < 250; i++ {
		binary.LittleEndian.PutUint16(raw[2*i:], uint16(int16(i*100)))
	}
	got := drain(t, PCM{Format: "s16le", Channels: 1, Rate: 1000}, raw)
	if len(got) != 3 || len(got[0]) != 200 || len(got[2]) != 100 {
		t.Fatalf("chunk sizes %d: %v", len(got), []int{len(got[0]), len(got[1]), len(got[2])})
	}
	if got[0][2] != got[0][3] || math.Abs(float64(got[0][2])-100.0/32768) > 1e-9 {
		t.Fatalf("mono should duplicate: %v %v", got[0][2], got[0][3])
	}
}

func TestPCMFormatsAndRefusals(t *testing.T) {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint32(raw, math.Float32bits(0.25))
	binary.LittleEndian.PutUint32(raw[4:], math.Float32bits(-0.5))
	got := drain(t, PCM{Format: "f32le", Channels: 2, Rate: 10}, raw)
	if len(got) != 1 || got[0][0] != 0.25 || got[0][1] != -0.5 {
		t.Fatalf("%v", got)
	}
	// A torn last frame is dropped, not sent half-stereo.
	if got := drain(t, PCM{Format: "s32le", Channels: 2, Rate: 10}, make([]byte, 12)); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("%v", got)
	}
	for _, p := range []PCM{{Format: "mp3", Channels: 2, Rate: 1}, {Format: "s16le", Channels: 3, Rate: 1}, {Format: "s16le", Channels: 2}} {
		if _, _, err := p.Chunks(bytes.NewReader(nil)); err == nil {
			t.Errorf("%+v should be refused", p)
		}
	}
}
