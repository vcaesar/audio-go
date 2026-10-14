package audiogo

import "unsafe"

// DeviceConfig type. Backend specific sections (Wasapi, Alsa, Pulse, AAudio)
// and Resampling are kept for malgo compatibility and are not used by the
// native Go backends.
type DeviceConfig struct {
	DeviceType                DeviceType
	SampleRate                uint32
	PeriodSizeInFrames        uint32
	PeriodSizeInMilliseconds  uint32
	Periods                   uint32
	PerformanceProfile        PerformanceProfile
	NoPreSilencedOutputBuffer uint32
	NoClip                    uint32
	NoDisableDenormals        uint32
	NoFixedSizedCallback      uint32
	Resampling                ResampleConfig
	Playback                  SubConfig
	Capture                   SubConfig
	Wasapi                    WasapiDeviceConfig
	Alsa                      AlsaDeviceConfig
	Pulse                     PulseDeviceConfig
	AAudio                    AAudioDeviceConfig
	// TODO: Add support for coreaudio, opensl
}

// DefaultDeviceConfig returns a default device config: native format,
// channels and sample rate, 10 ms periods, 3 periods.
func DefaultDeviceConfig(deviceType DeviceType) DeviceConfig {
	return DeviceConfig{
		DeviceType:               deviceType,
		PeriodSizeInMilliseconds: 10,
		Periods:                  3,
		Resampling: ResampleConfig{
			Algorithm: ResampleAlgorithmLinear,
			Linear:    ResampleLinearConfig{LpfOrder: 4},
		},
	}
}

// SubConfig type.
type SubConfig struct {
	// DeviceID points to a DeviceID (see DeviceID.Pointer); nil selects the
	// default device. The identifier is copied on InitDevice.
	DeviceID   unsafe.Pointer
	Format     FormatType
	Channels   uint32
	ChannelMap unsafe.Pointer
	ShareMode  ShareMode

	// Unexposed: channelMixMode, calculateLFEFromSpatialChannels
}

// WasapiDeviceConfig type.
type WasapiDeviceConfig struct {
	NoAutoConvertSRC     uint32
	NoDefaultQualitySRC  uint32
	NoAutoStreamRouting  uint32
	NoHardwareOffloading uint32
}

// AlsaDeviceConfig type.
type AlsaDeviceConfig struct {
	NoMMap         uint32
	NoAutoFormat   uint32
	NoAutoChannels uint32
	NoAutoResample uint32
}

// PulseDeviceConfig type.
type PulseDeviceConfig struct {
	StreamNamePlayback string
	StreamNameCapture  string
}

// ResampleConfig type.
type ResampleConfig struct {
	Algorithm ResampleAlgorithm
	Linear    ResampleLinearConfig

	// Unexposed: format, channels, sampleRateIn, sampleRateOut, pBackendVTable, pBackendUserData
}

// ResampleLinearConfig type.
type ResampleLinearConfig struct {
	LpfOrder uint32
}

// ResampleSpeexConfig type.
type ResampleSpeexConfig struct {
	Quality int
}

// AAudioDeviceConfig type.
type AAudioDeviceConfig struct {
	Usage                          AAudioUsage
	ContentType                    AAudioContentType
	InputPreset                    AAudioInputPreset
	AllowedCapturePolicy           AAudioAllowedCapturePolicy
	NoAutoStartAfterReroute        uint32
	EnableCompatibilityWorkarounds uint32
}
