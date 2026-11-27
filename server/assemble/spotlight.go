package assemble

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"plebian/server/grammar"
)

// Heard lights heard lines past chance for a cue (n.underlay); it returns them quoted,
// with who said each and when, for this request only.
type Heard func(ctx context.Context, cue []byte) []byte

// Spotlight is the quoted record beside the inbound, capped at SpotlightShare of B and
// filled in order: open loops; kept orders and strikes waiting for a rebuild; then lit
// lines and heard lines lit past chance (a third each, room heard lines leave going to
// lit lines); use counts ride with lit lines. Every line is a byte-exact span or a count
// the harness computed.
func (a *Assembler) Spotlight(ctx context.Context, model string, B int, inbound, lastWords []byte, inView map[int64]bool, heard Heard, now float64) ([]byte, []int64) {
	capTokens := int(float64(B) * a.P.SpotlightShare)
	var b bytes.Buffer
	var shown []int64
	used := 0
	put := func(s string) bool {
		n := a.Counter.CountText(model, []byte(s))
		if used+n > capTokens {
			return false
		}
		b.WriteString(s)
		used += n
		return true
	}
	// Open loops first: redlines, questions, kept orders.
	rows, err := a.S.DB.QueryContext(ctx, `SELECT id, kind, text, opened_turn FROM loops WHERE closed_turn IS NULL ORDER BY id`)
	if err == nil {
		type loop struct {
			id         int64
			kind, text string
			turn       int64
		}
		var ls []loop
		for rows.Next() {
			var l loop
			rows.Scan(&l.id, &l.kind, &l.text, &l.turn)
			ls = append(ls, l)
		}
		rows.Close()
		for _, l := range ls {
			if !put(fmt.Sprintf("<open kind=\"%s\" turn=\"%d\">%s</open>\n", l.kind, l.turn, l.text)) {
				break
			}
		}
	}
	rest := capTokens - used
	third := rest / 3
	// Lit lines: two beams, half the light each (the inbound, the agent's last words).
	var lit []string
	seen := map[int64]bool{}
	for k, cue := range [][]byte{inbound, lastWords} {
		if len(bytes.TrimSpace(cue)) == 0 {
			continue
		}
		for i, l := range a.R.Lamp(ctx, cue, now, inView) {
			if seen[l.Fragment] || i >= 8 {
				continue
			}
			seen[l.Fragment] = true
			shown = append(shown, l.Fragment)
			lit = append(lit, fmt.Sprintf("<lit beam=\"%d\" turn=\"%d\">%s</lit>\n", k+1, l.Turn, l.Text))
		}
	}
	var heardText []byte
	if heard != nil {
		heardText = heard(ctx, inbound)
	}
	heardBudget := third
	if n := a.Counter.CountText(model, heardText); n < heardBudget {
		heardBudget = n
	}
	litBudget := rest - heardBudget - third/3 // counts ride with lit lines
	lused := 0
	for _, l := range lit {
		n := a.Counter.CountText(model, []byte(l))
		if lused+n > litBudget {
			break
		}
		b.WriteString(l)
		lused += n
	}
	if len(heardText) > 0 && a.Counter.CountText(model, heardText) <= heardBudget {
		b.Write(heardText)
	}
	// Use counts for the inbound's rarest words, in each body apart.
	type wc struct {
		w    string
		info float64
	}
	var ws []wc
	for _, t := range grammar.Tokenize(inbound) {
		if grammar.IsWord(t) {
			if id, ok := a.G.Terminal(string(t)); ok {
				ws = append(ws, wc{string(t), a.R.Info(ctx, id, grammar.Thread)})
			}
		}
	}
	sort.SliceStable(ws, func(i, j int) bool { return ws[i].info > ws[j].info })
	done := map[string]bool{}
	for i := 0; i < len(ws) && len(done) < 3; i++ {
		w := strings.ToLower(ws[i].w)
		if done[w] {
			continue
		}
		done[w] = true
		c := a.R.Counts(ctx, ws[i].w)
		b.WriteString(fmt.Sprintf("<harness kind=\"counts\" word=%q thread=\"%d\" heard=\"%d\" first_turn=\"%d\" last_turn=\"%d\"/>\n", ws[i].w, c.Thread, c.Heard, c.FirstTurn, c.LastTurn))
	}
	if b.Len() == 0 {
		return nil, shown
	}
	return append(append([]byte("<spotlight>\n"), b.Bytes()...), []byte("</spotlight>")...), shown
}
