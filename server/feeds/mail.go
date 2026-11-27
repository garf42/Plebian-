package feeds

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"plebian/server/config"
	"plebian/server/models"
	"plebian/server/tools"
	"plebian/server/underlay"
	"plebian/server/watch"
)

// Account is one IMAP/SMTP account, kept in the secrets file.
type Account struct {
	IMAP     string `json:"imap"` // host:port
	SMTP     string `json:"smtp"` // host:port
	User     string `json:"user"`
	Password string `json:"password"`
}

const mailSecret = "mail"

// Mail is the mail feed: it lands INBOX as messages arrive (IDLE) and sends approved drafts.
type Mail struct {
	L       *Lander
	Watch   *watch.Watch
	Secrets *config.Secrets
	Usual   func(ctx context.Context) time.Duration // the owner's usual time to answer
	Log     *log.Logger

	once sync.Once
	kick chan struct{}
}

func (m *Mail) init() { m.once.Do(func() { m.kick = make(chan struct{}, 1) }) }

func (m *Mail) account() (Account, bool) {
	var a Account
	err := json.Unmarshal([]byte(m.Secrets.Get(mailSecret)), &a)
	return a, err == nil && a.IMAP != "" && a.User != ""
}

// Connect checks an account by signing in, then keeps it and restarts the feed with it.
func (m *Mail) Connect(ctx context.Context, a Account) error {
	m.init()
	c, err := dialIMAP(a.IMAP, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Login(a.User, a.Password).Wait(); err != nil {
		return fmt.Errorf("IMAP sign-in: %w", err)
	}
	b, _ := json.Marshal(a)
	if err := m.Secrets.Set(mailSecret, string(b)); err != nil {
		return err
	}
	select {
	case m.kick <- struct{}{}:
	default:
	}
	return nil
}

// loopback reports a host on this machine, the only place a password may cross in clear.
func loopback(hostport string) bool {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func port(hostport string) string {
	_, p, _ := net.SplitHostPort(hostport)
	return p
}

// dialIMAP: implicit TLS on 993 (RFC 8314), STARTTLS elsewhere, clear text only to
// this machine.
func dialIMAP(addr string, h *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	o := &imapclient.Options{UnilateralDataHandler: h}
	switch {
	case port(addr) == "993":
		return imapclient.DialTLS(addr, o)
	case loopback(addr):
		return imapclient.DialInsecure(addr, o)
	}
	return imapclient.DialStartTLS(addr, o)
}

// Run keeps the feed going until ctx ends.
func (m *Mail) Run(ctx context.Context) {
	m.init()
	for ctx.Err() == nil {
		a, ok := m.account()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-m.kick:
			}
			continue
		}
		err := m.session(ctx, a)
		if ctx.Err() != nil {
			return
		}
		if err != nil && m.Log != nil {
			m.Log.Printf("mail: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
		case <-m.L.Clock.After(m.L.Clock.Now().Add(retry(m.Usual(ctx)))):
		}
	}
}

type mailCursor struct {
	UIDValidity uint32 `json:"uidvalidity"`
	UID         uint32 `json:"uid"`
}

// session signs in, lands what is new, then idles until the mailbox grows.
func (m *Mail) session(ctx context.Context, a Account) error {
	grew := make(chan struct{}, 1)
	c, err := dialIMAP(a.IMAP, &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				select {
				case grew <- struct{}{}:
				default:
				}
			}
		},
	})
	if err != nil {
		return err
	}
	defer c.Close()
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-c.Closed():
		}
	}()
	if err := c.Login(a.User, a.Password).Wait(); err != nil {
		return err
	}
	sel, err := c.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return err
	}
	for {
		if err := m.sync(ctx, c, sel.UIDValidity); err != nil {
			return err
		}
		idle, err := c.Idle() // go-imap re-issues IDLE every 28 minutes (RFC 2177: within 29)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			idle.Close()
			return nil
		case <-m.kick:
			idle.Close()
			return nil
		case <-grew:
		// A push a server drops would leave mail waiting until IDLE's restart; the feed
		// also looks again at the calendar's cadence (G026).
		case <-m.L.Clock.After(m.L.Clock.Now().Add(retry(m.Usual(ctx)))):
		case <-c.Closed():
			return errors.New("IMAP connection closed")
		}
		if err := idle.Close(); err != nil {
			return err
		}
		if err := idle.Wait(); err != nil {
			return err
		}
	}
}

