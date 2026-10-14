module github.com/vcaesar/audio-go/_examples/capture_mp3

go 1.25.0

require (
	github.com/braheezy/shine-mp3 v0.2.0
	github.com/vcaesar/audio-go v0.0.0
)

require github.com/hajimehoshi/go-mp3 v0.3.4

replace github.com/vcaesar/audio-go => ../..
