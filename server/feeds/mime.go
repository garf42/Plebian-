package feeds

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"strings"

	"plebian/server/tools"
)

// Attachment is one attached file, decoded.
type Attachment struct {
	Name string
	Type string
	Data []byte
}

// Parsed is a message read for its row: sender, subject, plain text and attachments.
type Parsed struct {
	From, To, Subject, MessageID, References string
	Text                                     string
	Attachments                              []Attachment
}

var words = new(mime.WordDecoder)

func header(h mail.Header, k string) string {
	v := h.Get(k)
	if d, err := words.DecodeHeader(v); err == nil {
		return d
	}
	return v
}

// ParseMail reads an RFC 5322 message as received. A part it can't read stays out of
// the text; the original keeps every byte.
func ParseMail(raw []byte) (Parsed, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Parsed{}, err
	}
	p := Parsed{From: header(m.Header, "From"), To: header(m.Header, "To"), Subject: header(m.Header, "Subject"),
		MessageID: m.Header.Get("Message-Id"), References: m.Header.Get("References")}
	var text, html []string
	walk(textproto(m.Header), m.Body, &text, &html, &p.Attachments)
	if len(text) == 0 {
		for _, h := range html {
			text = append(text, tools.Readable([]byte(h), ""))
		}
	}
	p.Text = strings.TrimSpace(strings.Join(text, "\n\n"))
	return p, nil
}

type partHeader interface{ Get(string) string }

func textproto(h mail.Header) partHeader { return h }

func decode(h partHeader, r io.Reader) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, newlineStripper{r})
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	return io.ReadAll(r)
}

// newlineStripper drops CR and LF so base64 bodies split across lines decode.
type newlineStripper struct{ r io.Reader }

func (n newlineStripper) Read(b []byte) (int, error) {
	k, err := n.r.Read(b)
	j := 0
	for _, c := range b[:k] {
		if c != '\r' && c != '\n' {
			b[j] = c
			j++
		}
	}
	return j, err
}

func walk(h partHeader, body io.Reader, text, html *[]string, atts *[]Attachment) {
	ct := h.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = "application/octet-stream"
	}
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextRawPart() // raw: the transfer encoding is decoded here, once
			if err != nil {
				return
			}
			walk(part.Header, part, text, html, atts)
		}
	}
	disp, dparams, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	if d, err := words.DecodeHeader(name); err == nil {
		name = d
	}
	b, err := decode(h, body)
	if err != nil {
		return
	}
	if mt == "text/plain" && disp != "attachment" && name == "" {
		*text = append(*text, strings.ReplaceAll(string(b), "\r\n", "\n"))
		return
	}
	if mt == "text/html" && disp != "attachment" && name == "" {
		*html = append(*html, string(b)) // read only when no plain part carries the words
		return
	}
	if name == "" {
		name = "part"
		if ext, _ := mime.ExtensionsByType(mt); len(ext) > 0 {
			name += ext[0]
		}
	}
	*atts = append(*atts, Attachment{Name: filepath.Base(name), Type: mt, Data: b})
}
