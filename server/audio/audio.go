// Package audio is n.audio: the Omi's frames become stretches of talk — conversations
// bounded by a pause longer than the usual gap in speech — with lines per speaker, wall
// times and the original (Ogg Opus) in the blob store. Transcription runs as audio
// arrives; a stretch lands when its conversation ends.
package audio

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"plebian/server/clock"
	"plebian/server/store"
	"plebian/server/underlay"
	"plebian/server/watch"
)

// Speech is a detected span of speech, by sample offset from a stream's start.
type Speech struct {
	Start   int
	Samples []float32
}

// Turn is one speaker's span of a buffer.
type Turn struct{ Start, End, Speaker int }

type VAD interface {
	Accept(pcm []float32) []Speech
	Flush() []Speech
}

// Engine is the speech stack (server/speech on the box).
type Engine interface {
	NewVAD() VAD
	Transcribe(pcm []float32) string
	Diarize(pcm []float32) []Turn
	Embed(pcm []float32) []float32
}

type Decoder interface {
	Decode(frame []byte) ([]float32, error)
}

// Rate is 16 kHz, the Omi's and every model's.
const Rate = 16000

// VADSilence is the Silero minimum silence (sherpa-onnx's example): the pause placed
// between speech spans when a stretch is diarized as one buffer.
const VADSilence = Rate / 2

// VADReach is how far back the detector can start a segment: its buffer, two of its
// longest segments (2 × 10 s). Frames older than that, with no stretch open, can't
// belong to one.
const VADReach = 20 * Rate

// MatchThreshold is the cosine similarity at which a voice is one already heard: the
// threshold sherpa-onnx's speaker-identification examples use (gap G014).
const MatchThreshold = 0.6

type Node struct {
	S          *store.Store
	Clock      clock.Clock
	E          Engine
	NewDecoder func(codec int) (Decoder, error)
	FrameOf    func(codec int) (int, error)
	Gap        func(ctx context.Context) time.Duration // the usual gap in speech
	Land       func(ctx context.Context, heardID int64)
	Ask        func(ctx context.Context, text string) error
	Watch      *watch.Watch
	Log        *log.Logger
	mu         sync.Mutex // one stretch closes at a time (voices, counts)
}

type seg struct {
	start, end int
	samples    []float32
	text       string
}

type session struct {
	n        *Node
	codec    int
	fs       int
	wall0    time.Time
	samples  int
	dec      Decoder
	vad      VAD
	frames   [][]byte
	frame0   int // sample offset of frames[0]
	segs     []seg
	last     int // last packet number
	started  bool
	onSpeech func(minute string, seconds float64)
}

func (n *Node) newSession(codec int, wall0 time.Time) (*session, error) {
	if n.E == nil {
		return nil, errors.New("speech models are not installed")
	}
	fs, err := n.FrameOf(codec)
	if err != nil {
		return nil, err
	}
	dec, err := n.NewDecoder(codec)
	if err != nil {
		return nil, err
	}
	return &session{n: n, codec: codec, fs: fs, wall0: wall0, dec: dec, vad: n.E.NewVAD()}, nil
}

func (s *session) wall(sample int) time.Time {
	return s.wall0.Add(time.Duration(sample) * time.Second / Rate)
}

// push decodes one frame (nil conceals a lost one) and handles what the detector finds.
func (s *session) push(ctx context.Context, frame []byte) error {
	pcm, err := s.dec.Decode(frame)
	if err != nil {
		return err
	}
	s.frames = append(s.frames, frame)
	s.samples += len(pcm)
	for _, sp := range s.vad.Accept(pcm) {
		s.speech(ctx, sp)
	}
	gap := int(s.n.Gap(ctx).Seconds() * Rate)
	if len(s.segs) > 0 && s.samples-s.segs[len(s.segs)-1].end > gap {
		return s.close(ctx)
	}
	if len(s.segs) == 0 && s.samples-s.frame0 > 2*VADReach {
		drop := (s.samples - s.frame0 - VADReach) / s.fs
		s.frames = s.frames[drop:]
		s.frame0 += drop * s.fs
	}
	return nil
}

