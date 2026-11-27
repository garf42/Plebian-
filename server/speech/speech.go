// Package speech is the box's speech stack (D6): libopus to decode the Omi's frames,
// sherpa-onnx for voice activity (Silero), transcription (Parakeet-TDT 0.6B int8),
// speaker segmentation (pyannote 3.0) and speaker embeddings (TitaNet-small).
package speech

/*
#cgo CFLAGS: -I/usr/include/opus
#cgo LDFLAGS: -l:libopus.a -lm
#include <opus.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

// Rate is the sample rate every model here takes, and the Omi's.
const Rate = 16000

// FrameSamples is samples per Opus frame by the Omi's codec id: 21 is opusFS320
// (320 samples, 20 ms), 20 the older 160-sample frames (Omi app, bt_device.dart).
func FrameSamples(codec int) (int, error) {
	switch codec {
	case 21:
		return 320, nil
	case 20:
		return 160, nil
	}
	return 0, fmt.Errorf("codec %d is not Opus", codec)
}

// Decoder decodes one stream's Opus frames to 16 kHz mono float samples.
type Decoder struct {
	st    *C.OpusDecoder
	frame int
}

func NewDecoder(codec int) (*Decoder, error) {
	n, err := FrameSamples(codec)
	if err != nil {
		return nil, err
	}
	var e C.int
	st := C.opus_decoder_create(Rate, 1, &e)
	if e != C.OPUS_OK {
		return nil, fmt.Errorf("opus_decoder_create: %d", int(e))
	}
	d := &Decoder{st: st, frame: n}
	runtime.SetFinalizer(d, func(d *Decoder) { C.opus_decoder_destroy(d.st) })
	return d, nil
}

// Decode decodes one frame; a nil frame asks the decoder to conceal a lost one.
func (d *Decoder) Decode(frame []byte) ([]float32, error) {
	out := make([]float32, d.frame*6) // the largest Opus frame (120 ms) at 16 kHz
	var p *C.uchar
	if len(frame) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&frame[0]))
	}
	n := C.opus_decode_float(d.st, p, C.opus_int32(len(frame)), (*C.float)(unsafe.Pointer(&out[0])), C.int(d.frame), 0)
	if n < 0 {
		return nil, fmt.Errorf("opus_decode: %d", int(n))
	}
	return out[:int(n)], nil
}

// Models are the files under <root>/speech.
type Models struct{ Dir string }

func (m Models) p(parts ...string) string { return filepath.Join(append([]string{m.Dir}, parts...)...) }

const parakeet = "sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8"

// Present reports whether every model file is in place.
func (m Models) Present() error {
	for _, f := range []string{m.p("silero_vad.onnx"), m.p("nemo_en_titanet_small.onnx"), m.p("sherpa-onnx-pyannote-segmentation-3-0", "model.onnx"),
		m.p(parakeet, "encoder.int8.onnx"), m.p(parakeet, "decoder.int8.onnx"), m.p(parakeet, "joiner.int8.onnx"), m.p(parakeet, "tokens.txt")} {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("speech model missing: %s", f)
		}
	}
	return nil
}

// Engine holds the loaded models. Each method is safe for one caller at a time; the
// audio node serializes calls.
type Engine struct {
	mu   sync.Mutex
	m    Models
	asr  *sherpa.OfflineRecognizer
	dz   *sherpa.OfflineSpeakerDiarization
	emb  *sherpa.SpeakerEmbeddingExtractor
	vadc sherpa.VadModelConfig
}

// Threads is the CPU threads each model may use: every core the box has.
var Threads = runtime.NumCPU()

func Load(m Models) (*Engine, error) {
	if err := m.Present(); err != nil {
		return nil, err
	}
	e := &Engine{m: m}
	rc := sherpa.OfflineRecognizerConfig{
		FeatConfig:     sherpa.FeatureConfig{SampleRate: Rate, FeatureDim: 80},
		DecodingMethod: "greedy_search",
	}
	rc.ModelConfig.Transducer = sherpa.OfflineTransducerModelConfig{Encoder: m.p(parakeet, "encoder.int8.onnx"), Decoder: m.p(parakeet, "decoder.int8.onnx"), Joiner: m.p(parakeet, "joiner.int8.onnx")}
	rc.ModelConfig.Tokens = m.p(parakeet, "tokens.txt")
	rc.ModelConfig.ModelType = "nemo_transducer"
	rc.ModelConfig.NumThreads = Threads
	if e.asr = sherpa.NewOfflineRecognizer(&rc); e.asr == nil {
		return nil, errors.New("speech: recognizer did not load")
	}
	// Clustering threshold 0.7: the probe's (two voices found, identical across runs).
	dc := sherpa.OfflineSpeakerDiarizationConfig{
		Segmentation: sherpa.OfflineSpeakerSegmentationModelConfig{Pyannote: sherpa.OfflineSpeakerSegmentationPyannoteModelConfig{Model: m.p("sherpa-onnx-pyannote-segmentation-3-0", "model.onnx")}, NumThreads: Threads},
		Embedding:    sherpa.SpeakerEmbeddingExtractorConfig{Model: m.p("nemo_en_titanet_small.onnx"), NumThreads: Threads},
		Clustering:   sherpa.FastClusteringConfig{NumClusters: -1, Threshold: 0.7},
	}
	if e.dz = sherpa.NewOfflineSpeakerDiarization(&dc); e.dz == nil {
		return nil, errors.New("speech: diarization did not load")
	}
	if e.emb = sherpa.NewSpeakerEmbeddingExtractor(&sherpa.SpeakerEmbeddingExtractorConfig{Model: m.p("nemo_en_titanet_small.onnx"), NumThreads: Threads}); e.emb == nil {
		return nil, errors.New("speech: embedding extractor did not load")
	}
	// Silero settings of sherpa-onnx's own VAD example.
	e.vadc = sherpa.VadModelConfig{SampleRate: Rate, NumThreads: 1}
	e.vadc.SileroVad = sherpa.SileroVadModelConfig{Model: m.p("silero_vad.onnx"), Threshold: 0.5, MinSilenceDuration: 0.5, MinSpeechDuration: 0.25, MaxSpeechDuration: 10, WindowSize: 512}
	return e, nil
}

// Segment is a span of speech in a buffer, by sample offsets.
type Segment struct {
	Start, End int
}

// VAD finds the speech in pcm. shortcut: runs over a whole buffer at a time; a streaming
// detector per live session replaces it if a stretch grows past what memory holds.
func (e *Engine) VAD(pcm []float32) []Segment {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := sherpa.NewVoiceActivityDetector(&e.vadc, float32(len(pcm))/Rate+1)
	defer sherpa.DeleteVoiceActivityDetector(v)
	var out []Segment
	w := e.vadc.SileroVad.WindowSize
	drain := func() {
		for !v.IsEmpty() {
			s := v.Front()
			out = append(out, Segment{Start: s.Start, End: s.Start + len(s.Samples)})
			v.Pop()
		}
	}
	for i := 0; i+w <= len(pcm); i += w {
		v.AcceptWaveform(pcm[i : i+w])
		drain()
	}
	v.Flush()
	drain()
	return out
}

// Transcribe returns the words in pcm.
func (e *Engine) Transcribe(pcm []float32) string {
	if len(pcm) == 0 {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s := sherpa.NewOfflineStream(e.asr)
	defer sherpa.DeleteOfflineStream(s)
	s.AcceptWaveform(Rate, pcm)
	e.asr.Decode(s)
	return s.GetResult().Text
}

// Turn is one speaker's span within a stretch, by sample offsets.
type Turn struct {
	Start, End int
	Speaker    int
}

// Diarize splits pcm by speaker.
func (e *Engine) Diarize(pcm []float32) []Turn {
	if len(pcm) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Turn
	for _, s := range e.dz.Process(pcm) {
		out = append(out, Turn{Start: int(s.Start * Rate), End: int(s.End * Rate), Speaker: s.Speaker})
	}
	return out
}

// Embed returns a speaker embedding of pcm, or nil when it is too short to give one.
func (e *Engine) Embed(pcm []float32) []float32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.emb.CreateStream()
	defer sherpa.DeleteOnlineStream(s)
	s.AcceptWaveform(Rate, pcm)
	s.InputFinished()
	if !e.emb.IsReady(s) {
		return nil
	}
	return e.emb.Compute(s)
}

// VADStream finds speech in a live stream as samples arrive.
type VADStream struct {
	e   *Engine
	v   *sherpa.VoiceActivityDetector
	w   int
	buf []float32
}

// NewVAD opens a streaming detector. Its buffer holds two of the longest segments the
// detector emits (MaxSpeechDuration), so a forced cut never overflows it.
func (e *Engine) NewVAD() *VADStream {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := sherpa.NewVoiceActivityDetector(&e.vadc, 2*e.vadc.SileroVad.MaxSpeechDuration)
	s := &VADStream{e: e, v: v, w: e.vadc.SileroVad.WindowSize}
	runtime.SetFinalizer(s, func(s *VADStream) { sherpa.DeleteVoiceActivityDetector(s.v) })
	return s
}

// Accept feeds samples and returns the speech segments that ended, with their samples;
// offsets count from the first sample the stream accepted.
func (s *VADStream) Accept(pcm []float32) []Speech {
	s.e.mu.Lock()
	defer s.e.mu.Unlock()
	s.buf = append(s.buf, pcm...)
	for len(s.buf) >= s.w {
		s.v.AcceptWaveform(s.buf[:s.w])
		s.buf = s.buf[s.w:]
	}
	return s.drain()
}

// Flush ends the stream and returns what speech remained.
func (s *VADStream) Flush() []Speech {
	s.e.mu.Lock()
	defer s.e.mu.Unlock()
	s.v.Flush()
	return s.drain()
}

func (s *VADStream) drain() []Speech {
	var out []Speech
	for !s.v.IsEmpty() {
		f := s.v.Front()
		out = append(out, Speech{Start: f.Start, Samples: append([]float32(nil), f.Samples...)})
		s.v.Pop()
	}
	return out
}

// Speech is one detected segment.
type Speech struct {
	Start   int
	Samples []float32
}
