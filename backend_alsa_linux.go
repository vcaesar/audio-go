//go:build linux && (amd64 || arm64 || riscv64 || loong64 || 386 || arm)

package audiogo

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The ALSA backend talks to the kernel PCM devices under /dev/snd with
// ioctls, the way alsa-lib's hw plugin does, so it needs no libasound. Only
// hardware devices (hw:CARD,DEV) are available; the hardware must support
// the requested format, channel count and rate (there is no conversion).
// Layouts follow include/uapi/sound/asound.h for little-endian targets with
// the generic ioctl encoding; C unsigned long is Go uint on all of them.

func init() {
	openers[BackendAlsa] = func(func(string)) (backend, error) {
		if _, err := os.Stat(alsaProcPCM); err != nil {
			return nil, err
		}
		return alsaBackend{}, nil
	}
}

const alsaProcPCM = "/proc/asound/pcm"

type sndMask struct{ bits [8]uint32 }

type sndInterval struct{ min, max, flags uint32 }

const (
	intervalOpenMin = 1 << 0
	intervalOpenMax = 1 << 1
	intervalInteger = 1 << 2
	intervalEmpty   = 1 << 3
)

// sndHwParams mirrors struct snd_pcm_hw_params.
type sndHwParams struct {
	flags     uint32
	masks     [3]sndMask
	mres      [5]sndMask
	intervals [12]sndInterval
	ires      [9]sndInterval
	rmask     uint32
	cmask     uint32
	info      uint32
	msbits    uint32
	rateNum   uint32
	rateDen   uint32
	fifoSize  uint
	sync      [16]byte
	reserved  [48]byte
}

// sndSwParams mirrors struct snd_pcm_sw_params.
type sndSwParams struct {
	tstampMode       int32
	periodStep       uint32
	sleepMin         uint32
	availMin         uint
	xferAlign        uint
	startThreshold   uint
	stopThreshold    uint
	silenceThreshold uint
	silenceSize      uint
	boundary         uint
	proto            uint32
	tstampType       uint32
	reserved         [56]byte
}

// sndXferi mirrors struct snd_xferi.
type sndXferi struct {
	result int
	buf    unsafe.Pointer
	frames uint
}

const (
	paramAccess     = 0
	paramFormat     = 1
	paramSubformat  = 2
	paramChannels   = 10
	paramRate       = 11
	paramPeriodSize = 13
	paramPeriods    = 15
	paramBufferSize = 17

	accessRWInterleaved = 3
)

// ALSA format numbers (SNDRV_PCM_FORMAT_*).
var alsaFormats = map[FormatType]uint32{
	FormatU8:  1,  // U8
	FormatS16: 2,  // S16_LE
	FormatS32: 10, // S32_LE
	FormatF32: 14, // FLOAT_LE
	FormatS24: 32, // S24_3LE
}

// alsaFormatPreference is the order in which a native format is chosen.
var alsaFormatPreference = []FormatType{FormatS16, FormatS32, FormatF32, FormatS24, FormatU8}

func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'A'<<8 | nr }

var (
	iocHwRefine    = ioc(3, 0x10, unsafe.Sizeof(sndHwParams{}))
	iocHwParams    = ioc(3, 0x11, unsafe.Sizeof(sndHwParams{}))
	iocSwParams    = ioc(3, 0x13, unsafe.Sizeof(sndSwParams{}))
	iocPrepare     = ioc(0, 0x40, 0)
	iocStart       = ioc(0, 0x42, 0)
	iocDrop        = ioc(0, 0x43, 0)
	iocWriteIFrame = ioc(1, 0x50, unsafe.Sizeof(sndXferi{}))
	iocReadIFrames = ioc(2, 0x51, unsafe.Sizeof(sndXferi{}))
)

func (p *sndHwParams) fillAny() {
	for i := range p.masks {
		for j := range p.masks[i].bits {
			p.masks[i].bits[j] = ^uint32(0)
		}
	}
	for i := range p.intervals {
		p.intervals[i] = sndInterval{max: ^uint32(0)}
	}
	p.rmask = ^uint32(0)
	p.info = ^uint32(0)
}

