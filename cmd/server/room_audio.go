package main

import "time"

// Shared room-audio format constants and helpers for decoding LiveKit room
// audio. LiveKit delivers Opus at 48 kHz stereo; the Nova Sonic path decodes
// those frames before downmixing to the 16 kHz mono PCM Bedrock expects, and
// paces agent output back out in fixed 20 ms stereo frames.
const (
	roomAudioSampleRate = 48000
	roomAudioChannels   = 2
	roomAudioMaxFrameMs = 60

	roomAudioMixInterval  = 20 * time.Millisecond
	roomAudioMixFrameSize = roomAudioSampleRate / 50 * roomAudioChannels
)

// audioMixDivisor returns a safe non-zero divisor for averaging count mixed
// PCM sources, clamped to the int32 range.
func audioMixDivisor(count int) int32 {
	if count <= 0 {
		return 1
	}
	if count > 1<<31-1 {
		return 1<<31 - 1
	}
	return int32(count) // #nosec G115 -- count is bounded to int32 range immediately above.
}

// clampPCM16 saturates a 32-bit accumulator to the signed 16-bit PCM range.
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

// roomAudioDecodeBufferSize returns the worst-case interleaved PCM buffer size
// (samples) needed to hold one decoded Opus frame for the given channel count.
func roomAudioDecodeBufferSize(channels int) int {
	if channels <= 0 {
		return 0
	}

	return roomAudioSampleRate * channels * roomAudioMaxFrameMs / 1000
}
