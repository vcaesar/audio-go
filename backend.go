package audiogo

import (
	"fmt"
	"sync"
)

// backend is one native audio API (CoreAudio, ALSA, WASAPI, null).
type backend interface {
	devices(kind DeviceType) ([]DeviceInfo, error)
	deviceInfo(kind DeviceType, id DeviceID) (DeviceInfo, error)
	openDevice(p *deviceParams, s *sink) (stream, error)
	uninit() error
}

// stream is an initialized device of a backend. Data callbacks are only made
// between start and the return of stop. A stream that stops on its own error
// calls sink.fail(run) after its worker has exited; stop must still be called.
type stream interface {
	start(run uint64) error
	// stop is synchronous and idempotent: no data callback runs after it returns.
	stop() error
	close()
}

// openers holds the backends compiled for this platform.
var openers = map[Backend]func(log func(string)) (backend, error){
	BackendNull: func(func(string)) (backend, error) { return nullBackend{}, nil },
}

// defaultBackends is the priority order used when InitContext gets no backends.
var defaultBackends = []Backend{BackendWasapi, BackendCoreaudio, BackendAlsa, BackendNull}

var backendNames = map[Backend]string{
	BackendWasapi: "WASAPI", BackendDsound: "DirectSound", BackendWinmm: "WinMM",
	BackendCoreaudio: "Core Audio", BackendSndio: "sndio", BackendAudio4: "audio(4)",
	BackendOss: "OSS", BackendPulseaudio: "PulseAudio", BackendAlsa: "ALSA",
	BackendJack: "JACK", BackendAaudio: "AAudio", BackendOpensl: "OpenSL|ES",
	BackendWebaudio: "Web Audio", BackendCustom: "Custom", BackendNull: "Null",
}

// String returns the backend name.
func (b Backend) String() string {
	if s, ok := backendNames[b]; ok {
		return s
	}
	return fmt.Sprintf("Backend(%d)", uint32(b))
}

// subParams is the resolved configuration of one direction of a device.
// Zero format, channels or rate mean "native"; the backend replaces them with
// what it opened.
type subParams struct {
	id        *DeviceID
	format    FormatType
	channels  uint32
	shareMode ShareMode
	// Set by the backend: the format the backend or hardware actually runs at.
	internalFormat     FormatType
	internalChannels   uint32
	internalSampleRate uint32
}

func (s *subParams) frameBytes() int {
	return SampleSizeInBytes(s.format) * int(s.channels)
}

// deviceParams is what a backend needs to open a device.
type deviceParams struct {
	deviceType   DeviceType
	sampleRate   uint32
	periodFrames uint32 // 0: derive from periodMillis
	periodMillis uint32
	periods      uint32
	capture      subParams
	playback     subParams
}

func newDeviceParams(c *DeviceConfig) *deviceParams {
	p := &deviceParams{
		deviceType:   c.DeviceType,
		sampleRate:   c.SampleRate,
		periodFrames: c.PeriodSizeInFrames,
		periodMillis: c.PeriodSizeInMilliseconds,
		periods:      c.Periods,
		capture:      c.Capture.params(),
		playback:     c.Playback.params(),
	}
	if p.periodMillis == 0 {
		p.periodMillis = 10
		if c.PerformanceProfile == Conservative {
			p.periodMillis = 100
		}
	}
	if p.periods == 0 {
		p.periods = 3
	}
	return p
}

func (s *SubConfig) params() subParams {
	p := subParams{format: s.Format, channels: s.Channels, shareMode: s.ShareMode}
	if s.DeviceID != nil {
		id := *(*DeviceID)(s.DeviceID)
		p.id = &id
	}
	return p
}

// hasCapture and hasPlayback report which directions the device type uses.
func (p *deviceParams) hasCapture() bool {
	return p.deviceType == Capture || p.deviceType == Duplex || p.deviceType == Loopback
}

func (p *deviceParams) hasPlayback() bool {
	return p.deviceType == Playback || p.deviceType == Duplex
}

// framesPerPeriod returns the period size at the given rate.
func (p *deviceParams) framesPerPeriod(rate uint32) uint32 {
	if p.periodFrames != 0 {
		return p.periodFrames
	}
	return max(1, uint32(uint64(rate)*uint64(p.periodMillis)/1000))
}

// fillDefaults replaces the "native" zero values that the backend left open.
func (s *subParams) fillDefaults(format FormatType, channels uint32) {
	if s.format == FormatUnknown {
		s.format = format
	}
	if s.channels == 0 {
		s.channels = channels
	}
}

// sink connects a stream to the Device that owns it.
// Every start is a numbered run, so that a backend reporting a failure late
// cannot stop a later run.
type sink struct {
	data   DataProc
	onStop StopProc

	mu      sync.Mutex
	run     uint64
	started bool
}

func (s *sink) process(out, in []byte, frames uint32) {
	if s.data != nil {
		s.data(out, in, frames)
	}
}

// begin starts a new run and returns its number.
func (s *sink) begin() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.run++
	s.started = true
	return s.run
}

func (s *sink) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// finish marks run stopped. It reports whether this call stopped it, which
// is when the Stop callback must be called.
func (s *sink) finish(run uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.run != run {
		return false
	}
	s.started = false
	return true
}

func (s *sink) notifyStop() {
	if s.onStop != nil {
		s.onStop()
	}
}

// fail is called by a backend whose stream stopped on an error.
func (s *sink) fail(run uint64) {
	if s.finish(run) {
		s.notifyStop()
	}
}

// deviceIDString stores s in a DeviceID; backends keep their native ids as text.
func deviceIDString(s string) DeviceID {
	var id DeviceID
	copy(id[:len(id)-1], s)
	return id
}

// str returns the text stored by deviceIDString.
func (d *DeviceID) str() string {
	n := 0
	for n < len(d) && d[n] != 0 {
		n++
	}
	return string(d[:n])
}

func newDeviceInfo(id DeviceID, name string, isDefault bool, formats []DataFormat) DeviceInfo {
	info := DeviceInfo{ID: id, FormatCount: uint32(len(formats)), Formats: formats}
	copy(info.name[:len(info.name)-1], name)
	if isDefault {
		info.IsDefault = 1
	}
	return info
}
