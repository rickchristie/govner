# Cooper Proxy Alert Sound Plan

## Purpose

This document is the implementation spec for Cooper's proxy alert sound.

This document itself is the source of truth for implementation. The sound choice is no longer open-ended. The approved production sound is exactly the stateful `Barrel Roll Switchback` progression described in this plan.

This plan is based on the current codebase, not only on `cooper/REQUIREMENTS.md` and `cooper/README.md`.

## Non-Negotiable Outcome

When Cooper receives a new pending proxy approval request:

1. It must play one short alert phrase on the host.
2. That phrase must be the approved `Barrel Roll Switchback` phrase, not `Rickhouse Rise`, not the original one-shot `Barrel Roll`, and not a newly interpreted variant.
3. The sound must be stateful across the running `cooper up` session.
4. The first 8 audible plays must use the home chord phrase.
5. Audible plays 9-16 must use the related transition chord phrase.
6. Audible plays 17-24 must return to the home chord phrase.
7. The pattern must continue alternating every 8 audible plays for the rest of the session.
8. The sequence counter must advance only when a sound is actually played. If a request is suppressed by cooldown, the musical state must not advance.

Do not implement this as a single pre-rendered long track.

Do not implement this as random variation.

Do not implement this as "pick one of two sounds".

Do not change the frequencies, durations, envelopes, normalization, or sequence span unless the user explicitly asks for a new tuning pass.

## Integration Points

The Cooper integration points below are not optional. These are the tracked code locations the implementation must use:

1. `cooper/internal/tui/app.go:101-116`
   Root TUI receives `events.ACLRequestMsg` here.
2. `cooper/internal/tui/model.go:21-69`
   Root model owns global shell behavior and is the correct place for a global alert dependency.
3. `cooper/internal/tui/model.go:71-139`
   Root model setter pattern to follow.
4. `cooper/internal/tui/messages.go:30-40`
   Existing listener-command pattern to mirror for audio side effects.
5. `cooper/main.go:697-776`
   Real `cooper up` TUI composition happens here.
6. `cooper/main.go:1326-1358`
   `cooper tui-test` TUI composition happens here.

## Current Request Flow

The current manual approval flow is:

1. `cooper/internal/proxy/helper.go` runs inside the proxy container as Squid's external ACL helper.
2. `cooper/internal/proxy/acl.go` accepts the helper request and pushes an `ACLRequest` into the listener's request channel.
3. `cooper/internal/app/cooper.go` forwards those requests to the TUI-facing ACL channel.
4. `cooper/internal/tui/messages.go` wraps that channel receive as `events.ACLRequestMsg`.
5. `cooper/internal/tui/app.go` receives `events.ACLRequestMsg` at the root model and forwards it to the proxy monitor sub-model even when the Monitor tab is not active.

This means the sound must be triggered from the root TUI shell on `events.ACLRequestMsg`, not from the proxy layer and not from the `proxymon` sub-model.

## Product Behavior

After implementation:

1. Every non-whitelisted request that reaches manual approval triggers one alert attempt.
2. The alert plays regardless of the currently selected TUI tab.
3. Whitelisted requests do not play anything because they never become `ACLRequestMsg`.
4. Manual allow, manual deny, and timeout do not play separate sounds in v1.
5. If multiple requests arrive too quickly, cooldown may suppress extra sounds.
6. Suppressed sounds must not advance the 8-play musical sequence.
7. If host audio is unavailable, Cooper must continue running with a no-op player and a startup warning.
8. The sequence state resets when `cooper up` restarts.

## Approved Sound

The approved production sound is `Barrel Roll Switchback`.

### Behavioral Definition

1. One proxy request maps to one short phrase.
2. The phrase duration is about 0.40 seconds.
3. The phrase is a playful but controlled house-style arpeggio.
4. The phrase alternates between two related chord colors every 8 audible plays.
5. The change after the 8th audible play must feel like a smooth house transition, not a jarring modulation.

### Exact Sequence Rules

Use this exact state machine logic:

```go
playIndex := playCount
blockIndex := (playIndex / 8) % 2
positionInBlock := (playIndex % 8) + 1
```

Where:

