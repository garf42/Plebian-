package speech

/*
#cgo pkg-config: opus
#include <opus.h>

// The Omi firmware's encoder setup, call for call (omi/firmware/omi/src/lib/core/codec.c, codec_start).
// opus_encoder_ctl is variadic, which cgo cannot call, hence this wrapper.
static int omi_encoder_setup(OpusEncoder *e) {
	int err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_BITRATE(32000))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_VBR(1))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_VBR_CONSTRAINT(0))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(3))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_LSB_DEPTH(16))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_DTX(0))) != OPUS_OK) return err;
	if ((err = opus_encoder_ctl(e, OPUS_SET_INBAND_FEC(0))) != OPUS_OK) return err;
	return opus_encoder_ctl(e, OPUS_SET_PACKET_LOSS_PERC(0));
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Codec is one of the Omi's Opus wire formats, by its codec id.
type Codec struct {
	ID           int
	FrameSamples int // PCM samples per frame at 16 kHz
	MaxBytes     int // the firmware's output buffer, the most one frame can take
}

// Codecs the Omi app models: 21 (current firmware, config.h CODEC_PACKAGE_SAMPLES
// 320, CODEC_OUTPUT_MAX_BYTES 160) and 20 (the older 160-sample, 80-byte frames).
var (
	Codec21 = Codec{ID: 21, FrameSamples: 320, MaxBytes: 160}
	Codec20 = Codec{ID: 20, FrameSamples: 160, MaxBytes: 80}
)

// CodecByID returns the codec with that id.
func CodecByID(id int) (Codec, error) {
	switch id {
	case 21:
		return Codec21, nil
	case 20:
		return Codec20, nil
	}
	return Codec{}, fmt.Errorf("codec %d: only 20 and 21 are Opus on the Omi", id)
}

// SampleRate is the Omi's microphone rate.
const SampleRate = 16000

// FrameMs is one frame's duration.
func (c Codec) FrameMs() int { return c.FrameSamples * 1000 / SampleRate }

// Encode turns 16 kHz mono PCM into Opus frames as the firmware encodes them:
// OPUS_APPLICATION_RESTRICTED_LOWDELAY, 32 kbps VBR, complexity 3. A final
// partial frame is completed with silence, as the microphone keeps recording.
func Encode(pcm []int16, c Codec) ([][]byte, error) {
	var cerr C.int
	enc := C.opus_encoder_create(SampleRate, 1, C.OPUS_APPLICATION_RESTRICTED_LOWDELAY, &cerr)
	if cerr != C.OPUS_OK {
		return nil, fmt.Errorf("opus_encoder_create: %d", int(cerr))
	}
	defer C.opus_encoder_destroy(enc)
	if e := C.omi_encoder_setup(enc); e != C.OPUS_OK {
		return nil, fmt.Errorf("opus_encoder_ctl: %d", int(e))
	}
	if rem := len(pcm) % c.FrameSamples; rem != 0 {
		pcm = append(append([]int16{}, pcm...), make([]int16, c.FrameSamples-rem)...)
	}
	out := make([]byte, c.MaxBytes)
	var frames [][]byte
	for off := 0; off < len(pcm); off += c.FrameSamples {
		n := C.opus_encode(enc, (*C.opus_int16)(unsafe.Pointer(&pcm[off])), C.int(c.FrameSamples),
			(*C.uchar)(unsafe.Pointer(&out[0])), C.opus_int32(len(out)))
		if n < 0 {
			return nil, fmt.Errorf("opus_encode: %d", int(n))
		}
		frames = append(frames, append([]byte{}, out[:n]...))
	}
	return frames, nil
}

// Decode turns frames back into 16 kHz mono PCM, failing on any frame that is
// not valid Opus of the codec's frame length.
func Decode(frames [][]byte, c Codec) ([]int16, error) {
	var cerr C.int
	dec := C.opus_decoder_create(SampleRate, 1, &cerr)
	if cerr != C.OPUS_OK {
		return nil, fmt.Errorf("opus_decoder_create: %d", int(cerr))
	}
	defer C.opus_decoder_destroy(dec)
	buf := make([]int16, c.FrameSamples)
	var pcm []int16
	for i, f := range frames {
		if len(f) == 0 {
			return nil, fmt.Errorf("frame %d is empty", i)
		}
		n := C.opus_decode(dec, (*C.uchar)(unsafe.Pointer(&f[0])), C.opus_int32(len(f)),
			(*C.opus_int16)(unsafe.Pointer(&buf[0])), C.int(c.FrameSamples), 0)
		if int(n) != c.FrameSamples {
			return nil, fmt.Errorf("frame %d decoded to %d samples", i, int(n))
		}
		pcm = append(pcm, buf...)
	}
	return pcm, nil
}
