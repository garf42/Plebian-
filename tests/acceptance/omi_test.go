package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Line is one line the speech fixture speaks, in voice "a" or "b".
type Line struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	PauseMs int    `json:"pause_ms"`
}

var talk = []Line{
	{Speaker: "a", Text: "the twenty foot culvert pipe is three forty", PauseMs: 800},
	{Speaker: "b", Text: "have them dump the gravel down by the culvert", PauseMs: 800},
	{Speaker: "a", Text: "so about that gravel delivery I can bring it wednesday", PauseMs: 800},
	{Speaker: "b", Text: "fifteen yards should do it", PauseMs: 800},
}

// Speech asks the speech fixture for Omi codec-21 frames of the lines (20 ms each).
func Speech(t testing.TB, sp *Fixture, lines []Line) [][]byte {
	var r struct {
		Frames  [][]byte `json:"frames"`
		FrameMs int      `json:"frame_ms"`
	}
	if err := json.Unmarshal(post(t, sp.URL("/frames"), map[string]any{"lines": lines, "codec": 21}), &r); err != nil || len(r.Frames) == 0 || r.FrameMs != 20 {
		t.Fatalf("speech fixture: %v (%d frames, %d ms)", err, len(r.Frames), r.FrameMs)
	}
	return r.Frames
}

// Now reads the server's clock (the time fixture).
func (h *H) Now() time.Time {
	var ms int64
	get(h.T, h.Clock.URL("/now"), &ms)
	return time.UnixMilli(ms).UTC()
}

// Live streams frames over /v1/audio/live as the app relays them from the Omi: packet
// numbers from first (wrapping at u16), arrival stamped 20 ms apart from at. It returns
// the speech messages the server sent while the stream was open.
func (h *H) Live(frames [][]byte, first uint16, at time.Time) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+h.Addr+"/v1/audio/live?codec=21&offset_ms=0",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.Token}}})
	if err != nil {
		h.T.Fatalf("audio live: %v", err)
	}
	c.SetReadLimit(1 << 20)
	got := make(chan string, 1024)
	go func() {
		defer close(got)
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				got <- string(b)
			}
		}
	}()
	for i, f := range frames {
		b := make([]byte, 10, 10+len(f))
		binary.LittleEndian.PutUint16(b, first+uint16(i))
		binary.LittleEndian.PutUint64(b[2:], uint64(at.Add(time.Duration(i)*20*time.Millisecond).UnixMilli()))
		if err := c.Write(ctx, websocket.MessageBinary, append(b, f...)); err != nil {
			h.T.Fatalf("audio live frame %d: %v", i, err)
		}
	}
	// Live means before the stream closes: wait for the ribbon's fill, then close.
	var live []string
	deadline := time.After(2 * time.Minute)
wait:
	for {
		select {
		case m, ok := <-got:
			if !ok {
				break wait
			}
			live = append(live, m)
			if strings.Contains(m, `"speech"`) {
				break wait
			}
		case <-deadline:
			break wait
		}
	}
	c.Close(websocket.StatusNormalClosure, "")
	for m := range got {
		live = append(live, m)
	}
	return live
}

