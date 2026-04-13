package alertsound

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
)

func TestBuildSwitchbackPhrases(t *testing.T) {
	home, minor := buildSwitchbackPhrases()

	if len(home.Samples) != 19200 {
		t.Fatalf("home sample length = %d, want 19200", len(home.Samples))
	}
	if len(minor.Samples) != 19200 {
		t.Fatalf("minor sample length = %d, want 19200", len(minor.Samples))
	}

	assertPeak(t, "home", home.Samples)
	assertPeak(t, "minor", minor.Samples)
	assertWAVHash(t, "home", home.WAV, "6caccbcc712a883e506e231f11de68c1bf9201b03f584d8bf223b455b9c48fc6")
	assertWAVHash(t, "minor", minor.WAV, "34c647132eae24963049341768fc2deb4e5d73ab182a0b8bd3aa73e0d6eaa868")
	assertNearSilence(t, "home-start", home.Samples[:8])
	assertNearSilence(t, "home-end", home.Samples[len(home.Samples)-8:])
	assertNearSilence(t, "minor-start", minor.Samples[:8])
	assertNearSilence(t, "minor-end", minor.Samples[len(minor.Samples)-8:])
}

func assertPeak(t *testing.T, name string, samples []float32) {
	t.Helper()

	peak := 0.0
	for _, sample := range samples {
		if a := math.Abs(float64(sample)); a > peak {
			peak = a
		}
	}
	if peak > 0.820001 {
		t.Fatalf("%s peak = %.6f, want <= 0.82", name, peak)
	}
	if peak < 0.819 {
		t.Fatalf("%s peak = %.6f, want close to 0.82", name, peak)
	}
}

func assertWAVHash(t *testing.T, name string, wav []byte, want string) {
	t.Helper()
	sum := sha256.Sum256(wav)
	got := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("%s wav hash = %s, want %s", name, got, want)
	}
}

func assertNearSilence(t *testing.T, name string, samples []float32) {
	t.Helper()
	for i, sample := range samples {
		if math.Abs(float64(sample)) > 0.02 {
			t.Fatalf("%s sample %d = %.6f, want near zero", name, i, sample)
		}
	}
}
