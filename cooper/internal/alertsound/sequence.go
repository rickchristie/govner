package alertsound

type phraseID string

const (
	phraseHome  phraseID = "home-cmaj9"
	phraseMinor phraseID = "minor-am9"
)

type phrase struct {
	ID      phraseID
	Samples []float32
	WAV     []byte
}

// resolvePhraseForPlayIndex implements the approved per-session "Barrel Roll
// Switchback" progression: 8 audible plays on the home phrase, then 8 on the
// related minor phrase, alternating forever. Callers must pass a play index
// that advances only after real playback so cooldown-suppressed requests do
// not jump the musical state forward.
func resolvePhraseForPlayIndex(playIndex int, home, minor phrase) (phrase, int) {
	blockIndex := (playIndex / 8) % 2
	position := (playIndex % 8) + 1
	if blockIndex == 0 {
		return home, position
	}
	return minor, position
}
