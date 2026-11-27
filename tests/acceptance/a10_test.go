package acceptance

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// PDF is a one-page PDF whose only text is line, with a correct cross-reference table.
func PDF(line string) []byte {
	content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", line)
	objs := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%s\nendstream", len(content), content),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func invoiceMail() string {
	pdf := base64.StdEncoding.EncodeToString(PDF("Total due: 412.50 USD"))
	var lines []string
	for len(pdf) > 76 {
		lines, pdf = append(lines, pdf[:76]), pdf[76:]
	}
	lines = append(lines, pdf)
	return "From: Vendor AP <ap@vendor.example>\r\nTo: owner@example.com\r\nSubject: Invoice 1042\r\nMessage-ID: <inv-1042@vendor.example>\r\n" +
		"Date: Tue, 1 Dec 2026 09:30:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlease find invoice 1042 attached.\r\n" +
		"--b1\r\nContent-Type: application/pdf; name=\"invoice.pdf\"\r\nContent-Disposition: attachment; filename=\"invoice.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		strings.Join(lines, "\r\n") + "\r\n--b1--\r\n"
}

const daleMail = "From: Dale <dale@quarry.example>\r\nTo: owner@example.com\r\nSubject: Gravel\r\nMessage-ID: <gravel-7@quarry.example>\r\n" +
	"Date: Tue, 1 Dec 2026 14:00:00 +0000\r\n\r\nCan I bring the gravel Wednesday?\r\n"

func rawPost(t testing.TB, url string, body []byte) {
	resp, err := http.Post(url, "text/calendar", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("POST %s: %v", url, err)
	}
	resp.Body.Close()
}

