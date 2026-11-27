package speech

import (
	"bytes"
	"testing"
)

// Script is the probe's exchange: two voices, pauses between turns.
var Script = []Line{
	{"a", "the twenty foot culvert pipe is three forty and the thirty foot is four ninety", 800},
	{"b", "have them dump the gravel down by the culvert not at the top of the drive", 800},
}

// Kill test for the demon "the speech fixture differs between runs" (ledger L0005's clause).
func TestSpeechFixtureDeterministic(t *testing.T) {
	for _, c := range []Codec{Codec21, Codec20} {
		f1, s1, err := Frames(Script, c)
		if err != nil {
			t.Fatal(err)
		}
		f2, s2, err := Frames(Script, c)
		if err != nil {
			t.Fatal(err)
		}
		if len(f1) == 0 || len(f1) != len(f2) || len(s1) != len(s2) {
			t.Fatalf("codec %d: %d vs %d frames", c.ID, len(f1), len(f2))
		}
		for i := range f1 {
			if !bytes.Equal(f1[i], f2[i]) {
				t.Fatalf("codec %d: frame %d differs between runs", c.ID, i)
			}
		}
		for i := range s1 {
			if s1[i] != s2[i] {
				t.Fatalf("segment %d differs: %+v %+v", i, s1[i], s2[i])
			}
		}
	}
}

func TestFramesDecodeAtTheOmiShape(t *testing.T) {
	pcm, segs, err := Synthesize(Script)
	if err != nil {
		t.Fatal(err)
	}
	if segs[1].StartMs < segs[0].EndMs+800 || segs[1].Speaker != "b" {
		t.Fatalf("segments %+v", segs)
	}
	for _, c := range []Codec{Codec21, Codec20} {
		frames, err := Encode(pcm, c)
		if err != nil {
			t.Fatal(err)
		}
		if want := (len(pcm) + c.FrameSamples - 1) / c.FrameSamples; len(frames) != want {
			t.Fatalf("codec %d: %d frames, want %d", c.ID, len(frames), want)
		}
		for i, f := range frames {
			if len(f) == 0 || len(f) > c.MaxBytes {
				t.Fatalf("codec %d frame %d: %d bytes", c.ID, i, len(f))
			}
		}
		out, err := Decode(frames, c)
		if err != nil || len(out) != len(frames)*c.FrameSamples {
			t.Fatalf("codec %d decode: %v", c.ID, err)
		}
	}
	w := WAV(pcm)
	back, rate, err := parseWAV(w)
	if err != nil || rate != SampleRate || len(back) != len(pcm) {
		t.Fatalf("wav round trip: %v %d", err, rate)
	}
}