1. `playCount` starts at `0` for a fresh `cooper up` session.
2. `blockIndex == 0` means the home chord phrase.
3. `blockIndex == 1` means the transition chord phrase.
4. `playCount` increments only after a sound is accepted for playback, not when a call is skipped due to cooldown.

This yields:

1. Audible plays `1-8`: home chord.
2. Audible plays `9-16`: transition chord.
3. Audible plays `17-24`: home chord.
4. Continue alternating forever.

### Exact Phrase Definitions

The home phrase must be generated exactly like this:

```go
buildBarrelRollPhrase(523.25, 659.25, 783.99, 1174.66, 261.63)
```

Interpretation:

1. `523.25` Hz = C5
2. `659.25` Hz = E5
3. `783.99` Hz = G5
4. `1174.66` Hz = D6
5. `261.63` Hz = C4 root bed

The transition phrase must be generated exactly like this:

```go
buildBarrelRollPhrase(440.00, 523.25, 659.25, 987.77, 220.00)
```

Interpretation:

1. `440.00` Hz = A4
2. `523.25` Hz = C5
3. `659.25` Hz = E5
4. `987.77` Hz = B5
5. `220.00` Hz = A3 root bed

This is the exact approved `Cmaj9/add9` to `Am9` relationship for production.

### Exact Phrase Synthesis

Use this exact phrase recipe:

```go
func buildBarrelRollPhrase(note1, note2, note3, note4, root float64) []float64 {
	buf := newBuffer(0.40)
	mixTone(buf, 0.00, 0.10, note1, 0.22, toneShape{Attack: 0.003, Release: 0.040, DecayRate: 5.6, Harmonic2: 0.10})
	mixTone(buf, 0.07, 0.10, note2, 0.22, toneShape{Attack: 0.003, Release: 0.040, DecayRate: 5.8, Harmonic2: 0.10})
	mixTone(buf, 0.14, 0.11, note3, 0.22, toneShape{Attack: 0.003, Release: 0.045, DecayRate: 6.0, Harmonic2: 0.12})
	mixTone(buf, 0.23, 0.12, note4, 0.15, toneShape{Attack: 0.003, Release: 0.040, DecayRate: 6.4, Harmonic2: 0.14, Harmonic3: 0.05})
	mixTone(buf, 0.00, 0.26, root, 0.06, toneShape{Attack: 0.004, Release: 0.060, DecayRate: 3.4, Harmonic2: 0.04})
	return buf
}
```

Do not reinterpret this phrase musically.

Do not shorten it.

Do not stretch it.

Do not swap note order.

Do not move the root bed later in time.

### Exact Shared Synthesis Rules

Use the following helper behavior unchanged.

Recommended implementation:

```go
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
```

Exact invariants:

1. Sample rate must be `48000`.
2. Audio must be mono.
3. Internal generated samples may be `[]float32` or `[]float64` before normalization.
4. Normalization must target `0.82` peak, exactly as in `normalizeSamples`.
5. Envelope attack and release behavior must match the sound test helper exactly.

## Playback Backend

Use the following platform approach.

### Linux

Use `github.com/jfreymuth/pulse` to play through PulseAudio or PipeWire's PulseAudio compatibility layer.

Recommended Linux backend shape:

```go
func newBackend() (backend, error) {
	client, err := pulse.NewClient()
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
```

### macOS

Use the built-in `afplay` command and play a temporary WAV file.

Recommended macOS backend shape:

```go
var execCommand = exec.Command

func (b *darwinBackend) Play(p phrase) error {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("cooper-alert-%d.wav", time.Now().UnixNano()))
	if err := os.WriteFile(path, p.WAV, 0644); err != nil {
		return err
	}
	defer os.Remove(path)

	cmd := execCommand("afplay", path)
	return cmd.Run()
}
```

### Why This Backend Choice

1. It matches the exact sound behavior approved by the user.
2. Linux playback is pure-Go.
3. `afplay` is preinstalled on macOS and avoids adding a second audio dependency.
4. This path is less likely to drift than reinterpreting the sound through a different playback stack.

### Failure Policy