func (m *Mail) sync(ctx context.Context, c *imapclient.Client, validity uint32) error {
	var cur mailCursor
	json.Unmarshal([]byte(cursor(ctx, m.L.S, "mail")), &cur)
	if cur.UIDValidity != validity {
		cur = mailCursor{UIDValidity: validity} // the UIDs restarted; rows already landed land nothing again
	}
	set := imap.UIDSet{imap.UIDRange{Start: imap.UID(cur.UID + 1), Stop: 0}}
	msgs, err := c.Fetch(set, &imap.FetchOptions{UID: true, InternalDate: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		if uint32(msg.UID) <= cur.UID || len(msg.BodySection) == 0 {
			continue // "n:*" answers the last message even when none is newer
		}
		raw := msg.BodySection[0].Bytes
		if _, _, err := m.land(ctx, "INBOX", validity, uint32(msg.UID), raw, msg.InternalDate); err != nil {
			return err
		}
		cur.UID = uint32(msg.UID)
		b, _ := json.Marshal(cur)
		if err := setCursor(ctx, m.L.S, "mail", string(b)); err != nil {
			return err
		}
	}
	return nil
}

// MailRow is a message's heard row: one line from its sender at its INTERNALDATE (Date
// headers can lie), the subject, the plain text, and what is attached.
func MailRow(p Parsed, at time.Time) underlay.Row {
	var b strings.Builder
	fmt.Fprintf(&b, "Subject: %s\n", p.Subject)
	if p.Text != "" {
		b.WriteString(p.Text + "\n")
	}
	for _, a := range p.Attachments {
		fmt.Fprintf(&b, "attachment: %s (%s, %d bytes)\n", a.Name, a.Type, len(a.Data))
	}
	return underlay.Row{Source: "mail", Lines: Lines(p.From, at, b.String())}
}

// Lines splits a message or entry into heard lines, one per line of text that holds
// words, each from its sender at its wall time: the lamp counts lines, and a message's
// lines are its own (the original keeps every byte).
func Lines(who string, at time.Time, text string) []underlay.Line {
	var out []underlay.Line
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, underlay.Line{Speaker: who, At: at.UTC().Format(time.RFC3339Nano), Text: l})
		}
	}
	if len(out) == 0 {
		out = append(out, underlay.Line{Speaker: who, At: at.UTC().Format(time.RFC3339Nano), Text: ""})
	}
	return out
}

func (m *Mail) land(ctx context.Context, box string, validity, uid uint32, raw []byte, internal time.Time) (int64, bool, error) {
	p, err := ParseMail(raw)
	if err != nil {
		p = Parsed{Text: "(the message could not be read; the original is kept)"}
	}
	ext := fmt.Sprintf("%s/%d/%d", box, validity, uid)
	return m.L.Put(ctx, m.Watch, "mail", ext, internal, internal, MailRow(p, internal), raw)
}

