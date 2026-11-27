// Package mail is an IMAP server with IDLE (go-imap v2's in-memory backend)
// and an SMTP server (go-smtp) for one account, with a control API.
package mail

import (
	"bytes"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"plebian/tests/fixtures"
)

// Sent is one message the SMTP server accepted.
type Sent struct {
	Time time.Time `json:"time"`
	User string    `json:"user"`
	From string    `json:"from"`
	To   []string  `json:"to"`
	Data []byte    `json:"data"` // DATA after dot-unstuffing (RFC 5321 4.5.2); base64 in JSON
}

// Conn is what one IMAP client sent, byte for byte.
type Conn struct {
	Opened   time.Time `json:"opened"`
	Received []byte    `json:"received"`
}

// Mail is the fixture's state.
type Mail struct {
	mu       sync.Mutex
	user     string
	password string
	box      *imapmemserver.User
	sent     []Sent
	conns    []*Conn
}

// New returns a mail fixture whose one account is user/password with an INBOX.
func New(user, password string) *Mail {
	// The memserver's own credentials are never checked: Login below is.
	box := imapmemserver.NewUser(user, "")
	if err := box.Create("INBOX", nil); err != nil {
		panic(err)
	}
	return &Mail{user: user, password: password, box: box}
}

func (m *Mail) check(user, password string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return user == m.user && subtle.ConstantTimeCompare([]byte(password), []byte(m.password)) == 1
}

// Append stores raw in mailbox with the given INTERNALDATE; IDLE clients see it at once.
func (m *Mail) Append(mailbox string, raw []byte, internal time.Time) (*imap.AppendData, error) {
	return m.box.Append(mailbox, literal{bytes.NewReader(raw), int64(len(raw))}, &imap.AppendOptions{Time: internal})
}

// SentMessages returns every accepted message, oldest first.
func (m *Mail) SentMessages() []Sent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Sent{}, m.sent...)
}

type literal struct {
	io.Reader
	n int64
}

func (l literal) Size() int64 { return l.n }

type session struct {
	*imapmemserver.UserSession
	m *Mail
}

func (s *session) Login(user, password string) error {
	if !s.m.check(user, password) {
		return imapserver.ErrAuthFailed
	}
	s.UserSession = imapmemserver.NewUserSession(s.m.box)
	return nil
}

func (s *session) Close() error {
	if s.UserSession == nil {
		return nil
	}
	return s.UserSession.Close()
}

// tee records every byte a client sends on its connection.
type tee struct {
	net.Conn
	m *Mail
	c *Conn
}

func (t tee) Read(b []byte) (int, error) {
	n, err := t.Conn.Read(b)
	t.m.mu.Lock()
	t.c.Received = append(t.c.Received, b[:n]...)
	t.m.mu.Unlock()
	return n, err
}

type teeListener struct {
	net.Listener
	m *Mail
}

func (l teeListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	rec := &Conn{Opened: time.Now().UTC()}
	l.m.mu.Lock()
	l.m.conns = append(l.m.conns, rec)
	l.m.mu.Unlock()
	return tee{c, l.m, rec}, nil
}

type smtpSession struct {
	m    *Mail
	user string
	msg  Sent
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *smtpSession) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, user, password string) error {
		if !s.m.check(user, password) {
			return errors.New("invalid credentials")
		}
		s.user = user
		return nil
	}), nil
}

func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	if s.user == "" {
		return smtp.ErrAuthRequired
	}
	s.msg = Sent{From: from}
	return nil
}

func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.msg.To = append(s.msg.To, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.msg.Data, s.msg.Time, s.msg.User = b, time.Now().UTC(), s.user
	s.m.mu.Lock()
	s.m.sent = append(s.m.sent, s.msg)
	s.m.mu.Unlock()
	return nil
}

func (s *smtpSession) Reset()        { s.msg = Sent{} }
func (s *smtpSession) Logout() error { return nil }

// Start serves IMAP, SMTP and the control API. With useTLS both mail
// listeners speak implicit TLS with a fresh certificate printed as ca_pem.
func (m *Mail) Start(addr, imapAddr, smtpAddr string, useTLS bool) (*fixtures.Server, error) {
	srv, err := fixtures.ServeHTTP(addr, m.Handler())
	if err != nil {
		return nil, err
	}
	var tlsCfg *tls.Config
	if useTLS {
		cert, pemCA, err := fixtures.SelfSigned()
		if err != nil {
			srv.Close()
			return nil, err
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}}
		srv.Info["ca_pem"] = pemCA
	}
	listen := func(a string) (net.Listener, error) {
		ln, err := net.Listen("tcp", a)
		if err != nil || tlsCfg == nil {
			return ln, err
		}
		return tls.NewListener(ln, tlsCfg), nil
	}
	iln, err := listen(imapAddr)
	if err != nil {
		srv.Close()
		return nil, err
	}
	is := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{m: m}, nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true, // plaintext only on 127.0.0.1, or under -tls
	})
	go func() { _ = is.Serve(teeListener{iln, m}) }()
	srv.OnClose(is.Close)
	sln, err := listen(smtpAddr)
	if err != nil {
		srv.Close()
		return nil, err
	}
	ss := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) { return &smtpSession{m: m}, nil }))
	ss.Domain = "localhost"
	ss.AllowInsecureAuth = true
	go func() { _ = ss.Serve(sln) }()
	srv.OnClose(ss.Close)
	srv.Info["imap"], srv.Info["smtp"] = iln.Addr().String(), sln.Addr().String()
	srv.Info["tls"] = useTLS
	m.mu.Lock()
	srv.Info["user"], srv.Info["password"] = m.user, m.password
	m.mu.Unlock()
	return srv, nil
}

// Handler is the control API.
func (m *Mail) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /control/append", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Mailbox      string    `json:"mailbox"`
			Raw          string    `json:"raw"`
			RawB64       []byte    `json:"raw_b64"`
			InternalDate time.Time `json:"internal_date"`
		}
		if !fixtures.ReadJSON(w, r, &req) {
			return
		}
		raw := []byte(req.Raw)
		if req.RawB64 != nil {
			raw = req.RawB64
		}
		if len(raw) == 0 || req.InternalDate.IsZero() {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "raw (or raw_b64) and internal_date are required"})
			return
		}
		if req.Mailbox == "" {
			req.Mailbox = "INBOX"
		}
		d, err := m.Append(req.Mailbox, raw, req.InternalDate)
		if err != nil {
			fixtures.WriteJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		fixtures.WriteJSON(w, 200, map[string]uint32{"uid": uint32(d.UID), "uidvalidity": d.UIDValidity})
	})
	mux.HandleFunc("GET /control/sent", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, m.SentMessages())
	})
	mux.HandleFunc("GET /control/imap", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		out := make([]Conn, len(m.conns))
		for i, c := range m.conns {
			out[i] = Conn{c.Opened, append([]byte{}, c.Received...)}
		}
		m.mu.Unlock()
		fixtures.WriteJSON(w, 200, out)
	})
	mux.HandleFunc("POST /control/credentials", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ User, Password string }
		if !fixtures.ReadJSON(w, r, &req) {
			return
		}
		if req.User == "" || req.Password == "" {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "user and password are required"})
			return
		}
		m.mu.Lock()
		m.user, m.password = req.User, req.Password
		m.mu.Unlock()
		fixtures.WriteJSON(w, 200, map[string]string{"user": req.User})
	})
	return mux
}