// packet handles a live frame by its u16 packet number: duplicates and late frames are
// dropped, lost frames concealed, so timing holds across the wrap at 65535.
func (s *session) packet(ctx context.Context, num uint16, frame []byte) error {
	if !s.started {
		s.started, s.last = true, int(num)
		return s.push(ctx, frame)
	}
	d := (int(num) - s.last + 65536) % 65536
	if d == 0 || d > 32768 {
		return nil
	}
	for i := 1; i < d; i++ {
		if err := s.push(ctx, nil); err != nil {
			return err
		}
	}
	s.last = int(num)
	return s.push(ctx, frame)
}

func (s *session) speech(ctx context.Context, sp Speech) {
	sg := seg{start: sp.Start, end: sp.Start + len(sp.Samples), samples: sp.Samples}
	sg.text = strings.TrimSpace(s.n.E.Transcribe(sp.Samples))
	s.segs = append(s.segs, sg)
	// Speech seconds per minute go out as audio arrives (the ribbon's live fill).
	for t := sg.start; t < sg.end; {
		w := s.wall(t)
		m := w.Truncate(time.Minute)
		next := t + int(m.Add(time.Minute).Sub(w).Seconds()*Rate)
		if next > sg.end || next <= t {
			next = sg.end
		}
		sec := float64(next-t) / Rate
		minute := m.UTC().Format(time.RFC3339)
		var total float64
		s.n.S.DB.QueryRowContext(ctx, `INSERT INTO speech(minute, seconds) VALUES(?,?) ON CONFLICT(minute) DO UPDATE SET seconds=seconds+excluded.seconds RETURNING seconds`, minute, sec).Scan(&total)
		s.n.S.Emit(ctx, s.n.Clock.Now().UTC().Format(time.RFC3339Nano), "speech", map[string]any{"minute": minute, "seconds": total}, 0)
		if s.onSpeech != nil {
			s.onSpeech(minute, total)
		}
		t = next
	}
}

// end flushes the detector and closes the open stretch.
func (s *session) end(ctx context.Context) error {
	for _, sp := range s.vad.Flush() {
		s.speech(ctx, sp)
	}
	if len(s.segs) == 0 {
		return nil
	}
	return s.close(ctx)
}

// close lands the open stretch: speakers, lines, original, heard row.
func (s *session) close(ctx context.Context) error {
	segs := s.segs
	s.segs = nil
	first, last := segs[0].start, segs[len(segs)-1].end
	// Frames from the one holding the first speech to the one holding the last.
	fi := (first - s.frame0) / s.fs
	li := (last - s.frame0 + s.fs - 1) / s.fs
	if fi < 0 {
		fi = 0
	}
	if li > len(s.frames) {
		li = len(s.frames)
	}
	frames := s.frames[fi:li]
	start := s.wall(s.frame0 + fi*s.fs)
	s.frames = s.frames[li:]
	s.frame0 += li * s.fs
	return s.n.land(ctx, segs, frames, s.fs, start, s.wall)
}