func (p *sndHwParams) setMask(param int, v uint32) {
	m := &p.masks[param]
	*m = sndMask{}
	m.bits[v>>5] = 1 << (v & 31)
}

func (p *sndHwParams) hasMask(param int, v uint32) bool {
	return p.masks[param].bits[v>>5]&(1<<(v&31)) != 0
}

func (p *sndHwParams) interval(param int) *sndInterval { return &p.intervals[param-8] }

// bounds returns the closed range of an interval parameter.
func (p *sndHwParams) bounds(param int) (lo, hi uint32) {
	iv := p.interval(param)
	lo, hi = iv.min, iv.max
	if iv.flags&intervalOpenMin != 0 {
		lo++
	}
	if iv.flags&intervalOpenMax != 0 && hi > 0 {
		hi--
	}
	return lo, hi
}

func (p *sndHwParams) setRange(param int, lo, hi uint32) {
	*p.interval(param) = sndInterval{min: lo, max: hi, flags: intervalInteger}
}

type alsaPCM struct {
	fd int
}

func alsaIoctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func alsaPath(card, dev int, kind DeviceType) string {
	suffix := "p"
	if kind == Capture {
		suffix = "c"
	}
	return fmt.Sprintf("/dev/snd/pcmC%dD%d%s", card, dev, suffix)
}

// openPCM opens a PCM without waiting for a busy device, then switches the
// descriptor to blocking transfers (applied by the next PREPARE).
func openPCM(card, dev int, kind DeviceType) (*alsaPCM, error) {
	fd, err := syscall.Open(alsaPath(card, dev, kind), syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, alsaError(err)
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, alsaError(err)
	}
	return &alsaPCM{fd: fd}, nil
}

func alsaError(err error) error {
	switch {
	case errors.Is(err, syscall.EBUSY):
		return ErrBusy
	case errors.Is(err, syscall.ENOENT), errors.Is(err, syscall.ENODEV), errors.Is(err, syscall.ENXIO):
		return ErrNoDevice
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return ErrAccessDenied
	default:
		return ErrFailedToOpenBackendDevice
	}
}

func (p *alsaPCM) refine(hw *sndHwParams) error {
	hw.rmask = ^uint32(0)
	return alsaIoctl(p.fd, iocHwRefine, unsafe.Pointer(hw))
}

func (p *alsaPCM) close() { syscall.Close(p.fd) }

// capabilities returns the open configuration space of the device.
func (p *alsaPCM) capabilities() (sndHwParams, error) {
	var hw sndHwParams
	hw.fillAny()
	hw.setMask(paramAccess, accessRWInterleaved)
	err := p.refine(&hw)
	return hw, err
}

// chooseFormat resolves the native defaults of sub against the device.
func chooseFormat(caps *sndHwParams, sub *subParams, rate *uint32) error {
	if sub.format == FormatUnknown {
		for _, f := range alsaFormatPreference {
			if caps.hasMask(paramFormat, alsaFormats[f]) {
				sub.format = f
				break
			}
		}
	}
	code, ok := alsaFormats[sub.format]
	if !ok || !caps.hasMask(paramFormat, code) {
		return ErrFormatNotSupported
	}
	clo, chi := caps.bounds(paramChannels)
	if sub.channels == 0 {
		sub.channels = min(max(2, clo), chi)
	}
	rlo, rhi := caps.bounds(paramRate)
	if *rate == 0 {
		*rate = min(max(48000, rlo), rhi)
	}
	if sub.channels < clo || sub.channels > chi || *rate < rlo || *rate > rhi {
		return ErrFormatNotSupported
	}
	return nil
}

