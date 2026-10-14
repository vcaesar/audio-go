//go:build darwin

package audiogo

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Calls into the system frameworks without cgo or purego: the functions are
// imported with cgo_import_dynamic (as golang.org/x/sys/unix and crypto/x509
// do), reached through the assembly trampolines in sys_darwin_*.s and called
// with syscall.syscall9, the runtime's libc call path.

//go:linkname syscall9 syscall.syscall9
func syscall9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:cgo_import_dynamic ag_AudioQueueNewInput AudioQueueNewInput "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueNewOutput AudioQueueNewOutput "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueAllocateBuffer AudioQueueAllocateBuffer "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueEnqueueBuffer AudioQueueEnqueueBuffer "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueStart AudioQueueStart "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueStop AudioQueueStop "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueDispose AudioQueueDispose "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioQueueSetProperty AudioQueueSetProperty "/System/Library/Frameworks/AudioToolbox.framework/Versions/A/AudioToolbox"
//go:cgo_import_dynamic ag_AudioObjectGetPropertyDataSize AudioObjectGetPropertyDataSize "/System/Library/Frameworks/CoreAudio.framework/Versions/A/CoreAudio"
//go:cgo_import_dynamic ag_AudioObjectGetPropertyData AudioObjectGetPropertyData "/System/Library/Frameworks/CoreAudio.framework/Versions/A/CoreAudio"
//go:cgo_import_dynamic ag_CFStringCreateWithCString CFStringCreateWithCString "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"
//go:cgo_import_dynamic ag_CFStringGetCString CFStringGetCString "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"
//go:cgo_import_dynamic ag_CFRelease CFRelease "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"
//go:cgo_import_dynamic ag_dispatch_semaphore_create dispatch_semaphore_create "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic ag_dispatch_semaphore_wait dispatch_semaphore_wait "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic ag_dispatch_semaphore_signal dispatch_semaphore_signal "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic ag_dispatch_release dispatch_release "/usr/lib/libSystem.B.dylib"

// Trampoline addresses, set in sys_darwin_*.s.
var (
	fnAudioQueueNewInput             uintptr
	fnAudioQueueNewOutput            uintptr
	fnAudioQueueAllocateBuffer       uintptr
	fnAudioQueueEnqueueBuffer        uintptr
	fnAudioQueueStart                uintptr
	fnAudioQueueStop                 uintptr
	fnAudioQueueDispose              uintptr
	fnAudioQueueSetProperty          uintptr
	fnAudioObjectGetPropertyDataSize uintptr
	fnAudioObjectGetPropertyData     uintptr
	fnCFStringCreateWithCString      uintptr
	fnCFStringGetCString             uintptr
	fnCFRelease                      uintptr
	fnDispatchSemaphoreCreate        uintptr
	fnDispatchSemaphoreWait          uintptr
	fnDispatchSemaphoreSignal        uintptr
	fnDispatchRelease                uintptr

	// queueCallbackAddr is the C entry point of queueCallback in assembly:
	// void cb(queueRing *ring, AudioQueueRef aq, AudioQueueBufferRef buf).
	// It runs on the Audio Queue thread without entering Go: it appends buf
	// to ring.bufs, publishes ring.head with release semantics and signals
	// ring.sem with dispatch_semaphore_signal.
	queueCallbackAddr uintptr
)

// queueRing is shared with queueCallback; the offsets are fixed in assembly.
// Only one Audio Queue writes a ring, and a queue never owns more than
// maxQueueBuffers buffers, so the ring cannot overflow.
type queueRing struct {
	sem  uintptr // offset 0
	head uint32  // offset 8
	_    uint32
	bufs [queueRingSize]uintptr // offset 16
}

const (
	queueRingSize   = 64 // must match the mask in assembly
	maxQueueBuffers = 16
)

// audioQueueBuffer mirrors AudioQueueBuffer.
type audioQueueBuffer struct {
	capacity        uint32
	data            unsafe.Pointer
	size            uint32
	userData        unsafe.Pointer
	packetDescCap   uint32
	packetDescs     unsafe.Pointer
	packetDescCount uint32
}

// audioStreamBasicDescription mirrors AudioStreamBasicDescription.
type audioStreamBasicDescription struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

// propertyAddress mirrors AudioObjectPropertyAddress.
type propertyAddress struct {
	selector, scope, element uint32
}

// audioValueRange mirrors AudioValueRange.
type audioValueRange struct {
	min, max float64
}

// cptr converts a C address to a pointer. C memory is not managed by Go.
func cptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func osStatus(r uintptr) int32 { return int32(uint32(r)) }

func audioQueueNewInput(format *audioStreamBasicDescription, ring *queueRing, aq *uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueNewInput, uintptr(unsafe.Pointer(format)), queueCallbackAddr,
		uintptr(unsafe.Pointer(ring)), 0, 0, 0, uintptr(unsafe.Pointer(aq)), 0, 0)
	runtime.KeepAlive(format)
	runtime.KeepAlive(aq)
	return osStatus(r)
}

