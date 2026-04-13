package alertsound

import "testing"

func TestResolvePhraseForPlayIndex(t *testing.T) {
	home := phrase{ID: phraseHome}
	minor := phrase{ID: phraseMinor}

	for i := 0; i < 24; i++ {
		got, position := resolvePhraseForPlayIndex(i, home, minor)
		switch {
		case i < 8:
			if got.ID != phraseHome || position != i+1 {
				t.Fatalf("index %d = (%s, %d), want (%s, %d)", i, got.ID, position, phraseHome, i+1)
			}
		case i < 16:
			wantPos := (i % 8) + 1
			if got.ID != phraseMinor || position != wantPos {
				t.Fatalf("index %d = (%s, %d), want (%s, %d)", i, got.ID, position, phraseMinor, wantPos)
			}
		default:
			wantPos := (i % 8) + 1
			if got.ID != phraseHome || position != wantPos {
				t.Fatalf("index %d = (%s, %d), want (%s, %d)", i, got.ID, position, phraseHome, wantPos)
			}
		}
	}

	for i := 24; i < 32; i++ {
		got, position := resolvePhraseForPlayIndex(i, home, minor)
		wantPos := (i % 8) + 1
		if got.ID != phraseMinor || position != wantPos {
			t.Fatalf("index %d = (%s, %d), want (%s, %d)", i, got.ID, position, phraseMinor, wantPos)
		}
	}
}
