package assemble

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/models"
	"plebian/server/recall"
	"plebian/server/store"
)

// Assembler keeps the strata (TP-04, TP-05): the tail folds into recent strata only as
// an exchange opens and only when the request would pass B; every fourth merge is a
// promotion that also rewrites the deep strata. Bands are frozen between rebuilds, so
// consecutive requests diverge only at band boundaries.
type Assembler struct {
	S       *store.Store
	G       *grammar.G
	R       *recall.R
	P       core.Params
	Counter *models.Counter
}

// state is what the assembler keeps between turns (settings key "strata").
type state struct {
	Merges int     `json:"merges"`
	Recent []int64 `json:"recent"` // turns folded in the last merges, at most PromotionEvery merges of them
	Deep   []int64 `json:"deep"`   // fragment shortlist for the deep strata
	Bands  struct {
		Deep, Recent string
	} `json:"bands"`
}

func (a *Assembler) load(ctx context.Context) state {
	var st state
	json.Unmarshal([]byte(a.S.Setting(ctx, "strata")), &st)
	return st
}

func (a *Assembler) save(ctx context.Context, st state) {
	b, _ := json.Marshal(st)
	a.S.SetSetting(ctx, "strata", string(b))
}

// Bands returns the current frozen strata bands.
func (a *Assembler) Bands(ctx context.Context) (deep, recent []byte) {
	st := a.load(ctx)
	return []byte(st.Bands.Deep), []byte(st.Bands.Recent)
}

type ranked struct {
	id     int64
	turn   int64
	text   []byte
	writer string
	v      float64
	tokens int
	order  bool
}

// rank scores fragments of the given turns by v = S × L × τ, readings excluded, lines
// lighter than the median line scaled down by their share of it.
func (a *Assembler) rank(ctx context.Context, model string, turns []int64, fragments []int64, now float64) []ranked {
	var out []ranked
	add := func(rows *sql.Rows) {
		defer rows.Close()
		for rows.Next() {
			var id, tn int64
			var start, stop int
			var writer, kind string
			var light float64
			if rows.Scan(&id, &tn, &start, &stop, &writer, &kind, &light) != nil {
				continue
			}
			if kind == core.PartReading || core.Writer(writer).Heard() || writer == string(core.Harness) {
				continue
			}
			b, err := a.S.TurnBytes(ctx, tn)
			if err != nil || stop > len(b) {
				continue
			}
			text := b[start:stop]
			s := a.R.Ink(ctx, text, grammar.Thread, now)
			tau := 0.5
			if writer == string(core.Owner) {
				tau = 1 // the owner's own words are the record of what was said
			} else {
				tau = a.R.Trust(ctx, text)
			}
			v := s * light * tau
			words := len(strings.Fields(string(text)))
			if words < a.P.MedianLineWords {
				v *= float64(words) / float64(a.P.MedianLineWords)
			}
			out = append(out, ranked{id: id, turn: tn, text: text, writer: writer, v: v, tokens: a.Counter.CountText(model, text)})
		}
	}
	for _, t := range turns {
		if rows, err := a.S.DB.QueryContext(ctx, `SELECT id, turn, start, stop, writer, kind, light FROM fragments WHERE turn=?`, t); err == nil {
			add(rows)
		}
	}
	for _, f := range fragments {
		if rows, err := a.S.DB.QueryContext(ctx, `SELECT id, turn, start, stop, writer, kind, light FROM fragments WHERE id=?`, f); err == nil {
			add(rows)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].v != out[j].v {
			return out[i].v > out[j].v
		}
		return out[i].id < out[j].id
	})
	return out
}

// render quotes fragments in turn order: each line a byte-exact span of a sealed turn,
// its marker the harness's own.
func render(rs []ranked) []byte {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].id < rs[j].id })
	var b bytes.Buffer
	for _, r := range rs {
		fmt.Fprintf(&b, "<line turn=\"%d\" who=\"%s\">%s</line>\n", r.turn, r.writer, r.text)
	}
	return b.Bytes()
}

// fill takes ranked fragments until the band's tokens are spent.
func fill(rs []ranked, budget int) []ranked {
	var out []ranked
	used := 0
	for _, r := range rs {
		if used+r.tokens > budget {
			continue
		}
		out = append(out, r)
		used += r.tokens
	}
	return out
}

// keptOrders are the kept orders' own words, first in the deep strata.
func (a *Assembler) keptOrders(ctx context.Context, model string) []ranked {
	rows, err := a.S.DB.QueryContext(ctx, `SELECT id, text, opened_turn FROM loops WHERE kind='order' AND closed_turn IS NULL ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ranked
	for rows.Next() {
		var id, tn int64
		var text string
		rows.Scan(&id, &text, &tn)
		out = append(out, ranked{id: -id, turn: tn, text: []byte(text), writer: "owner", tokens: a.Counter.CountText(model, []byte(text)), order: true})
	}
	return out
}

// Merge folds tail sheets (all but the newest) into the recent strata and rebuilds the
// bands. B is the target model's budget; fixed is what tools, system and the spotlight
// cap take. Returns how many sheets folded.
func (a *Assembler) Merge(ctx context.Context, model string, B, fixed int, tail []int64, now float64) int {
	if len(tail) < 2 {
		return 0
	}
	st := a.load(ctx)
	folded := tail[:len(tail)-1]
	st.Merges++
	st.Recent = append(st.Recent, folded...)
	strata := (B - fixed) / 2
	if strata < 0 {
		strata = 0
	}
	deepBudget := int(float64(strata) * a.P.DeepShare)
	recentBudget := strata - deepBudget
	if st.Merges%a.P.PromotionEvery == 0 || st.Bands.Deep == "" {
		// Promotion: the recent pool's best fragments join the deep shortlist.
		pool := a.rank(ctx, model, st.Recent, st.Deep, now)
		var keep []int64
		for _, r := range fill(pool, deepBudget) {
			keep = append(keep, r.id)
		}
		st.Deep = keep
		orders := a.keptOrders(ctx, model)
		used := 0
		for _, o := range orders {
			used += o.tokens
		}
		deep := append(orders, fill(a.rank(ctx, model, nil, st.Deep, now), int(math.Max(0, float64(deepBudget-used))))...)
		st.Bands.Deep = string(renderOrdersFirst(deep))
		st.Recent = nil
		st.Recent = append(st.Recent, folded...)
	}
	st.Bands.Recent = string(render(fill(a.rank(ctx, model, st.Recent, nil, now), recentBudget)))
	a.save(ctx, st)
	for _, layer := range []string{"deep", "recent"} {
		b := []byte(st.Bands.Deep)
		if layer == "recent" {
			b = []byte(st.Bands.Recent)
		}
		h := core.Hash(b)
		a.S.DB.ExecContext(ctx, `INSERT INTO bands(layer, bytes, sha, turn) VALUES(?,?,?,?)`, layer, b, h[:], int64(now))
	}
	return len(folded)
}

func renderOrdersFirst(rs []ranked) []byte {
	var orders, rest []ranked
	for _, r := range rs {
		if r.order {
			orders = append(orders, r)
		} else {
			rest = append(rest, r)
		}
	}
	var b bytes.Buffer
	for _, o := range orders {
		fmt.Fprintf(&b, "<order turn=\"%d\">%s</order>\n", o.turn, o.text)
	}
	b.Write(render(rest))
	return b.Bytes()
}
