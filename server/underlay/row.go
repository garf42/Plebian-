// Package underlay is n.underlay: heard rows (one per stretch of talk, message or
// calendar entry, exact and hashed), landings, lookup by row id, and taps.
package underlay

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Line is one speaker's or sender's words as transcribed or received.
type Line struct {
	Speaker string `json:"speaker"`
	At      string `json:"at"`
	Text    string `json:"text"`
}

// Row is a heard row's content; Bytes is its exact form.
type Row struct {
	Source string // talk | mail | text | calendar
	Lines  []Line
}

const magic = "plebian-heard 1\n"

// Bytes: a header, then per line "line <speaker> <at> <len>\n<text>\n". Lengths keep the
// text raw so fragment offsets point into it.
func (r Row) Bytes() []byte {
	var b bytes.Buffer
	b.WriteString(magic)
	b.WriteString("source " + r.Source + "\n")
	for _, l := range r.Lines {
		fmt.Fprintf(&b, "line %s %s %d\n", esc(l.Speaker), esc(l.At), len(l.Text))
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func esc(s string) string {
	if s == "" {
		return "-"
	}
	return strings.NewReplacer("%", "%25", " ", "%20", "\n", "%0A", "-", "%2D").Replace(s)
}

func unesc(s string) string {
	if s == "-" {
		return ""
	}
	return strings.NewReplacer("%2D", "-", "%0A", "\n", "%20", " ", "%25", "%").Replace(s)
}

func Parse(b []byte) (Row, error) {
	var r Row
	if !bytes.HasPrefix(b, []byte(magic)) {
		return r, errors.New("not a heard row")
	}
	b = b[len(magic):]
	line, rest, ok := bytes.Cut(b, []byte("\n"))
	if !ok || !bytes.HasPrefix(line, []byte("source ")) {
		return r, errors.New("heard row: no source")
	}
	r.Source = string(line[len("source "):])
	b = rest
	for len(b) > 0 {
		line, rest, ok = bytes.Cut(b, []byte("\n"))
		f := strings.Fields(string(line))
		if !ok || len(f) != 4 || f[0] != "line" {
			return r, fmt.Errorf("heard row: bad line header %q", line)
		}
		n, err := strconv.Atoi(f[3])
		if err != nil || n < 0 || n+1 > len(rest) || rest[n] != '\n' {
			return r, errors.New("heard row: bad length")
		}
		r.Lines = append(r.Lines, Line{Speaker: unesc(f[1]), At: unesc(f[2]), Text: string(rest[:n])})
		b = rest[n+1:]
	}
	return r, nil
}
