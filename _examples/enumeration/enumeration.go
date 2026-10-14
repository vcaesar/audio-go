package main

import (
	"fmt"
	"os"

	audiogo "github.com/vcaesar/audio-go"
)

func main() {
	context, err := audiogo.InitContext(nil, audiogo.ContextConfig{}, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() {
		_ = context.Uninit()
		context.Free()
	}()

	// Playback devices.
	infos, err := context.Devices(audiogo.Playback)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("Playback Devices")
	for i, info := range infos {
		e := "ok"
		full, err := context.DeviceInfo(audiogo.Playback, info.ID, audiogo.Shared)
		if err != nil {
			e = err.Error()
		}
		fmt.Printf("    %d: %v, %s, [%s], formats: %+v\n",
			i, info.ID, info.Name(), e, full.Formats)
	}

	fmt.Println()

	// Capture devices.
	infos, err = context.Devices(audiogo.Capture)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("Capture Devices")
	for i, info := range infos {
		e := "ok"
		full, err := context.DeviceInfo(audiogo.Capture, info.ID, audiogo.Shared)
		if err != nil {
			e = err.Error()
		}
		fmt.Printf("    %d: %v, %s, [%s], formats: %+v\n",
			i, info.ID, info.Name(), e, full.Formats)
	}
}
