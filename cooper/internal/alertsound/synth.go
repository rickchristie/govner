package alertsound

import "math"

const sampleRate = 48000

type toneShape struct {
	Attack       float64
	Release      float64
	DecayRate    float64
	Harmonic2    float64
	Harmonic3    float64
	Harmonic5    float64
	VibratoHz    float64
	VibratoDepth float64
}

func buildSwitchbackPhrases() (phrase, phrase) {
	homeSamples := normalizeSamples(buildBarrelRollPhrase(523.25, 659.25, 783.99, 1174.66, 261.63))
	minorSamples := normalizeSamples(buildBarrelRollPhrase(440.00, 523.25, 659.25, 987.77, 220.00))
	return phrase{ID: phraseHome, Samples: homeSamples, WAV: renderWAV(homeSamples)}, phrase{ID: phraseMinor, Samples: minorSamples, WAV: renderWAV(minorSamples)}
}

func buildBarrelRollPhrase(note1, note2, note3, note4, root float64) []float64 {
	buf := newBuffer(0.40)
	mixTone(buf, 0.00, 0.10, note1, 0.22, toneShape{Attack: 0.003, Release: 0.040, DecayRate: 5.6, Harmonic2: 0.10})
	mixTone(buf, 0.07, 0.10, note2, 0.22, toneShape{Attack: 0.003, Release: 0.040, DecayRate: 5.8, Harmonic2: 0.10})
	mixTone(buf, 0.14, 0.11, note3, 0.22, toneShape{Attack: 0.003, Release: 0.045, DecayRate: 6.0, Harmonic2: 0.12})
	mixTone(buf, 0.23, 0.12, note4, 0.15, toneShape{Attack: 0.003, Release: 0.040, DecayRate: 6.4, Harmonic2: 0.14, Harmonic3: 0.05})
	mixTone(buf, 0.00, 0.26, root, 0.06, toneShape{Attack: 0.004, Release: 0.060, DecayRate: 3.4, Harmonic2: 0.04})
	return buf
}

func newBuffer(seconds float64) []float64 {
	return make([]float64, int(seconds*sampleRate))
}

func mixTone(buf []float64, start, dur, freq, amp float64, shape toneShape) {
	startIdx := int(start * sampleRate)
	count := int(dur * sampleRate)
	if count <= 0 || startIdx >= len(buf) {
		return
	}
	if startIdx+count > len(buf) {
		count = len(buf) - startIdx
	}

	for i := 0; i < count; i++ {
		t := float64(i) / sampleRate
		phase := 2*math.Pi*freq*t + shape.VibratoDepth*math.Sin(2*math.Pi*shape.VibratoHz*t)
		env := amp * envelope(t, dur, shape.Attack, shape.Release) * math.Exp(-t*shape.DecayRate)
		if env == 0 {
			continue
		}

		sample := math.Sin(phase)
		sample += shape.Harmonic2 * math.Sin(2*phase)
		sample += shape.Harmonic3 * math.Sin(3*phase)
		sample += shape.Harmonic5 * math.Sin(5*phase)
		buf[startIdx+i] += env * sample
	}
}

func envelope(t, dur, attack, release float64) float64 {
	if t < 0 || t > dur {
		return 0
	}
	if attack <= 0 {
		attack = 0.001
	}
	if release <= 0 {
		release = 0.001
	}

	a := 1.0
	if t < attack {
		a = t / attack
	}

	r := 1.0
	if t > dur-release {
		r = (dur - t) / release
	}

	if a < 0 {
		a = 0
	}
	if r < 0 {
		r = 0
	}
	return a * r
}

func normalizeSamples(samples []float64) []float32 {
	peak := 0.0
	for _, sample := range samples {
		if a := math.Abs(sample); a > peak {
			peak = a
		}
	}

	scale := 0.82
	if peak > 0 {
		scale /= peak
	}

	out := make([]float32, len(samples))
	for i, sample := range samples {
		out[i] = float32(clamp(sample*scale, -1, 1))
	}
	return out
}