// configure commits format, channels and rate exactly and a period geometry
// close to the request. It returns the period and buffer size in frames.
func (p *alsaPCM) configure(sub *subParams, rate, periodFrames, periods uint32, kind DeviceType) (period, buffer uint32, err error) {
	var hw sndHwParams
	hw.fillAny()
	hw.setMask(paramAccess, accessRWInterleaved)
	hw.setMask(paramFormat, alsaFormats[sub.format])
	hw.setMask(paramSubformat, 0)
	hw.setRange(paramChannels, sub.channels, sub.channels)
	hw.setRange(paramRate, rate, rate)
	if err := p.refine(&hw); err != nil {
		return 0, 0, ErrFormatNotSupported
	}
	// HW_PARAMS picks the smallest remaining value, so asking for at least the
	// requested period size and count selects the nearest supported geometry
	// above it.
	geo := hw
	_, phi := geo.bounds(paramPeriodSize)
	geo.setRange(paramPeriodSize, min(periodFrames, phi), phi)
	_, nhi := geo.bounds(paramPeriods)
	geo.setRange(paramPeriods, min(periods, nhi), nhi)
	if p.refine(&geo) == nil {
		hw = geo
	}
	if err := alsaIoctl(p.fd, iocHwParams, unsafe.Pointer(&hw)); err != nil {
		return 0, 0, ErrFailedToOpenBackendDevice
	}
	period, _ = hw.bounds(paramPeriodSize)
	buffer, _ = hw.bounds(paramBufferSize)

	sw := sndSwParams{
		availMin:      uint(period),
		stopThreshold: uint(buffer),
		boundary:      alsaBoundary(uint(buffer)),
	}
	if kind == Capture {
		sw.startThreshold = uint(buffer) + 1 // explicit START
	} else {
		sw.startThreshold = uint(buffer) // starts once the buffer is full
	}
	if err := alsaIoctl(p.fd, iocSwParams, unsafe.Pointer(&sw)); err != nil {
		return 0, 0, ErrFailedToOpenBackendDevice
	}
	if err := alsaIoctl(p.fd, iocPrepare, nil); err != nil {
		return 0, 0, ErrFailedToOpenBackendDevice
	}
	sub.internalFormat, sub.internalChannels, sub.internalSampleRate = sub.format, sub.channels, rate
	return period, buffer, nil
}

// alsaBoundary returns the largest power-of-two multiple of the buffer size
// that stays below the kernel's pointer limit, as alsa-lib does.
func alsaBoundary(buffer uint) uint {
	if buffer == 0 {
		return 0
	}
	limit := uint(1) << (unsafe.Sizeof(uint(0))*8 - 4) // 1<<60 on 64-bit, 1<<28 on 32-bit
	b := buffer
	for b <= limit/2 {
		b *= 2
	}
	return b
}

// transfer reads or writes all frames, continuing after partial transfers
// and interrupted calls and recovering from xruns and suspends. It returns
// early once stopping is set.
func (p *alsaPCM) transfer(req uintptr, buf []byte, frames uint32, frameBytes int, stopping *atomic.Bool) error {
	for done := uint32(0); done < frames; {
		if stopping.Load() {
			return nil
		}
		x := sndXferi{buf: unsafe.Pointer(&buf[int(done)*frameBytes]), frames: uint(frames - done)}
		err := alsaIoctl(p.fd, req, unsafe.Pointer(&x))
		runtime.KeepAlive(buf)
		switch {
		case err == nil:
			done += uint32(x.result)
		case errors.Is(err, syscall.EINTR), errors.Is(err, syscall.EAGAIN):
		case errors.Is(err, syscall.EPIPE), errors.Is(err, syscall.ESTRPIPE):
			if err := alsaIoctl(p.fd, iocPrepare, nil); err != nil {
				return err
			}
			if req == iocReadIFrames {
				if err := alsaIoctl(p.fd, iocStart, nil); err != nil {
					return err
				}
			}
		default:
			return err
		}
	}
	return nil
}

// alsaDevice is one line of /proc/asound/pcm.
type alsaDevice struct {
	card, dev         int
	name              string
	playback, capture bool
}

func (d alsaDevice) id() DeviceID { return deviceIDString(fmt.Sprintf("hw:%d,%d", d.card, d.dev)) }

