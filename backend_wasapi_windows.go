//go:build windows && (amd64 || arm64)

package audiogo

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The WASAPI backend calls the Windows Core Audio COM interfaces through
// their vtables with syscall.SyscallN, so it needs no cgo. Streams run in
// shared mode with AUTOCONVERTPCM, so Windows converts format, channels and
// sample rate to and from the engine mix format.

func init() {
	openers[BackendWasapi] = func(func(string)) (backend, error) {
		if err := startCOM(); err != nil {
			return nil, err
		}
		return wasapiBackend{}, nil
	}
}

type comGUID struct {
	data1        uint32
	data2, data3 uint16
	data4        [8]byte
}

var (
	clsidMMDeviceEnumerator = comGUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = comGUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = comGUID{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = comGUID{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	iidIAudioRenderClient   = comGUID{0xF294ACFC, 0x3146, 0x4483, [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	subtypePCM              = comGUID{0x00000001, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	subtypeFloat            = comGUID{0x00000003, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	pkeyFriendlyName        = propertyKey{comGUID{0xA45C254E, 0xDF1C, 0x4EFD, [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}}, 14}
)

type propertyKey struct {
	fmtid comGUID
	pid   uint32
}

// propVariant mirrors PROPVARIANT on 64-bit Windows.
type propVariant struct {
	vt  uint16
	_   [6]byte
	val unsafe.Pointer
	_   [8]byte
}

// waveFormatExtensible mirrors WAVEFORMATEXTENSIBLE.
type waveFormatExtensible struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	size           uint16
	validBits      uint16
	channelMask    uint32
	subFormat      comGUID
}

// vtable indices.
const (
	mRelease = 2

	mEnumAudioEndpoints      = 3
	mGetDefaultAudioEndpoint = 4
	mGetDevice               = 5

	mCollGetCount = 3
	mCollItem     = 4

	mDevActivate          = 3
	mDevOpenPropertyStore = 4
	mDevGetID             = 5

	mPropGetValue = 5

	mClientInitialize        = 3
	mClientGetBufferSize     = 4
	mClientGetCurrentPadding = 6
	mClientGetMixFormat      = 8
	mClientStart             = 10
	mClientStop              = 11
	mClientReset             = 12
	mClientSetEventHandle    = 13
	mClientGetService        = 14

	mBufGetBuffer         = 3 // IAudioCaptureClient and IAudioRenderClient
	mBufReleaseBuffer     = 4
	mCaptureGetNextPacket = 5
)

const (
	eRender  = 0
	eCapture = 1
	eConsole = 0

	deviceStateActive = 1
	clsctxAll         = 0x17

	streamFlagsLoopback      = 0x00020000
	streamFlagsEventCallback = 0x00040000
	streamFlagsSRCDefault    = 0x08000000
	streamFlagsAutoConvert   = 0x80000000
	bufferFlagsSilent        = 0x2

	waveFormatPCM           = 1
	waveFormatFloat         = 3
	waveFormatTagExtensible = 0xFFFE

	vtLPWSTR   = 31
	waitFailed = 0xFFFFFFFF
	hrNotFound = 0x80070490
	hrChanged  = 0x80010106 // RPC_E_CHANGED_MODE
	hnsPerSec  = 10_000_000
)

var (
	modole32             = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = modole32.NewProc("CoInitializeEx")
	procCoCreateInstance = modole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = modole32.NewProc("CoTaskMemFree")
	procPropVariantClear = modole32.NewProc("PropVariantClear")
	modkernel32          = syscall.NewLazyDLL("kernel32.dll")
	procCreateEventW     = modkernel32.NewProc("CreateEventW")
	procWaitForMultiple  = modkernel32.NewProc("WaitForMultipleObjects")
)

func failed(hr uintptr) bool { return uint32(hr)&0x80000000 != 0 }

var (
	comOnce sync.Once
	comErr  error
)

// startCOM keeps one thread in the multithreaded apartment for the life of
// the process, which makes every other thread an implicit MTA member.
func startCOM() error {
	comOnce.Do(func() {
		done := make(chan error)
		go func() {
			runtime.LockOSThread()
			hr, _, _ := procCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
			if failed(hr) && uint32(hr) != hrChanged {
				done <- ErrFailedToInitBackend
				return
			}
			done <- nil
			select {} // park the MTA thread forever
		}()
		comErr = <-done
	})
	return comErr
}

func vtbl(obj unsafe.Pointer, i int) uintptr {
	return (*(**[32]uintptr)(obj))[i]
}

// hr returns the HRESULT of a syscall.SyscallN call. Method calls are written
// as hr(syscall.SyscallN(vtbl(obj, m), uintptr(obj), ...)) so that every
// uintptr(unsafe.Pointer(&x)) argument is converted in the SyscallN call
// itself, where the compiler keeps x alive and in place.
func hr(r, _ uintptr, _ syscall.Errno) uintptr { return r }

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		hr(syscall.SyscallN(vtbl(obj, mRelease), uintptr(obj)))
	}
}

func coTaskMemFree(p unsafe.Pointer) {
	if p != nil {
		procCoTaskMemFree.Call(uintptr(p))
	}
}

func newEnumerator() (unsafe.Pointer, error) {
	var enum unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if failed(hr) {
		return nil, ErrFailedToInitBackend
	}
	return enum, nil
}

func dataFlow(kind DeviceType) uintptr {
	if kind == Capture {
		return eCapture
	}
	return eRender
}

func endpointID(dev unsafe.Pointer) (string, error) {
	var p *uint16
	if failed(hr(syscall.SyscallN(vtbl(dev, mDevGetID), uintptr(dev), uintptr(unsafe.Pointer(&p))))) {
		return "", ErrNoDevice
	}
	defer coTaskMemFree(unsafe.Pointer(p))
	return utf16PtrToString(p), nil
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

func friendlyName(dev unsafe.Pointer) string {
	var store unsafe.Pointer
	if failed(hr(syscall.SyscallN(vtbl(dev, mDevOpenPropertyStore), uintptr(dev), 0, uintptr(unsafe.Pointer(&store))))) {
		return ""
	}
	defer comRelease(store)
	var pv propVariant
	if failed(hr(syscall.SyscallN(vtbl(store, mPropGetValue), uintptr(store), uintptr(unsafe.Pointer(&pkeyFriendlyName)), uintptr(unsafe.Pointer(&pv))))) {
		return ""
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	if pv.vt != vtLPWSTR {
		return ""
	}
	return utf16PtrToString((*uint16)(pv.val))
}

func activateClient(dev unsafe.Pointer) (unsafe.Pointer, error) {
	var client unsafe.Pointer
	if failed(hr(syscall.SyscallN(vtbl(dev, mDevActivate), uintptr(dev), uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))))) {
		return nil, ErrFailedToOpenBackendDevice
	}
	return client, nil
}

// mixFormat returns the shared-mode engine format of a client.
func mixFormat(client unsafe.Pointer) (DataFormat, error) {
	var wf *waveFormatExtensible
	if failed(hr(syscall.SyscallN(vtbl(client, mClientGetMixFormat), uintptr(client), uintptr(unsafe.Pointer(&wf))))) {
		return DataFormat{}, ErrFailedToOpenBackendDevice
	}
	defer coTaskMemFree(unsafe.Pointer(wf))
	return formatFromWave(wf), nil
}

func formatFromWave(wf *waveFormatExtensible) DataFormat {
	isFloat := wf.formatTag == waveFormatFloat || (wf.formatTag == waveFormatTagExtensible && wf.subFormat == subtypeFloat)
	f := DataFormat{Channels: uint32(wf.channels), SampleRate: wf.samplesPerSec}
	switch {
	case isFloat && wf.bitsPerSample == 32:
		f.Format = FormatF32
	case wf.bitsPerSample == 8:
		f.Format = FormatU8
	case wf.bitsPerSample == 16:
		f.Format = FormatS16
	case wf.bitsPerSample == 24:
		f.Format = FormatS24
	case wf.bitsPerSample == 32:
		f.Format = FormatS32
	}
	return f
}

func waveFormat(format FormatType, channels, rate uint32) waveFormatExtensible {
	bytes := uint16(SampleSizeInBytes(format))
	wf := waveFormatExtensible{
		formatTag:      waveFormatTagExtensible,
		channels:       uint16(channels),
		samplesPerSec:  rate,
		avgBytesPerSec: rate * uint32(bytes) * channels,
		blockAlign:     bytes * uint16(channels),
		bitsPerSample:  bytes * 8,
		size:           22,
		validBits:      bytes * 8,
		subFormat:      subtypePCM,
	}
	if format == FormatF32 {
		wf.subFormat = subtypeFloat
	}
	switch channels {
	case 1:
		wf.channelMask = 0x4 // front center
	case 2:
		wf.channelMask = 0x3 // front left, front right
	}
	return wf
}

type wasapiBackend struct{}

func (wasapiBackend) uninit() error { return nil }

// withEndpoints calls fn for every active endpoint of kind; dev is released after fn.
func withEndpoints(kind DeviceType, fn func(dev unsafe.Pointer, id string, isDefault bool)) error {
	enum, err := newEnumerator()
	if err != nil {
		return err
	}
	defer comRelease(enum)
	var defID string
	var def unsafe.Pointer
	if !failed(hr(syscall.SyscallN(vtbl(enum, mGetDefaultAudioEndpoint), uintptr(enum), dataFlow(kind), eConsole, uintptr(unsafe.Pointer(&def))))) {
		defID, _ = endpointID(def)
		comRelease(def)
	}
	var coll unsafe.Pointer
	if failed(hr(syscall.SyscallN(vtbl(enum, mEnumAudioEndpoints), uintptr(enum), dataFlow(kind), deviceStateActive, uintptr(unsafe.Pointer(&coll))))) {
		return ErrFailedToInitBackend
	}
	defer comRelease(coll)
	var n uint32
	if failed(hr(syscall.SyscallN(vtbl(coll, mCollGetCount), uintptr(coll), uintptr(unsafe.Pointer(&n))))) {
		return ErrFailedToInitBackend
	}
	for i := range n {
		var dev unsafe.Pointer
		if failed(hr(syscall.SyscallN(vtbl(coll, mCollItem), uintptr(coll), uintptr(i), uintptr(unsafe.Pointer(&dev))))) {
			continue
		}
		if id, err := endpointID(dev); err == nil {
			fn(dev, id, id == defID)
		}
		comRelease(dev)
	}
	return nil
}

func endpointInfo(dev unsafe.Pointer, id string, isDefault bool) DeviceInfo {
	var formats []DataFormat
	if client, err := activateClient(dev); err == nil {
		if f, err := mixFormat(client); err == nil {
			formats = []DataFormat{f}
		}
		comRelease(client)
	}
	return newDeviceInfo(deviceIDString(id), friendlyName(dev), isDefault, formats)
}

func (wasapiBackend) devices(kind DeviceType) ([]DeviceInfo, error) {
	var infos []DeviceInfo
	err := withEndpoints(kind, func(dev unsafe.Pointer, id string, isDefault bool) {
		infos = append(infos, endpointInfo(dev, id, isDefault))
	})
	return infos, err
}

func (wasapiBackend) deviceInfo(kind DeviceType, id DeviceID) (DeviceInfo, error) {
	var info DeviceInfo
	found := false
	err := withEndpoints(kind, func(dev unsafe.Pointer, eid string, isDefault bool) {
		if deviceIDString(eid) == id {
			info, found = endpointInfo(dev, eid, isDefault), true
		}
	})
	if err == nil && !found {
		err = ErrNoDevice
	}
	return info, err
}

// openEndpoint returns the endpoint with the given id, or the default one.
func openEndpoint(kind DeviceType, id *DeviceID) (unsafe.Pointer, error) {
	enum, err := newEnumerator()
	if err != nil {
		return nil, err
	}
	defer comRelease(enum)
	var dev unsafe.Pointer
	var res uintptr
	if id == nil {
		res = hr(syscall.SyscallN(vtbl(enum, mGetDefaultAudioEndpoint), uintptr(enum), dataFlow(kind), eConsole, uintptr(unsafe.Pointer(&dev))))
	} else {
		w, err := syscall.UTF16PtrFromString(id.str())
		if err != nil {
			return nil, ErrNoDevice
		}
		res = hr(syscall.SyscallN(vtbl(enum, mGetDevice), uintptr(enum), uintptr(unsafe.Pointer(w)), uintptr(unsafe.Pointer(&dev))))
		runtime.KeepAlive(w)
	}
	if failed(res) {
		return nil, ErrNoDevice
	}
	return dev, nil
}

func (wasapiBackend) openDevice(p *deviceParams, s *sink) (stream, error) {
	st := &wasapiStream{sink: s}
	for _, kind := range []DeviceType{Capture, Playback} {
		if (kind == Capture && !p.hasCapture()) || (kind == Playback && !p.hasPlayback()) {
			continue
		}
		sub := &p.capture
		if kind == Playback {
			sub = &p.playback
		}
		if sub.shareMode == Exclusive {
			st.close()
			return nil, ErrShareModeNotSupported
		}
		// Loopback captures what a render endpoint plays.
		endpoint, loopback := kind, kind == Capture && p.deviceType == Loopback
		if loopback {
			endpoint = Playback
		}
		c, err := newWasapiClient(p, sub, endpoint, kind, loopback)
		if err != nil {
			st.close()
			return nil, err
		}
		if kind == Capture {
			st.in = c
		} else {
			st.out = c
		}
	}
	if st.in != nil && st.out != nil {
		st.duplexIn = make([]byte, int(st.out.bufferFrames)*st.in.frameBytes)
	}
	return st, nil
}

type wasapiClient struct {
	client       unsafe.Pointer
	service      unsafe.Pointer // IAudioCaptureClient or IAudioRenderClient
	event        uintptr
	bufferFrames uint32
	frameBytes   int
}

func newWasapiClient(p *deviceParams, sub *subParams, endpoint, kind DeviceType, loopback bool) (*wasapiClient, error) {
	dev, err := openEndpoint(endpoint, sub.id)
	if err != nil {
		return nil, err
	}
	defer comRelease(dev)
	c := &wasapiClient{}
	if c.client, err = activateClient(dev); err != nil {
		return nil, err
	}
	mix, err := mixFormat(c.client)
	if err != nil {
		c.release()
		return nil, err
	}
	sub.internalFormat, sub.internalChannels, sub.internalSampleRate = mix.Format, mix.Channels, mix.SampleRate
	sub.fillDefaults(mix.Format, mix.Channels)
	if sub.format == FormatUnknown {
		sub.format = FormatF32
	}
	if p.sampleRate == 0 {
		p.sampleRate = mix.SampleRate
	}
	c.frameBytes = sub.frameBytes()
	wf := waveFormat(sub.format, sub.channels, p.sampleRate)
	flags := uintptr(streamFlagsEventCallback | streamFlagsAutoConvert | streamFlagsSRCDefault)
	if loopback {
		flags |= streamFlagsLoopback
	}
	period := uint64(p.framesPerPeriod(p.sampleRate)) * hnsPerSec / uint64(p.sampleRate)
	duration := period * uint64(max(p.periods, 2))
	if failed(hr(syscall.SyscallN(vtbl(c.client, mClientInitialize), uintptr(c.client), 0, flags, uintptr(duration), 0, uintptr(unsafe.Pointer(&wf)), 0))) {
		c.release()
		return nil, ErrFormatNotSupported
	}
	if failed(hr(syscall.SyscallN(vtbl(c.client, mClientGetBufferSize), uintptr(c.client), uintptr(unsafe.Pointer(&c.bufferFrames))))) {
		c.release()
		return nil, ErrFailedToOpenBackendDevice
	}
	c.event, _, _ = procCreateEventW.Call(0, 0, 0, 0)
	if c.event == 0 {
		c.release()
		return nil, ErrOutOfMemory
	}
	if failed(hr(syscall.SyscallN(vtbl(c.client, mClientSetEventHandle), uintptr(c.client), c.event))) {
		c.release()
		return nil, ErrFailedToOpenBackendDevice
	}
	iid := &iidIAudioRenderClient
	if kind == Capture {
		iid = &iidIAudioCaptureClient
	}
	if failed(hr(syscall.SyscallN(vtbl(c.client, mClientGetService), uintptr(c.client), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&c.service))))) {
		c.release()
		return nil, ErrFailedToOpenBackendDevice
	}
	return c, nil
}

func (c *wasapiClient) release() {
	if c == nil {
		return
	}
	comRelease(c.service)
	comRelease(c.client)
	c.service, c.client = nil, nil
	if c.event != 0 {
		syscall.CloseHandle(syscall.Handle(c.event)) //nolint:errcheck // best-effort cleanup
		c.event = 0
	}
}

type wasapiStream struct {
	sink     *sink
	in, out  *wasapiClient
	fifo     []byte // duplex: captured bytes waiting for the playback side
	duplexIn []byte
	stopping atomic.Bool
	wg       sync.WaitGroup
}

func (st *wasapiStream) start(run uint64) error {
	st.stopping.Store(false)
	st.fifo = st.fifo[:0]
	if st.out != nil && !st.render() { // pre-fill so playback starts with data
		return ErrFailedToStartBackendDevice
	}
	for _, c := range []*wasapiClient{st.in, st.out} {
		if c != nil && failed(hr(syscall.SyscallN(vtbl(c.client, mClientStart), uintptr(c.client)))) {
			st.stopClients() //nolint:errcheck // already failing; the start error wins
			return ErrFailedToStartBackendDevice
		}
	}
	st.wg.Add(1)
	go st.run(run)
	return nil
}

func (st *wasapiStream) run(run uint64) {
	defer func() {
		if !st.stopping.Load() {
			go st.sink.fail(run) // device error: report the stop
		}
	}()
	defer st.wg.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var events []uintptr
	for _, c := range []*wasapiClient{st.in, st.out} {
		if c != nil {
			events = append(events, c.event)
		}
	}
	for !st.stopping.Load() {
		// On a timeout the buffers are polled anyway: loopback clients may not
		// signal their event while nothing is playing.
		r, _, _ := procWaitForMultiple.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), 0, 50)
		if r == waitFailed {
			break
		}
		if st.stopping.Load() {
			break
		}
		if st.in != nil && !st.capture() {
			break
		}
		if st.out != nil && !st.render() {
			break
		}
	}
}

