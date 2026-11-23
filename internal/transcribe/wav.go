package transcribe

import (
	"bytes"
	"encoding/binary"
	"math"
)

// EncodeWAV16 is mono float samples (-1..1) as a canonical 16-bit PCM WAV: a
// 44-byte header and the samples, which every speech service reads.
func EncodeWAV16(samples []float32, rate int) []byte {
	var b bytes.Buffer
	n := len(samples) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+n))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(n))
	buf := make([]byte, 2)
	for _, s := range samples {
		v := math.Max(-1, math.Min(1, float64(s)))
		binary.LittleEndian.PutUint16(buf, uint16(int16(math.Round(v*32767))))
		b.Write(buf)
	}
	return b.Bytes()
}