// Send sends one complete message, exactly its bytes, to the recipients its headers name.
func (m *Mail) Send(ctx context.Context, raw []byte) error {
	a, ok := m.account()
	if !ok {
		return errors.New("no mail account is connected")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	var rcpt []string
	for _, k := range []string{"To", "Cc", "Bcc"} {
		if v := msg.Header.Get(k); v != "" {
			as, err := mail.ParseAddressList(v)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			for _, x := range as {
				rcpt = append(rcpt, x.Address)
			}
		}
	}
	if len(rcpt) == 0 {
		return errors.New("the message names no recipient")
	}
	host, _, _ := net.SplitHostPort(a.SMTP)
	var c *smtp.Client
	switch {
	case port(a.SMTP) == "465":
		conn, err := tls.Dial("tcp", a.SMTP, &tls.Config{ServerName: host})
		if err != nil {
			return err
		}
		if c, err = smtp.NewClient(conn, host); err != nil {
			return err
		}
	default:
		if c, err = smtp.Dial(a.SMTP); err != nil {
			return err
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		} else if !loopback(a.SMTP) {
			return errors.New("the SMTP server offers no TLS; the password stays home")
		}
	}
	defer c.Close()
	// net/smtp's PlainAuth refuses clear text except to localhost, as this does.
	if err := c.Auth(plainAuth{a.User, a.Password, loopback(a.SMTP)}); err != nil {
		return err
	}
	if err := c.Mail(a.User); err != nil {
		return err
	}
	for _, r := range rcpt {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// plainAuth is SASL PLAIN that also allows a loopback host by address (net/smtp's own
// allows only the name "localhost").
type plainAuth struct {
	user, pass string
	local      bool
}

func (p plainAuth) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !s.TLS && !p.local {
		return "", nil, errors.New("unencrypted connection")
	}
	return "PLAIN", []byte("\x00" + p.user + "\x00" + p.pass), nil
}

func (p plainAuth) Next([]byte, bool) ([]byte, error) { return nil, nil }

type sendIn struct {
	To        string `json:"to"`
	Cc        string `json:"cc"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	InReplyTo int64  `json:"in_reply_to"` // a mail row id; the reply joins its thread
	Raw       string `json:"raw"`         // set only by Compose: the complete message
}

// SendTool is `mail_send`: an outside change, drafted as the complete message.
type SendTool struct {
	M     *Mail
	Clock interface{ Now() time.Time }
}

func (t SendTool) Def() models.Tool {
	return models.Tool{Name: "mail_send", Description: "Send a mail from the owner's account; drafted as the complete message, sent only on the owner's word.",
		Schema: tools.Schema([]string{"to", "body"}, map[string][2]string{
			"to":          {"string", "recipients, as in a To header"},
			"cc":          {"string", "copies, as in a Cc header"},
			"subject":     {"string", "the subject (a reply keeps the thread's)"},
			"body":        {"string", "the plain-text body"},
			"in_reply_to": {"integer", "the heard row id of the mail this answers"},
		})}
}
func (SendTool) Class(json.RawMessage) tools.Class { return tools.Outside }
func (SendTool) ReachesOut(json.RawMessage) bool   { return true }

// Compose fixes the whole message when it is drafted, Date and Message-ID included, so
// the bytes sent are the bytes shown (n.feeds).
func (t SendTool) Compose(ctx context.Context, in json.RawMessage) (json.RawMessage, string, error) {
	var x sendIn
	if err := json.Unmarshal(in, &x); err != nil {
		return nil, "", err
	}
	if x.Raw != "" {
		return in, x.Raw, nil
	}
	a, ok := t.M.account()
	if !ok {
		return nil, "", errors.New("no mail account is connected")
	}
	if _, err := mail.ParseAddressList(x.To); err != nil {
		return nil, "", fmt.Errorf("to: %w", err)
	}
	subject := x.Subject
	var refs []string
	if x.InReplyTo != 0 {
		var sha string
		if err := t.M.L.S.DB.QueryRowContext(ctx, `SELECT blob FROM heard WHERE id=? AND source='mail'`, x.InReplyTo).Scan(&sha); err != nil {
			return nil, "", fmt.Errorf("no mail row %d", x.InReplyTo)
		}
		orig, err := t.M.L.S.Blobs.Get(ctx, sha)
		if err != nil {
			return nil, "", err
		}
		p, err := ParseMail(orig)
		if err != nil {
			return nil, "", err
		}
		if p.References != "" {
			refs = append(refs, strings.Fields(p.References)...)
		}
		if p.MessageID != "" {
			refs = append(refs, p.MessageID)
		}
		if subject == "" {
			subject = p.Subject
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
		}
	}
	id := make([]byte, 16)
	rand.Read(id)
	domain := a.User[strings.LastIndex(a.User, "@")+1:]
	var b strings.Builder
	fmt.Fprintf(&b, "Date: %s\r\n", t.Clock.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "From: %s\r\n", a.User)
	fmt.Fprintf(&b, "To: %s\r\n", x.To)
	if x.Cc != "" {
		fmt.Fprintf(&b, "Cc: %s\r\n", x.Cc)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain)
	if len(refs) > 0 {
		fmt.Fprintf(&b, "In-Reply-To: %s\r\n", refs[len(refs)-1])
		fmt.Fprintf(&b, "References: %s\r\n", strings.Join(refs, " "))
	}
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(x.Body, "\r\n", "\n"), "\n", "\r\n"))
	if !strings.HasSuffix(b.String(), "\r\n") {
		b.WriteString("\r\n")
	}
	x.Raw = b.String()
	out, _ := json.Marshal(sendIn{Raw: x.Raw})
	return out, x.Raw, nil
}

func (t SendTool) Run(ctx context.Context, in json.RawMessage) (tools.Result, error) {
	payload, raw, err := t.Compose(ctx, in)
	if err != nil {
		return tools.Result{Reading: []byte(err.Error()), Exit: 2}, nil
	}
	_ = payload
	if err := t.M.Send(ctx, []byte(raw)); err != nil {
		return tools.Result{Reading: []byte("not sent: " + err.Error()), Exit: 1}, nil
	}
	return tools.Result{Reading: []byte("sent")}, nil
}
