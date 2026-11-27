package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"plebian/server/store"
	"plebian/server/tools"
)

// PutOut puts a message's attachments in the tools' outside folder through the runner
// (the only writer there) and returns their paths; the body stays a heard row.
func PutOut(s *store.Store, runner tools.Client) func(ctx context.Context, row int64) ([]string, error) {
	return func(ctx context.Context, row int64) ([]string, error) {
		var source, sha string
		if err := s.DB.QueryRowContext(ctx, `SELECT source, blob FROM heard WHERE id=?`, row).Scan(&source, &sha); err != nil {
			return nil, fmt.Errorf("no row %d", row)
		}
		if source != "mail" {
			return nil, fmt.Errorf("row %d is %s, not a message with attachments", row, source)
		}
		raw, err := s.Blobs.Get(ctx, sha)
		if err != nil {
			return nil, err
		}
		p, err := ParseMail(raw)
		if err != nil {
			return nil, err
		}
		if len(p.Attachments) == 0 {
			return nil, fmt.Errorf("message %d has no attachments", row)
		}
		files := map[string][]byte{}
		for i, a := range p.Attachments {
			name := fmt.Sprintf("%d/%s", row, a.Name)
			if _, dup := files[name]; dup {
				name = fmt.Sprintf("%d/%d-%s", row, i, a.Name)
			}
			files[name] = a.Data
		}
		d, err := runner.Do(ctx, tools.Job{Put: files})
		if err != nil {
			return nil, err
		}
		if d.Exit != 0 {
			return nil, errors.New(d.Error)
		}
		return strings.Fields(string(d.Stdout)), nil
	}
}

// Setup serves the Mail, Calendar and Texts cards.
type Setup struct {
	Mail  *Mail
	Cal   *Calendar
	Texts *Texts
}

const maxBody = 1 << 20

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Handle answers POST /v1/setup/{mail|calendar|texts}; it returns false for other cards.
func (s *Setup) Handle(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	fail := func(err error) {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	switch r.PathValue("card") {
	case "mail":
		var a Account
		if err := dec.Decode(&a); err != nil || a.IMAP == "" || a.SMTP == "" || a.User == "" {
			fail(errors.New("imap, smtp and user are required"))
			return true
		}
		if err := s.Mail.Connect(ctx, a); err != nil {
			fail(err)
			return true
		}
		writeJSON(w, map[string]bool{"ok": true})
	case "calendar":
		var x struct{ URL string }
		if err := dec.Decode(&x); err != nil {
			fail(err)
			return true
		}
		if err := s.Cal.Connect(ctx, x.URL); err != nil {
			fail(err)
			return true
		}
		writeJSON(w, map[string]bool{"ok": true})
	case "texts":
		var x struct {
			Bridge  string            `json:"bridge"`
			Cookies map[string]string `json:"cookies"`
		}
		if err := dec.Decode(&x); err != nil {
			fail(err)
			return true
		}
		if len(x.Cookies) == 0 {
			fail(errors.New("the Google cookies are required (signed in from the live view)"))
			return true
		}
		emoji, err := s.Texts.Connect(ctx, x.Bridge, x.Cookies)
		if err != nil {
			fail(err)
			return true
		}
		writeJSON(w, map[string]string{"emoji": emoji})
	default:
		return false
	}
	return true
}

// Cards are the three feed cards' statuses: their last landing, or not connected.
func (s *Setup) Cards(ctx context.Context) []map[string]any {
	var out []map[string]any
	for _, c := range []struct {
		id, feed string
		set      bool
	}{
		{"mail", "mail", s.Mail.Secrets.Get(mailSecret) != ""},
		{"calendar", "calendar", s.Cal.Secrets.Get(calendarSecret) != ""},
		{"texts", "text", s.Texts.Secrets.Get(textsSecret) != ""},
	} {
		status := "not connected"
		if c.set {
			status = "connected"
			var last string
			s.Mail.L.S.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(landed_at),'') FROM heard WHERE source=?`, c.feed).Scan(&last)
			if last != "" {
				status = "last landing " + last
			}
		}
		out = append(out, map[string]any{"id": c.id, "status": status, "required": false})
	}
	return out
}
