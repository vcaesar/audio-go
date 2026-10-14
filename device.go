package audiogo

import "sync"

// DataProc type.
type DataProc func(pOutputSample, pInputSamples []byte, framecount uint32)

// StopProc type.
type StopProc func()

// DeviceCallbacks contains callbacks for one initialized device.
type DeviceCallbacks struct {
	// Data is called for the full duplex IO.
	Data DataProc
	// Stop is called when the device stopped.
	Stop StopProc
}

// Device represents a streaming instance.
type Device struct {
	mu      sync.Mutex
	params  *deviceParams
	sink    *sink
	stream  stream
	run     uint64 // run number of the last successful start
	running bool   // stream.start succeeded and stream.stop is still due
}

// InitDevice initializes a device.
//
// The device ID can be nil, in which case the default device is used. Otherwise, you
// can retrieve the ID by calling Context.Devices() and use the ID from the returned data.
//
// Set device ID to nil to use the default device. Do _not_ rely on the first device ID returned
// by Context.Devices() to be the default device.
//
// The returned instance has to be cleaned up using Uninit().
func InitDevice(context Context, deviceConfig DeviceConfig, deviceCallbacks DeviceCallbacks) (*Device, error) {
	state := context.state
	if state == nil {
		var err error
		if state, err = initContext(nil, nil); err != nil {
			return nil, err
		}
	}
	switch deviceConfig.DeviceType {
	case Playback, Capture, Duplex, Loopback:
	default:
		return nil, ErrInvalidArgs
	}
	for _, f := range []FormatType{deviceConfig.Capture.Format, deviceConfig.Playback.Format} {
		if f != FormatUnknown && SampleSizeInBytes(f) == 0 {
			return nil, ErrFormatNotSupported
		}
	}
	p := newDeviceParams(&deviceConfig)
	s := &sink{data: deviceCallbacks.Data, onStop: deviceCallbacks.Stop}
	st, err := state.backend.openDevice(p, s)
	if err != nil {
		return nil, err
	}
	return &Device{params: p, sink: s, stream: st}, nil
}

// Type returns device type.
func (dev *Device) Type() DeviceType {
	return dev.params.deviceType
}

// PlaybackFormat returns device playback format.
func (dev *Device) PlaybackFormat() FormatType {
	return dev.params.playback.format
}

// CaptureFormat returns device capture format.
func (dev *Device) CaptureFormat() FormatType {
	return dev.params.capture.format
}

// PlaybackChannels returns number of playback channels.
func (dev *Device) PlaybackChannels() uint32 {
	return dev.params.playback.channels
}

// CaptureChannels returns number of playback channels.
func (dev *Device) CaptureChannels() uint32 {
	return dev.params.capture.channels
}

// SampleRate returns sample rate.
func (dev *Device) SampleRate() uint32 {
	return dev.params.sampleRate
}

// CaptureInternalSampleRate returns the sample rate negotiated with the audio
// backend for capture. This may differ from SampleRate() when the backend
// resamples, for example when Core Audio converts from the hardware rate.
func (dev *Device) CaptureInternalSampleRate() uint32 {
	return dev.params.capture.internalSampleRate
}

// PlaybackInternalSampleRate returns the sample rate negotiated with the audio
// backend for playback.
func (dev *Device) PlaybackInternalSampleRate() uint32 {
	return dev.params.playback.internalSampleRate
}

// CaptureInternalFormat returns the sample format negotiated with the audio
// backend for capture. This may differ from CaptureFormat() when the backend
// performs format conversion internally.
func (dev *Device) CaptureInternalFormat() FormatType {
	return dev.params.capture.internalFormat
}

// PlaybackInternalFormat returns the sample format negotiated with the audio
// backend for playback.
func (dev *Device) PlaybackInternalFormat() FormatType {
	return dev.params.playback.internalFormat
}

// CaptureInternalChannels returns the channel count negotiated with the audio
// backend for capture. This may differ from CaptureChannels() when the backend
// performs channel conversion internally.
func (dev *Device) CaptureInternalChannels() uint32 {
	return dev.params.capture.internalChannels
}

// PlaybackInternalChannels returns the channel count negotiated with the audio
// backend for playback.
func (dev *Device) PlaybackInternalChannels() uint32 {
	return dev.params.playback.internalChannels
}

// Start activates the device.
// For playback devices this begins playback. For capture devices it begins recording.
// Starting a started device is a no-op.
func (dev *Device) Start() error {
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.stream == nil {
		return ErrDeviceNotInitialized
	}
	if dev.running {
		if dev.sink.isStarted() {
			return nil
		}
		// The previous run stopped on a backend error; release it first.
		dev.running = false
		if err := dev.stream.stop(); err != nil {
			return err
		}
	}
	run := dev.sink.begin()
	if err := dev.stream.start(run); err != nil {
		dev.sink.finish(run)
		return err
	}
	dev.run, dev.running = run, true
	return nil
}

// IsStarted determines whether or not the device is started.
func (dev *Device) IsStarted() bool {
	return dev.sink.isStarted()
}

// Stop puts the device to sleep, but does not uninitialize it. Use Start() to start it up again.
// No data callback runs after Stop returns. The Stop callback is called
// without internal locks held, so it may call Start.
func (dev *Device) Stop() error {
	dev.mu.Lock()
	if dev.stream == nil {
		dev.mu.Unlock()
		return ErrDeviceNotInitialized
	}
	if !dev.running {
		dev.mu.Unlock()
		return nil
	}
	dev.running = false
	err := dev.stream.stop()
	notify := dev.sink.finish(dev.run)
	dev.mu.Unlock()
	if notify {
		dev.sink.notifyStop()
	}
	return err
}

// Uninit uninitializes a device.
//
// This will explicitly stop the device. You do not need to call Stop() beforehand, but it's
// harmless if you do.
func (dev *Device) Uninit() {
	// A failed stop still leaves the stream released; close it regardless.
	if err := dev.Stop(); err == ErrDeviceNotInitialized {
		return
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.stream != nil {
		dev.stream.close()
		dev.stream = nil
	}
}
