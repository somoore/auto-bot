package main

/*
#cgo pkg-config: opus
#include <opus.h>
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

	return encoder, nil
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