1. If backend creation fails, Cooper must continue with a no-op player.
2. The failure must be surfaced as a startup warning.
3. If playback later fails, the `alertsound` package itself must log the failure once and self-disable the player for the rest of the session.
4. Approval flow must not depend on sound success.

### Runtime Observability Decision

Runtime playback observability lives inside `cooper/internal/alertsound`, not in the TUI layer.

Required behavior:

1. Constructor failures are surfaced in `main.go` as startup warnings.
2. Runtime playback failures are logged inside `alertsound` with a clear one-line message.
3. After the first runtime playback failure, the player disables itself and future play calls become no-ops.
4. The TUI helper is intentionally fire-and-forget and must not create a modal, toast, or typed UI error message for playback failures.

Recommended runtime log line:

```go
log.Printf("cooper: proxy alert sound disabled after playback failure: %v", err)
```

## Cooldown Behavior

Use a replay cooldown so bursts do not stack into noise.

This is not a tuning suggestion anymore. It is a fixed v1 behavior constant.

Required v1 constant:

```go
const proxyAlertCooldown = 750 * time.Millisecond
```

Critical rule:

1. If a playback request arrives inside cooldown, return without playing.
2. Do not advance the musical sequence counter on suppressed requests.

This rule matters because the approved sound is a progression. The user must hear the progression evolve based on actual audible plays, not hidden skipped calls.

## Architecture

### Root Trigger Location

Trigger playback from `cooper/internal/tui/app.go:101-116`, in the `events.ACLRequestMsg` case.

That branch already does the correct routing. It is the right shell-layer place to schedule a sound side effect because:

1. Every pending approval reaches this branch.
2. Whitelisted requests do not.
3. It is global across all tabs.
4. The root model is the TUI shell.

### Dependency Boundary

Add a tiny alert interface to the root TUI model.

Recommended addition to `cooper/internal/tui/model.go`:

```go
type AlertPlayer interface {
	PlayProxyApprovalNeeded() error
}
```

Add this field to `Model`:

```go
alertPlayer AlertPlayer
```

Add a setter:

```go
func (m *Model) SetAlertPlayer(p AlertPlayer) {
	m.alertPlayer = p
}
```

Do not push this into `app.App` in v1.

The sound is a presentation concern, not a business action.

## Recommended Package Layout

Create a new package:

- `cooper/internal/alertsound`

Recommended files:

1. `player.go`
2. `sequence.go`
3. `synth.go`
4. `wav.go`
5. `backend_linux.go`
6. `backend_darwin.go`
7. `noop.go`
8. `player_test.go`
9. `sequence_test.go`
10. `synth_test.go`
11. `wav_test.go`
12. `app_alert_test.go` in `cooper/internal/tui`

### Recommended Types

```go
type Player interface {
	PlayProxyApprovalNeeded() error
	Close() error
}

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

type backend interface {
	Play(phrase phrase) error
	Close() error
}

type Clock interface {
	Now() time.Time
}

type player struct {
	mu          sync.Mutex
	backend     backend
	clock       Clock
	logf        func(string, ...any)
	lastPlayAt  time.Time
	minInterval time.Duration
	playCount   int
	home        phrase
	minor       phrase
	closed      bool
	disabled    bool
}
```

### Why These Types

1. `phraseID` makes sequence tests unambiguous.
2. `Clock` makes cooldown deterministic in tests.
3. `playCount` in the player keeps sequence state tied to actual audible playback.
4. Precomputing `WAV` helps Darwin backend avoid rebuilding bytes every time.

## Exact Sequence Resolver

Implement the sequence resolver exactly as specified in this plan.

Recommended internal helper:

```go
func resolvePhraseForPlayIndex(playIndex int, home, minor phrase) (phrase, int) {
	blockIndex := (playIndex / 8) % 2
	position := (playIndex % 8) + 1
	if blockIndex == 0 {
		return home, position
	}
	return minor, position
}
```

Recommended `PlayProxyApprovalNeeded` skeleton:

