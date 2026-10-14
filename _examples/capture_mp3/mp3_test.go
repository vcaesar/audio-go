package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/hajimehoshi/go-mp3"
)

func sinePCM(sampleRate, channels int, seconds float64) []byte {
	frames := int(float64(sampleRate) * seconds)
	pcm := make([]byte, 0, frames*channels*2)
	for i := range frames {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 8000)
		for range channels {
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(v))
		}
	}
	return pcm
}

// decode returns the sample rate and stereo s16 PCM of an MP3 stream.
func decode(t *testing.T, b []byte) (int, []byte) {
	t.Helper()
	d, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatal(err)
	}
	return d.SampleRate(), pcm
}

func peak(pcm []byte) int {
	m := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		m = max(m, v, -v)
	}
	return m
}

func TestEncodeMP3RoundTrip(t *testing.T) {
	for _, tc := range []struct{ rate, channels int }{
		{48000, 1},
		{48000, 2},
		{44100, 1},
		{44100, 2},
		{32000, 1},
		{22050, 1},
		{16000, 2},
	} {
		var buf bytes.Buffer
		if err := encodeMP3(&buf, sinePCM(tc.rate, tc.channels, 1), tc.rate, tc.channels); err != nil {
			t.Fatalf("%d Hz %d ch: %v", tc.rate, tc.channels, err)
		}
		rate, pcm := decode(t, buf.Bytes())
		if rate != tc.rate {
			t.Errorf("%d Hz %d ch: decoded rate %d", tc.rate, tc.channels, rate)
		}
		// Decoder output is stereo s16. Encoding pads the last MP3 frame;
		// at padded rates (44100, 22050) go-mp3 drops the first frame
		// (ffmpeg warns "invalid new backstep -1" and keeps it).
		if frames := len(pcm) / 4; frames < tc.rate-1152 || frames > tc.rate+1152 {
			t.Errorf("%d Hz %d ch: decoded %d frames, want ~%d", tc.rate, tc.channels, frames, tc.rate)
		}
		if p := peak(pcm); p < 4000 || p > 12000 {
			t.Errorf("%d Hz %d ch: decoded peak %d, want ~8000", tc.rate, tc.channels, p)
		}
	}
}

func TestEncodeMP3PartialFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := encodeMP3(&buf, sinePCM(48000, 1, 0.01), 48000, 1); err != nil {
		t.Fatal(err)
	}
	if _, pcm := decode(t, buf.Bytes()); len(pcm)/4 != 1152 {
		t.Fatalf("decoded %d frames, want one MP3 frame (1152)", len(pcm)/4)
	}
}

func TestEncodeMP3Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := encodeMP3(&buf, nil, 48000, 1); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("got %d bytes for empty input", buf.Len())
	}
}

func TestEncodeMP3Unsupported(t *testing.T) {
	for _, tc := range []struct{ rate, channels int }{
		{96000, 1},
		{44100, 0},
		{44100, 3},
	} {
		if err := encodeMP3(&bytes.Buffer{}, sinePCM(44100, 1, 0.1), tc.rate, tc.channels); err == nil {
			t.Errorf("%d Hz %d ch: expected error", tc.rate, tc.channels)
		}
	}
}
