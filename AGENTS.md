# audiogo

Pure Go (no cgo, no purego, no miniaudio) port of gen2brain/malgo's API: Core Audio on macOS, ALSA ioctls on Linux, WASAPI on Windows, plus a null backend.

## Commands

- Test: `CGO_ENABLED=0 go test ./...` (always keep `CGO_ENABLED=0`; the whole point is a cgo-free build)
- Single test: `CGO_ENABLED=0 go test -run TestNewDeviceParams .`
- Build: `CGO_ENABLED=0 go build ./...`
- Other platforms' backends only compile under their GOOS/GOARCH; check them with e.g. `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet .` and `GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go vet .`
- `_examples/capture_mp3` is a separate module (`replace ... => ../..`): run/test it from its directory (`cd _examples/capture_mp3 && go run . -o xx.mp3`). `./...` from the root does not cover it, and `_examples/` is ignored by the go tool due to the `_` prefix.

## Architecture

Single package `audiogo` at the repo root (module `github.com/vcaesar/audio-go`, go 1.25).

- `context.go`, `device.go`, `device_config.go`, `device_info.go`, `enumerations.go`, `errors.go`, `miniaudio.go`: public malgo-compatible API.
- `backend.go`: internal `backend` / `stream` interfaces, `openers` registry, `defaultBackends` priority, `sink` (run-numbered start/stop bookkeeping), `deviceParams`.
- `backend_null.go`: timer-driven null backend (all platforms).
- `backend_coreaudio_darwin.go` + `sys_darwin.go` + `sys_darwin_{amd64,arm64}.s`: Core Audio via `//go:cgo_import_dynamic`, asm trampolines and `syscall.syscall9` (`//go:linkname`).
- `backend_alsa_linux.go`: raw ioctls on `/dev/snd/pcmC*D*`, devices from `/proc/asound/pcm`.
- `backend_wasapi_windows.go`: COM vtables via `syscall.SyscallN`.
- `_examples/{enumeration,capture,capture_mp3}`: example programs.

## Key patterns and gotchas

- New backends register in `func init()` by setting `openers[BackendX]`; `InitContext` walks `defaultBackends` and skips kinds with no opener.
- Platform files use both a filename suffix and a `//go:build` line; the Linux/Windows constraints list explicit architectures (`linux && (amd64 || arm64 || riscv64 || loong64 || 386 || arm)`, `windows && (amd64 || arm64)`). Keep tags and test-file tags in sync.
- `stream.stop()` must be synchronous and idempotent: no data callback may run after it returns. A stream failing on its own calls `sink.fail(run)` after its worker exits; the run number prevents a late failure from stopping a newer run.
- Darwin: adding a framework function needs three edits: a `//go:cgo_import_dynamic ag_X` line and `fnX` var in `sys_darwin.go`, plus a trampoline in **both** `sys_darwin_amd64.s` and `sys_darwin_arm64.s`. Wrap pointer args with `runtime.KeepAlive`.
- Darwin: the Audio Queue callback (`queueCallback` in asm) never enters Go. `queueRing` field offsets (0/8/16) and `queueRingSize = 64` are hard-coded in the asm mask; change both together.
- C/kernel struct mirrors (Core Audio, ALSA `sndHwParams`/`sndSwParams`/`sndXferi` and ioctl numbers, WASAPI `waveFormatExtensible`/`propVariant`/`comGUID`) are pinned by size/offset tests; ALSA has separate 32-bit values. Update tests only against the real ABI headers.
- `DeviceID` stores backend-native IDs as NUL-terminated text (`deviceIDString` / `(*DeviceID).str()`); long IDs are truncated.
- Zero format/channels/sample rate mean "device native"; backends fill them (`subParams.fillDefaults`). Defaults: `periodMillis` 10 (100 for `Conservative`), `periods` 3.
- Backend-specific config fields (`ContextConfig.Alsa/Pulse/Jack/CoreAudio`, `DeviceConfig.Wasapi/Alsa/Pulse/AAudio`, `Resampling`, `ChannelMap`) exist only for malgo source compatibility and are ignored. Don't wire them up.
- Backend enum values match miniaudio's `ma_backend` (including `BackendCustom`); don't renumber.
- ALSA does no conversion (unsupported formats return `ErrFormatNotSupported`, a busy device returns `ErrBusy`); WASAPI `Exclusive` returns `ErrShareModeNotSupported`; Core Audio has no loopback support.

## Testing

- Standard `testing` only, no assertion libs. `audiogo_internal_test.go` (package `audiogo`) for internals, `audiogo_test.go` (package `audiogo_test`) for the public API, `backend_*_<os>_test.go` per backend with matching build tags.
- Hardware tests skip with `t.Skipf` when there is no default device or the backend is unavailable. macOS capture also needs mic permission for the terminal/IDE.
- To fake a backend in tests, temporarily replace an `openers` entry and restore it afterward (see `audiogo_internal_test.go`, the `openers[BackendCustom]` swap).
- `test/` is gitignored; put scratch files in `test/tmp` and `test/logs`.