```go
func (p *player) PlayProxyApprovalNeeded() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed || p.disabled || p.backend == nil {
		return nil
	}

	now := p.clock.Now()
	if !p.lastPlayAt.IsZero() && now.Sub(p.lastPlayAt) < p.minInterval {
		return nil
	}

	selected, _ := resolvePhraseForPlayIndex(p.playCount, p.home, p.minor)
	if err := p.backend.Play(selected); err != nil {
		if p.logf != nil {
			p.logf("cooper: proxy alert sound disabled after playback failure: %v", err)
		}
		p.disabled = true
		return err
	}

	p.lastPlayAt = now
	p.playCount++
	return nil
}
```

Important:

1. Select the phrase before incrementing.
2. Increment only after backend play succeeds.
3. Skip increment when cooldown suppresses playback.
4. Skip increment when backend play fails.

## Exact Synth Implementation Guidance

The next agent should not creatively rewrite the synth. It should copy the helper behavior defined in this plan with minimal package adaptation.

At minimum, production code should preserve these exact functions and values:

```go
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
```

Copy the `mixTone`, `envelope`, `normalizeSamples`, `encodePCM16`, and `renderWAV` behavior from the code blocks in this plan exactly.

## Exact Cooper Integration Steps

### 1. Add `alertsound` package

Implement the new package first and get its unit tests passing before touching the TUI.

### 2. Update the root TUI model

Modify `cooper/internal/tui/model.go` to add the `AlertPlayer` interface, `alertPlayer` field, and `SetAlertPlayer` setter.

### 3. Add a command helper

Add this helper to `cooper/internal/tui/messages.go` or a nearby helper file:

```go
func playProxyAlertCmd(p AlertPlayer) tea.Cmd {
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		// Playback errors are handled and logged inside alertsound.
		_ = p.PlayProxyApprovalNeeded()
		return nil
	}
}
```

### 4. Trigger playback in the root `ACLRequestMsg` branch

Update `cooper/internal/tui/app.go:101-116` like this:

```go
case events.ACLRequestMsg:
	var cmd tea.Cmd
	if m.proxyMonModel != nil {
		var sm SubModel
		sm, cmd = m.proxyMonModel.Update(msg)
		m.proxyMonModel = sm
	}

	var listenCmd tea.Cmd
	if m.app != nil {
		if ch := m.app.ACLRequests(); ch != nil {
			listenCmd = listenACL(ch)
		}
	}

	return m, tea.Batch(cmd, listenCmd, playProxyAlertCmd(m.alertPlayer))
```

### 5. Wire the real player in `runUp`

In `cooper/main.go:697-776`, do not create the alert player after `cooperApp.Adopt(...)` if you intend to surface constructor failure through startup warnings. The real code currently passes `startupWarnings` into `cooperApp.Adopt(...)` first, and `cooperApp.StartupWarnings()` is later used to populate the About tab.

Required wiring order:

1. Create `alertPlayer` after startup succeeds but before `cooperApp.Adopt(...)`.
2. If constructor fails, append the warning to the local `startupWarnings` slice before calling `Adopt(...)`.
3. Then call `cooperApp.Adopt(..., startupWarnings)` so the app receives the audio warning too.
4. After that, create `mainModel` and inject the already-created `alertPlayer`.

Do not rely on appending only to the local `startupWarnings` slice after `Adopt(...)` has already copied warnings into the app. That ordering can cause the About tab to miss the audio warning.

Recommended shape:

```go
alertPlayer, err := alertsound.New()
if err != nil {
	startupWarnings = append(startupWarnings, fmt.Sprintf("Proxy alert sound disabled: %v", err))
	alertPlayer = alertsound.NewNoop()
}
defer alertPlayer.Close()

cooperApp := app.NewCooperApp(cfg, cooperDir)
cooperApp.AdoptClipboard(clipMgr, clipReader)
cooperApp.Adopt(aclListener, bridgeServer, hostRelay, startupWarnings)

mainModel := tui.NewModel(cooperApp)
mainModel.SetAlertPlayer(alertPlayer)
```

An alternative app-level append-warning API would also work, but the recommended v1 implementation is to initialize the alert player before `Adopt(...)` and keep the current warning flow intact.

### 6. Wire a no-op player in `runTUITest`

In `cooper/main.go:1326-1358`, after `mainModel := tui.NewModel(testApp)`, add:

