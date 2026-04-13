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

func resolvePhraseForPlayIndex(playIndex int, home, minor phrase) (phrase, int) {
	blockIndex := (playIndex / 8) % 2
	position := (playIndex % 8) + 1
	if blockIndex == 0 {
		return home, position
	}
	return minor, position
}
