//go:build darwin

package audiogo

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

func init() {
	openers[BackendCoreaudio] = func(log func(string)) (backend, error) {
		return coreAudio{log: log}, nil
	}
}

func fourCC(s string) uint32 { return binary.BigEndian.Uint32([]byte(s)) }

var (
	selDevices        = fourCC("dev#")
	selDefaultInput   = fourCC("dIn ")
	selDefaultOutput  = fourCC("dOut")
	selName           = fourCC("lnam")
	selUID            = fourCC("uid ")
	selStreamConfig   = fourCC("slay")
	selNominalRate    = fourCC("nsrt")
	selAvailableRates = fourCC("nsr#")
	scopeGlobal       = fourCC("glob")
	scopeInput        = fourCC("inpt")
	scopeOutput       = fourCC("outp")
	queueCurrentDev   = fourCC("aqcd")
	formatLinearPCM   = fourCC("lpcm")
	errUnsupportedFmt = int32(fourCC("fmt?"))
)

const (
	systemObject     = 1
	formatFlagFloat  = 1 << 0
	formatFlagSigned = 1 << 2
	formatFlagPacked = 1 << 3
	coreAudioFormat  = FormatF32
)

// coreAudio is the macOS backend. Devices are enumerated with the CoreAudio
// HAL, streams use Audio Queues, which convert format, channels and sample
// rate to and from the hardware.
type coreAudio struct {
	log func(string)
}

func (coreAudio) uninit() error { return nil }

func scopeOf(kind DeviceType) uint32 {
	if kind == Capture {
		return scopeInput
	}
	return scopeOutput
}

func getProperty(obj, sel, scope uint32, data unsafe.Pointer, size uint32) error {
	addr := propertyAddress{selector: sel, scope: scope}
	if st := audioObjectGetPropertyData(obj, &addr, 0, nil, &size, data); st != 0 {
		return fmt.Errorf("core audio: get property %q: OSStatus %d", binary.BigEndian.AppendUint32(nil, sel), st)
	}
	return nil
}

// getPropertyBytes reads a variable sized property.
func getPropertyBytes(obj, sel, scope uint32) ([]byte, error) {
	addr := propertyAddress{selector: sel, scope: scope}
	var size uint32
	if st := audioObjectGetPropertyDataSize(obj, &addr, &size); st != 0 {
		return nil, fmt.Errorf("core audio: property size %q: OSStatus %d", binary.BigEndian.AppendUint32(nil, sel), st)
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]uint64, (size+7)/8) // 8-byte aligned
	b := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), size)
	if st := audioObjectGetPropertyData(obj, &addr, 0, nil, &size, unsafe.Pointer(&buf[0])); st != 0 {
		return nil, fmt.Errorf("core audio: property %q: OSStatus %d", binary.BigEndian.AppendUint32(nil, sel), st)
	}
	return b[:size], nil
}

func stringProperty(obj, sel uint32) (string, error) {
	var ref uintptr
	if err := getProperty(obj, sel, scopeGlobal, unsafe.Pointer(&ref), uint32(unsafe.Sizeof(ref))); err != nil {
		return "", err
	}
	defer cfRelease(ref)
	return goStringFromCF(ref), nil
}

func deviceIDs() ([]uint32, error) {
	b, err := getPropertyBytes(systemObject, selDevices, scopeGlobal)
	if err != nil {
		return nil, err
	}
	ids := make([]uint32, len(b)/4)
	for i := range ids {
		ids[i] = binary.NativeEndian.Uint32(b[i*4:])
	}
	return ids, nil
}

func defaultDevice(kind DeviceType) (uint32, error) {
	sel := selDefaultOutput
	if kind == Capture {
		sel = selDefaultInput
	}
	var dev uint32
	if err := getProperty(systemObject, sel, scopeGlobal, unsafe.Pointer(&dev), 4); err != nil {
		return 0, err
	}
	if dev == 0 {
		return 0, ErrNoDevice
	}
	return dev, nil
}

// channelCount sums the channels of the device streams in the scope.
func channelCount(dev, scope uint32) uint32 {
	b, err := getPropertyBytes(dev, selStreamConfig, scope)
	if err != nil {
		return 0
	}
	return parseBufferListChannels(b)
}

