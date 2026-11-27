package audio_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plebian/server/audio"
	"plebian/server/clock"
	"plebian/server/speech"
	"plebian/server/store"
	"plebian/server/underlay"
	fspeech "plebian/tests/fixtures/speech"
)

type engine struct{ e *speech.Engine }
type vad struct{ v *speech.VADStream }

func conv(ss []speech.Speech) []audio.Speech {
	out := make([]audio.Speech, len(ss))
	for i, s := range ss {
		out[i] = audio.Speech{Start: s.Start, Samples: s.Samples}
	}
	return out
}
func (v vad) Accept(p []float32) []audio.Speech { return conv(v.v.Accept(p)) }
func (v vad) Flush() []audio.Speech             { return conv(v.v.Flush()) }
func (e engine) NewVAD() audio.VAD              { return vad{e.e.NewVAD()} }
func (e engine) Transcribe(p []float32) string  { return e.e.Transcribe(p) }
func (e engine) Embed(p []float32) []float32    { return e.e.Embed(p) }
func (e engine) Diarize(p []float32) []audio.Turn {
	var out []audio.Turn
	for _, t := range e.e.Diarize(p) {
		out = append(out, audio.Turn{Start: t.Start, End: t.End, Speaker: t.Speaker})
	}
	return out
}

var script = []fspeech.Line{
	{Speaker: "a", Text: "the twenty foot culvert pipe is three forty", PauseMs: 800},
	{Speaker: "b", Text: "have them dump the gravel down by the culvert", PauseMs: 800},
	{Speaker: "a", Text: "so about that gravel delivery I can bring it wednesday", PauseMs: 800},
	{Speaker: "b", Text: "fifteen yards should do it", PauseMs: 800},
}

func node(t *testing.T) (*audio.Node, *store.Store, *[]string) {
	dir := os.Getenv("PLEBIAN_SPEECH_MODELS")
	if dir == "" {
		t.Skip("PLEBIAN_SPEECH_MODELS (a directory with the speech models) is not set")
	}
	eng, err := speech.Load(speech.Models{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, "store.db"), store.DirBlobs{Dir: filepath.Join(root, "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	n := &audio.Node{S: s, Clock: clock.System{}, E: engine{eng}, FrameOf: speech.FrameSamples,
		NewDecoder: func(c int) (audio.Decoder, error) { return speech.NewDecoder(c) },
		Gap:        func(context.Context) time.Duration { return time.Minute },
		Ask:        func(_ context.Context, q string) error { asked = append(asked, q); return nil }}
	return n, s, &asked
}

func feed(t *testing.T, n *audio.Node, frames [][]byte, first uint16, at time.Time) {
	i := 0
	err := n.Live(context.Background(), 21, func() (audio.Frame, error) {
		if i == len(frames) {
			return audio.Frame{}, io.EOF
		}
		f := audio.Frame{Packet: first + uint16(i), Arrival: at, Data: frames[i]}
		i++
		return f, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func rows(t *testing.T, s *store.Store) []underlay.Row {
	r, err := s.DB.Query(`SELECT id FROM heard WHERE id NOT IN (SELECT covers FROM heard WHERE covers IS NOT NULL) ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for r.Next() {
		var id int64
		r.Scan(&id)
		ids = append(ids, id)
	}
	r.Close()
	var out []underlay.Row
	for _, id := range ids {
		b, err := s.HeardBytes(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		row, _ := underlay.Parse(b)
		out = append(out, row)
	}
	return out
}

// Two voices across a u16 packet wrap land as one stretch with two speaker labels;
// heard again in a second stretch, each unnamed voice raises one question.
func TestTwoVoicesAcrossWrap(t *testing.T) {
	n, s, asked := node(t)
	c, _ := fspeech.CodecByID(21)
	frames, _, err := fspeech.Frames(script, c)
	if err != nil {
		t.Fatal(err)
	}
	feed(t, n, frames, 65530, time.Now())
	rs := rows(t, s)
	if len(rs) != 1 {
		t.Fatalf("%d stretches landed", len(rs))
	}
	speakers := map[string]bool{}
	var text []string
	for _, l := range rs[0].Lines {
		speakers[l.Speaker] = true
		text = append(text, strings.ToLower(l.Text))
	}
	all := strings.Join(text, " ")
	t.Logf("lines: %+v", rs[0].Lines)
	if len(speakers) != 2 {
		t.Fatalf("speakers %v", speakers)
	}
	for _, w := range []string{"culvert", "gravel", "wednesday", "yards"} {
		if !strings.Contains(all, w) {
			t.Fatalf("%q not heard in %q", w, all)
		}
	}
	if len(*asked) != 0 {
		t.Fatalf("a voice heard once raised a question: %v", *asked)
	}
	feed(t, n, frames, 100, time.Now().Add(time.Hour))
	rs = rows(t, s)
	if len(rs) != 2 {
		t.Fatalf("%d stretches after the second", len(rs))
	}
	if len(*asked) != 2 {
		t.Fatalf("questions %v", *asked)
	}
	for l := range speakers {
		found := false
		for _, l2 := range rs[1].Lines {
			if l2.Speaker == l {
				found = true
			}
		}
		if !found {
			t.Fatalf("voice %q not carried into the second stretch: %+v", l, rs[1].Lines)
		}
	}
}
