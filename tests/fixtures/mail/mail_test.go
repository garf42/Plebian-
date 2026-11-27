package mail

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

const raw = "From: Ridgeline <billing@ridgeline.example>\r\nTo: owner@plebian.example\r\nSubject: Invoice INV-0042\r\nDate: Mon, 01 Jan 2001 00:00:00 +0000\r\n\r\nThe invoice is attached.\r\n"

func TestIMAPIdlePushAndSMTPRecord(t *testing.T) {
	m := New("owner", "pw1")
	srv, err := m.Start("127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	exists := make(chan uint32, 4)
	c, err := imapclient.DialInsecure(srv.Info["imap"].(string), &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				exists <- *d.NumMessages
			}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("owner", "wrong").Wait(); err == nil {
		t.Fatal("wrong password accepted")
	}
	if err := c.Login("owner", "pw1").Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	idle, err := c.Idle()
	if err != nil {
		t.Fatal(err)
	}
	internal := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	body, _ := json.Marshal(map[string]any{"raw": raw, "internal_date": internal})
	resp, err := http.Post("http://"+srv.Info["addr"].(string)+"/control/append", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("append: %v %v", err, resp.StatusCode)
	}
	select {
	case n := <-exists:
		if n != 1 {
			t.Fatalf("EXISTS %d", n)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no IDLE push")
	}
	if err := idle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idle.Wait(); err != nil {
		t.Fatal(err)
	}
	section := &imap.FetchItemBodySection{}
	msgs, err := c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{InternalDate: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatal(err)
	}
	if !msgs[0].InternalDate.Equal(internal) || string(msgs[0].FindBodySection(section)) != raw {
		t.Fatalf("fetched %v %q", msgs[0].InternalDate, msgs[0].FindBodySection(section))
	}

	sc, err := smtp.Dial(srv.Info["smtp"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Auth(sasl.NewPlainClient("", "owner", "pw1")); err != nil {
		t.Fatal(err)
	}
	reply := "From: owner@plebian.example\r\nTo: billing@ridgeline.example\r\nSubject: Re: Invoice\r\n\r\n.leading dot\r\nPaid.\r\n"
	if err := sc.SendMail("owner@plebian.example", []string{"billing@ridgeline.example"}, strings.NewReader(reply)); err != nil {
		t.Fatal(err)
	}
	sent := m.SentMessages()
	if len(sent) != 1 || string(sent[0].Data) != reply || sent[0].To[0] != "billing@ridgeline.example" {
		t.Fatalf("sent %+v", sent)
	}
	if !bytes.Contains(m.conns[0].Received, []byte("IDLE")) {
		t.Fatalf("imap log %q", m.conns[0].Received)
	}
}
