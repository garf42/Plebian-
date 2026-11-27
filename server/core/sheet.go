// Package core holds the vocabulary every node shares: a sheet's parts, writers and
// their exact byte form. A sheet is the record of one model call (TP-01); its bytes
// never change once sealed.
package core

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Writer is who wrote a span (TP-03 fragments). Heard writers carry the speaker or sender.
type Writer string

const (
	Owner   Writer = "owner"
	Agent   Writer = "agent"
	Tool    Writer = "tool"
	Harness Writer = "harness" // counts and markers the harness computes, marked as its own
)

// HeardWriter is a voice or sender overheard on the underlay.
func HeardWriter(source, who string) Writer { return Writer("heard:" + source + ":" + who) }

func (w Writer) Heard() bool { return strings.HasPrefix(string(w), "heard:") }

// Opener is what opened a turn (TP-02 step 1). The draft rule reads it (AG-05).
type Opener string

const (
	ByOwner    Opener = "owner"    // the owner's messages
	ByTool     Opener = "tool"     // a tool's result
	ByClock    Opener = "clock"    // the clock (Laplace rule)
	ByOrder    Opener = "order"    // a kept order coming due
	ByLanding  Opener = "landing"  // a landing that lit a loop or order
	ByQuestion Opener = "question" // a question code raised
	ByJob      Opener = "job"      // a job's result; keeps the opener of the turn that started it
)

// WordToGoOut reports whether a turn this opener opened may send a read past the
// machine on its own word (AG-05: an owner message, a kept order or a clock turn).
func (o Opener) WordToGoOut() bool { return o == ByOwner || o == ByOrder || o == ByClock }

// Part kinds inside a sheet.
const (
	PartOwner    = "owner"    // one owner message; Meta: client message id
	PartAgent    = "agent"    // the agent's text
	PartCall     = "call"     // a tool call; Meta: "<call id> <tool>", Text: input JSON
	PartReading  = "reading"  // a tool's result; Meta: "<call id> <exit> <class> <outside 0|1>"
	PartLanding  = "landing"  // a landing; Text: lit loop ids and heard row ids, never heard text
	PartClock    = "clock"    // a clock or order turn; Text: what came due
	PartQuestion = "question" // a question code raised; Text: the harness's statement
	PartJob      = "job"      // a job's result; Meta: job id
)

// Part is one block of a sheet. Text is stored raw, so fragment offsets point into it.
type Part struct {
	Kind   string
	Writer Writer
	At     string // RFC 3339 wall time, for addressing only (TP-T15)
	Meta   string
	Text   []byte
}

// Sheet is one model call: what opened it and what the model wrote.
type Sheet struct {
	Opener Opener
	Parts  []Part
}

const magic = "plebian-sheet 1\n"

// Bytes is the sheet's exact byte form: a header line per part giving the text length,
// then the raw text. Deterministic; the hash fixed at seal is over these bytes.
func (s Sheet) Bytes() []byte {
	var b bytes.Buffer
	b.WriteString(magic)
	b.WriteString("opener " + string(s.Opener) + "\n")
	for _, p := range s.Parts {
		fmt.Fprintf(&b, "part %s %s %s %s %d\n", p.Kind, field(string(p.Writer)), field(p.At), field(p.Meta), len(p.Text))
		b.Write(p.Text)
		b.WriteByte('\n')
	}
	b.WriteString("end\n")
	return b.Bytes()
}

// TextOffsets returns, for each part, the byte offset of its text inside Bytes().
func (s Sheet) TextOffsets() []int {
	out := make([]int, len(s.Parts))
	n := len(magic) + len("opener "+string(s.Opener)+"\n")
	for i, p := range s.Parts {
		n += len(fmt.Sprintf("part %s %s %s %s %d\n", p.Kind, field(string(p.Writer)), field(p.At), field(p.Meta), len(p.Text)))
		out[i] = n
		n += len(p.Text) + 1
	}
	return out
}

// field keeps header fields space-free and non-empty, reversibly.
func field(s string) string {
	if s == "" {
		return "-"
	}
	return strings.NewReplacer("%", "%25", " ", "%20", "\n", "%0A", "-", "%2D").Replace(s)
}

func unfield(s string) string {
	if s == "-" {
		return ""
	}
	return strings.NewReplacer("%2D", "-", "%0A", "\n", "%20", " ", "%25", "%").Replace(s)
}

// ParseSheet rebuilds a sheet from its exact bytes.
func ParseSheet(b []byte) (Sheet, error) {
	var s Sheet
	if !bytes.HasPrefix(b, []byte(magic)) {
		return s, errors.New("not a sheet")
	}
	b = b[len(magic):]
	line, rest, ok := bytes.Cut(b, []byte("\n"))
	if !ok || !bytes.HasPrefix(line, []byte("opener ")) {
		return s, errors.New("sheet: no opener")
	}
	s.Opener = Opener(line[len("opener "):])
	b = rest
	for {
		line, rest, ok = bytes.Cut(b, []byte("\n"))
		if !ok {
			return s, errors.New("sheet: truncated")
		}
		if string(line) == "end" {
			if len(rest) != 0 {
				return s, errors.New("sheet: trailing bytes")
			}
			return s, nil
		}
		f := strings.Fields(string(line))
		if len(f) != 6 || f[0] != "part" {
			return s, fmt.Errorf("sheet: bad part header %q", line)
		}
		n, err := strconv.Atoi(f[5])
		if err != nil || n < 0 || n+1 > len(rest) || rest[n] != '\n' {
			return s, errors.New("sheet: bad part length")
		}
		s.Parts = append(s.Parts, Part{Kind: f[1], Writer: Writer(unfield(f[2])), At: unfield(f[3]), Meta: unfield(f[4]), Text: append([]byte(nil), rest[:n]...)})
		b = rest[n+1:]
	}
}

// Hash is the sha256 of exact bytes.
func Hash(b []byte) [32]byte { return sha256.Sum256(b) }
