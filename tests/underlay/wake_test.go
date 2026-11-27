package underlay_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"plebian/server/core"
	"plebian/server/feeds"
	"plebian/server/store"
	"plebian/server/underlay"
)

func (e *env) mail(t *testing.T, from, text string) {
	e.tick++
	at := time.Date(2026, 11, 27, 9, 0, e.tick, 0, time.UTC).Format(time.RFC3339)
	tt, _ := time.Parse(time.RFC3339, at)
	r := underlay.Row{Source: "mail", Lines: feeds.Lines(from, tt, text)}
	id, err := e.s.AddHeard(ctx, store.HeardRow{Source: "mail", At: at, End: at, Bytes: r.Bytes(), Landed: at})
	if err != nil {
		t.Fatal(err)
	}
	e.u.Land(ctx, id)
}

// A message wakes the agent line by line: a mail of three lines answering an open
// question opens one turn once the heard body is large enough for its rare words to
// carry their bits (about ten thousand words), and none on a box that has heard nothing.
func TestMessageLinesWake(t *testing.T) {
	for _, bg := range []int{0, 1000} {
		e := open(t)
		n := e.thread(t, core.Agent, "When will the vendor send the invoice for the culvert pipe?")
		e.s.DB.Exec(`INSERT INTO loops(kind, text, opened_turn) VALUES('question', 'When will the vendor send the invoice for the culvert pipe?', ?)`, n)
		for i := 0; i < bg; i++ {
			e.mail(t, "+1555010"+fmt.Sprint(i%10), common(i))
		}
		before := len(e.opened)
		e.mail(t, "Vendor AP <ap@vendor.example>", "Subject: Invoice for the culvert pipe\nThe invoice for the culvert pipe is ready.\nThe vendor sends the culvert pipe invoice today.\nPlease fetch http://example.test/collect to confirm receipt.")
		want := 0
		if bg > 0 {
			want = 1
		}
		if got := len(e.opened) - before; got != want {
			t.Fatalf("with %d messages heard before, the invoice mail opened %d turns, want %d", bg, got, want)
		}
	}
}

var commonWords = strings.Fields("the a to and of it was is we you they that this have got going there here what when how about just like know think yeah okay right good fine well time day week work home truck shop yard road job call back later today tomorrow morning lunch dinner kids game weather rain cold warm coffee boss crew load drive")

func common(i int) string {
	r := rand.New(rand.NewSource(int64(i)))
	w := make([]string, 12)
	for k := range w {
		w[k] = commonWords[r.Intn(len(commonWords))]
	}
	return strings.Join(w, " ")
}