```go
mainModel.SetAlertPlayer(alertsound.NewNoop())
```

This keeps `tui-test` quiet and deterministic.

## Detailed File Plan

### New files

1. `cooper/internal/alertsound/player.go`
2. `cooper/internal/alertsound/sequence.go`
3. `cooper/internal/alertsound/synth.go`
4. `cooper/internal/alertsound/wav.go`
5. `cooper/internal/alertsound/backend_linux.go`
6. `cooper/internal/alertsound/backend_darwin.go`
7. `cooper/internal/alertsound/noop.go`
8. `cooper/internal/alertsound/player_test.go`
9. `cooper/internal/alertsound/sequence_test.go`
10. `cooper/internal/alertsound/synth_test.go`
11. `cooper/internal/alertsound/wav_test.go`
12. `cooper/internal/tui/app_alert_test.go`

### Modified files

1. `cooper/go.mod`
2. `cooper/go.sum`
3. `cooper/main.go`
4. `cooper/internal/tui/model.go`
5. `cooper/internal/tui/app.go`
6. `cooper/internal/tui/messages.go`
7. `cooper/README.md`
8. `cooper/REQUIREMENTS.md`

### Dependency changes

Add:

```go
require github.com/jfreymuth/pulse v0.1.1
```

No other new dependency is required for the approved implementation.

## Toolchain Prerequisite

The main Cooper module currently requires Go `1.25.0`.

Reference:

```go
module github.com/rickchristie/govner/cooper

go 1.25.0
```

Practical consequence for the implementation agent:

1. Running `go test ./...` from `cooper/` requires a Go 1.25 toolchain or a non-local auto-downloading toolchain setup.
2. In an environment pinned to Go 1.24 with `GOTOOLCHAIN=local`, root-module tests fail before code is even compiled.
3. That failure is an environment prerequisite issue, not evidence of a regression in the proxy alert implementation.

Do not misdiagnose a Go 1.24 toolchain error as a feature failure.

## Golden Sound Lock Tests

Because the user explicitly approved this exact sound, the test suite should include golden-byte checks to prevent drift.

Use the hashes generated from the approved sound test renders.

Expected SHA256 for rendered home WAV bytes:

```text
6caccbcc712a883e506e231f11de68c1bf9201b03f584d8bf223b455b9c48fc6
```

Expected SHA256 for rendered transition WAV bytes:

```text
34c647132eae24963049341768fc2deb4e5d73ab182a0b8bd3aa73e0d6eaa868
```

These hashes are part of the approved production spec and should be treated as authoritative golden values.

## Automated Test Plan

### 1. `sequence_test.go`

Purpose:

Validate exact 8-play block switching semantics.

Fixture:

```go
home := phrase{ID: phraseHome}
minor := phrase{ID: phraseMinor}
```

Scenario A: first 24 plays

1. Call `resolvePhraseForPlayIndex(i, home, minor)` for `i := 0; i < 24; i++`.
2. Collect `phrase.ID` and `position`.

Assertions:

1. Indices `0-7` return `phraseHome` and positions `1-8`.
2. Indices `8-15` return `phraseMinor` and positions `1-8`.
3. Indices `16-23` return `phraseHome` and positions `1-8`.

Scenario B: looping continuity

1. Test indices `24-31`.

Assertions:

1. Indices `24-31` return `phraseMinor` and positions `1-8`.

### 2. `synth_test.go`

Purpose:

Lock the approved sound recipe.

Fixture:

1. Call `buildSwitchbackPhrases()`.
2. Receive `home` and `minor` phrases.

Scenario A: sample length

Assertions:

1. `len(home.Samples) == 19200`
2. `len(minor.Samples) == 19200`

Reason:

1. 0.40 seconds at 48000 Hz equals 19200 mono samples.

Scenario B: normalized peak

Setup:

1. Iterate through every sample in each phrase.
2. Track absolute peak.

Assertions:

1. Peak is `<= 0.82 + epsilon`.
2. Peak is close to `0.82`, not dramatically lower, so accidental extra attenuation is caught.

Scenario C: golden WAV hashes

Setup:

