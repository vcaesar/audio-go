## audio-go

A pure Go port of [gen2brain/malgo](https://github.com/gen2brain/malgo)'s API.
No cgo, no purego dll, no miniaudio and no
shared library to build or ship: `CGO_ENABLED=0 go build` produces a binary
that only links what the operating system already has.

| OS                                               | Backend            | How                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------------------------------ | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| macOS (amd64, arm64)                             | `BackendCoreaudio` | Core Audio HAL for devices, Audio Queues for streams. Framework functions are imported with `//go:cgo_import_dynamic` and called through `syscall.syscall9` (the path `golang.org/x/sys/unix` uses). The Audio Queue callback is ~10 lines of assembly (`sys_darwin_*.s`) that queues the buffer and signals a dispatch semaphore, so no C thread ever enters the Go runtime. |
| Linux (amd64, arm64, riscv64, loong64, 386, arm) | `BackendAlsa`      | Kernel ioctls on `/dev/snd/pcmC*D*` (no libasound).                                                                                                                                                                                                                                                                                                                           |
| Windows (amd64, arm64)                           | `BackendWasapi`    | WASAPI shared mode through COM vtables and `syscall.SyscallN`.                                                                                                                                                                                                                                                                                                                |
| all                                              | `BackendNull`      | Timer driven: capture delivers silence, playback discards.                                                                                                                                                                                                                                                                                                                    |

`InitContext(nil, ...)` picks the platform backend and falls back to null.

### Usage

The API is malgo's:

```go
ctx, err := audiogo.InitContext(nil, audiogo.ContextConfig{}, nil)
if err != nil { ... }
defer func() { _ = ctx.Uninit(); ctx.Free() }()

cfg := audiogo.DefaultDeviceConfig(audiogo.Capture)
cfg.Capture.Format = audiogo.FormatS16
cfg.Capture.Channels = 1
cfg.SampleRate = 48000

dev, err := audiogo.InitDevice(ctx.Context, cfg, audiogo.DeviceCallbacks{
	Data: func(out, in []byte, frames uint32) { /* consume in */ },
})
if err != nil { ... }
defer dev.Uninit()
err = dev.Start()
```

Zero format, channels or sample rate select the device's native value. See
`_examples/enumeration` and `_examples/capture`. `_examples/capture_mp3`
records to an MP3 file (`cd _examples/capture_mp3 && go run . -o xx.mp3`); it
is a separate module so the pure-Go encoder stays out of this module's deps.

### Behavior per backend

- **Core Audio** converts format, channels and sample rate. Device IDs hold
  the device UID. Duplex runs two queues; captured audio is handed to the
  data callback together with the playback buffer. Loopback is not supported.
  Capture needs the microphone permission of the hosting app (Terminal, IDE).
- **ALSA** opens hardware devices only (`hw:CARD,DEV`, listed from
  `/proc/asound/pcm`) and does no conversion: the hardware must support the
  requested format, channel count and rate, otherwise `InitDevice` returns
  `ErrFormatNotSupported`. A device held by PulseAudio/PipeWire returns
  `ErrBusy`. Loopback is not supported.
- **WASAPI** uses shared mode with `AUTOCONVERTPCM`, so Windows converts.
  `Exclusive` share mode returns `ErrShareModeNotSupported`. Loopback captures
  a render endpoint.

### Differences from malgo

- No library loading: `Load`, `SetLibraryPath`, `LibraryEnv` and
  `ContextConfig.AllocationCallbacks` are gone.
- `Context.Backend()` reports the selected backend; `Backend` has `String()`.
- The backend-specific config sections (`ContextConfig.Alsa/Pulse/Jack/CoreAudio`,
  `DeviceConfig.Wasapi/Alsa/Pulse/AAudio`, `Resampling`, `ChannelMap`) are kept
  for source compatibility and ignored.
- `BackendCustom` was missing in malgo, which shifted `BackendNull`; the values
  match `ma_backend`.
- No data callback runs after `Stop` returns; `Stop` callback fires once per start.

### Tests

    CGO_ENABLED=0 go test ./...

Core Audio tests use the default devices and skip when there are none.
