package phoneclient

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// PCM describes raw samples on a reader, as `arecord -t raw` or
// `ffmpeg -f s16le -` write them.
type PCM struct {
	Format   string // "s16le", "s32le" or "f32le"
	Channels int    // 1 or 2; a mono source is sent as both channels
	Rate     int
}

func (p PCM) width() (int, error) {
	switch p.Format {
	case "s16le":
		return 2, nil
	case "s32le", "f32le":
		return 4, nil
	}
	return 0, fmt.Errorf("phoneclient: format %q is not s16le, s32le or f32le", p.Format)
}

// Chunks reads r to the end and sends it as tenth-of-a-second stereo float32
// chunks, closing the channel at the end. A short last chunk is sent as is.
// A read error ends the stream (and is returned on errc) after what was read.
func (p PCM) Chunks(r io.Reader) (<-chan []float32, <-chan error, error) {
	w, err := p.width()
	if err != nil {
		return nil, nil, err
	}
	if p.Channels != 1 && p.Channels != 2 {
		return nil, nil, fmt.Errorf("phoneclient: %d channels: only 1 or 2", p.Channels)
	}
	if p.Rate <= 0 {
		return nil, nil, fmt.Errorf("phoneclient: no sample rate")
	}
	frame := w * p.Channels
	buf := make([]byte, FramesPerChunk(p.Rate)*frame)
	out := make(chan []float32)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		for {
			n, err := io.ReadFull(r, buf)
			n -= n % frame
			if n > 0 {
				out <- p.decode(buf[:n], w)
			}
			if err != nil {
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					errc <- err
				}
				return
			}
		}
	}()
	return out, errc, nil
}

func (p PCM) decode(b []byte, w int) []float32 {
	frames := len(b) / (w * p.Channels)
	out := make([]float32, 2*frames)
	sample := func(i int) float32 {
		switch p.Format {
		case "s16le":
			return float32(int16(binary.LittleEndian.Uint16(b[i:]))) / 32768
		case "s32le":
			return float32(int32(binary.LittleEndian.Uint32(b[i:]))) / 2147483648
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(b[i:]))
	}
	for f := 0; f < frames; f++ {
		l := sample(f * w * p.Channels)
		r := l
		if p.Channels == 2 {
			r = sample(f*w*p.Channels + w)
		}
		out[2*f], out[2*f+1] = l, r
	}
	return out
}