// File posts one offline file as the app's storage sync reads it out of the Omi.
func (h *H) File(frames [][]byte, index int, deviceMs, offsetMs int64) (sent, answered string, status int) {
	var body []byte
	for _, f := range frames {
		body = binary.LittleEndian.AppendUint16(body, uint16(len(f)))
		body = append(body, f...)
	}
	sum := sha256.Sum256(body)
	req, _ := http.NewRequest("POST", "http://"+h.Addr+"/v1/audio/files", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("X-Omi-File-Index", fmt.Sprint(index))
	req.Header.Set("X-Omi-File-Timestamp", fmt.Sprint(deviceMs))
	req.Header.Set("X-Omi-Clock-Offset-Ms", fmt.Sprint(offsetMs))
	req.Header.Set("X-Omi-Codec", "21")
	resp, err := h.http.Do(req)
	if err != nil {
		h.T.Fatalf("audio file: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var r struct{ Sha256 string }
	json.Unmarshal(b, &r)
	return hex.EncodeToString(sum[:]), r.Sha256, resp.StatusCode
}

// HeardRow is a heard row as a page shows it.
type HeardRow struct {
	ID     string
	Source string
	At     string
	End    string
	Blob   string
	Lines  []struct{ Speaker, Text, At string }
}

func (r HeardRow) Text() string {
	var s []string
	for _, l := range r.Lines {
		s = append(s, strings.ToLower(l.Text))
	}
	return strings.Join(s, " ")
}

func (r HeardRow) Speakers() map[string]bool {
	out := map[string]bool{}
	for _, l := range r.Lines {
		out[l.Speaker] = true
	}
	return out
}

// PageDate is the page a wall time falls on: pages cut at 4 am in the server's zone.
func PageDate(t time.Time) string {
	loc, _ := time.LoadLocation("America/Chicago")
	return t.In(loc).Add(-4 * time.Hour).Format("2006-01-02")
}

// Heard reads the talk rows on the page holding t.
func (h *H) Heard(t time.Time) []HeardRow {
	var p struct{ Heard []HeardRow }
	h.Do("GET", "/v1/days/"+PageDate(t), nil, &p, true)
	var out []HeardRow
	for _, r := range p.Heard {
		if r.Source == "talk" {
			out = append(out, r)
		}
	}
	return out
}

func init() {
	// A05 (server half; the app half runs on the emulator): two voices land as heard rows
	// with speaker labels, the ribbon fills while the stream is open, a tap is written.
	var s5 *a05
	register(Scenario{
		ID: "A05",
		Setup: func(t testing.TB) *H {
			s5 = &a05{}
			h := InstallSpeech(t)
			h.Fixtures["speech"] = StartFixture(t, "speech")
			return h
		},
		Drive:  func(h *H) { driveA05(h, s5) },
		Assert: func(h *H, c *C) { assertA05(c, s5) },
	})

	// A06 (server half): files recorded out of range sync late and out of order and land
	// at their true wall time; a file sent twice lands once.
	type a06 struct {
		wall         [2]time.Time
		sent, answer [2]string
		status       [2]int
		resent       string
		rows         []HeardRow
	}
	var s6 *a06
	register(Scenario{
		ID: "A06",
		Setup: func(t testing.TB) *H {
			s6 = &a06{}
			h := InstallSpeech(t)
			h.Fixtures["speech"] = StartFixture(t, "speech")
			return h
		},
		Drive: func(h *H) {
			sp := h.Fixtures["speech"]
			f := [2][][]byte{Speech(h.T, sp, talk[:2]), Speech(h.T, sp, talk[2:])}
			const offset = int64(-37 * 60 * 1000) // the Omi's clock runs 37 minutes ahead of the phone's
			s6.wall[0] = h.Now().Add(-3 * time.Hour).Truncate(time.Second)
			s6.wall[1] = s6.wall[0].Add(20 * time.Minute)
			dev := func(i int) int64 { return s6.wall[i].UnixMilli() - offset }
			// The newer file first, then the older one: out of order.
			s6.sent[1], s6.answer[1], s6.status[1] = h.File(f[1], 2, dev(1), offset)
			s6.sent[0], s6.answer[0], s6.status[0] = h.File(f[0], 1, dev(0), offset)
			_, s6.resent, _ = h.File(f[0], 1, dev(0), offset) // the app retries after a lost answer
			Until(3*time.Minute, func() bool { return len(h.Heard(s6.wall[0])) >= 2 })
			time.Sleep(3 * time.Second)
			s6.rows = h.Heard(s6.wall[0])
		},
		Assert: func(h *H, c *C) {
			c.Check("rows_landed", len(s6.rows) == 2, "%d talk rows landed for two files", len(s6.rows))
			c.Check("durable_answer_is_body_sha", s6.status == [2]int{200, 200} && s6.answer == s6.sent, "answers %v for bodies %v (%v)", s6.answer, s6.sent, s6.status)
			c.Check("resend_lands_once", s6.resent == s6.sent[0] && len(s6.rows) == 2, "resent answer %q, %d rows", s6.resent, len(s6.rows))
			ok := len(s6.rows) == 2
			var ats []time.Time
			for _, r := range s6.rows {
				t, err := time.Parse(time.RFC3339Nano, r.At)
				ok = ok && err == nil
				ats = append(ats, t)
			}
			if ok {
				if ats[0].After(ats[1]) {
					ats[0], ats[1] = ats[1], ats[0]
				}
				for i := range ats {
					d := ats[i].Sub(s6.wall[i])
					ok = ok && d >= 0 && d < 2*time.Second // speech starts within the file's first two seconds
				}
			}
			c.Check("true_wall_time", ok, "rows at %v for files recorded at %v", ats, s6.wall)
			if len(s6.rows) == 2 {
				first := s6.rows[0]
				if s6.rows[1].At < first.At {
					first = s6.rows[1]
				}
				c.Check("older_file_holds_older_words", strings.Contains(first.Text(), "culvert"), "the earlier row heard %q", first.Text())
			}
		},
	})

	// A07: a voice heard in a second stretch with no name opens one question; the owner's
	// own-words answer names it; later stretches carry the name.
	type a07 struct {
		questions int
		voice     int
		named     bool
		third     HeardRow
		asked     int
	}
	var s7 *a07
	register(Scenario{
		ID: "A07",
		Setup: func(t testing.TB) *H {
			s7 = &a07{}
			h := InstallSpeech(t)
			h.Fixtures["speech"] = StartFixture(t, "speech")
			h.Model("anthropic", []map[string]any{
				{"match": "has no name", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "ask", "input": map[string]any{"question": "Whose voice is this?"}}}}},
				{"match": "asked", "reply": map[string]any{"text": "Whose voice is this?"}},
			})
			return h
		},
		Drive: func(h *H) {
			sp := h.Fixtures["speech"]
			at := h.Now().Add(-2 * time.Hour)
			h.Live(Speech(h.T, sp, talk), 0, at)
			Until(3*time.Minute, func() bool { return len(h.Heard(at)) == 1 })
			// The second stretch holds one voice only.
			h.Live(Speech(h.T, sp, []Line{talk[0], talk[2]}), 9000, at.Add(30*time.Minute))
			Until(3*time.Minute, func() bool { return len(h.Heard(at)) == 2 })
			Until(90*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
			re := regexp.MustCompile(`has no name: voice (\d+)`)
			for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
				if m := re.FindSubmatch(r.Body); m != nil {
					s7.asked++
					fmt.Sscan(string(m[1]), &s7.voice)
				}
			}
			var open struct {
				Questions []struct{ ID, Text string }
			}
			h.Do("GET", "/v1/open", nil, &open, true)
			s7.questions = len(open.Questions)
			if s7.voice == 0 || len(open.Questions) == 0 {
				return
			}
			// The owner answers in their own words; the model points at them.
			post(h.T, h.Fixtures["model:anthropic"].URL("/control/script"), map[string]any{"steps": []map[string]any{
				{"match": "Dale", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "keep", "input": map[string]any{"words": "Dale", "voice": s7.voice}}}}},
				{"match": "named", "reply": map[string]any{"text": "Dale it is."}},
			}})
			id := fmt.Sprint(time.Now().UnixNano())
			h.Do("POST", "/v1/messages", map[string]any{"client_msg_id": id, "text": "that's Dale, he hauls the gravel", "sent_at": time.Now().UTC().Format(time.RFC3339Nano),
				"quote": map[string]any{"kind": "question", "id": open.Questions[0].ID}}, nil, true)
			h.Advance(2 * time.Minute)
			s7.named = Until(90*time.Second, func() bool {
				for _, m := range AgentMessages(h.Today()) {
					if m == "Dale it is." {
						return true
					}
				}
				return false
			})
			third := at.Add(60 * time.Minute)
			h.Live(Speech(h.T, sp, []Line{talk[0], talk[2]}), 20000, third)
			Until(3*time.Minute, func() bool { return len(h.Heard(at)) == 3 })
			for _, r := range h.Heard(at) {
				if t, err := time.Parse(time.RFC3339Nano, r.At); err == nil && !t.Before(third) {
					s7.third = r
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("one_question", s7.asked == 1 && s7.questions == 1, "the harness asked %d times; %d questions open", s7.asked, s7.questions)
			c.Check("answer_names_voice", s7.named, "the owner's answer did not name voice %d", s7.voice)
			c.Check("later_stretch_carries_name", len(s7.third.Lines) > 0 && s7.third.Speakers()["Dale"] && len(s7.third.Speakers()) == 1, "the third stretch's speakers %v", s7.third.Speakers())
		},
	})

	// A09: a heard fact asked by text is answered with who said it and when: the rows the
	// agent looks up are quoted beside the result with each line's speaker and time.
	type a09 struct {
		reply   []string
		quoted  string
		rowAt   string
		speaker string
	}
	var s9 *a09
	register(Scenario{
		ID: "A09",
		Setup: func(t testing.TB) *H {
			s9 = &a09{}
			h := InstallSpeech(t)
			h.Fixtures["speech"] = StartFixture(t, "speech")
			h.Model("anthropic", []map[string]any{
				{"match": "when is the gravel", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "lookup", "input": map[string]any{"words": "gravel wednesday"}}}}},
				{"match": "(?i)wednesday", "reply": map[string]any{"text": "Wednesday, the speaker said it this morning."}},
			})
			return h
		},
		Drive: func(h *H) {
			at := h.Now().Add(-90 * time.Minute)
			h.Live(Speech(h.T, h.Fixtures["speech"], talk), 0, at)
			Until(3*time.Minute, func() bool { return len(h.Heard(at)) == 1 })
			for _, r := range h.Heard(at) {
				for _, l := range r.Lines {
					if strings.Contains(strings.ToLower(l.Text), "wednesday") {
						s9.rowAt, s9.speaker = l.At, l.Speaker
					}
				}
			}
			h.Say("when is the gravel coming?")
			Until(90*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
			s9.reply = AgentMessages(h.Today())
			for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
				var body map[string]any
				json.Unmarshal(r.Body, &body)
				b, _ := json.Marshal(body)
				for _, ln := range strings.Split(strings.ReplaceAll(string(b), `\n`, "\n"), "\n") {
					if strings.Contains(strings.ToLower(ln), "wednesday") && strings.Contains(ln, " · ") {
						s9.quoted = ln
					}
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("fact_heard", s9.rowAt != "" && s9.speaker != "", "no heard line held the fact")
			c.Check("answered", len(s9.reply) == 1 && strings.HasPrefix(s9.reply[0], "Wednesday"), "agent replied %q", s9.reply)
			c.Check("who_and_when_in_view", s9.quoted != "" && strings.Contains(s9.quoted, s9.speaker+" · "+s9.rowAt), "the request quoted %q; the line is %s at %s", s9.quoted, s9.speaker, s9.rowAt)
		},
	})
}