func audioQueueNewOutput(format *audioStreamBasicDescription, ring *queueRing, aq *uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueNewOutput, uintptr(unsafe.Pointer(format)), queueCallbackAddr,
		uintptr(unsafe.Pointer(ring)), 0, 0, 0, uintptr(unsafe.Pointer(aq)), 0, 0)
	runtime.KeepAlive(format)
	runtime.KeepAlive(aq)
	return osStatus(r)
}

func audioQueueAllocateBuffer(aq uintptr, size uint32, buf *uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueAllocateBuffer, aq, uintptr(size), uintptr(unsafe.Pointer(buf)), 0, 0, 0, 0, 0, 0)
	runtime.KeepAlive(buf)
	return osStatus(r)
}

func audioQueueEnqueueBuffer(aq, buf uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueEnqueueBuffer, aq, buf, 0, 0, 0, 0, 0, 0, 0)
	return osStatus(r)
}

func audioQueueStart(aq uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueStart, aq, 0, 0, 0, 0, 0, 0, 0, 0)
	return osStatus(r)
}

// audioQueueStop stops immediately and synchronously.
func audioQueueStop(aq uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueStop, aq, 1, 0, 0, 0, 0, 0, 0, 0)
	return osStatus(r)
}

func audioQueueDispose(aq uintptr) int32 {
	r, _, _ := syscall9(fnAudioQueueDispose, aq, 1, 0, 0, 0, 0, 0, 0, 0)
	return osStatus(r)
}

func audioQueueSetProperty(aq uintptr, id uint32, data unsafe.Pointer, size uint32) int32 {
	r, _, _ := syscall9(fnAudioQueueSetProperty, aq, uintptr(id), uintptr(data), uintptr(size), 0, 0, 0, 0, 0)
	runtime.KeepAlive(data)
	return osStatus(r)
}

func audioObjectGetPropertyDataSize(obj uint32, addr *propertyAddress, size *uint32) int32 {
	r, _, _ := syscall9(fnAudioObjectGetPropertyDataSize, uintptr(obj), uintptr(unsafe.Pointer(addr)), 0, 0,
		uintptr(unsafe.Pointer(size)), 0, 0, 0, 0)
	runtime.KeepAlive(addr)
	runtime.KeepAlive(size)
	return osStatus(r)
}

func audioObjectGetPropertyData(obj uint32, addr *propertyAddress, qualSize uint32, qual unsafe.Pointer, size *uint32, data unsafe.Pointer) int32 {
	r, _, _ := syscall9(fnAudioObjectGetPropertyData, uintptr(obj), uintptr(unsafe.Pointer(addr)), uintptr(qualSize),
		uintptr(qual), uintptr(unsafe.Pointer(size)), uintptr(data), 0, 0, 0)
	runtime.KeepAlive(addr)
	runtime.KeepAlive(qual)
	runtime.KeepAlive(size)
	runtime.KeepAlive(data)
	return osStatus(r)
}

const cfStringEncodingUTF8 = 0x08000100

// cfString returns a CFStringRef the caller must release with cfRelease.
func cfString(s string) uintptr {
	b := append([]byte(s), 0)
	r, _, _ := syscall9(fnCFStringCreateWithCString, 0, uintptr(unsafe.Pointer(&b[0])), cfStringEncodingUTF8, 0, 0, 0, 0, 0, 0)
	runtime.KeepAlive(b)
	return r
}

func goStringFromCF(ref uintptr) string {
	if ref == 0 {
		return ""
	}
	var buf [1024]byte
	r, _, _ := syscall9(fnCFStringGetCString, ref, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), cfStringEncodingUTF8, 0, 0, 0, 0, 0)
	runtime.KeepAlive(&buf)
	if uint8(r) == 0 {
		return ""
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n])
}

func cfRelease(ref uintptr) {
	if ref != 0 {
		syscall9(fnCFRelease, ref, 0, 0, 0, 0, 0, 0, 0, 0)
	}
}

func dispatchSemaphoreCreate() uintptr {
	r, _, _ := syscall9(fnDispatchSemaphoreCreate, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	return r
}

// dispatchSemaphoreWait blocks the calling thread until the semaphore is signaled.
func dispatchSemaphoreWait(sem uintptr) {
	syscall9(fnDispatchSemaphoreWait, sem, ^uintptr(0), 0, 0, 0, 0, 0, 0, 0)
}

func dispatchSemaphoreSignal(sem uintptr) {
	syscall9(fnDispatchSemaphoreSignal, sem, 0, 0, 0, 0, 0, 0, 0, 0)
}

func dispatchRelease(obj uintptr) {
	syscall9(fnDispatchRelease, obj, 0, 0, 0, 0, 0, 0, 0, 0)
}
