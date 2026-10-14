//go:build windows && (amd64 || arm64)

package audiogo

import (
	"testing"
	"unsafe"
)

func TestWASAPILayout(t *testing.T) {
	if unsafe.Sizeof(waveFormatExtensible{}) != 40 || unsafe.Sizeof(propVariant{}) != 24 || unsafe.Sizeof(comGUID{}) != 16 {
		t.Fatal("WASAPI struct sizes")
	}
}

func TestWaveFormatRoundTrip(t *testing.T) {
	for _, f := range []FormatType{FormatU8, FormatS16, FormatS24, FormatS32, FormatF32} {
		wf := waveFormat(f, 2, 48000)
		if wf.blockAlign != uint16(2*SampleSizeInBytes(f)) || wf.avgBytesPerSec != 48000*uint32(wf.blockAlign) || wf.channelMask != 3 {
			t.Fatalf("%d: %+v", f, wf)
		}
		got := formatFromWave(&wf)
		if got != (DataFormat{Format: f, Channels: 2, SampleRate: 48000}) {
			t.Fatalf("round trip %d: %+v", f, got)
		}
	}
	plain := waveFormatExtensible{formatTag: waveFormatFloat, channels: 1, samplesPerSec: 44100, bitsPerSample: 32}
	if formatFromWave(&plain).Format != FormatF32 {
		t.Fatal("WAVE_FORMAT_IEEE_FLOAT")
	}
}

func TestWASAPIEnumeration(t *testing.T) {
	ctx, err := InitContext([]Backend{BackendWasapi}, ContextConfig{}, nil)
	if err != nil {
		t.Skipf("WASAPI unavailable: %v", err)
	}
	devs, err := ctx.Devices(Playback)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		if _, err := ctx.DeviceInfo(Playback, d.ID, Shared); err != nil {
			t.Errorf("DeviceInfo(%s): %v", d.Name(), err)
		}
	}
}
