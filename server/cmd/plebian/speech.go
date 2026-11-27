package main

import (
	"plebian/server/audio"
	"plebian/server/speech"
)

// engine adapts the speech stack to the audio node's interface.
type engine struct{ e *speech.Engine }

type vad struct{ v *speech.VADStream }

func conv(ss []speech.Speech) []audio.Speech {
	out := make([]audio.Speech, len(ss))
	for i, s := range ss {
		out[i] = audio.Speech{Start: s.Start, Samples: s.Samples}
	}
	return out
}

func (v vad) Accept(pcm []float32) []audio.Speech { return conv(v.v.Accept(pcm)) }
func (v vad) Flush() []audio.Speech               { return conv(v.v.Flush()) }

func (e engine) NewVAD() audio.VAD               { return vad{e.e.NewVAD()} }
func (e engine) Transcribe(pcm []float32) string { return e.e.Transcribe(pcm) }
func (e engine) Embed(pcm []float32) []float32   { return e.e.Embed(pcm) }
func (e engine) Diarize(pcm []float32) []audio.Turn {
	var out []audio.Turn
	for _, t := range e.e.Diarize(pcm) {
		out = append(out, audio.Turn{Start: t.Start, End: t.End, Speaker: t.Speaker})
	}
	return out
}

func newDecoder(codec int) (audio.Decoder, error) { return speech.NewDecoder(codec) }
