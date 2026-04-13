package alertsound

import (
	"encoding/binary"
	"testing"
)

func TestRenderWAV(t *testing.T) {
	samples := []float32{0, 0.5, -0.5, 0.25}
	wav := renderWAV(samples)

	if string(wav[0:4]) != "RIFF" {
		t.Fatalf("riff header = %q, want RIFF", string(wav[0:4]))
	}
	if string(wav[8:12]) != "WAVE" {
		t.Fatalf("wave header = %q, want WAVE", string(wav[8:12]))
	}
	if binary.LittleEndian.Uint16(wav[20:22]) != 1 {
		t.Fatalf("audio format = %d, want PCM", binary.LittleEndian.Uint16(wav[20:22]))
	}
	if binary.LittleEndian.Uint16(wav[22:24]) != 1 {
		t.Fatalf("channels = %d, want 1", binary.LittleEndian.Uint16(wav[22:24]))
	}
	if binary.LittleEndian.Uint32(wav[24:28]) != sampleRate {
		t.Fatalf("sample rate = %d, want %d", binary.LittleEndian.Uint32(wav[24:28]), sampleRate)
	}
	if binary.LittleEndian.Uint16(wav[34:36]) != 16 {
		t.Fatalf("bits per sample = %d, want 16", binary.LittleEndian.Uint16(wav[34:36]))
	}
	wantDataSize := uint32(len(samples) * 2)
	if binary.LittleEndian.Uint32(wav[40:44]) != wantDataSize {
		t.Fatalf("data size = %d, want %d", binary.LittleEndian.Uint32(wav[40:44]), wantDataSize)
	}
}