// parseProcPCM parses lines like
// "00-00: ALC892 Analog : ALC892 Analog : playback 1 : capture 1".
func parseProcPCM(r io.Reader) []alsaDevice {
	var devs []alsaDevice
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 3 {
			continue
		}
		var d alsaDevice
		if _, err := fmt.Sscanf(strings.TrimSpace(fields[0]), "%d-%d", &d.card, &d.dev); err != nil {
			continue
		}
		d.name = strings.TrimSpace(fields[1])
		for _, f := range fields[3:] {
			f = strings.TrimSpace(f)
			d.playback = d.playback || strings.HasPrefix(f, "playback")
			d.capture = d.capture || strings.HasPrefix(f, "capture")
		}
		devs = append(devs, d)
	}
	return devs
}

// parseHwID parses "hw:CARD,DEV".
func parseHwID(s string) (card, dev int, ok bool) {
	rest, found := strings.CutPrefix(s, "hw:")
	if !found {
		return 0, 0, false
	}
	c, d, found := strings.Cut(rest, ",")
	if !found {
		return 0, 0, false
	}
	card, err1 := strconv.Atoi(c)
	dev, err2 := strconv.Atoi(d)
	return card, dev, err1 == nil && err2 == nil
}

type alsaBackend struct{}

func (alsaBackend) uninit() error { return nil }

