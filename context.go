package audiogo

import (
	"fmt"
	"sync"
)

// LogProc type.
type LogProc func(message string)

// AlsaContextConfig type.
type AlsaContextConfig struct {
	UseVerboseDeviceEnumeration uint32
}

// PulseContextConfig type.
type PulseContextConfig struct {
	ApplicationName string
	ServerName      string
	// Enables autospawning of the PulseAudio daemon if necessary.
	TryAutoSpawn uint32
}

// CoreAudioConfig type.
type CoreAudioConfig struct {
	SessionCategory        IOSSessionCategory
	SessionCategoryOptions IOSSessionCategoryOptions
}

// JackContextConfig type.
type JackContextConfig struct {
	ClientName     string
	TryStartServer uint32
}

// ContextConfig type. It is kept for malgo compatibility; the native Go
// backends do not use these settings.
type ContextConfig struct {
	ThreadPriority ThreadPriority
	Alsa           AlsaContextConfig
	Pulse          PulseContextConfig
	CoreAudio      CoreAudioConfig
	Jack           JackContextConfig
}

// Context is used for selecting and initializing the relevant backends.
type Context struct {
	state *contextState
}

type contextState struct {
	mu      sync.Mutex
	kind    Backend
	backend backend
	log     LogProc
}

func (s *contextState) logf(format string, args ...any) {
	s.mu.Lock()
	log := s.log
	s.mu.Unlock()
	if log != nil {
		log(fmt.Sprintf(format, args...))
	}
}

// DefaultContext is an unspecified context. It can be used to initialize a streaming
// function with implicit context defaults.
var DefaultContext Context = Context{}

// Uninit uninitializes a context.
// Results are undefined if you call this while any device created by this context is still active.
func (ctx Context) Uninit() error {
	if ctx.state == nil {
		return ErrInvalidArgs
	}
	return ctx.state.backend.uninit()
}

// Backend returns the backend the context selected.
func (ctx Context) Backend() Backend {
	if ctx.state == nil {
		return BackendNull
	}
	return ctx.state.kind
}

// Devices retrieves basic information about every active playback or capture device.
func (ctx Context) Devices(kind DeviceType) ([]DeviceInfo, error) {
	if ctx.state == nil || (kind != Playback && kind != Capture) {
		return nil, ErrInvalidArgs
	}
	devices, err := ctx.state.backend.devices(kind)
	if devices == nil && err == nil {
		devices = []DeviceInfo{}
	}
	return devices, err
}

// DeviceInfo retrieves information about a device of the given type, with the specified ID and share mode.
func (ctx Context) DeviceInfo(kind DeviceType, id DeviceID, _ ShareMode) (DeviceInfo, error) {
	if ctx.state == nil || (kind != Playback && kind != Capture) {
		return DeviceInfo{}, ErrInvalidArgs
	}
	return ctx.state.backend.deviceInfo(kind, id)
}

// SetLogProc sets the logging callback for the context.
func (ctx Context) SetLogProc(proc LogProc) {
	if ctx.state == nil {
		return
	}
	ctx.state.mu.Lock()
	ctx.state.log = proc
	ctx.state.mu.Unlock()
}

// AllocatedContext is a Context that has been created by the application.
// It must be freed after use in order to release resources.
type AllocatedContext struct {
	Context
}

// InitContext creates and initializes a context. backends is the priority
// list to try; nil uses the platform default followed by the null backend.
// When the application no longer needs the context instance, it needs to call Free() .
func InitContext(backends []Backend, _ ContextConfig, logProc LogProc) (*AllocatedContext, error) {
	state, err := initContext(backends, logProc)
	if err != nil {
		return nil, err
	}
	return &AllocatedContext{Context: Context{state: state}}, nil
}

func initContext(backends []Backend, logProc LogProc) (*contextState, error) {
	if len(backends) == 0 {
		backends = defaultBackends
	}
	state := &contextState{log: logProc}
	var errs []error
	for _, kind := range backends {
		open, ok := openers[kind]
		if !ok {
			continue
		}
		b, err := open(func(m string) { state.logf("%s", m) })
		if err != nil {
			state.logf("%s backend unavailable: %v", kind, err)
			errs = append(errs, err)
			continue
		}
		state.kind, state.backend = kind, b
		state.logf("Attempting to initialize %s backend... OK", kind)
		return state, nil
	}
	if len(errs) > 0 {
		return nil, ErrFailedToInitBackend
	}
	return nil, ErrNoBackend
}

// Free must be called when the allocated data is no longer used.
// This function must only be called for an uninitialized context.
func (ctx *AllocatedContext) Free() {
	ctx.Context = Context{}
}
