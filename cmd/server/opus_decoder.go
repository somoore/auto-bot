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

type opusDecoder struct {
	ptr      *C.OpusDecoder
	mem      []byte
	channels int
}

func newOpusDecoder(sampleRate int, channels int) (*opusDecoder, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported opus channel count %d", channels)
	}

	size := C.opus_decoder_get_size(C.int(channels))
	mem := make([]byte, int(size))
	decoder := &opusDecoder{
		ptr:      (*C.OpusDecoder)(unsafe.Pointer(&mem[0])),
		mem:      mem,
		channels: channels,
	}

	if code := C.opus_decoder_init(decoder.ptr, C.opus_int32(sampleRate), C.int(channels)); code != 0 {
		return nil, fmt.Errorf("init opus decoder: %s", opusErrorText(code))
	}

	return decoder, nil
}

func (decoder *opusDecoder) Decode(data []byte, pcm []int16) (int, error) {
	if decoder == nil {
		return 0, fmt.Errorf("opus decoder is nil")
	}
	if len(data) == 0 {
		return 0, nil
	}
	if len(pcm) == 0 {
		return 0, fmt.Errorf("opus decode target buffer is empty")
	}
	if cap(pcm)%decoder.channels != 0 {
		return 0, fmt.Errorf("opus decode target buffer capacity must be a multiple of channels")
	}

	samplesPerChannel := int(C.opus_decode(
		decoder.ptr,
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.opus_int32(len(data)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(cap(pcm)/decoder.channels),
		0,
	))
	if samplesPerChannel < 0 {
		return 0, fmt.Errorf("decode opus: %s", opusErrorText(C.int(samplesPerChannel)))
	}

	return samplesPerChannel, nil
}

func opusErrorText(code C.int) string {
	return C.GoString(C.opus_strerror(code))
}