// capture drains all available packets.
func (st *wasapiStream) capture() bool {
	c := st.in
	for {
		var packet uint32
		if failed(hr(syscall.SyscallN(vtbl(c.service, mCaptureGetNextPacket), uintptr(c.service), uintptr(unsafe.Pointer(&packet))))) {
			return false
		}
		if packet == 0 {
			return true
		}
		var data *byte
		var frames, flags uint32
		if failed(hr(syscall.SyscallN(vtbl(c.service, mBufGetBuffer), uintptr(c.service), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0))) {
			return false
		}
		buf := unsafe.Slice(data, int(frames)*c.frameBytes)
		if flags&bufferFlagsSilent != 0 {
			clear(buf)
		}
		if st.out != nil {
			st.fifo = append(st.fifo, buf...)
			if over := len(st.fifo) - 8*int(c.bufferFrames)*c.frameBytes; over > 0 {
				st.fifo = append(st.fifo[:0], st.fifo[over:]...)
			}
		} else if frames > 0 {
			st.sink.process(nil, buf, frames)
		}
		if failed(hr(syscall.SyscallN(vtbl(c.service, mBufReleaseBuffer), uintptr(c.service), uintptr(frames)))) {
			return false
		}
	}
}

// render fills the free part of the playback buffer.
func (st *wasapiStream) render() bool {
	c := st.out
	var padding uint32
	if failed(hr(syscall.SyscallN(vtbl(c.client, mClientGetCurrentPadding), uintptr(c.client), uintptr(unsafe.Pointer(&padding))))) {
		return false
	}
	frames := c.bufferFrames - padding
	if frames == 0 {
		return true
	}
	var data *byte
	if failed(hr(syscall.SyscallN(vtbl(c.service, mBufGetBuffer), uintptr(c.service), uintptr(frames), uintptr(unsafe.Pointer(&data))))) {
		return false
	}
	out := unsafe.Slice(data, int(frames)*c.frameBytes)
	clear(out)
	var in []byte
	if st.in != nil {
		in = st.duplexIn[:int(frames)*st.in.frameBytes]
		clear(in)
		k := copy(in, st.fifo)
		st.fifo = append(st.fifo[:0], st.fifo[k:]...)
	}
	st.sink.process(out, in, frames)
	return !failed(hr(syscall.SyscallN(vtbl(c.service, mBufReleaseBuffer), uintptr(c.service), uintptr(frames), 0)))
}

func (st *wasapiStream) stopClients() error {
	var err error
	for _, c := range []*wasapiClient{st.in, st.out} {
		if c != nil {
			if failed(hr(syscall.SyscallN(vtbl(c.client, mClientStop), uintptr(c.client)))) {
				err = ErrFailedToStopBackendDevice
			}
			hr(syscall.SyscallN(vtbl(c.client, mClientReset), uintptr(c.client)))
		}
	}
	return err
}

func (st *wasapiStream) stop() error {
	st.stopping.Store(true)
	st.wg.Wait()
	return st.stopClients()
}

func (st *wasapiStream) close() {
	st.in.release()
	st.out.release()
	st.in, st.out = nil, nil
}
