package alertsound

import (
	"encoding/binary"
	"math"
)

func clamp[T ~float32 | ~float64](v, lo, hi T) T {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func encodePCM16(samples []float32) []byte {
	pcm := make([]byte, len(samples)*2)
	for i, sample := range samples {
		s16 := int16(math.Round(float64(clamp(float64(sample), -1, 1)) * 32767))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(s16))
	}
	return pcm
}

func renderWAV(samples []float32) []byte {
	pcm := encodePCM16(samples)
	dataLen := uint32(len(pcm))
	riffLen := 36 + dataLen
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(wav[4:8], riffLen)
	copy(wav[8:12], []byte("WAVE"))
	copy(wav[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], sampleRate)
	binary.LittleEndian.PutUint32(wav[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(wav[40:44], dataLen)
	copy(wav[44:], pcm)
	return wav
}
