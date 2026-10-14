package audiogo

import (
	"encoding/hex"
	"fmt"
	"unsafe"
)

// DeviceID type. Backends store their native device identifier in it
// (Core Audio UID, ALSA hw:card,device, WASAPI endpoint id).
type DeviceID [256]byte

// String returns the string representation of the identifier.
// It is the hexadecimal form of the underlying bytes of a minimum length of 2 digits, with trailing zeroes removed.
func (d DeviceID) String() string {
	displayLen := len(d)
	for (displayLen > 1) && (d[displayLen-1] == 0) {
		displayLen--
	}
	return hex.EncodeToString(d[:displayLen])
}

// Pointer returns a pointer to a copy of the identifier, suitable for
// SubConfig.DeviceID.
func (d *DeviceID) Pointer() unsafe.Pointer {
	id := *d
	return unsafe.Pointer(&id)
}

// DeviceInfo type.
type DeviceInfo struct {
	ID          DeviceID
	name        [256]byte
	IsDefault   uint32
	FormatCount uint32
	Formats     []DataFormat
}

// Name returns the name of the device.
func (d *DeviceInfo) Name() string {
	// find the first null byte in d.name
	var end int
	for end = 0; end < len(d.name) && d.name[end] != 0; end++ {
	}
	return string(d.name[:end])
}

// String returns string.
func (d *DeviceInfo) String() string {
	return fmt.Sprintf("{ID: [%v], Name: %s}", d.ID, d.Name())
}

// DataFormat is a native data format supported by a device.
type DataFormat struct {
	Format     FormatType
	Channels   uint32
	SampleRate uint32
	Flags      uint32
}
