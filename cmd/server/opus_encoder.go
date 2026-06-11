package main

/*
#cgo pkg-config: opus
#include <opus.h>

static int opus_set_bitrate_c(OpusEncoder* enc, opus_int32 bitrate) {
	return opus_encoder_ctl(enc, OPUS_SET_BITRATE(bitrate));
}

static int opus_set_complexity_c(OpusEncoder* enc, int complexity) {
	return opus_encoder_ctl(enc, OPUS_SET_COMPLEXITY(complexity));
}

static int opus_set_inband_fec_c(OpusEncoder* enc, int enabled) {
	return opus_encoder_ctl(enc, OPUS_SET_INBAND_FEC(enabled));
}

static int opus_set_packet_loss_perc_c(OpusEncoder* enc, int percent) {
	return opus_encoder_ctl(enc, OPUS_SET_PACKET_LOSS_PERC(percent));
}

static int opus_set_dtx_c(OpusEncoder* enc, int enabled) {
	return opus_encoder_ctl(enc, OPUS_SET_DTX(enabled));
}

static int opus_set_signal_voice_c(OpusEncoder* enc) {
	return opus_encoder_ctl(enc, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const opusMaxPacketSize = 4096

type opusEncoder struct {
	ptr      *C.OpusEncoder
	mem      []byte
	channels int
}

func newOpusEncoder(sampleRate int, channels int) (*opusEncoder, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported opus channel count %d", channels)
	}

	size := C.opus_encoder_get_size(C.int(channels))
	mem := make([]byte, int(size))
	encoder := &opusEncoder{
		ptr:      (*C.OpusEncoder)(unsafe.Pointer(&mem[0])),
		mem:      mem,
		channels: channels,
	}

	if code := C.opus_encoder_init(
		encoder.ptr,
		C.opus_int32(sampleRate),
		C.int(channels),
		C.OPUS_APPLICATION_VOIP,
	); code != 0 {
		return nil, fmt.Errorf("init opus encoder: %s", opusErrorText(code))
	}

	if err := encoder.configureVoiceDefaults(); err != nil {
		return nil, err
	}

	return encoder, nil
}

func (encoder *opusEncoder) configureVoiceDefaults() error {
	settings := []struct {
		name string
		code C.int
	}{
		{name: "bitrate", code: C.opus_set_bitrate_c(encoder.ptr, C.opus_int32(64000))},
		{name: "complexity", code: C.opus_set_complexity_c(encoder.ptr, C.int(6))},
		{name: "inband FEC", code: C.opus_set_inband_fec_c(encoder.ptr, C.int(1))},
		{name: "packet loss percentage", code: C.opus_set_packet_loss_perc_c(encoder.ptr, C.int(5))},
		{name: "DTX", code: C.opus_set_dtx_c(encoder.ptr, C.int(0))},
		{name: "voice signal", code: C.opus_set_signal_voice_c(encoder.ptr)},
	}
	for _, setting := range settings {
		if setting.code != 0 {
			return fmt.Errorf("configure opus %s: %s", setting.name, opusErrorText(setting.code))
		}
	}
	return nil
}

func (encoder *opusEncoder) Encode(pcm []int16) ([]byte, error) {
	if encoder == nil {
		return nil, fmt.Errorf("opus encoder is nil")
	}
	if len(pcm) == 0 {
		return nil, nil
	}
	if len(pcm)%encoder.channels != 0 {
		return nil, fmt.Errorf("opus encode source sample count must be a multiple of channels")
	}

	packet := make([]byte, opusMaxPacketSize)
	written := int(C.opus_encode(
		encoder.ptr,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)/encoder.channels),
		(*C.uchar)(unsafe.Pointer(&packet[0])),
		C.opus_int32(len(packet)),
	))
	if written < 0 {
		return nil, fmt.Errorf("encode opus: %s", opusErrorText(C.int(written)))
	}

	return packet[:written], nil
}