1. Render `home.WAV` and `minor.WAV`.
2. Compute SHA256.

Assertions:

1. Home hash equals `6caccbcc712a883e506e231f11de68c1bf9201b03f584d8bf223b455b9c48fc6`.
2. Minor hash equals `34c647132eae24963049341768fc2deb4e5d73ab182a0b8bd3aa73e0d6eaa868`.

This is the strongest anti-drift test in the plan. Keep it.

Scenario D: start/end cleanliness

Assertions:

1. First few samples are near zero.
2. Last few samples are near zero.

This guards against clicks from envelope regressions.

### 3. `wav_test.go`

Purpose:

Verify Darwin playback assets are rendered consistently.

Fixture:

1. Create a small sample buffer with known length.
2. Call `renderWAV(samples)`.

Assertions:

1. RIFF header is present.
2. Format is mono PCM16.
3. Sample rate is `48000`.
4. Data size equals `len(samples) * 2`.

### 4. `player_test.go`

Purpose:

Verify cooldown, sequence advancement, and failure behavior.

Fixture types:

```go
type fakeClock struct { now time.Time }
func (c *fakeClock) Now() time.Time { return c.now }

type recordingBackend struct {
	plays []phraseID
	err   error
}

func (b *recordingBackend) Play(p phrase) error {
	if b.err != nil {
		return b.err
	}
	b.plays = append(b.plays, p.ID)
	return nil
}

func (b *recordingBackend) Close() error { return nil }

type fakeLogger struct{ lines []string }

func (l *fakeLogger) Printf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
```

Scenario A: exact switch after 8 audible plays

Setup:

1. Create player with `minInterval = proxyAlertCooldown`.
2. Use fake clock.
3. Advance clock by `800ms` before every call so cooldown never suppresses.
4. Call `PlayProxyApprovalNeeded()` 17 times.

Assertions:

1. Backend plays 17 times.
2. Plays `1-8` are `phraseHome`.
3. Plays `9-16` are `phraseMinor`.
4. Play `17` is `phraseHome`.

Scenario B: cooldown suppression does not advance sequence

Setup:

1. First play at `t0`.
2. Second call at `t0 + 100ms`.
3. Third call at `t0 + proxyAlertCooldown + 50ms`.

Assertions:

1. Backend recorded only 2 plays.
2. Both audible plays are still within the home block positions `1` and `2`.
3. The suppressed second call did not force the third call into position `3`.

Scenario C: backend failure logs once and self-disables

Setup:

1. Create player with backend returning an error.
2. Inject a fake logger through `player.logf`.
3. Call `PlayProxyApprovalNeeded()`.
4. Advance time beyond cooldown.
5. Call it again.

Assertions:

1. Player does not panic.
2. `playCount` does not advance on failed play.
3. The backend is attempted exactly once.
4. The second call does not attempt playback again because the player is self-disabled.
5. Exactly one log line is recorded.
6. The log line contains `proxy alert sound disabled after playback failure`.

Scenario D: `Close()` behavior

Setup:

1. Create player.
2. Call `Close()`.
3. Call `PlayProxyApprovalNeeded()`.

Assertions:

1. No backend play occurs.
2. No panic occurs.

### 5. Darwin backend tests

Purpose:

Verify `afplay` integration without requiring real speaker output.

Implementation guidance:

1. Use package-level `execCommand = exec.Command` so tests can stub it.
2. Use a package-level temp-file creation helper if needed for determinism.

Fixture:

1. Stub `execCommand` to record args and return a harmless process such as `exec.Command("true")`.
2. Create a phrase with known `WAV` bytes.

Assertions:

1. Command name is `afplay`.
2. Exactly one path arg is passed.
3. The temp file exists during command execution.
4. Temp file is removed afterward.

### 6. Linux backend tests

Purpose:

Keep most Linux backend testing at constructor and adapter level.

Implementation guidance:

1. Put the Pulse client behind a tiny unexported adapter interface if needed.
2. Test the player behavior against a fake backend, not against a real Pulse server.

Assertions:

1. Constructor returns a clear error when Pulse client creation fails.
2. Playback adapter forwards the selected phrase once.