// parseBufferListChannels reads an AudioBufferList: UInt32 count, then
// 16-byte AudioBuffers (channels, byte size, data pointer) at offset 8.
func parseBufferListChannels(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	var channels uint32
	n := binary.NativeEndian.Uint32(b)
	for i := range n {
		off := 8 + int(i)*16
		if off+4 > len(b) {
			break
		}
		channels += binary.NativeEndian.Uint32(b[off:])
	}
	return channels
}

func nominalRate(dev uint32) uint32 {
	var rate float64
	if getProperty(dev, selNominalRate, scopeGlobal, unsafe.Pointer(&rate), 8) != nil {
		return 0
	}
	return uint32(rate)
}

func availableRates(dev uint32) []uint32 {
	b, err := getPropertyBytes(dev, selAvailableRates, scopeGlobal)
	if err != nil || len(b) < 16 {
		return nil
	}
	ranges := unsafe.Slice((*audioValueRange)(unsafe.Pointer(&b[0])), len(b)/16)
	return ratesFromRanges(ranges)
}

func ratesFromRanges(ranges []audioValueRange) []uint32 {
	var rates []uint32
	add := func(r uint32) {
		for _, x := range rates {
			if x == r {
				return
			}
		}
		rates = append(rates, r)
	}
	for _, r := range ranges {
		add(uint32(r.min))
		add(uint32(r.max))
	}
	return rates
}

// deviceInfoOf returns the info of a HAL device, or ok=false when it has no
// channels in the direction.
func deviceInfoOf(dev uint32, kind DeviceType, def uint32) (DeviceInfo, bool) {
	channels := channelCount(dev, scopeOf(kind))
	if channels == 0 {
		return DeviceInfo{}, false
	}
	uid, err := stringProperty(dev, selUID)
	if err != nil || uid == "" {
		return DeviceInfo{}, false
	}
	name, err := stringProperty(dev, selName)
	if err != nil {
		name = uid
	}
	rates := availableRates(dev)
	if len(rates) == 0 {
		if r := nominalRate(dev); r != 0 {
			rates = []uint32{r}
		}
	}
	formats := make([]DataFormat, 0, len(rates))
	for _, r := range rates {
		formats = append(formats, DataFormat{Format: coreAudioFormat, Channels: channels, SampleRate: r})
	}
	return newDeviceInfo(deviceIDString(uid), name, dev == def, formats), true
}

