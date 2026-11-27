package acceptance

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func init() {
	type a13 struct {
		clockBefore, clockAfter int
		entries                 []map[string]any
		heldAfter               bool
		lateOnDay, lateOnNext   bool
		lateText                string
	}
	var st *a13
	clockRequests := func(h *H) int {
		n := 0
		for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
			if strings.Contains(string(r.Body), `the owner asked for \"entry\"`) {
				n++
			}
		}
		return n
	}
	register(Scenario{
		ID: "A13",
		Setup: func(t testing.TB) *H {
			st = &a13{}
			h := fresh(t)
			entry := map[string]any{"tool_calls": []map[string]any{{"name": "entry", "input": map[string]any{"lines": []map[string]any{{"text": "Dale brings the gravel Wednesday."}}}}}}
			h.Model("anthropic", []map[string]any{
				{"match": "write today's entry", "reply": entry}, {"match": "held until", "reply": map[string]any{"text": "Written."}},
				{"match": "write today's entry", "reply": entry}, {"match": "held until", "reply": map[string]any{"text": "Written."}},
				{"match": "the owner asked for", "reply": entry}, {"match": "held until", "reply": map[string]any{"text": "Written."}},
				{"match": "Wednesday is right", "reply": map[string]any{"text": "Settled."}},
				{"match": "late note", "reply": map[string]any{"text": "Noted."}},
			})
			return h
		},
		Drive: func(h *H) {
			written := func(n int) bool {
				return Until(90*time.Second, func() bool {
					c := 0
					for _, m := range AgentMessages(h.Today()) {
						if m == "Written." {
							c++
						}
					}
					return c >= n
				})
			}
			t0 := h.Now()
			h.Say("write today's entry")
			written(1)
			h.Advance(t0.Add(24 * time.Hour).Sub(h.Now()))
			h.Say("write today's entry")
			written(1)
			h.Advance(t0.Add(47 * time.Hour).Sub(h.Now())) // the third night, an hour before
			time.Sleep(3 * time.Second)
			st.clockBefore = clockRequests(h)
			h.Advance(t0.Add(48*time.Hour + 5*time.Minute).Sub(h.Now()))
			Until(90*time.Second, func() bool { return clockRequests(h) > 0 })
			written(1)
			night3 := h.Now()
			var p struct{ Entry []map[string]any }
			h.Do("GET", "/v1/days/"+PageDate(night3), nil, &p, true)
			st.entries = p.Entry
			if len(p.Entry) > 0 {
				id := fmt.Sprint(p.Entry[0]["id"])
				h.Do("POST", "/v1/messages", map[string]any{"client_msg_id": "q-" + id, "text": "yes, Wednesday is right", "sent_at": time.Now().UTC().Format(time.RFC3339Nano),
					"quote": map[string]any{"kind": "line", "id": id}}, nil, true)
				h.Advance(2 * time.Minute)
				Until(60*time.Second, func() bool {
					h.Do("GET", "/v1/days/"+PageDate(night3), nil, &p, true)
					return len(p.Entry) > 0 && p.Entry[0]["held"] == false
				})
				st.heldAfter = len(p.Entry) > 0 && p.Entry[0]["held"] == true
			}
			st.clockAfter = clockRequests(h)
			// Past midnight, before 4 am: the message stays with its day.
			loc, _ := time.LoadLocation("America/Chicago")
			n := night3.In(loc)
			late := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc).Add(26 * time.Hour) // 02:00 the next morning
			if n.Hour() < 4 {
				late = late.Add(-24 * time.Hour)
			}
			h.Advance(late.Sub(h.Now()))
			st.lateText = fmt.Sprintf("late note %d", late.Unix())
			h.Say(st.lateText)
			day := PageDate(late)
			has := func(date string) bool {
				var q struct{ Thread []map[string]any }
				h.Do("GET", "/v1/days/"+date, nil, &q, true)
				for _, e := range q.Thread {
					if e["text"] == st.lateText {
						return true
					}
				}
				return false
			}
			st.lateOnDay = has(day) && day == n.Format("2006-01-02")
			st.lateOnNext = has(late.In(loc).Format("2006-01-02"))
		},
		Assert: func(h *H, c *C) {
			c.Check("entry_written_by_clock", st.clockAfter >= 1 && len(st.entries) == 1, "clock requests %d; third night's entry lines %d", st.clockAfter, len(st.entries))
			c.Check("none_opened_before", st.clockBefore == 0, "the clock opened %d turns before the third night's hour", st.clockBefore)
			held := len(st.entries) == 1 && st.entries[0]["held"] == true
			c.Check("entry_held_until_answered", held && !st.heldAfter, "held when written %v; held after the owner answered it %v", held, st.heldAfter)
			c.Check("page_cut_at_4am", st.lateOnDay && !st.lateOnNext, "a 2 am message on its day %v, on the next %v", st.lateOnDay, st.lateOnNext)
		},
	})
}

func TestA13(t *testing.T) { Run(t, "A13") }