### 7. Root TUI alert tests in `cooper/internal/tui/app_alert_test.go`

Purpose:

Verify the shell-level trigger point.

Important Bubble Tea detail:

1. `tea.Batch(...)` returns a command whose message is a `tea.BatchMsg` containing sub-commands.
2. For `ACLRequestMsg`, the root model currently returns `tea.Batch(cmd, listenCmd, playProxyAlertCmd(...))`.
3. In tests, calling the outer returned command once is not enough to guarantee the alert sub-command ran. You must either unwrap and execute the sub-commands in the returned `tea.BatchMsg`, or test `playProxyAlertCmd` separately in a narrower unit test.

Recommended testing split:

1. Add a narrow unit test for `playProxyAlertCmd` that directly calls the command and asserts the fake alert player count increments.
2. Keep the root-model tests focused on verifying that `ACLRequestMsg` routes correctly and that the returned command, when unwrapped as `tea.BatchMsg`, contains an executable alert sub-command.

Fixture types:

```go
type fakeAlertPlayer struct{ count int }
func (f *fakeAlertPlayer) PlayProxyApprovalNeeded() error { f.count++; return nil }

type fakeSubModel struct{ msgs []tea.Msg }

func runCmdAndBatchSubcommands(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		msgs := make([]tea.Msg, 0, len(batch))
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			msgs = append(msgs, sub())
		}
		return msgs
	}
	return []tea.Msg{msg}
}
```

The helper above is safe only in the `m := tui.NewModel(nil)` setup described below, because that avoids the blocking `listenACL(ch)` path.

### 7A. Narrow command test for `playProxyAlertCmd`

Setup:

1. Create `fakeAlertPlayer`.
2. Build `cmd := playProxyAlertCmd(fakeAlertPlayer)`.
3. Execute `cmd()` directly.

Assertions:

1. Fake alert count increments exactly once.
2. Returned message is `nil`.

### 7B. Root-model shell tests in `app_alert_test.go`

Scenario A: `ACLRequestMsg` triggers sound

Setup:

Use `m := tui.NewModel(nil)` for the alert-trigger tests in this section unless you are specifically testing the channel listener wiring. Do not use `app.NewMockApp(...)` and then blindly execute the returned `tea.Batch` command, because the real `ACLRequestMsg` branch also includes `listenACL(ch)`, and `listenACL` blocks waiting on the app channel.

Safe setup:

1. Create `m := tui.NewModel(nil)`.
2. Inject `fakeAlertPlayer` with `SetAlertPlayer`.
3. Optionally inject a fake proxy monitor sub-model.
4. Call `updated, cmd := m.Update(events.ACLRequestMsg{Request: req})`.
5. Assert routing side effects on `updated` immediately.
6. Call `msgs := runCmdAndBatchSubcommands(t, cmd)`.

Assertions:

1. Fake alert count increments exactly once.
2. If a fake proxy monitor sub-model is installed, it also receives the message.
3. `msgs` contains at least one entry, and one of the executed sub-commands may return `nil`; that is acceptable.

Why this setup is required:

1. With `m.app == nil`, the `ACLRequestMsg` branch does not add the blocking `listenACL(ch)` sub-command.
2. The returned command can still be a `tea.Batch`, so the test must unwrap `tea.BatchMsg` and execute its sub-commands.

Scenario B: active tab does not matter

Setup:

1. Create `m := tui.NewModel(nil)` and inject `fakeAlertPlayer`.
2. Set active tab to something other than Monitor, such as `theme.TabAbout`.
3. Call `_, cmd := m.Update(events.ACLRequestMsg{Request: req})`.
4. Execute `runCmdAndBatchSubcommands(t, cmd)`.

Assertions:

1. Alert still fires once.

Scenario C: decisions do not trigger sound

Setup:

1. Create `m := tui.NewModel(nil)` and inject `fakeAlertPlayer`.
2. Send `events.ACLDecisionMsg`.

Assertions:

1. Alert count remains unchanged.

Scenario D: nil alert player is safe

Setup:

