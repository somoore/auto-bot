package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	roomAudioSampleRate = 48000
	roomAudioChannels   = 2
	roomAudioMaxFrameMs = 60

	roomAudioMixInterval  = 20 * time.Millisecond
	roomAudioMixFrameSize = roomAudioSampleRate / 50 * roomAudioChannels
	audioSourceLimit      = roomAudioMixFrameSize * 50
)

type mixedAudioSink interface {
	WriteMixedPCM([]int16) error
}

type audioMixer struct {
	mu        sync.Mutex
	sinks     map[string]mixedAudioSink
	input     chan audioInput
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

type audioInput struct {
	trackKey string
	pcm      []int16
	remove   bool
}

type audioSource struct {
	buffer []int16
}

func newAudioMixer() *audioMixer {
	mixer := &audioMixer{
		sinks: map[string]mixedAudioSink{},
		input: make(chan audioInput, 128),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}

	go mixer.run()
	return mixer
}

func (mixer *audioMixer) submit(trackKey string, pcm []int16) {
	if mixer == nil || trackKey == "" || len(pcm) == 0 {
		return
	}

	select {
	case <-mixer.stop:
		return
	default:
	}

	select {
	case mixer.input <- audioInput{trackKey: trackKey, pcm: pcm}:
	default:
		log.Warnf("Dropping decoded audio frame for track=%s", trackKey)
	}
}

func (mixer *audioMixer) removeTrack(trackKey string) {
	if mixer == nil || trackKey == "" {
		return
	}

	select {
	case <-mixer.stop:
		return
	default:
	}

	select {
	case mixer.input <- audioInput{trackKey: trackKey, remove: true}:
	default:
		log.Warnf("Dropping decoded audio remove for track=%s", trackKey)
	}
}

func (mixer *audioMixer) setSink(key string, sink mixedAudioSink) {
	if mixer == nil || key == "" {
		return
	}

	mixer.mu.Lock()
	defer mixer.mu.Unlock()

	if sink == nil {
		delete(mixer.sinks, key)
		return
	}
	mixer.sinks[key] = sink
}

func (mixer *audioMixer) removeSink(key string) {
	if mixer == nil || key == "" {
		return
	}

	mixer.mu.Lock()
	delete(mixer.sinks, key)
	mixer.mu.Unlock()
}

func (mixer *audioMixer) close() {
	if mixer == nil {
		return
	}

	mixer.closeOnce.Do(func() {
		close(mixer.stop)
		<-mixer.done
	})
}

func (mixer *audioMixer) run() {
	defer close(mixer.done)

	ticker := time.NewTicker(roomAudioMixInterval)
	defer ticker.Stop()

	sources := map[string]*audioSource{}
	for {
		select {
		case <-mixer.stop:
			return
		case input := <-mixer.input:
			if input.remove {
				delete(sources, input.trackKey)
				continue
			}

			source := sources[input.trackKey]
			if source == nil {
				source = &audioSource{}
				sources[input.trackKey] = source
			}

			source.buffer = append(source.buffer, input.pcm...)
			if overflow := len(source.buffer) - audioSourceLimit; overflow > 0 {
				source.buffer = source.buffer[overflow:]
			}
		case <-ticker.C:
			mixedPCM := mixAudioFrame(sources)
			if len(mixedPCM) == 0 {
				continue
			}

			for key, sink := range mixer.snapshotSinks() {
				if err := sink.WriteMixedPCM(mixedPCM); err != nil {
					log.Errorf("Failed to write mixed audio sink=%s: %v", key, err)
				}
			}
		}
	}
}

func (mixer *audioMixer) snapshotSinks() map[string]mixedAudioSink {
	mixer.mu.Lock()
	defer mixer.mu.Unlock()

	sinks := make(map[string]mixedAudioSink, len(mixer.sinks))
	for key, sink := range mixer.sinks {
		sinks[key] = sink
	}

	return sinks
}

func mixAudioFrame(sources map[string]*audioSource) []int16 {
	readySources := make([]*audioSource, 0, len(sources))
	for _, source := range sources {
		if len(source.buffer) >= roomAudioMixFrameSize {
			readySources = append(readySources, source)
		}
	}
	if len(readySources) == 0 {
		return nil
	}

	mixedPCM := make([]int16, roomAudioMixFrameSize)
	for sampleIndex := range mixedPCM {
		var sampleSum int32
		for _, source := range readySources {
			sampleSum += int32(source.buffer[sampleIndex])
		}
		mixedPCM[sampleIndex] = clampPCM16(sampleSum / int32(len(readySources)))
	}

	for _, source := range readySources {
		source.buffer = source.buffer[roomAudioMixFrameSize:]
	}

	return mixedPCM
}

func clampPCM16(sample int32) int16 {
	switch {
	case sample > 32767:
		return 32767
	case sample < -32768:
		return -32768
	default:
		return int16(sample)
	}
}

func roomAudioTrackKey(remoteTrack *webrtc.TrackRemote) string {
	return fmt.Sprintf("%s:%s:%d", remoteTrack.StreamID(), remoteTrack.ID(), remoteTrack.SSRC())
}

func newRoomAudioDecoder(remoteTrack *webrtc.TrackRemote) (*opusDecoder, int, error) {
	if remoteTrack.Kind() != webrtc.RTPCodecTypeAudio {
		return nil, 0, nil
	}

	codec := remoteTrack.Codec()
	if !strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) {
		return nil, 0, fmt.Errorf("unsupported audio codec %q", codec.MimeType)
	}

	clockRate := int(codec.ClockRate)
	if clockRate == 0 {
		clockRate = roomAudioSampleRate
	}
	if clockRate != roomAudioSampleRate {
		return nil, 0, fmt.Errorf("unsupported opus clock rate %d", codec.ClockRate)
	}

	channels := normalizedRoomAudioChannels(codec.Channels)
	decoder, err := newOpusDecoder(clockRate, channels)
	if err != nil {
		return nil, 0, err
	}

	return decoder, channels, nil
}

func normalizedRoomAudioChannels(channels uint16) int {
	switch channels {
	case 1:
		return 1
	case 2:
		return 2
	default:
		return roomAudioChannels
	}
}

func roomAudioDecodeBufferSize(channels int) int {
	if channels <= 0 {
		return 0
	}

	return roomAudioSampleRate * channels * roomAudioMaxFrameMs / 1000
}

func decodeOpusToRoomPCM(decoder *opusDecoder, buffer []int16, channels int, payload []byte) ([]int16, error) {
	if decoder == nil || channels == 0 || len(payload) == 0 {
		return nil, nil
	}

	samplesPerChannel, err := decoder.Decode(payload, buffer)
	if err != nil {
		return nil, err
	}

	return normalizeRoomAudioPCM(buffer[:samplesPerChannel*channels], channels), nil
}

func normalizeRoomAudioPCM(pcm []int16, channels int) []int16 {
	switch channels {
	case 1:
		stereoPCM := make([]int16, len(pcm)*roomAudioChannels)
		for sampleIndex, sample := range pcm {
			baseIndex := sampleIndex * roomAudioChannels
			stereoPCM[baseIndex] = sample
			stereoPCM[baseIndex+1] = sample
		}
		return stereoPCM
	case roomAudioChannels:
		return append([]int16(nil), pcm...)
	default:
		return nil
	}
}