func (n *Node) land(ctx context.Context, segs []seg, frames [][]byte, fs int, start time.Time, wall func(int) time.Time) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Diarize the stretch's speech as one buffer, the spans apart by the detector's silence.
	var buf []float32
	offs := make([]int, len(segs))
	for i, sg := range segs {
		if i > 0 {
			buf = append(buf, make([]float32, VADSilence)...)
		}
		offs[i] = len(buf)
		buf = append(buf, sg.samples...)
	}
	turns := n.E.Diarize(buf)
	who := make([]int, len(segs))
	for i, sg := range segs {
		best, bestOv := 0, -1
		ov := map[int]int{}
		for _, t := range turns {
			a, b := max(t.Start, offs[i]), min(t.End, offs[i]+len(sg.samples))
			if b > a {
				ov[t.Speaker] += b - a
			}
		}
		for sp, o := range ov {
			if o > bestOv || (o == bestOv && sp < best) {
				best, bestOv = sp, o
			}
		}
		who[i] = best
	}
	// One voice per diarized speaker, matched across stretches by embedding.
	voiceOf := map[int]int64{}
	order := []int{}
	for _, w := range who {
		if _, ok := voiceOf[w]; !ok {
			voiceOf[w] = 0
			order = append(order, w)
		}
	}
	for _, sp := range order {
		var pcm []float32
		for i, w := range who {
			if w == sp {
				pcm = append(pcm, segs[i].samples...)
			}
		}
		id, err := n.voice(ctx, n.E.Embed(pcm), voiceOf)
		if err != nil {
			return err
		}
		voiceOf[sp] = id
	}
	labels := map[int64]string{}
	for _, id := range voiceOf {
		labels[id] = n.label(ctx, id)
	}
	var row underlay.Row
	row.Source = "talk"
	for i, sg := range segs {
		lab := labels[voiceOf[who[i]]]
		if sg.text == "" {
			continue
		}
		if k := len(row.Lines); k > 0 && row.Lines[k-1].Speaker == lab {
			row.Lines[k-1].Text += " " + sg.text
			continue
		}
		row.Lines = append(row.Lines, underlay.Line{Speaker: lab, At: wall(sg.start).UTC().Format(time.RFC3339Nano), Text: sg.text})
	}
	if len(row.Lines) == 0 {
		return nil
	}
	ogg := Ogg(frames, fs, uint32(start.UnixMilli()))
	sha, err := n.S.Blobs.Put(ctx, ogg)
	if err != nil {
		return err
	}
	at := wall(segs[0].start).UTC().Format(time.RFC3339Nano)
	end := wall(segs[len(segs)-1].end).UTC().Format(time.RFC3339Nano)
	id, err := n.S.AddHeard(ctx, store.HeardRow{Source: "talk", At: at, End: end, Bytes: row.Bytes(), Blob: sha, Ext: sha, Landed: n.Clock.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	// The ribbon's minutes stay as they were; they are kept with the stretch.
	n.S.DB.ExecContext(ctx, `UPDATE speech SET stretch=? WHERE minute >= ? AND minute <= ? AND stretch IS NULL`, id, start.Truncate(time.Minute).UTC().Format(time.RFC3339), end)
	n.S.DB.ExecContext(ctx, `UPDATE taps SET stretch=? WHERE stretch IS NULL AND at >= ? AND at <= ?`, id, start.UTC().Format(time.RFC3339Nano), end)
	n.S.Emit(ctx, n.Clock.Now().UTC().Format(time.RFC3339Nano), "heard", map[string]any{"id": fmt.Sprint(id), "source": "talk", "at": at, "end": end, "lines": row.Lines, "settled": false, "blob": sha}, 0)
	for _, vid := range voiceOf {
		if err := n.counted(ctx, vid); err != nil {
			return err
		}
	}
	if n.Land != nil {
		n.Land(ctx, id)
	}
	return nil
}

func cosine(a, b []float32) float64 {
	var ab, aa, bb float64
	for i := range a {
		if i >= len(b) {
			break
		}
		ab += float64(a[i]) * float64(b[i])
		aa += float64(a[i]) * float64(a[i])
		bb += float64(b[i]) * float64(b[i])
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return ab / math.Sqrt(aa*bb)
}

func encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decode(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

// voice returns the voice an embedding belongs to, or a new one. Voices already used in
// this stretch are not matched again (two speakers in one stretch are two voices).
func (n *Node) voice(ctx context.Context, emb []float32, taken map[int]int64) (int64, error) {
	used := map[int64]bool{}
	for _, v := range taken {
		used[v] = true
	}
	if len(emb) > 0 {
		rows, err := n.S.DB.QueryContext(ctx, `SELECT id, embedding, stretches FROM voices ORDER BY id`)
		if err != nil {
			return 0, err
		}
		var best int64
		var bestSim float64
		var bestEmb []float32
		var bestN int
		for rows.Next() {
			var id int64
			var b []byte
			var k int
			rows.Scan(&id, &b, &k)
			if used[id] {
				continue
			}
			e := decode(b)
			if s := cosine(emb, e); s >= MatchThreshold && s > bestSim {
				best, bestSim, bestEmb, bestN = id, s, e, k
			}
		}
		rows.Close()
		if best != 0 {
			// The voice's embedding is the mean of the stretches it was heard in.
			for i := range bestEmb {
				if i < len(emb) {
					bestEmb[i] = (bestEmb[i]*float32(bestN) + emb[i]) / float32(bestN+1)
				}
			}
			n.S.DB.ExecContext(ctx, `UPDATE voices SET embedding=? WHERE id=?`, encode(bestEmb), best)
			return best, nil
		}
	}
	res, err := n.S.DB.ExecContext(ctx, `INSERT INTO voices(embedding) VALUES(?)`, encode(emb))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (n *Node) label(ctx context.Context, id int64) string {
	var name string
	n.S.DB.QueryRowContext(ctx, `SELECT name FROM voices WHERE id=?`, id).Scan(&name)
	if name != "" {
		return name
	}
	return fmt.Sprintf("voice %d", id)
}

// counted adds a stretch to a voice; a voice heard in a second stretch with no name
// raises one question (a voice heard once raises none).
func (n *Node) counted(ctx context.Context, id int64) error {
	var k, asked int
	var name string
	if err := n.S.DB.QueryRowContext(ctx, `UPDATE voices SET stretches=stretches+1 WHERE id=? RETURNING stretches, asked, name`, id).Scan(&k, &asked, &name); err != nil {
		return err
	}
	if name == "" && k >= 2 && asked == 0 {
		n.S.DB.ExecContext(ctx, `UPDATE voices SET asked=1 WHERE id=?`, id)
		if n.Ask != nil {
			return n.Ask(ctx, fmt.Sprintf("a voice heard in %d stretches has no name: voice %d (its lines are labelled \"voice %d\")", k, id, id))
		}
	}
	return nil
}

// Name names a voice in the owner's words (a keep call points at them); earlier
// stretches are relabelled as newer readings that cover them.
func (n *Node) Name(ctx context.Context, id int64, words string, owner bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	old := n.label(ctx, id)
	o := 0
	if owner {
		o = 1
	}
	res, err := n.S.DB.ExecContext(ctx, `UPDATE voices SET name=?, owner=? WHERE id=?`, words, o, id)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c != 1 {
		return fmt.Errorf("no voice %d", id)
	}
	rows, err := n.S.DB.QueryContext(ctx, `SELECT id, at, end_at, blob, ext FROM heard WHERE source='talk' AND id NOT IN (SELECT covers FROM heard WHERE covers IS NOT NULL) ORDER BY id`)
	if err != nil {
		return err
	}
	type r struct {
		id                 int64
		at, end, blob, ext string
	}
	var rs []r
	for rows.Next() {
		var x r
		rows.Scan(&x.id, &x.at, &x.end, &x.blob, &x.ext)
		rs = append(rs, x)
	}
	rows.Close()
	for _, x := range rs {
		b, err := n.S.HeardBytes(ctx, x.id)
		if err != nil {
			return err
		}
		row, err := underlay.Parse(b)
		if err != nil {
			return err
		}
		changed := false
		for i := range row.Lines {
			if row.Lines[i].Speaker == old {
				row.Lines[i].Speaker = words
				changed = true
			}
		}
		if !changed {
			continue
		}
		if _, err := n.S.AddHeard(ctx, store.HeardRow{Source: "talk", At: x.at, End: x.end, Bytes: row.Bytes(), Blob: x.blob, Ext: x.ext, Covers: x.id, Landed: n.Clock.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			return err
		}
	}
	return nil
}

// File lands one offline file: frames as the app read them from the Omi (u16 length +
// frame each), at device time plus the offset taken at connect.
func (n *Node) File(ctx context.Context, body []byte, codec int, deviceMs, offsetMs int64) (string, error) {
	var frames [][]byte
	for b := body; len(b) > 0; {
		if len(b) < 2 {
			return "", errors.New("offline file: truncated length")
		}
		k := int(binary.LittleEndian.Uint16(b))
		if len(b) < 2+k {
			return "", errors.New("offline file: truncated frame")
		}
		frames = append(frames, b[2:2+k])
		b = b[2+k:]
	}
	sha, err := n.S.Blobs.Put(ctx, body) // durable before the acknowledgement
	if err != nil {
		return "", err
	}
	var seen int
	n.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audio_files WHERE sha=?`, sha).Scan(&seen)
	if seen > 0 {
		return sha, nil // already landed: the app repeats an upload it got no answer for
	}
	s, err := n.newSession(codec, time.UnixMilli(deviceMs+offsetMs).UTC())
	if err != nil {
		return sha, err
	}
	for _, f := range frames {
		if err := s.push(ctx, f); err != nil {
			return sha, err
		}
	}
	if err := s.end(ctx); err != nil {
		return sha, err
	}
	n.S.DB.ExecContext(ctx, `INSERT OR IGNORE INTO audio_files(sha, device_ms, offset_ms, landed_at) VALUES(?,?,?,?)`, sha, deviceMs, offsetMs, n.Clock.Now().UTC().Format(time.RFC3339Nano))
	if n.Watch != nil {
		n.Watch.Arrived(ctx, n.Clock.Now())
	}
	return sha, nil
}

// Frame is one live frame as the app forwards it.
type Frame struct {
	Packet  uint16
	Arrival time.Time
	Data    []byte
}

// ReadFrame reads one binary WebSocket message: u16 packet, i64 arrival ms, payload.
func ReadFrame(b []byte) (Frame, error) {
	if len(b) < 10 {
		return Frame{}, errors.New("frame too short")
	}
	return Frame{Packet: binary.LittleEndian.Uint16(b), Arrival: time.UnixMilli(int64(binary.LittleEndian.Uint64(b[2:10]))).UTC(), Data: append([]byte(nil), b[10:]...)}, nil
}

// Live runs one live stream until next returns io.EOF; the open stretch closes then.
func (n *Node) Live(ctx context.Context, codec int, next func() (Frame, error), onSpeech func(minute string, seconds float64)) error {
	var s *session
	for {
		f, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if errors.Is(err, errSkip) {
			continue
		}
		if err != nil {
			if s != nil {
				s.end(ctx)
			}
			return err
		}
		if s == nil {
			s, err = n.newSession(codec, f.Arrival)
			if err != nil {
				return err
			}
			s.onSpeech = onSpeech
		}
		if n.Watch != nil {
			n.Watch.Arrived(ctx, n.Clock.Now())
		}
		if err := s.packet(ctx, f.Packet, f.Data); err != nil {
			s.end(ctx)
			return err
		}
	}
	if s != nil {
		return s.end(ctx)
	}
	return nil
}

// OmiState keeps the app's report of the Omi and passes it to the app as an event.
func (n *Node) OmiState(ctx context.Context, body []byte) error {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return err
	}
	n.S.SetSetting(ctx, "omi_state", string(body))
	state := "out_of_range"
	if c, _ := v["connected"].(bool); c {
		state = "live"
		if w, _ := v["waiting_files"].(float64); w > 0 {
			state = "syncing"
		}
	}
	v["state"] = state
	_, err := n.S.Emit(ctx, n.Clock.Now().UTC().Format(time.RFC3339Nano), "omi", v, 0)
	return err
}
