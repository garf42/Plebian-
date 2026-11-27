// Command fixture starts one fixture and prints one JSON line {"addr": ..., ...} once
// it is listening, then serves until killed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"plebian/tests/fixtures"
	"plebian/tests/fixtures/clock"
	"plebian/tests/fixtures/ical"
	"plebian/tests/fixtures/mail"
	"plebian/tests/fixtures/model"
	"plebian/tests/fixtures/s3"
	"plebian/tests/fixtures/texts"
	"plebian/tests/fixtures/web"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fixture model|time|mail|ical|texts|web|s3 [-addr 127.0.0.1:0]")
		os.Exit(2)
	}
	kind := os.Args[1]
	fs := flag.NewFlagSet(kind, flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "listen address")
	key := fs.String("key", "", "credential the fixture accepts (random when empty)")
	user := fs.String("user", "owner", "account name (mail, web)")
	imapAddr := fs.String("imap", "127.0.0.1:0", "IMAP listen address (mail)")
	smtpAddr := fs.String("smtp", "127.0.0.1:0", "SMTP listen address (mail)")
	useTLS := fs.Bool("tls", false, "implicit TLS on the mail listeners")
	fs.Parse(os.Args[2:])
	if *key == "" {
		*key = fixtures.Secret(16)
	}
	var srv *fixtures.Server
	var err error
	serve := func(h http.Handler) { srv, err = fixtures.ServeHTTP(*addr, h) }
	switch kind {
	case "model":
		serve(model.New(*key).Handler())
	case "time":
		serve(clock.New().Handler())
	case "ical":
		serve(ical.New().Handler())
	case "texts":
		serve(texts.New().Handler())
	case "web":
		serve(web.New(*user, *key).Handler())
	case "s3":
		b := s3.New(*user, *key)
		serve(b.Handler())
	case "mail":
		srv, err = mail.New(*user, *key).Start(*addr, *imapAddr, *smtpAddr, *useTLS)
	default:
		fmt.Fprintln(os.Stderr, "unknown fixture", kind)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, ok := srv.Info["key"]; !ok {
		srv.Info["key"] = *key
	}
	if kind == "s3" || kind == "web" {
		srv.Info["user"] = *user
	}
	b, _ := json.Marshal(srv.Info)
	fmt.Println(string(b))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	srv.Close()
}