type a05 struct {
	rows   []HeardRow
	live   []string
	ribbon float64
	tap    map[string]any
	at     time.Time
	page   struct {
		Taps   []map[string]any
		Ribbon struct{ Taps []string }
	}
}

func driveA05(h *H, s5 *a05) {
	at := h.Now().Add(-10 * time.Minute)
	s5.at = at
	s5.live = h.Live(Speech(h.T, h.Fixtures["speech"], talk), 65530, at) // across the u16 wrap
	Until(3*time.Minute, func() bool { return len(h.Heard(at)) > 0 })
	s5.rows = h.Heard(at)
	h.Do("POST", "/v1/taps", map[string]any{"at": at.Add(3 * time.Second).Format(time.RFC3339Nano), "how": "omi"}, &s5.tap, true)
	var p struct {
		Ribbon struct {
			Hours []struct{ Seconds float64 }
		}
	}
	h.Do("GET", "/v1/days/"+PageDate(at), nil, &p, true)
	for _, hr := range p.Ribbon.Hours {
		s5.ribbon += hr.Seconds
	}
	h.Do("GET", "/v1/days/"+PageDate(at), nil, &s5.page, true)
}

func assertA05(c *C, s5 *a05) {
	c.Check("heard_row_landed", len(s5.rows) == 1, "%d talk rows landed", len(s5.rows))
	var r HeardRow
	if len(s5.rows) > 0 {
		r = s5.rows[0]
	}
	c.Check("two_speaker_labels", len(r.Speakers()) == 2, "speakers %v in %q", r.Speakers(), r.Text())
	ok := true
	for _, w := range []string{"culvert", "gravel", "wednesday", "yards"} {
		ok = ok && strings.Contains(r.Text(), w)
	}
	c.Check("words_heard", ok, "heard %q", r.Text())
	live := false
	for _, m := range s5.live {
		live = live || strings.Contains(m, `"speech"`)
	}
	c.Check("ribbon_fills_live", live, "no speech message while the stream was open (%d messages)", len(s5.live))
	c.Check("ribbon_on_page", s5.ribbon > 0, "the page's ribbon holds %v seconds", s5.ribbon)
	c.Check("tap_written", s5.tap["how"] == "omi" && len(s5.page.Taps) == 1 && len(s5.page.Ribbon.Taps) == 1, "tap %v; page taps %d, ribbon taps %d", s5.tap, len(s5.page.Taps), len(s5.page.Ribbon.Taps))
}
