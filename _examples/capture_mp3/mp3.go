package main

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/braheezy/shine-mp3/pkg/mp3"
)

// bitrate is the fixed bitrate (kbps) of shine's encoder.
const bitrate = 128

// checkMP3 reports whether the encoder supports the sample rate and channels.
func checkMP3(sampleRate, channels int) error {
	if channels != 1 && channels != 2 {
		return fmt.Errorf("mp3: unsupported channel count %d", channels)
	}
	if _, err := mp3.CheckConfig(sampleRate, bitrate); err != nil {
		return fmt.Errorf("mp3: %w", err)
	}
	return nil
}

// encodeMP3 encodes interleaved little-endian signed 16-bit PCM to a stereo
// MP3. Mono input is duplicated to both channels: shine-mp3 v0.2.0 writes
// mono streams that decoders reject.
func encodeMP3(w io.Writer, pcm []byte, sampleRate, channels int) error {
	if err := checkMP3(sampleRate, channels); err != nil {
		return err
	}

	n := len(pcm) / 2 / channels
	samples := make([]int16, 0, 2*n)
	for i := range n {
		l := int16(binary.LittleEndian.Uint16(pcm[2*i*channels:]))
		r := int16(binary.LittleEndian.Uint16(pcm[2*(i*channels+channels-1):]))
		samples = append(samples, l, r)
	}

	enc := mp3.NewEncoder(sampleRate, 2)
	// One stereo MP3 frame holds 2*1152 samples (2*576 for MPEG-2/2.5).
	frame := 2 * 1152
	if sampleRate < 32000 {
		frame = 2 * 576
	}
	for i := 0; i < len(samples); i += frame {
		data, written := enc.EncodeBufferInterleaved(samples[i:min(i+frame, len(samples))])
		if _, err := w.Write(data[:written]); err != nil {
			return err
		}
	}
	return nil
}
