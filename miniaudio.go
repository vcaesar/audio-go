package audiogo

// SampleSizeInBytes retrieves the size of a sample in bytes for the given format.
func SampleSizeInBytes(format FormatType) int {
	switch format {
	case FormatU8:
		return 1
	case FormatS16:
		return 2
	case FormatS24:
		return 3
	case FormatS32, FormatF32:
		return 4
	default:
		return 0
	}
}
