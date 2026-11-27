// Package fixtures holds what every fixture shares: the byte-exact request log,
// JSON helpers, listeners, a throwaway TLS certificate and a PDF writer.
package fixtures

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Request is one HTTP request exactly as a fixture received it.
type Request struct {
	Time    time.Time   `json:"time"`
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"` // exact bytes; base64 in JSON
}

// Log records requests in arrival order.
type Log struct {
	mu     sync.Mutex
	reqs   []Request
	redact []string
}

// NewLog returns a log that replaces the values of the named headers.
func NewLog(redact ...string) *Log { return &Log{redact: redact} }

// Record stores r with its whole body and hands the body back to r.
func (l *Log) Record(r *http.Request) (Request, error) {
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	h := r.Header.Clone()
	for _, k := range l.redact {
		if h.Get(k) != "" {
			h.Set(k, "[redacted]")
		}
	}
	if r.Host != "" {
		h.Set("Host", r.Host)
	}
	req := Request{Time: time.Now().UTC(), Method: r.Method, URL: r.URL.RequestURI(), Headers: h, Body: body}
	l.mu.Lock()
	l.reqs = append(l.reqs, req)
	l.mu.Unlock()
	return req, err
}

// List returns a copy of every request recorded, oldest first.
func (l *Log) List() []Request {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Request{}, l.reqs...)
}

// Reset forgets every request.
func (l *Log) Reset() {
	l.mu.Lock()
	l.reqs = nil
	l.mu.Unlock()
}

// Recording wraps h so every request is logged before h sees it.
func (l *Log) Recording(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := l.Record(r); err != nil {
			http.Error(w, "fixture: reading body: "+err.Error(), http.StatusBadRequest)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// WriteJSON writes v as the JSON body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ReadJSON decodes the request body into v and answers 400 itself on failure.
func ReadJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "fixture: bad JSON: " + err.Error()})
		return false
	}
	return true
}

// Server is one started fixture: what it prints and how to stop it.
type Server struct {
	Info  map[string]any
	close []func() error
}

// OnClose adds f to what Close runs.
func (s *Server) OnClose(f func() error) { s.close = append(s.close, f) }

// Close stops everything the fixture started.
func (s *Server) Close() error {
	var first error
	for i := len(s.close) - 1; i >= 0; i-- {
		if err := s.close[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ServeHTTP listens on addr and serves h; the bound address lands in Info["addr"].
func ServeHTTP(addr string, h http.Handler) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	hs := &http.Server{Handler: h}
	go func() { _ = hs.Serve(ln) }()
	s := &Server{Info: map[string]any{"addr": ln.Addr().String()}}
	s.OnClose(hs.Close)
	return s, nil
}

// Secret returns n random bytes as hex: a credential a fixture accepts and prints.
func Secret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// SelfSigned makes a certificate for 127.0.0.1 and localhost and returns it
// with its PEM, which a client adds as its only trusted root.
func SelfSigned() (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "plebian fixture"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour), // shortcut: a day, the longest a test run lasts; upgrade if fixtures run longer
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	pemCert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pemCert, nil
}

// PDF writes a one-page PDF 1.4 holding lines as text, one per line, in the
// standard Helvetica font, so a PDF text extractor returns them verbatim.
func PDF(lines []string) []byte {
	var content strings.Builder
	// Letter page (612x792 pt), 12 pt type on 16 pt lines from a 72 pt (1 in) margin.
	content.WriteString("BT /F1 12 Tf 72 720 Td 16 TL\n")
	for _, l := range lines {
		r := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`)
		fmt.Fprintf(&content, "(%s) Tj T*\n", r.Replace(l))
	}
	content.WriteString("ET")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// InvoiceLines is the text of the generated invoice the web store serves and
// scenarios attach to mail: every fact on it is a fixed, checkable string.
var InvoiceLines = []string{
	"INVOICE INV-0042",
	"Ridgeline Gravel and Pipe",
	"Bill to: Plebian Owner",
	"15 yards crushed gravel, delivered 2026-11-12",
	"20 ft culvert pipe x 1",
	"Total due: USD 412.50",
	"Payment due by 2026-12-15",
}
