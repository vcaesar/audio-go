//go:build darwin

#include "textflag.h"

TEXT AudioQueueNewInput_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueNewInput(SB)
GLOBL	·fnAudioQueueNewInput(SB), RODATA, $8
DATA	·fnAudioQueueNewInput(SB)/8, $AudioQueueNewInput_trampoline<>(SB)

TEXT AudioQueueNewOutput_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueNewOutput(SB)
GLOBL	·fnAudioQueueNewOutput(SB), RODATA, $8
DATA	·fnAudioQueueNewOutput(SB)/8, $AudioQueueNewOutput_trampoline<>(SB)

TEXT AudioQueueAllocateBuffer_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueAllocateBuffer(SB)
GLOBL	·fnAudioQueueAllocateBuffer(SB), RODATA, $8
DATA	·fnAudioQueueAllocateBuffer(SB)/8, $AudioQueueAllocateBuffer_trampoline<>(SB)

TEXT AudioQueueEnqueueBuffer_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueEnqueueBuffer(SB)
GLOBL	·fnAudioQueueEnqueueBuffer(SB), RODATA, $8
DATA	·fnAudioQueueEnqueueBuffer(SB)/8, $AudioQueueEnqueueBuffer_trampoline<>(SB)

TEXT AudioQueueStart_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueStart(SB)
GLOBL	·fnAudioQueueStart(SB), RODATA, $8
DATA	·fnAudioQueueStart(SB)/8, $AudioQueueStart_trampoline<>(SB)

TEXT AudioQueueStop_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueStop(SB)
GLOBL	·fnAudioQueueStop(SB), RODATA, $8
DATA	·fnAudioQueueStop(SB)/8, $AudioQueueStop_trampoline<>(SB)

TEXT AudioQueueDispose_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueDispose(SB)
GLOBL	·fnAudioQueueDispose(SB), RODATA, $8
DATA	·fnAudioQueueDispose(SB)/8, $AudioQueueDispose_trampoline<>(SB)

TEXT AudioQueueSetProperty_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioQueueSetProperty(SB)
GLOBL	·fnAudioQueueSetProperty(SB), RODATA, $8
DATA	·fnAudioQueueSetProperty(SB)/8, $AudioQueueSetProperty_trampoline<>(SB)

TEXT AudioObjectGetPropertyDataSize_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioObjectGetPropertyDataSize(SB)
GLOBL	·fnAudioObjectGetPropertyDataSize(SB), RODATA, $8
DATA	·fnAudioObjectGetPropertyDataSize(SB)/8, $AudioObjectGetPropertyDataSize_trampoline<>(SB)

TEXT AudioObjectGetPropertyData_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_AudioObjectGetPropertyData(SB)
GLOBL	·fnAudioObjectGetPropertyData(SB), RODATA, $8
DATA	·fnAudioObjectGetPropertyData(SB)/8, $AudioObjectGetPropertyData_trampoline<>(SB)

TEXT CFStringCreateWithCString_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_CFStringCreateWithCString(SB)
GLOBL	·fnCFStringCreateWithCString(SB), RODATA, $8
DATA	·fnCFStringCreateWithCString(SB)/8, $CFStringCreateWithCString_trampoline<>(SB)

TEXT CFStringGetCString_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_CFStringGetCString(SB)
GLOBL	·fnCFStringGetCString(SB), RODATA, $8
DATA	·fnCFStringGetCString(SB)/8, $CFStringGetCString_trampoline<>(SB)

TEXT CFRelease_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_CFRelease(SB)
GLOBL	·fnCFRelease(SB), RODATA, $8
DATA	·fnCFRelease(SB)/8, $CFRelease_trampoline<>(SB)

TEXT DispatchSemaphoreCreate_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_dispatch_semaphore_create(SB)
GLOBL	·fnDispatchSemaphoreCreate(SB), RODATA, $8
DATA	·fnDispatchSemaphoreCreate(SB)/8, $DispatchSemaphoreCreate_trampoline<>(SB)

TEXT DispatchSemaphoreWait_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_dispatch_semaphore_wait(SB)
GLOBL	·fnDispatchSemaphoreWait(SB), RODATA, $8
DATA	·fnDispatchSemaphoreWait(SB)/8, $DispatchSemaphoreWait_trampoline<>(SB)

TEXT DispatchSemaphoreSignal_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_dispatch_semaphore_signal(SB)
GLOBL	·fnDispatchSemaphoreSignal(SB), RODATA, $8
DATA	·fnDispatchSemaphoreSignal(SB)/8, $DispatchSemaphoreSignal_trampoline<>(SB)

TEXT DispatchRelease_trampoline<>(SB),NOSPLIT,$0-0
	JMP	ag_dispatch_release(SB)
GLOBL	·fnDispatchRelease(SB), RODATA, $8
DATA	·fnDispatchRelease(SB)/8, $DispatchRelease_trampoline<>(SB)

// void queueCallback(queueRing *ring, AudioQueueRef aq, AudioQueueBufferRef buf)
// C ABI: R0 = ring, R2 = buf. Uses only caller-saved registers and tail-calls
// dispatch_semaphore_signal(ring->sem).
TEXT queueCallback<>(SB),NOSPLIT|NOFRAME,$0
	MOVWU	8(R0), R3
	AND	$63, R3, R4
	ADD	$16, R0, R5
	MOVD	R2, (R5)(R4<<3)
	ADD	$1, R3, R3
	ADD	$8, R0, R6
	STLRW	R3, (R6)
	MOVD	0(R0), R0
	JMP	ag_dispatch_semaphore_signal(SB)

GLOBL	·queueCallbackAddr(SB), RODATA, $8
DATA	·queueCallbackAddr(SB)/8, $queueCallback<>(SB)
