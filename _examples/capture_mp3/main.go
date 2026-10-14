// This example captures audio from the default microphone until you press
// Enter (or -d elapses) and saves it as an MP3 file.
//
//	go run . -o xx.mp3
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"time"

	audiogo "github.com/vcaesar/audio-go"
)

func main() {
	out := flag.String("o", "xx.mp3", "output MP3 file")
	rate := flag.Uint("r", 48000, "sample rate: 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025 or 8000")
	channels := flag.Uint("c", 1, "capture channels: 1 or 2 (the MP3 is always stereo)")
	duration := flag.Duration("d", 0, "record duration; 0 waits for Enter")
	flag.Parse()

	if err := run(*out, uint32(*rate), uint32(*channels), *duration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out string, rate, channels uint32, duration time.Duration) error {
	ctx, err := audiogo.InitContext(nil, audiogo.ContextConfig{}, func(message string) {
		fmt.Printf("LOG <%v>\n", message)
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	cfg := audiogo.DefaultDeviceConfig(audiogo.Capture)
	cfg.Capture.Format = audiogo.FormatS16
	cfg.Capture.Channels = channels
	cfg.SampleRate = rate

	var pcm []byte
	device, err := audiogo.InitDevice(ctx.Context, cfg, audiogo.DeviceCallbacks{
		Data: func(_, in []byte, _ uint32) {
			pcm = append(pcm, in...)
		},
	})
	if err != nil {
		return err
	}
	if err := checkMP3(int(device.SampleRate()), int(device.CaptureChannels())); err != nil {
		device.Uninit()
		return err
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		return err
	}
	fmt.Printf("Recording (%s, %d Hz, %d ch)...\n", ctx.Backend(), device.SampleRate(), device.CaptureChannels())
	if duration > 0 {
		time.Sleep(duration)
	} else {
		fmt.Println("Press Enter to stop recording...")
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	// Uninit stops the device; no data callback runs after it returns.
	device.Uninit()

	var buf bytes.Buffer
	if err := encodeMP3(&buf, pcm, int(device.SampleRate()), int(device.CaptureChannels())); err != nil {
		return err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("Saved %s (%d bytes PCM -> %d bytes MP3)\n", out, len(pcm), buf.Len())
	return nil
}
