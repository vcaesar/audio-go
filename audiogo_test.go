package audiogo_test

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	audiogo "github.com/vcaesar/audio-go"
)

func nullContext(t *testing.T) *audiogo.AllocatedContext {
	t.Helper()
	ctx, err := audiogo.InitContext([]audiogo.Backend{audiogo.BackendNull}, audiogo.ContextConfig{}, nil)
	if err != nil {
		t.Fatalf("InitContext: %v", err)
	}
	t.Cleanup(func() {
		if err := ctx.Uninit(); err != nil {
			t.Errorf("Uninit: %v", err)
		}
		ctx.Free()
	})
	return ctx
}

func TestContextLifecycle(t *testing.T) {
	var logs atomic.Int32
	ctx, err := audiogo.InitContext([]audiogo.Backend{audiogo.BackendNull},
		audiogo.ContextConfig{ThreadPriority: audiogo.ThreadPriorityNormal},
		func(string) { logs.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Context == (audiogo.Context{}) {
		t.Fatal("context value expected")
	}
	if err := ctx.Uninit(); err != nil {
		t.Fatal(err)
	}
	ctx.Free()
	if ctx.Context != (audiogo.Context{}) {
		t.Fatal("context must be reset after Free")
	}
	ctx.Free() // second Free is a no-op
	if logs.Load() == 0 {
		t.Error("expected log messages through LogProc")
	}
}

func TestContextDeviceEnumeration(t *testing.T) {
	ctx := nullContext(t)
	for _, kind := range []audiogo.DeviceType{audiogo.Playback, audiogo.Capture} {
		devices, err := ctx.Devices(kind)
		if err != nil {
			t.Fatalf("Devices(%d): %v", kind, err)
		}
		if len(devices) != 1 {
			t.Fatalf("null backend: want 1 device, got %d", len(devices))
		}
		if !strings.Contains(devices[0].Name(), "NULL") {
			t.Errorf("unexpected name %q", devices[0].Name())
		}
		info, err := ctx.DeviceInfo(kind, devices[0].ID, audiogo.Shared)
		if err != nil {
			t.Fatalf("DeviceInfo: %v", err)
		}
		if info.FormatCount == 0 || len(info.Formats) != int(info.FormatCount) {
			t.Errorf("expected native formats, got %+v", info)
		}
	}
}

func TestDefaultDeviceConfig(t *testing.T) {
	cfg := audiogo.DefaultDeviceConfig(audiogo.Capture)
	if cfg.DeviceType != audiogo.Capture {
		t.Fatalf("device type = %d", cfg.DeviceType)
	}
	if cfg.Resampling.Algorithm != audiogo.ResampleAlgorithmLinear || cfg.Resampling.Linear.LpfOrder == 0 {
		t.Errorf("unexpected resampling defaults: %+v", cfg.Resampling)
	}
	if cfg.PeriodSizeInMilliseconds == 0 || cfg.Periods == 0 {
		t.Errorf("unexpected period defaults: %+v", cfg)
	}
}

func TestResultError(t *testing.T) {
	if got := audiogo.ErrNoDevice.Error(); got != "miniaudio: No device" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestCapturePlayback(t *testing.T) {
	ctx := nullContext(t)

	cfg := audiogo.DefaultDeviceConfig(audiogo.Capture)
	cfg.Capture.Format = audiogo.FormatS16
	cfg.Capture.Channels = 2
	cfg.SampleRate = 44100
	cfg.Pulse.StreamNameCapture = "audiogo-test"

	var frames, bytes atomic.Int64
	stopped := make(chan struct{}, 1)
	dev, err := audiogo.InitDevice(ctx.Context, cfg, audiogo.DeviceCallbacks{
		Data: func(out, in []byte, n uint32) {
			if out != nil {
				t.Error("capture device must not get an output buffer")
			}
			frames.Add(int64(n))
			bytes.Add(int64(len(in)))
		},
		Stop: func() { stopped <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if dev.Type() != audiogo.Capture || dev.CaptureFormat() != audiogo.FormatS16 ||
		dev.CaptureChannels() != 2 || dev.SampleRate() != 44100 {
		t.Fatalf("device state: type=%d format=%d ch=%d rate=%d",
			dev.Type(), dev.CaptureFormat(), dev.CaptureChannels(), dev.SampleRate())
	}
	if dev.CaptureInternalSampleRate() == 0 || dev.CaptureInternalChannels() == 0 || dev.CaptureInternalFormat() == audiogo.FormatUnknown {
		t.Error("internal format not reported")
	}
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	if !dev.IsStarted() {
		t.Fatal("device not started")
	}
	waitFor(t, func() bool { return frames.Load() > 0 })
	if err := dev.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Error("stop callback not called")
	}
	if dev.IsStarted() {
		t.Error("device still started")
	}
	dev.Uninit()
	if got, want := bytes.Load(), frames.Load()*4; got != want {
		t.Errorf("input bytes = %d, want frames*4 = %d", got, want)
	}

	// Playback through the default context.
	cfg = audiogo.DefaultDeviceConfig(audiogo.Playback)
	cfg.Playback.Format = audiogo.FormatF32
	cfg.Playback.Channels = 1
	var outBytes atomic.Int64
	dev, err = audiogo.InitDevice(ctx.Context, cfg, audiogo.DeviceCallbacks{
		Data: func(out, in []byte, n uint32) {
			if in != nil || len(out) != int(n)*4 {
				t.Errorf("playback buffers: out=%d in=%v frames=%d", len(out), in != nil, n)
			}
			outBytes.Add(int64(len(out)))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return outBytes.Load() > 0 })
	dev.Uninit()
}

func TestDeviceSelectedByID(t *testing.T) {
	ctx := nullContext(t)
	devices, err := ctx.Devices(audiogo.Capture)
	if err != nil || len(devices) == 0 {
		t.Fatalf("Devices: %v %d", err, len(devices))
	}
	cfg := audiogo.DefaultDeviceConfig(audiogo.Capture)
	cfg.Capture.DeviceID = devices[0].ID.Pointer()
	dev, err := audiogo.InitDevice(ctx.Context, cfg, audiogo.DeviceCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	dev.Uninit()
}

func TestInitDeviceError(t *testing.T) {
	ctx := nullContext(t)
	_, err := audiogo.InitDevice(ctx.Context, audiogo.DefaultDeviceConfig(audiogo.Loopback), audiogo.DeviceCallbacks{})
	if !errors.Is(err, audiogo.ErrDeviceTypeNotSupported) {
		t.Fatalf("null backend loopback: got %v, want ErrDeviceTypeNotSupported", err)
	}
}

func TestNilContextMethods(t *testing.T) {
	if _, err := audiogo.DefaultContext.Devices(audiogo.Playback); !errors.Is(err, audiogo.ErrInvalidArgs) {
		t.Fatalf("got %v", err)
	}
	if err := audiogo.DefaultContext.Uninit(); !errors.Is(err, audiogo.ErrInvalidArgs) {
		t.Fatalf("got %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for audio callbacks")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
