// Package audiogo is a pure Go audio library with the API of
// github.com/gen2brain/malgo. It needs no cgo, no purego and no shared
// library: every backend talks to the operating system directly.
//
//   - macOS: Core Audio (HAL enumeration, Audio Queue streams). System
//     frameworks are imported with cgo_import_dynamic and called through the
//     runtime's libc call path, the way golang.org/x/sys/unix does; the Audio
//     Queue callback is a small assembly routine that never enters Go.
//   - Linux: ALSA kernel ioctls on /dev/snd (hw devices, no libasound).
//   - Windows: WASAPI shared mode through COM vtables and syscall.SyscallN.
//   - Everywhere: a null backend, driven by a timer.
//
// Build with CGO_ENABLED=0 and cross compile like any Go package.
package audiogo
