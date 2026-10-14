package audiogo

import (
	"strings"
	"testing"
)

func TestSampleSizeInBytes(t *testing.T) {
	cases := map[FormatType]int{
		FormatUnknown: 0, FormatU8: 1, FormatS16: 2, FormatS24: 3, FormatS32: 4, FormatF32: 4, FormatType(99): 0,
	}
	for format, want := range cases {
		if got := SampleSizeInBytes(format); got != want {
			t.Errorf("SampleSizeInBytes(%d) = %d, want %d", format, got, want)
		}
	}
}

func TestDeviceIDString(t *testing.T) {
	var id DeviceID
	if got := id.String(); got != "00" {
		t.Errorf("zero id = %q, want 00", got)
	}
	copy(id[:], []byte{0xab, 0x00, 0x01})
	if got := id.String(); got != "ab0001" {
		t.Errorf("id = %q, want ab0001", got)
	}
}

func TestDeviceIDPointerCopies(t *testing.T) {
	id := DeviceID{1, 2, 3}
	p := (*DeviceID)(id.Pointer())
	id[0] = 9
	if p[0] != 1 || p[2] != 3 {
		t.Fatalf("Pointer must return an independent copy, got %v", p[:3])
	}
}

func TestDeviceIDText(t *testing.T) {
	id := deviceIDString("hw:1,0")
	if got := id.str(); got != "hw:1,0" {
		t.Fatalf("str() = %q", got)
	}
	long := deviceIDString(strings.Repeat("x", 400))
	if got := long.str(); len(got) != len(long)-1 {
		t.Fatalf("long id must be truncated with a terminating NUL, got %d bytes", len(got))
	}
}

func TestNewDeviceInfo(t *testing.T) {
	formats := []DataFormat{{Format: FormatS16, Channels: 2, SampleRate: 48000}}
	info := newDeviceInfo(deviceIDString("mic"), "Mic", true, formats)
	if info.Name() != "Mic" || info.IsDefault != 1 || info.FormatCount != 1 || info.Formats[0] != formats[0] {
		t.Fatalf("unexpected info %+v", info)
	}
	if !strings.Contains(info.String(), "Name: Mic") {
		t.Fatalf("String() = %q", info.String())
	}
}

func TestNewDeviceParams(t *testing.T) {
	id := DeviceID{4, 2}
	cfg := DeviceConfig{
		DeviceType: Duplex,
		SampleRate: 48000,
		Capture:    SubConfig{DeviceID: id.Pointer(), Format: FormatS16, Channels: 2, ShareMode: Exclusive},
		Playback:   SubConfig{Format: FormatF32, Channels: 1},
	}
	p := newDeviceParams(&cfg)
	if p.periodMillis != 10 || p.periods != 3 {
		t.Fatalf("defaults not applied: %+v", p)
	}
	if p.capture.id == nil || *p.capture.id != id || p.capture.shareMode != Exclusive || p.playback.id != nil {
		t.Fatal("device ids not converted")
	}
	if !p.hasCapture() || !p.hasPlayback() {
		t.Fatal("duplex uses both directions")
	}
	if got := p.framesPerPeriod(48000); got != 480 {
		t.Fatalf("framesPerPeriod = %d, want 480", got)
	}
	p.periodFrames = 256
	if got := p.framesPerPeriod(48000); got != 256 {
		t.Fatalf("explicit period ignored: %d", got)
	}
	cfg.PerformanceProfile = Conservative
	cfg.Periods = 5
	if p := newDeviceParams(&cfg); p.periodMillis != 100 || p.periods != 5 {
		t.Fatalf("conservative profile: %+v", p)
	}
	if p := newDeviceParams(&DeviceConfig{DeviceType: Loopback}); !p.hasCapture() || p.hasPlayback() {
		t.Fatal("loopback is capture only")
	}
}

func TestFillDefaults(t *testing.T) {
	s := subParams{channels: 1}
	s.fillDefaults(FormatF32, 2)
	if s.format != FormatF32 || s.channels != 1 || s.frameBytes() != 4 {
		t.Fatalf("fillDefaults: %+v", s)
	}
}

func TestSinkRuns(t *testing.T) {
	var stops int
	s := &sink{onStop: func() { stops++ }}
	s.fail(0) // not started
	first := s.begin()
	second := s.begin()
	s.fail(first) // a late failure of an old run must not stop the new one
	if !s.isStarted() || stops != 0 {
		t.Fatalf("stale failure stopped the device: started=%v stops=%d", s.isStarted(), stops)
	}
	s.fail(second)
	s.fail(second)
	if s.isStarted() || stops != 1 {
		t.Fatalf("started=%v stops=%d, want stopped once", s.isStarted(), stops)
	}
	s.process(nil, nil, 1) // nil Data is allowed
}

// fakeStream records calls and lets a test simulate a backend failure.
type fakeStream struct {
	sink              *sink
	run               uint64
	starts, stops     int
	closed            bool
	startErr, stopErr error
}

func (f *fakeStream) start(run uint64) error {
	f.starts++
	f.run = run
	return f.startErr
}

func (f *fakeStream) stop() error {
	f.stops++
	return f.stopErr
}

func (f *fakeStream) close() { f.closed = true }

func newFakeDevice(cb DeviceCallbacks) (*Device, *fakeStream) {
	s := &sink{data: cb.Data, onStop: cb.Stop}
	f := &fakeStream{sink: s}
	return &Device{params: &deviceParams{deviceType: Capture}, sink: s, stream: f}, f
}