func init() {
	type a10 struct {
		sources       map[string]int
		mailRow       string
		daleRow       string
		pdfAnswer     []string
		outsideSeen   bool
		noteRan       bool
		shown         string
		sentAfterNo   int
		declinedShown bool // the owner declined a draft that was shown
		sent          []struct{ Data []byte }
		emoji         string
	}
	var st *a10
	register(Scenario{
		ID: "A10",
		Setup: func(t testing.TB) *H {
			st = &a10{sources: map[string]int{}}
			h := fresh(t)
			h.Fixtures["mail"] = StartFixture(t, "mail", "-user", "owner@example.com")
			h.Fixtures["ical"] = StartFixture(t, "ical")
			h.Fixtures["texts"] = StartFixture(t, "texts")
			h.Model("anthropic", nil)
			return h
		},
		Drive: func(h *H) {
			mf, cf, tf := h.Fixtures["mail"], h.Fixtures["ical"], h.Fixtures["texts"]
			now := h.Now()
			// Mail.
			post(h.T, mf.URL("/control/append"), map[string]any{"raw": invoiceMail(), "internal_date": now.Add(-50 * time.Minute)})
			post(h.T, mf.URL("/control/append"), map[string]any{"raw": daleMail, "internal_date": now.Add(-40 * time.Minute)})
			if c := h.Do("POST", "/v1/setup/mail", map[string]string{"imap": fmt.Sprint(mf.Info["imap"]), "smtp": fmt.Sprint(mf.Info["smtp"]),
				"user": fmt.Sprint(mf.Info["user"]), "password": fmt.Sprint(mf.Info["password"])}, nil, true); c != 200 {
				h.T.Fatalf("mail setup: %d %s", c, h.LastBody)
			}
			// A calendar entry starting within the hour.
			start := now.Add(30 * time.Minute).UTC().Format("20060102T150405Z")
			end := now.Add(90 * time.Minute).UTC().Format("20060102T150405Z")
			rawPost(h.T, cf.URL("/control/body"), []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Plebian//Fixture//EN\r\nBEGIN:VEVENT\r\nUID:gravel-1@fixture\r\nDTSTAMP:20261120T090000Z\r\n"+
				"DTSTART:"+start+"\r\nDTEND:"+end+"\r\nSUMMARY:Gravel delivery\r\nLOCATION:the culvert\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
			if c := h.Do("POST", "/v1/setup/calendar", map[string]string{"url": cf.URL("/calendar.ics")}, nil, true); c != 200 {
				h.T.Fatalf("calendar setup: %d %s", c, h.LastBody)
			}
			// A text, once the link is paired and the phone tapped.
			var pr struct{ Emoji string }
			if c := h.Do("POST", "/v1/setup/texts", map[string]any{"bridge": tf.URL(""), "cookies": map[string]string{"SID": "fixture-session"}}, &pr, true); c != 200 {
				h.T.Fatalf("texts setup: %d %s", c, h.LastBody)
			}
			st.emoji = pr.Emoji
			post(h.T, tf.URL("/control/phone_tap"), map[string]any{})
			post(h.T, tf.URL("/control/inject"), map[string]any{"conversation_id": "c-dale", "message_id": "m1", "sender": "+15550104", "text": "running late, there by 10", "ts_ms": now.Add(-5 * time.Minute).UnixMilli()})
			h.Advance(2 * time.Minute) // past the texts feed's retry after the unpaired first look
			rowsOf := func() []HeardRow { return h.HeardBetween(now.Add(-50*time.Minute), now.Add(90*time.Minute)) }
			Until(90*time.Second, func() bool {
				seen := map[string]int{}
				for _, r := range rowsOf() {
					seen[r.Source]++
				}
				return seen["mail"] >= 2 && seen["calendar"] >= 1 && seen["text"] >= 1
			})
			for _, r := range rowsOf() {
				st.sources[r.Source]++
				if r.Source == "mail" && strings.Contains(r.Text(), "invoice 1042") {
					st.mailRow = r.ID
				}
				if r.Source == "mail" && strings.Contains(r.Text(), "gravel wednesday") {
					st.daleRow = r.ID
				}
			}
			if st.mailRow == "" || st.daleRow == "" {
				return
			}
			var dale int64
			fmt.Sscan(st.daleRow, &dale)
			var inv int64
			fmt.Sscan(st.mailRow, &inv)
			reply := map[string]any{"to": "Dale <dale@quarry.example>", "body": "Wednesday works.", "in_reply_to": dale}
			post(h.T, h.Fixtures["model:anthropic"].URL("/control/script"), map[string]any{"steps": []map[string]any{
				{"match": "invoice say", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "lookup", "input": map[string]any{"row": inv}}}}},
				{"match": "put out", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "read", "input": map[string]any{"command": fmt.Sprintf("pdftotext outside/%d/invoice.pdf -", inv), "outside": true}}}}},
				{"match": "Total due: 412.50", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "run", "input": map[string]any{"command": "echo 412.50 > total.txt"}}}}},
				{"match": "drafted", "reply": map[string]any{"text": "The invoice says Total due: 412.50 USD."}},
				{"match": "reply to Dale", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "mail_send", "input": reply}}}},
				{"match": "drafted", "reply": map[string]any{"text": "Drafted; it waits for your word."}},
				{"match": "don't send", "reply": map[string]any{"text": "Not sent."}},
				{"match": "yes, send", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "mail_send", "input": reply}}}},
				{"match": "sent", "reply": map[string]any{"text": "Sent."}},
			}})
			agentSaid := func(s string) func() bool {
				return func() bool {
					for _, m := range AgentMessages(h.Today()) {
						if m == s {
							return true
						}
					}
					return false
				}
			}
			h.Say("what does the vendor's invoice say is due?")
			Until(90*time.Second, agentSaid("The invoice says Total due: 412.50 USD."))
			for _, m := range AgentMessages(h.Today()) {
				if strings.Contains(m, "412.50") {
					st.pdfAnswer = append(st.pdfAnswer, m)
				}
			}
			for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
				// The request is JSON: newlines and angle brackets arrive escaped.
				b := strings.NewReplacer(`\n`, "\n", `\u003c`, "<", `\u003e`, ">").Replace(string(r.Body))
				if i := strings.Index(b, "<outside>"); i >= 0 && strings.Contains(b[i:], "Total due: 412.50") {
					st.outsideSeen = true
				}
			}
			if _, err := exec.Command("sudo", "-n", "test", "-e", filepath.Join(h.Root, "workspace", "total.txt")).Output(); err == nil {
				st.noteRan = true
			}
			h.Say("draft a reply to Dale saying Wednesday works")
			Until(90*time.Second, agentSaid("Drafted; it waits for your word."))
			var open struct{ Drafts []struct{ Text string } }
			h.Do("GET", "/v1/open", nil, &open, true)
			if len(open.Drafts) > 0 {
				st.shown = open.Drafts[len(open.Drafts)-1].Text
			}
			st.declinedShown = st.shown != ""
			h.Say("no, don't send that yet")
			Until(90*time.Second, agentSaid("Not sent."))
			time.Sleep(2 * time.Second)
			var sent []struct{ Data []byte }
			get(h.T, mf.URL("/control/sent"), &sent)
			st.sentAfterNo = len(sent)
			h.Say("yes, send it now")
			Until(90*time.Second, agentSaid("Sent."))
			get(h.T, mf.URL("/control/sent"), &st.sent)
		},
		Assert: func(h *H, c *C) {
			c.Check("mail_landed", st.sources["mail"] >= 2, "mail rows %d", st.sources["mail"])
			c.Check("calendar_landed", st.sources["calendar"] >= 1, "calendar rows %d", st.sources["calendar"])
			c.Check("text_landed", st.sources["text"] >= 1 && st.emoji != "", "text rows %d (emoji %q)", st.sources["text"], st.emoji)
			c.Check("pdf_answered", len(st.pdfAnswer) == 1, "answers naming the total: %q", st.pdfAnswer)
			c.Check("pdf_as_outside_words", st.outsideSeen && !st.noteRan, "outside-marked reading seen %v; a local change ran with it in view %v", st.outsideSeen, st.noteRan)
			full := strings.Contains(st.shown, "Date: ") && strings.Contains(st.shown, "Message-ID: <") && strings.Contains(st.shown, "In-Reply-To: <gravel-7@quarry.example>") && strings.Contains(st.shown, "Wednesday works.")
			c.Check("draft_is_whole_message", full, "draft shown %q", st.shown)
			c.Check("declined_sends_nothing", st.declinedShown && st.sentAfterNo == 0, "a shown draft declined %v; %d sent after", st.declinedShown, st.sentAfterNo)
			c.Check("approved_sends_shown_bytes_once", len(st.sent) == 1 && string(st.sent[0].Data) == st.shown, "%d sent; bytes equal the shown draft: %v", len(st.sent), len(st.sent) == 1 && string(st.sent[0].Data) == st.shown)
		},
	})
}

var _ = json.Marshal
