//go:build linux && (amd64 || arm64 || riscv64 || loong64 || 386 || arm)

package audiogo

import (
	"strings"
	"testing"
	"unsafe"
)

// Sizes and ioctl numbers from include/uapi/sound/asound.h, as verified by
// go-audio-capture's C probes.
func TestALSALayout(t *testing.T) {
	type want struct {
		hw, sw, xferi      uintptr
		refine, swp, readi uintptr
		writei             uintptr
	}
	w := want{hw: 608, sw: 136, xferi: 24, refine: 0xc2604110, swp: 0xc0884113, readi: 0x80184151, writei: 0x40184150}
	if unsafe.Sizeof(uint(0)) == 4 {
		w = want{hw: 604, sw: 104, xferi: 12, refine: 0xc25c4110, swp: 0xc0684113, readi: 0x800c4151, writei: 0x400c4150}
	}
	got := want{
		hw: unsafe.Sizeof(sndHwParams{}), sw: unsafe.Sizeof(sndSwParams{}), xferi: unsafe.Sizeof(sndXferi{}),
		refine: iocHwRefine, swp: iocSwParams, readi: iocReadIFrames, writei: iocWriteIFrame,
	}
	if got != w {
		t.Fatalf("ALSA ABI mismatch:\n got %#v\nwant %#v", got, w)
	}
	if iocPrepare != 0x4140 || iocStart != 0x4142 || iocDrop != 0x4143 || iocHwParams != iocHwRefine+1 {
		t.Fatal("control ioctl numbers")
	}
}

func TestParseProcPCM(t *testing.T) {
	in := `00-00: ALC892 Analog : ALC892 Analog : playback 1 : capture 1
00-03: HDMI 0 : HDMI 0 : playback 1
01-00: USB Audio : USB Audio : capture 1
garbage line
`
	devs := parseProcPCM(strings.NewReader(in))
	if len(devs) != 3 {
		t.Fatalf("devices = %+v", devs)
	}
	if d := devs[0]; d.card != 0 || d.dev != 0 || d.name != "ALC892 Analog" || !d.playback || !d.capture {
		t.Errorf("first = %+v", d)
	}
	if d := devs[1]; d.dev != 3 || !d.playback || d.capture {
		t.Errorf("hdmi = %+v", d)
	}
	if d := devs[2]; d.card != 1 || d.playback || !d.capture || d.id() != deviceIDString("hw:1,0") {
		t.Errorf("usb = %+v", d)
	}
}

func TestParseHwID(t *testing.T) {
	if c, d, ok := parseHwID("hw:2,1"); !ok || c != 2 || d != 1 {
		t.Fatalf("hw:2,1 -> %d %d %v", c, d, ok)
	}
	for _, s := range []string{"", "hw:", "hw:1", "plughw:1,0", "hw:a,0"} {
		if _, _, ok := parseHwID(s); ok {
			t.Errorf("%q must not parse", s)
		}
	}
}

func TestHwParamsHelpers(t *testing.T) {
	var hw sndHwParams
	hw.fillAny()
	if !hw.hasMask(paramFormat, 14) {
		t.Fatal("fillAny must allow every format")
	}
	hw.setMask(paramFormat, 2)
	if !hw.hasMask(paramFormat, 2) || hw.hasMask(paramFormat, 14) {
		t.Fatal("setMask")
	}
	*hw.interval(paramRate) = sndInterval{min: 44099, max: 48001, flags: intervalOpenMin | intervalOpenMax}
	if lo, hi := hw.bounds(paramRate); lo != 44100 || hi != 48000 {
		t.Fatalf("open bounds = %d..%d", lo, hi)
	}
}

func TestChooseFormat(t *testing.T) {
	var caps sndHwParams
	caps.fillAny()
	caps.masks[paramFormat] = sndMask{}
	caps.masks[paramFormat].bits[0] = 1<<10 | 1<<14 // S32_LE, FLOAT_LE
	caps.setRange(paramChannels, 1, 8)
	caps.setRange(paramRate, 8000, 96000)

	sub := subParams{}
	var rate uint32
	if err := chooseFormat(&caps, &sub, &rate); err != nil || sub.format != FormatS32 || sub.channels != 2 || rate != 48000 {
		t.Fatalf("native: %+v rate=%d err=%v", sub, rate, err)
	}
	sub = subParams{format: FormatS16}
	if err := chooseFormat(&caps, &sub, &rate); err != ErrFormatNotSupported {
		t.Fatalf("s16 must be rejected: %v", err)
	}
	sub = subParams{format: FormatF32, channels: 9}
	if err := chooseFormat(&caps, &sub, &rate); err != ErrFormatNotSupported {
		t.Fatalf("9 channels must be rejected: %v", err)
	}
	rate = 192000
	sub = subParams{format: FormatF32, channels: 1}
	if err := chooseFormat(&caps, &sub, &rate); err != ErrFormatNotSupported {
		t.Fatalf("192 kHz must be rejected: %v", err)
	}
}

func TestALSABoundary(t *testing.T) {
	b := alsaBoundary(1920)
	if b%1920 != 0 || b < 1<<27 || alsaBoundary(0) != 0 {
		t.Fatalf("boundary = %d", b)
	}
}

func TestALSAMissingDevice(t *testing.T) {
	if _, err := openPCM(99, 99, Capture); err != ErrNoDevice {
		t.Fatalf("got %v, want ErrNoDevice", err)
	}
	id := deviceIDString("bogus")
	if _, _, err := resolveALSA(Capture, &id); err != ErrNoDevice {
		t.Fatalf("got %v", err)
	}
}