func TestDeviceLifecycle(t *testing.T) {
	var stops int
	dev, f := newFakeDevice(DeviceCallbacks{Stop: func() { stops++ }})
	if err := dev.Stop(); err != nil || f.stops != 0 {
		t.Fatalf("Stop before Start: %v stops=%d", err, f.stops)
	}
	if err := dev.Start(); err != nil || !dev.IsStarted() {
		t.Fatal(err)
	}
	if err := dev.Start(); err != nil || f.starts != 1 {
		t.Fatalf("second Start must be a no-op: starts=%d", f.starts)
	}
	if err := dev.Stop(); err != nil || dev.IsStarted() || stops != 1 || f.stops != 1 {
		t.Fatalf("Stop: err=%v started=%v callbacks=%d stops=%d", err, dev.IsStarted(), stops, f.stops)
	}
	dev.Uninit()
	if !f.closed || dev.Start() != ErrDeviceNotInitialized {
		t.Fatal("Uninit must close the stream")
	}
	dev.Uninit() // second Uninit is a no-op
}

func TestDeviceBackendFailureThenRestart(t *testing.T) {
	var stops int
	dev, f := newFakeDevice(DeviceCallbacks{Stop: func() { stops++ }})
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	f.sink.fail(f.run) // the backend worker died
	if dev.IsStarted() || stops != 1 {
		t.Fatalf("failure: started=%v stops=%d", dev.IsStarted(), stops)
	}
	// Start must release the failed run before starting a new one.
	if err := dev.Start(); err != nil || f.stops != 1 || f.starts != 2 || !dev.IsStarted() {
		t.Fatalf("restart: err=%v stops=%d starts=%d", err, f.stops, f.starts)
	}
	dev.Uninit()
	if stops != 2 || f.stops != 2 {
		t.Fatalf("Uninit: callbacks=%d stops=%d", stops, f.stops)
	}
}

func TestDeviceStopCallbackMayRestart(t *testing.T) {
	var dev *Device
	restarted := false
	dev, _ = newFakeDevice(DeviceCallbacks{Stop: func() {
		if !restarted {
			restarted = true
			if err := dev.Start(); err != nil { // must not deadlock
				t.Error(err)
			}
		}
	}})
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	if err := dev.Stop(); err != nil {
		t.Fatal(err)
	}
	if !dev.IsStarted() {
		t.Fatal("Start from the Stop callback did not take effect")
	}
	dev.Uninit()
}

func TestDeviceStartError(t *testing.T) {
	dev, f := newFakeDevice(DeviceCallbacks{})
	f.startErr = ErrFailedToStartBackendDevice
	if err := dev.Start(); err != ErrFailedToStartBackendDevice || dev.IsStarted() {
		t.Fatalf("got %v started=%v", err, dev.IsStarted())
	}
	if err := dev.Stop(); err != nil || f.stops != 0 {
		t.Fatal("a failed start must not be stopped")
	}
	f.startErr = nil
	f.stopErr = ErrFailedToStopBackendDevice
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	dev.Uninit() // a failed stop still closes
	if !f.closed {
		t.Fatal("stream not closed after failed stop")
	}
}

func TestInitDeviceRejectsBadConfig(t *testing.T) {
	ctx, err := InitContext([]Backend{BackendNull}, ContextConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultDeviceConfig(Capture)
	cfg.Capture.Format = FormatType(42)
	if _, err := InitDevice(ctx.Context, cfg, DeviceCallbacks{}); err != ErrFormatNotSupported {
		t.Fatalf("bad format: %v", err)
	}
	if _, err := InitDevice(ctx.Context, DeviceConfig{DeviceType: 9}, DeviceCallbacks{}); err != ErrInvalidArgs {
		t.Fatalf("bad device type: %v", err)
	}
}

func TestErrorFromResult(t *testing.T) {
	if errorFromResult(0) != nil {
		t.Fatal("MA_SUCCESS must be nil")
	}
	if err := errorFromResult(-204); err != ErrNoDevice {
		t.Fatalf("got %v, want ErrNoDevice", err)
	}
	if got := Result(-999).Error(); got != "miniaudio: result -999" {
		t.Fatalf("unknown result = %q", got)
	}
}

// Values must match ma_backend; malgo omitted ma_backend_custom, which made
// BackendNull select the custom backend.
func TestBackendValues(t *testing.T) {
	if BackendWebaudio != 12 || BackendCustom != 13 || BackendNull != 14 {
		t.Fatalf("webaudio=%d custom=%d null=%d", BackendWebaudio, BackendCustom, BackendNull)
	}
	if Backend(BackendNull).String() != "Null" || Backend(99).String() != "Backend(99)" {
		t.Fatal("backend names")
	}
}

func TestInitContextNoBackend(t *testing.T) {
	if _, err := InitContext([]Backend{BackendJack}, ContextConfig{}, nil); err != ErrNoBackend {
		t.Fatalf("got %v, want ErrNoBackend", err)
	}
}

func TestInitContextFallsBack(t *testing.T) {
	saved := openers[BackendCustom]
	openers[BackendCustom] = func(func(string)) (backend, error) { return nil, ErrUnavailable }
	defer func() {
		if saved == nil {
			delete(openers, BackendCustom)
		} else {
			openers[BackendCustom] = saved
		}
	}()
	var logs []string
	ctx, err := InitContext([]Backend{BackendCustom, BackendNull}, ContextConfig{}, func(m string) { logs = append(logs, m) })
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Backend() != BackendNull || len(logs) != 2 || !strings.Contains(logs[0], "unavailable") {
		t.Fatalf("backend=%v logs=%q", ctx.Backend(), logs)
	}
	if _, err := InitContext([]Backend{BackendCustom}, ContextConfig{}, nil); err != ErrFailedToInitBackend {
		t.Fatalf("got %v, want ErrFailedToInitBackend", err)
	}
}
