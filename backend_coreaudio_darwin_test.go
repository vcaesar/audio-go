//go:build darwin

package audiogo

import (
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestQueueRingLayout(t *testing.T) {
	var r queueRing
	if unsafe.Offsetof(r.sem) != 0 || unsafe.Offsetof(r.head) != 8 || unsafe.Offsetof(r.bufs) != 16 || len(r.bufs) != 64 {
		t.Fatal("queueRing layout no longer matches sys_darwin_*.s")
	}
	if unsafe.Sizeof(audioStreamBasicDescription{}) != 40 || unsafe.Sizeof(propertyAddress{}) != 12 || unsafe.Sizeof(audioValueRange{}) != 16 {
		t.Fatal("Core Audio struct sizes")
	}
	var b audioQueueBuffer
	if unsafe.Offsetof(b.data) != 8 || unsafe.Offsetof(b.size) != 16 {
		t.Fatal("AudioQueueBuffer layout")
	}
}

func TestStreamFormat(t *testing.T) {
	f, err := streamFormat(FormatS16, 2, 44100)
	if err != nil || f.formatFlags != formatFlagSigned|formatFlagPacked || f.bytesPerFrame != 4 || f.bitsPerChannel != 16 || f.sampleRate != 44100 {
		t.Fatalf("s16: %+v %v", f, err)
	}
	if f, _ := streamFormat(FormatF32, 1, 48000); f.formatFlags&formatFlagFloat == 0 || f.bytesPerFrame != 4 {
		t.Fatalf("f32: %+v", f)
	}
	if f, _ := streamFormat(FormatS24, 1, 48000); f.bytesPerFrame != 3 || f.bitsPerChannel != 24 {
		t.Fatalf("s24: %+v", f)
	}
	if _, err := streamFormat(FormatUnknown, 1, 48000); err != ErrFormatNotSupported {
		t.Fatalf("unknown format: %v", err)
	}
}

func TestParseBufferListChannels(t *testing.T) {
	b := make([]byte, 8+2*16)
	*(*uint32)(unsafe.Pointer(&b[0])) = 2
	*(*uint32)(unsafe.Pointer(&b[8])) = 2
	*(*uint32)(unsafe.Pointer(&b[24])) = 6
	if got := parseBufferListChannels(b); got != 8 {
		t.Fatalf("channels = %d, want 8", got)
	}
	if parseBufferListChannels(b[:2]) != 0 || parseBufferListChannels(b[:24]) != 2 {
		t.Fatal("short buffer lists")
	}
}

func TestRatesFromRanges(t *testing.T) {
	got := ratesFromRanges([]audioValueRange{{44100, 44100}, {48000, 96000}, {48000, 48000}})
	if len(got) != 3 || got[0] != 44100 || got[1] != 48000 || got[2] != 96000 {
		t.Fatalf("rates = %v", got)
	}
}

func TestAppendBounded(t *testing.T) {
	f := appendBounded([]byte{1, 2}, []byte{3, 4, 5}, 4)
	if string(f) != string([]byte{2, 3, 4, 5}) {
		t.Fatalf("fifo = %v", f)
	}
}

func TestCFStringRoundTrip(t *testing.T) {
	ref := cfString("audiogo ü")
	if ref == 0 {
		t.Fatal("CFStringCreateWithCString failed")
	}
	defer cfRelease(ref)
	if got := goStringFromCF(ref); got != "audiogo ü" {
		t.Fatalf("round trip = %q", got)
	}
}

// coreAudioContext returns a Core Audio context, skipping without a device of kind.
func coreAudioContext(t *testing.T, kind DeviceType) *AllocatedContext {
	t.Helper()
	ctx, err := InitContext([]Backend{BackendCoreaudio}, ContextConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defaultDevice(kind); err != nil {
		t.Skipf("no default device: %v", err)
	}
	return ctx
}

func TestCoreAudioEnumeration(t *testing.T) {
	ctx := coreAudioContext(t, Playback)
	devs, err := ctx.Devices(Playback)
	if err != nil || len(devs) == 0 {
		t.Fatalf("Devices: %v %d", err, len(devs))
	}
	var defaults int
	for _, d := range devs {
		if d.IsDefault == 1 {
			defaults++
		}
		info, err := ctx.DeviceInfo(Playback, d.ID, Shared)
		if err != nil || info.Name() != d.Name() {
			t.Errorf("DeviceInfo(%s): %v", d.Name(), err)
		}
	}
	if defaults != 1 {
		t.Errorf("want exactly one default device, got %d", defaults)
	}
	if _, err := ctx.DeviceInfo(Playback, deviceIDString("no-such-uid"), Shared); err != ErrNoDevice {
		t.Errorf("unknown id: %v", err)
	}
}

func runCoreAudioDevice(t *testing.T, cfg DeviceConfig, check func(out, in []byte, frames uint32) bool) *Device {
	t.Helper()
	ctx := coreAudioContext(t, cfg.DeviceType)
	if cfg.DeviceType == Duplex {
		if _, err := defaultDevice(Capture); err != nil {
			t.Skipf("no capture device: %v", err)
		}
	}
	var good, bad atomic.Int64
	stopped := make(chan struct{}, 1)
	dev, err := InitDevice(ctx.Context, cfg, DeviceCallbacks{
		Data: func(out, in []byte, frames uint32) {
			if check(out, in, frames) {
				good.Add(1)
			} else {
				bad.Add(1)
			}
		},
		Stop: func() { stopped <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Uninit()
	for range 2 { // restart must work
		if err := dev.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for good.Load() < 3 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if err := dev.Stop(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("stop callback not called")
		}
		if good.Load() < 3 || bad.Load() != 0 {
			t.Fatalf("callbacks: good=%d bad=%d", good.Load(), bad.Load())
		}
		n := good.Load()
		time.Sleep(50 * time.Millisecond)
		if good.Load() != n {
			t.Fatal("data callback after Stop")
		}
		good.Store(0)
	}
	return dev
}

func TestCoreAudioPlayback(t *testing.T) {
	cfg := DefaultDeviceConfig(Playback)
	cfg.Playback.Format = FormatS16
	cfg.Playback.Channels = 1
	cfg.SampleRate = 44100
	dev := runCoreAudioDevice(t, cfg, func(out, in []byte, frames uint32) bool {
		return in == nil && len(out) == int(frames)*2 && frames == 441
	})
	if dev.PlaybackInternalChannels() == 0 || dev.PlaybackInternalSampleRate() == 0 || dev.PlaybackInternalFormat() != FormatF32 {
		t.Error("internal format not reported")
	}
}

func TestCoreAudioCapture(t *testing.T) {
	cfg := DefaultDeviceConfig(Capture)
	cfg.Capture.Format = FormatF32
	cfg.Capture.Channels = 2
	dev := runCoreAudioDevice(t, cfg, func(out, in []byte, frames uint32) bool {
		return out == nil && len(in) == int(frames)*8 && frames > 0
	})
	if dev.SampleRate() == 0 || dev.CaptureChannels() != 2 {
		t.Errorf("rate=%d channels=%d", dev.SampleRate(), dev.CaptureChannels())
	}
}

func TestCoreAudioDuplex(t *testing.T) {
	cfg := DefaultDeviceConfig(Duplex)
	cfg.Capture.Format = FormatS16
	cfg.Capture.Channels = 1
	cfg.Playback.Format = FormatS16
	cfg.Playback.Channels = 2
	cfg.SampleRate = 48000
	runCoreAudioDevice(t, cfg, func(out, in []byte, frames uint32) bool {
		return len(out) == int(frames)*4 && len(in) == int(frames)*2
	})
}

func TestCoreAudioDeviceByID(t *testing.T) {
	ctx := coreAudioContext(t, Playback)
	devs, err := ctx.Devices(Playback)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultDeviceConfig(Playback)
	cfg.Playback.DeviceID = devs[0].ID.Pointer()
	dev, err := InitDevice(ctx.Context, cfg, DeviceCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	dev.Uninit()
	missing := deviceIDString("no-such-uid")
	cfg.Playback.DeviceID = missing.Pointer()
	if _, err := InitDevice(ctx.Context, cfg, DeviceCallbacks{}); err != ErrNoDevice {
		t.Fatalf("missing device: %v", err)
	}
	if _, err := InitDevice(ctx.Context, DefaultDeviceConfig(Loopback), DeviceCallbacks{}); err != ErrDeviceTypeNotSupported {
		t.Fatalf("loopback: %v", err)
	}
}