func listALSA(kind DeviceType) ([]alsaDevice, error) {
	f, err := os.Open(alsaProcPCM)
	if err != nil {
		return nil, ErrNoBackend
	}
	defer f.Close()
	var out []alsaDevice
	for _, d := range parseProcPCM(f) {
		if (kind == Capture && d.capture) || (kind == Playback && d.playback) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (alsaBackend) devices(kind DeviceType) ([]DeviceInfo, error) {
	devs, err := listALSA(kind)
	if err != nil {
		return nil, err
	}
	infos := make([]DeviceInfo, 0, len(devs))
	for i, d := range devs {
		infos = append(infos, newDeviceInfo(d.id(), d.name, i == 0, nil))
	}
	return infos, nil
}

func (b alsaBackend) deviceInfo(kind DeviceType, id DeviceID) (DeviceInfo, error) {
	devs, err := listALSA(kind)
	if err != nil {
		return DeviceInfo{}, err
	}
	for i, d := range devs {
		if d.id() != id {
			continue
		}
		pcm, err := openPCM(d.card, d.dev, kind)
		if err != nil {
			return DeviceInfo{}, err
		}
		defer pcm.close()
		caps, err := pcm.capabilities()
		if err != nil {
			return DeviceInfo{}, ErrFailedToOpenBackendDevice
		}
		return newDeviceInfo(id, d.name, i == 0, alsaDataFormats(&caps)), nil
	}
	return DeviceInfo{}, ErrNoDevice
}

// alsaDataFormats lists the supported formats at the channel and rate bounds.
func alsaDataFormats(caps *sndHwParams) []DataFormat {
	_, chi := caps.bounds(paramChannels)
	rlo, rhi := caps.bounds(paramRate)
	var formats []DataFormat
	for _, f := range alsaFormatPreference {
		if !caps.hasMask(paramFormat, alsaFormats[f]) {
			continue
		}
		formats = append(formats, DataFormat{Format: f, Channels: chi, SampleRate: rlo})
		if rhi != rlo {
			formats = append(formats, DataFormat{Format: f, Channels: chi, SampleRate: rhi})
		}
	}
	return formats
}

// resolve returns the card and device for a direction: the given id or the
// first device that supports the direction.
func resolveALSA(kind DeviceType, id *DeviceID) (card, dev int, err error) {
	if id != nil {
		card, dev, ok := parseHwID(id.str())
		if !ok {
			return 0, 0, ErrNoDevice
		}
		return card, dev, nil
	}
	devs, err := listALSA(kind)
	if err != nil {
		return 0, 0, err
	}
	if len(devs) == 0 {
		return 0, 0, ErrNoDevice
	}
	return devs[0].card, devs[0].dev, nil
}

func (alsaBackend) openDevice(p *deviceParams, s *sink) (stream, error) {
	if p.deviceType == Loopback {
		return nil, ErrDeviceTypeNotSupported
	}
	st := &alsaStream{sink: s}
	var pcms []*alsaPCM
	fail := func(err error) (stream, error) {
		for _, pcm := range pcms {
			pcm.close()
		}
		return nil, err
	}
	for _, kind := range []DeviceType{Capture, Playback} {
		if (kind == Capture && !p.hasCapture()) || (kind == Playback && !p.hasPlayback()) {
			continue
		}
		sub := &p.capture
		if kind == Playback {
			sub = &p.playback
		}
		card, dev, err := resolveALSA(kind, sub.id)
		if err != nil {
			return fail(err)
		}
		pcm, err := openPCM(card, dev, kind)
		if err != nil {
			return fail(err)
		}
		pcms = append(pcms, pcm)
		caps, err := pcm.capabilities()
		if err != nil {
			return fail(ErrFailedToOpenBackendDevice)
		}
		if err := chooseFormat(&caps, sub, &p.sampleRate); err != nil {
			return fail(err)
		}
		period, _, err := pcm.configure(sub, p.sampleRate, p.framesPerPeriod(p.sampleRate), p.periods, kind)
		if err != nil {
			return fail(err)
		}
		if kind == Capture {
			st.in = pcm
			st.frames = period // capture paces a duplex device
			st.inBytes = sub.frameBytes()
			st.inBuf = make([]byte, int(period)*st.inBytes)
		} else {
			st.out = pcm
			if st.in == nil {
				st.frames = period
			}
			st.outBytes = sub.frameBytes()
		}
	}
	if st.out != nil {
		st.outBuf = make([]byte, int(st.frames)*st.outBytes)
	}
	return st, nil
}

type alsaStream struct {
	sink     *sink
	in, out  *alsaPCM
	frames   uint32
	inBuf    []byte
	outBuf   []byte
	inBytes  int
	outBytes int
	stopping atomic.Bool
	wg       sync.WaitGroup
}

func (st *alsaStream) start(run uint64) error {
	st.stopping.Store(false)
	if st.in != nil {
		if err := alsaIoctl(st.in.fd, iocStart, nil); err != nil {
			return ErrFailedToStartBackendDevice
		}
	}
	st.wg.Add(1)
	go st.run(run)
	return nil
}

// run moves one period per iteration; blocking reads or writes pace it.
func (st *alsaStream) run(run uint64) {
	defer func() {
		if !st.stopping.Load() {
			go st.sink.fail(run) // device error: report the stop
		}
	}()
	defer st.wg.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for !st.stopping.Load() {
		var in []byte
		if st.in != nil {
			if err := st.in.transfer(iocReadIFrames, st.inBuf, st.frames, st.inBytes, &st.stopping); err != nil {
				break
			}
			in = st.inBuf
		}
		if st.out != nil {
			clear(st.outBuf)
		}
		if st.stopping.Load() {
			break
		}
		st.sink.process(st.outBuf, in, st.frames)
		if st.out != nil {
			if err := st.out.transfer(iocWriteIFrame, st.outBuf, st.frames, st.outBytes, &st.stopping); err != nil {
				break
			}
		}
	}
}

func (st *alsaStream) stop() error {
	st.stopping.Store(true)
	// DROP wakes a transfer blocked in the kernel.
	for _, pcm := range []*alsaPCM{st.in, st.out} {
		if pcm != nil {
			alsaIoctl(pcm.fd, iocDrop, nil) //nolint:errcheck // wake-up only; checked by the DROP below
		}
	}
	st.wg.Wait()
	var err error
	for _, pcm := range []*alsaPCM{st.in, st.out} {
		if pcm != nil {
			if e := alsaIoctl(pcm.fd, iocDrop, nil); e != nil && !errors.Is(e, syscall.EBADFD) {
				err = ErrFailedToStopBackendDevice
			}
			if e := alsaIoctl(pcm.fd, iocPrepare, nil); e != nil {
				err = ErrFailedToStopBackendDevice
			}
		}
	}
	return err
}

func (st *alsaStream) close() {
	for _, pcm := range []*alsaPCM{st.in, st.out} {
		if pcm != nil {
			pcm.close()
		}
	}
}
