//go:build linux

package alertsound

import "github.com/jfreymuth/pulse"

type backend interface {
	Play(phrase) error
	Close() error
}

type pulseClient interface {
	NewPlayback(pulse.Reader, ...pulse.PlaybackOption) (*pulse.PlaybackStream, error)
	Close()
}

var newPulseClient = func() (pulseClient, error) {
	return pulse.NewClient()
}

type linuxBackend struct {
	client pulseClient
}

func newBackend() (backend, error) {
	client, err := newPulseClient()
	if err != nil {
		return nil, err
	}
	return &linuxBackend{client: client}, nil
}

func (b *linuxBackend) Play(p phrase) error {
	idx := 0
	stream, err := b.client.NewPlayback(
		pulse.Float32Reader(func(out []float32) (int, error) {
			if idx >= len(p.Samples) {
				return 0, pulse.EndOfData
			}
			n := copy(out, p.Samples[idx:])
			idx += n
			if idx >= len(p.Samples) {
				return n, pulse.EndOfData
			}
			return n, nil
		}),
		pulse.PlaybackSampleRate(sampleRate),
		pulse.PlaybackMono,
		pulse.PlaybackLatency(0.05),
	)
	if err != nil {
		return err
	}
	defer stream.Close()

	stream.Start()
	stream.Drain()
	return stream.Error()
}

func (b *linuxBackend) Close() error {
	if b.client != nil {
		b.client.Close()
	}
	return nil
}