func (coreAudio) devices(kind DeviceType) ([]DeviceInfo, error) {
	ids, err := deviceIDs()
	if err != nil {
		return nil, err
	}
	def, err := defaultDevice(kind)
	if err != nil && err != ErrNoDevice {
		return nil, err
	}
	var infos []DeviceInfo
	for _, dev := range ids {
		if info, ok := deviceInfoOf(dev, kind, def); ok {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

func (c coreAudio) deviceInfo(kind DeviceType, id DeviceID) (DeviceInfo, error) {
	dev, err := c.lookup(kind, &id)
	if err != nil {
		return DeviceInfo{}, err
	}
	def, err := defaultDevice(kind)
	if err != nil && err != ErrNoDevice {
		return DeviceInfo{}, err
	}
	info, ok := deviceInfoOf(dev, kind, def)
	if !ok {
		return DeviceInfo{}, ErrNoDevice
	}
	return info, nil
}

// lookup returns the HAL device with the UID stored in id, or the default
// device for a nil id.
func (coreAudio) lookup(kind DeviceType, id *DeviceID) (uint32, error) {
	if id == nil {
		return defaultDevice(kind)
	}
	ids, err := deviceIDs()
	if err != nil {
		return 0, err
	}
	want := id.str()
	for _, dev := range ids {
		if uid, err := stringProperty(dev, selUID); err == nil && uid == want && channelCount(dev, scopeOf(kind)) > 0 {
			return dev, nil
		}
	}
	return 0, ErrNoDevice
}

// streamFormat describes a packed linear PCM format for an Audio Queue.
func streamFormat(format FormatType, channels, rate uint32) (audioStreamBasicDescription, error) {
	size := uint32(SampleSizeInBytes(format))
	var flags uint32 = formatFlagPacked
	switch format {
	case FormatU8:
	case FormatS16, FormatS24, FormatS32:
		flags |= formatFlagSigned
	case FormatF32:
		flags |= formatFlagFloat
	default:
		return audioStreamBasicDescription{}, ErrFormatNotSupported
	}
	return audioStreamBasicDescription{
		sampleRate:       float64(rate),
		formatID:         formatLinearPCM,
		formatFlags:      flags,
		bytesPerPacket:   size * channels,
		framesPerPacket:  1,
		bytesPerFrame:    size * channels,
		channelsPerFrame: channels,
		bitsPerChannel:   size * 8,
	}, nil
}

func (c coreAudio) openDevice(p *deviceParams, s *sink) (stream, error) {
	if p.deviceType == Loopback {
		return nil, ErrDeviceTypeNotSupported
	}
	st := &caStream{sink: s, params: p, log: c.log}
	for _, kind := range []DeviceType{Capture, Playback} {
		if (kind == Capture && !p.hasCapture()) || (kind == Playback && !p.hasPlayback()) {
			continue
		}
		sub := &p.capture
		if kind == Playback {
			sub = &p.playback
		}
		dev, err := c.lookup(kind, sub.id)
		if err != nil {
			return nil, err
		}
		sub.internalFormat = coreAudioFormat
		sub.internalChannels = channelCount(dev, scopeOf(kind))
		sub.internalSampleRate = nominalRate(dev)
		sub.fillDefaults(coreAudioFormat, sub.internalChannels)
		if p.sampleRate == 0 {
			p.sampleRate = sub.internalSampleRate
		}
	}
	if p.sampleRate == 0 {
		p.sampleRate = 48000
	}
	for _, sub := range []*subParams{&p.capture, &p.playback} {
		if sub.channels > 0 {
			if _, err := streamFormat(sub.format, sub.channels, p.sampleRate); err != nil {
				return nil, err
			}
		}
	}
	st.sem = dispatchSemaphoreCreate()
	if st.sem == 0 {
		return nil, ErrOutOfMemory
	}
	// Create the queues now so that configuration errors surface in InitDevice.
	if err := st.build(); err != nil {
		st.close()
		return nil, err
	}
	return st, nil
}

// audioQueue is one Audio Queue with its buffers.
type audioQueue struct {
	aq         uintptr
	ring       *queueRing
	buffers    []uintptr
	tail       uint32
	frames     uint32
	frameBytes int
}

// ready calls fn for every buffer the queue handed back since the last call.
func (q *audioQueue) ready(fn func(buf uintptr)) {
	for head := atomic.LoadUint32(&q.ring.head); q.tail != head; q.tail++ {
		fn(q.ring.bufs[q.tail%queueRingSize])
	}
}

func (q *audioQueue) dispose() {
	if q != nil && q.aq != 0 {
		audioQueueDispose(q.aq)
		q.aq = 0
	}
}

func bufferOf(buf uintptr) *audioQueueBuffer { return (*audioQueueBuffer)(cptr(buf)) }

type caStream struct {
	sink   *sink
	params *deviceParams
	log    func(string)
	sem    uintptr

	in, out  *audioQueue
	fifo     []byte // duplex: captured bytes waiting for the playback side
	duplexIn []byte // duplex: input handed to the data callback
	stopping atomic.Bool
	wg       sync.WaitGroup
}

func (st *caStream) newQueue(kind DeviceType, sub *subParams) (*audioQueue, error) {
	format, err := streamFormat(sub.format, sub.channels, st.params.sampleRate)
	if err != nil {
		return nil, err
	}
	q := &audioQueue{ring: &queueRing{sem: st.sem}, frameBytes: sub.frameBytes(), frames: st.params.framesPerPeriod(st.params.sampleRate)}
	var status int32
	if kind == Capture {
		status = audioQueueNewInput(&format, q.ring, &q.aq)
	} else {
		status = audioQueueNewOutput(&format, q.ring, &q.aq)
	}
	if status != 0 {
		st.log(fmt.Sprintf("[Core Audio] AudioQueueNew failed: OSStatus %d", status))
		if status == errUnsupportedFmt {
			return nil, ErrFormatNotSupported
		}
		return nil, ErrFailedToOpenBackendDevice
	}
	if sub.id != nil {
		uid := cfString(sub.id.str())
		status = audioQueueSetProperty(q.aq, queueCurrentDev, unsafe.Pointer(&uid), uint32(unsafe.Sizeof(uid)))
		cfRelease(uid)
		if status != 0 {
			q.dispose()
			st.log(fmt.Sprintf("[Core Audio] select device: OSStatus %d", status))
			return nil, ErrFailedToOpenBackendDevice
		}
	}
	for range min(max(int(st.params.periods), 2), maxQueueBuffers) {
		var buf uintptr
		if status := audioQueueAllocateBuffer(q.aq, q.frames*uint32(q.frameBytes), &buf); status != 0 {
			q.dispose()
			return nil, ErrOutOfMemory
		}
		q.buffers = append(q.buffers, buf)
	}
	return q, nil
}

func (st *caStream) build() error {
	var err error
	p := st.params
	if p.hasCapture() {
		if st.in, err = st.newQueue(Capture, &p.capture); err != nil {
			return err
		}
	}
	if p.hasPlayback() {
		if st.out, err = st.newQueue(Playback, &p.playback); err != nil {
			st.in.dispose()
			st.in = nil
			return err
		}
	}
	if st.in != nil && st.out != nil {
		st.duplexIn = make([]byte, int(st.out.frames)*st.in.frameBytes)
	}
	return nil
}

func (st *caStream) start(uint64) error {
	if st.in == nil && st.out == nil {
		if err := st.build(); err != nil {
			return err
		}
	}
	st.stopping.Store(false)
	st.fifo = st.fifo[:0]
	if st.in != nil {
		for _, buf := range st.in.buffers {
			if status := audioQueueEnqueueBuffer(st.in.aq, buf); status != 0 {
				return st.failStart("enqueue capture buffer", status)
			}
		}
	}
	if st.out != nil {
		for _, buf := range st.out.buffers {
			st.fillPlayback(buf)
		}
	}
	for _, q := range []*audioQueue{st.in, st.out} {
		if q != nil {
			if status := audioQueueStart(q.aq); status != 0 {
				return st.failStart("AudioQueueStart", status)
			}
		}
	}
	st.wg.Add(1)
	go st.run()
	return nil
}

func (st *caStream) failStart(what string, status int32) error {
	st.log(fmt.Sprintf("[Core Audio] %s failed: OSStatus %d", what, status))
	st.teardown()
	return ErrFailedToStartBackendDevice
}

// run handles buffers returned by the queues until stop.
func (st *caStream) run() {
	defer st.wg.Done()
	for {
		dispatchSemaphoreWait(st.sem)
		if st.stopping.Load() {
			return
		}
		if st.in != nil {
			st.in.ready(st.handleCapture)
		}
		if st.out != nil {
			st.out.ready(st.fillPlayback)
		}
	}
}

func (st *caStream) handleCapture(buf uintptr) {
	b := bufferOf(buf)
	data := unsafe.Slice((*byte)(b.data), b.size)
	if st.out != nil {
		st.fifo = appendBounded(st.fifo, data, 8*int(st.in.frames)*st.in.frameBytes)
	} else if n := len(data) / st.in.frameBytes; n > 0 {
		st.sink.process(nil, data, uint32(n))
	}
	audioQueueEnqueueBuffer(st.in.aq, buf)
}

// appendBounded appends data and drops the oldest bytes beyond limit.
func appendBounded(fifo, data []byte, limit int) []byte {
	fifo = append(fifo, data...)
	if over := len(fifo) - limit; over > 0 {
		fifo = append(fifo[:0], fifo[over:]...)
	}
	return fifo
}

func (st *caStream) fillPlayback(buf uintptr) {
	q := st.out
	b := bufferOf(buf)
	n := int(q.frames) * q.frameBytes
	out := unsafe.Slice((*byte)(b.data), n)
	clear(out)
	var in []byte
	if st.in != nil {
		in = st.duplexIn[:int(q.frames)*st.in.frameBytes]
		clear(in)
		k := copy(in, st.fifo)
		st.fifo = append(st.fifo[:0], st.fifo[k:]...)
	}
	st.sink.process(out, in, q.frames)
	b.size = uint32(n)
	audioQueueEnqueueBuffer(q.aq, buf)
}

func (st *caStream) stop() error {
	if st.in == nil && st.out == nil {
		return nil
	}
	st.stopping.Store(true)
	dispatchSemaphoreSignal(st.sem)
	st.wg.Wait()
	var err error
	for _, q := range []*audioQueue{st.in, st.out} {
		if q != nil {
			if status := audioQueueStop(q.aq); status != 0 {
				err = ErrFailedToStopBackendDevice
			}
		}
	}
	st.teardown()
	return err
}

// teardown disposes the queues; start builds new ones.
func (st *caStream) teardown() {
	st.in.dispose()
	st.out.dispose()
	st.in, st.out = nil, nil
}

func (st *caStream) close() {
	st.teardown()
	if st.sem != 0 {
		dispatchRelease(st.sem)
		st.sem = 0
	}
}
