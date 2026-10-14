package audiogo

import "fmt"

// Result type.
type Result int32

func (r Result) Error() string {
	if s, ok := resultDescriptions[r]; ok {
		return errTag + s
	}
	return fmt.Sprintf("%sresult %d", errTag, int32(r))
}

// Errors.
const (
	errTag = "miniaudio: "
)

var (
	//  General errors.
	ErrGeneric                    = Result(-1)
	ErrInvalidArgs                = Result(-2)
	ErrInvalidOperation           = Result(-3)
	ErrOutOfMemory                = Result(-4)
	ErrOutOfRange                 = Result(-5)
	ErrAccessDenied               = Result(-6)
	ErrDoesNotExist               = Result(-7)
	ErrAlreadyExists              = Result(-8)
	ErrTooManyOpenFiles           = Result(-9)
	ErrInvalidFile                = Result(-10)
	ErrTooBig                     = Result(-11)
	ErrPathTooLong                = Result(-12)
	ErrNameTooLong                = Result(-13)
	ErrNotDirectory               = Result(-14)
	ErrIsDirectory                = Result(-15)
	ErrDirectoryNotEmpty          = Result(-16)
	ErrAtEnd                      = Result(-17)
	ErrNoSpace                    = Result(-18)
	ErrBusy                       = Result(-19)
	ErrIO                         = Result(-20)
	ErrInterrupt                  = Result(-21)
	ErrUnavailable                = Result(-22)
	ErrAlreadyInUse               = Result(-23)
	ErrBadAddress                 = Result(-24)
	ErrBadSeek                    = Result(-25)
	ErrBadPipe                    = Result(-26)
	ErrDeadlock                   = Result(-27)
	ErrTooManyLinks               = Result(-28)
	ErrNotImplemented             = Result(-29)
	ErrNoMessage                  = Result(-30)
	ErrBadMessage                 = Result(-31)
	ErrNoDataAvailable            = Result(-32)
	ErrInvalidData                = Result(-33)
	ErrTimeout                    = Result(-34)
	ErrNetwork                    = Result(-35)
	ErrNotUnique                  = Result(-36)
	ErrNotSocket                  = Result(-37)
	ErrNoAddress                  = Result(-38)
	ErrBadProtocol                = Result(-39)
	ErrProtocolUnavailable        = Result(-40)
	ErrProtocolNotSupported       = Result(-41)
	ErrProtocolFamilyNotSupported = Result(-42)
	ErrAddressFamilyNotSupported  = Result(-43)
	ErrSocketNotSupported         = Result(-44)
	ErrConnectionReset            = Result(-45)
	ErrAlreadyConnected           = Result(-46)
	ErrNotConnected               = Result(-47)
	ErrConnectionRefused          = Result(-48)
	ErrNoHost                     = Result(-49)
	ErrInProgress                 = Result(-50)
	ErrCancelled                  = Result(-51)
	ErrMemoryAlreadyMapped        = Result(-52)

	// General miniaudio-specific errors.
	ErrFormatNotSupported     = Result(-200)
	ErrDeviceTypeNotSupported = Result(-201)
	ErrShareModeNotSupported  = Result(-202)
	ErrNoBackend              = Result(-203)
	ErrNoDevice               = Result(-204)
	ErrAPINotFound            = Result(-205)
	ErrInvalidDeviceConfig    = Result(-206)
	ErrLoop                   = Result(-207)

	// State errors.

	ErrDeviceNotInitialized     = Result(-300)
	ErrDeviceAlreadyInitialized = Result(-301)
	ErrDeviceNotStarted         = Result(-302)
	ErrDeviceNotStopped         = Result(-303)

	// Operation errors.

	ErrFailedToInitBackend        = Result(-400)
	ErrFailedToOpenBackendDevice  = Result(-401)
	ErrFailedToStartBackendDevice = Result(-402)
	ErrFailedToStopBackendDevice  = Result(-403)
)

// errorFromResult returns error for result code.
func errorFromResult(r int32) error {
	switch r {
	case 0:
		return nil
	default:
		return Result(r)
	}
}

var resultDescriptions = map[Result]string{
	ErrGeneric: "Unknown error", ErrInvalidArgs: "Invalid argument", ErrInvalidOperation: "Invalid operation",
	ErrOutOfMemory: "Out of memory", ErrOutOfRange: "Out of range", ErrAccessDenied: "Permission denied",
	ErrDoesNotExist: "Resource does not exist", ErrAlreadyExists: "Resource already exists",
	ErrTooManyOpenFiles: "Too many open files", ErrInvalidFile: "Invalid file", ErrTooBig: "Too large",
	ErrPathTooLong: "Path too long", ErrNameTooLong: "Name too long", ErrNotDirectory: "Not a directory",
	ErrIsDirectory: "Is a directory", ErrDirectoryNotEmpty: "Directory not empty", ErrAtEnd: "At end",
	ErrNoSpace: "No space available", ErrBusy: "Device or resource busy", ErrIO: "Input/output error",
	ErrInterrupt: "Interrupted", ErrUnavailable: "Resource unavailable", ErrAlreadyInUse: "Resource already in use",
	ErrBadAddress: "Bad address", ErrBadSeek: "Illegal seek", ErrBadPipe: "Broken pipe", ErrDeadlock: "Deadlock",
	ErrTooManyLinks: "Too many links", ErrNotImplemented: "Not implemented", ErrNoMessage: "No message of desired type",
	ErrBadMessage: "Invalid message", ErrNoDataAvailable: "No data available", ErrInvalidData: "Invalid data",
	ErrTimeout: "Timeout", ErrNetwork: "Network unavailable", ErrNotUnique: "Not unique",
	ErrNotSocket: "Socket operation on non-socket", ErrNoAddress: "Destination address required",
	ErrBadProtocol: "Protocol wrong type for socket", ErrProtocolUnavailable: "Protocol not available",
	ErrProtocolNotSupported: "Protocol not supported", ErrProtocolFamilyNotSupported: "Protocol family not supported",
	ErrAddressFamilyNotSupported: "Address family not supported", ErrSocketNotSupported: "Socket type not supported",
	ErrConnectionReset: "Connection reset", ErrAlreadyConnected: "Already connected", ErrNotConnected: "Not connected",
	ErrConnectionRefused: "Connection refused", ErrNoHost: "No host", ErrInProgress: "Operation in progress",
	ErrCancelled: "Operation cancelled", ErrMemoryAlreadyMapped: "Memory already mapped",
	ErrFormatNotSupported: "Format not supported", ErrDeviceTypeNotSupported: "Device type not supported",
	ErrShareModeNotSupported: "Share mode not supported", ErrNoBackend: "No backend", ErrNoDevice: "No device",
	ErrAPINotFound: "API not found", ErrInvalidDeviceConfig: "Invalid device config",
	ErrDeviceNotInitialized: "Device not initialized", ErrDeviceNotStarted: "Device not started",
	ErrFailedToInitBackend: "Failed to initialize backend", ErrFailedToOpenBackendDevice: "Failed to open backend device",
	ErrFailedToStartBackendDevice: "Failed to start backend device", ErrFailedToStopBackendDevice: "Failed to stop backend device",
}