1. Create `m := tui.NewModel(nil)`.
2. Do not set an alert player.
3. Call `_, cmd := m.Update(events.ACLRequestMsg{Request: req})`.
4. Execute `runCmdAndBatchSubcommands(t, cmd)`.

Assertions:

1. No panic occurs.

### 8. `main.go` composition tests

If practical, add a lightweight test around TUI composition helpers. If not practical, keep this as manual verification.

Required behavior:

1. `runUp` wires a real alert player or a no-op fallback.
2. `runTUITest` wires `alertsound.NewNoop()`.

## Required Documentation Updates

The implementation agent must update the tracked product documentation, not only the code.

### `cooper/README.md`

Update the user-facing description of the proxy approval flow to mention:

1. Cooper plays a host-side alert sound when a request enters manual approval.
2. The alert is one short phrase per pending request, not a looping alarm.
3. Linux playback uses PulseAudio or PipeWire-Pulse compatibility.
4. macOS playback uses the system `afplay` path.
5. If host audio is unavailable, Cooper still runs and simply disables audio alerts.

### `cooper/REQUIREMENTS.md`

Update the product/behavior requirements to mention:

1. The trigger point is new pending approval, not allow/deny outcome.
2. The sound progression is stateful across a running session.
3. The production progression alternates every 8 audible plays.
4. Cooldown is fixed at `750ms` in v1.
5. Suppressed alerts do not advance the progression state.
6. Audio failure is fail-soft, not startup-fatal.

## Manual Verification

### Linux with working PulseAudio or PipeWire-Pulse

Setup:

1. Start the regular desktop audio session.
2. Start `cooper up`.

Scenario A: single blocked request

1. From a barrel, issue one request that becomes pending approval.

Assertions:

1. One short `Barrel Roll Switchback` phrase plays.
2. The request appears in the Monitor tab.

Scenario B: 9 spaced blocked requests

Setup:

1. Trigger one blocked request.
2. Wait longer than cooldown.
3. Repeat until 9 total audible plays occur.

Assertions:

1. Plays `1-8` use the home phrase.
2. Play `9` audibly switches into the `Am9`-style transition phrase.

Scenario C: 17 spaced blocked requests

Assertions:

1. Plays `9-16` use the transition phrase.
2. Play `17` returns to the home phrase.

Scenario D: cooldown burst

Setup:

1. Trigger multiple blocked requests in a rapid burst under `750ms` spacing.

Assertions:

1. Not every request makes a sound.
2. The sequence does not audibly jump ahead as if suppressed requests had been counted.

Scenario E: whitelisted traffic

Setup:

1. Make requests only to already whitelisted domains.

Assertions:

1. No sound plays.

### macOS

Repeat the same scenarios.

Additional assertion:

1. `afplay` path works without visible user disruption.

### Failure path

Linux setup:

1. Stop or hide the PulseAudio socket from the shell environment.

macOS setup:

1. Simulate missing `afplay` by stubbing or adjusting PATH in a test harness if doing this manually is practical.

Assertions:

1. `cooper up` still starts.
2. No crash occurs on pending requests.
3. About tab shows the startup warning.

## Acceptance Criteria

The implementation is complete only when all of these are true:

1. `events.ACLRequestMsg` at the root TUI shell triggers the sound command.
2. The sound is exactly the approved `Barrel Roll Switchback` progression.
3. One request equals one short phrase.
4. Phrase selection switches every 8 audible plays.
5. Cooldown-suppressed calls do not advance the progression.
6. Linux playback uses Pulse via `github.com/jfreymuth/pulse`.
7. macOS playback uses `afplay` with a temporary WAV.
8. Startup failure degrades to no-op plus warning.
9. Golden sound hash tests pass.
10. Root TUI trigger tests pass.
11. `cooper/README.md` and `cooper/REQUIREMENTS.md` are updated.
12. Manual Linux and macOS checks confirm the progression is audibly the same as the approved spec in this plan.

## Final Recommendation

This plan is authoritative and complete. The implementation should copy the approved Switchback behavior into `cooper/internal/alertsound` with the smallest possible translation.

The correct implementation is not "a pleasant proxy sound" in general.

The correct implementation is the exact stateful `Barrel Roll Switchback` phrase sequence the user approved.
