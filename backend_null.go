package audiogo

import (
	"sync"
	"time"
)

// Native format of the null backend.
const (
	nullFormat   = FormatF32
	nullChannels = 2
	nullRate     = 48000
)

// nullBackend is a device-less backend driven by a timer. Capture delivers
// silence and playback discards the output, at real-time pace.
type nullBackend struct{}

func (nullBackend) uninit() error { return nil }

func (nullBackend) devices(kind DeviceType) ([]DeviceInfo, error) {
	info, err := nullBackend{}.deviceInfo(kind, nullDeviceID(kind))
	if err != nil {
		return nil, err
	}
	return []DeviceInfo{info}, nil
}

func nullDeviceID(kind DeviceType) DeviceID {
	if kind == Capture {
		return deviceIDString("null:capture")
	}
	return deviceIDString("null:playback")
}

func (nullBackend) deviceInfo(kind DeviceType, id DeviceID) (DeviceInfo, error) {
	if id != nullDeviceID(kind) {
		return DeviceInfo{}, ErrNoDevice
	}
	name := "NULL Playback Device"
	if kind == Capture {
		name = "NULL Capture Device"
	}
	return newDeviceInfo(id, name, true, []DataFormat{{Format: nullFormat, Channels: nullChannels, SampleRate: nullRate}}), nil
}

func (nullBackend) openDevice(p *deviceParams, s *sink) (stream, error) {
	if p.deviceType == Loopback {
		return nil, ErrDeviceTypeNotSupported
	}
	for _, kind := range []DeviceType{Capture, Playback} {
		sub := &p.capture
		if kind == Playback {
			sub = &p.playback
		}
		if (kind == Capture && !p.hasCapture()) || (kind == Playback && !p.hasPlayback()) {
			continue
		}
		if sub.id != nil && *sub.id != nullDeviceID(kind) {
			return nil, ErrNoDevice
		}
		sub.fillDefaults(nullFormat, nullChannels)
		if SampleSizeInBytes(sub.format) == 0 {
			return nil, ErrFormatNotSupported
		}
		sub.internalFormat, sub.internalChannels = sub.format, sub.channels
	}
	if p.sampleRate == 0 {
		p.sampleRate = nullRate
	}
	p.capture.internalSampleRate = p.sampleRate
	p.playback.internalSampleRate = p.sampleRate
	frames := p.framesPerPeriod(p.sampleRate)
	n := &nullStream{sink: s, frames: frames, period: time.Duration(frames) * time.Second / time.Duration(p.sampleRate)}
	if p.hasCapture() {
		n.in = make([]byte, int(frames)*p.capture.frameBytes())
	}
	if p.hasPlayback() {
		n.out = make([]byte, int(frames)*p.playback.frameBytes())
	}
	return n, nil
}

type nullStream struct {
	sink    *sink
	frames  uint32
	period  time.Duration
	in, out []byte
	quit    chan struct{}
	wg      sync.WaitGroup
}

func (n *nullStream) start(uint64) error {
	n.quit = make(chan struct{})
	n.wg.Add(1)
	go n.run(n.quit)
	return nil
}

func (n *nullStream) run(quit chan struct{}) {
	defer n.wg.Done()
	ticker := time.NewTicker(n.period)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
			clear(n.out)
			n.sink.process(n.out, n.in, n.frames)
		}
	}
}

func (n *nullStream) stop() error {
	if n.quit != nil {
		close(n.quit)
		n.wg.Wait()
		n.quit = nil
	}
	return nil
}

func (n *nullStream) close() {}
