package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"tailscale.com/tsnet"
)

// Tailnet joins the owner's tailnet as its own device (tsnet; state under the root) and
// serves h there. Its WireGuard port is one the OS finds free (Port 0); the API listens
// on the port its caller names on tsnet's own network stack (serve uses HTTP's 80, so a
// paired address survives restarts).
type Tailnet struct {
	Dir        string
	Hostname   string
	AuthKey    string
	ControlURL string // Headscale; empty = Tailscale
	Log        *log.Logger
	OnLogin    func(url string)
	OnUp       func(addr string)
	srv        *tsnet.Server
}

var loginURL = regexp.MustCompile(`https://\S+`)

func (t *Tailnet) Serve(ctx context.Context, port int, h http.Handler) error {
	t.srv = &tsnet.Server{Dir: t.Dir, Hostname: t.Hostname, AuthKey: t.AuthKey, ControlURL: t.ControlURL, Port: 0,
		Logf: func(string, ...any) {}, // tsnet's own chatter stays out of the journal
		UserLogf: func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if t.Log != nil {
				t.Log.Print("tailnet: " + msg)
			}
			if u := loginURL.FindString(msg); u != "" && t.OnLogin != nil && strings.Contains(msg, "login") {
				t.OnLogin(u)
			}
		}}
	defer t.srv.Close()
	for {
		uctx, cancel := context.WithTimeout(ctx, 24*time.Hour) // Up waits for the owner to open the sign-in link
		st, err := t.srv.Up(uctx)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if t.Log != nil {
				t.Log.Printf("tailnet: %v", err)
			}
			continue
		}
		ip4, _ := t.srv.TailscaleIPs()
		ln, err := t.srv.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return err
		}
		addr := net.JoinHostPort(ip4.String(), fmt.Sprint(port))
		if t.OnUp != nil {
			t.OnUp(addr)
		}
		if t.Log != nil && st != nil && st.Self != nil {
			t.Log.Printf("tailnet: up as %s (%s)", strings.TrimSuffix(st.Self.DNSName, "."), addr)
		}
		hs := &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second}
		go func() { <-ctx.Done(); hs.Close() }()
		return hs.Serve(ln)
	}
}
