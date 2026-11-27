// Command plebian is the one program: install, update, uninstall, serve, pair, backup,
// restore, replay, and the tools user's runner.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"

	"plebian/server/api"
	"plebian/server/clock"
	"plebian/server/config"
	"plebian/server/core"
	"plebian/server/loop"
	"plebian/server/models"
	"plebian/server/ops"
	"plebian/server/store"
	"plebian/server/tools"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: plebian install|update|uninstall|serve|pair|backup|restore|replay|runner|version [flags]")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "version":
		fmt.Println(api.Version, api.Commit)
	case "install":
		err = install(args)
	case "uninstall":
		fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
		root := fs.String("root", "/opt/plebian", "Plebian's root directory")
		fs.Parse(args)
		err = ops.Uninstall(ops.Root(*root))
	case "serve":
		err = serve(args)
	case "pair":
		err = pair(args)
	case "runner":
		err = runner(args)
	case "replay":
		err = replay(args)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "plebian:", err)
		os.Exit(1)
	}
}

func hostTZ() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	if l, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(l, "zoneinfo/"); i >= 0 {
			return l[i+len("zoneinfo/"):]
		}
	}
	return "UTC"
}

func install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	root := fs.String("root", "/opt/plebian", "Plebian's root directory")
	tz := fs.String("tz", hostTZ(), "the owner's time zone (IANA name)")
	clk := fs.String("clock-source", "", "time endpoint answering {\"unix_ms\"} (tests); empty = host clock")
	texts := fs.String("texts-source", "", "texts endpoint (tests); empty = Google Messages")
	noTailnet := fs.Bool("no-tailnet", false, "serve on localhost only (CI)")
	authKey := fs.String("auth-key-file", "", "a Tailscale auth key file, instead of the sign-in link")
	fs.Parse(args)
	if _, err := time.LoadLocation(*tz); err != nil {
		return fmt.Errorf("--tz: %v", err)
	}
	r := ops.Root(*root)
	if err := ops.Install(ops.InstallOptions{Root: r, Config: ops.Config{TZ: *tz, ClockSource: *clk, TextsSource: *texts, Tailnet: !*noTailnet, AuthKeyFile: *authKey}, Out: os.Stdout}); err != nil {
		return err
	}
	fmt.Println("Plebian is installed in", r)
	return pairAs(r)
}

// pairAs makes a pairing code as the server user (the store is its alone) and prints it.
func pairAs(r ops.Root) error {
	if _, err := user.Lookup(ops.ServerUser); err == nil && os.Geteuid() == 0 {
		cmd := exec.Command("runuser", "-u", ops.ServerUser, "--", r.Bin(), "pair")
		cmd.Env = append(os.Environ(), "PLEBIAN_ROOT="+string(r))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	return pair(nil)
}

func pair(args []string) error {
	r, err := ops.FromExecutable()
	if err != nil {
		return err
	}
	if _, err := user.Lookup(ops.ServerUser); err == nil {
		if os.Geteuid() == 0 {
			return pairAs(r)
		}
		if u, err := user.Current(); err == nil && u.Username != ops.ServerUser {
			return fmt.Errorf("pair runs as root or as %s", ops.ServerUser)
		}
	}
	s, err := store.Open(r.Store(), store.DirBlobs{Dir: r.Blobs()})
	if err != nil {
		return err
	}
	defer s.Close()
	code, err := api.NewPairCode(context.Background(), s, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	var addr struct {
		Addr    string `json:"addr"`
		Tailnet string `json:"tailnet"`
	}
	if b, err := os.ReadFile(r.APIFile()); err == nil {
		json.Unmarshal(b, &addr)
	}
	host := addr.Tailnet
	if host == "" {
		host = addr.Addr
	}
	uri := fmt.Sprintf("plebian://pair?addr=%s&code=%s", host, code)
	if q, err := qrcode.New(uri, qrcode.Medium); err == nil {
		fmt.Println(q.ToSmallString(false))
	}
	fmt.Println("Pairing code:", code)
	fmt.Println(uri)
	return nil
}

func serve(args []string) error {
	r, err := ops.FromExecutable()
	if err != nil {
		return err
	}
	syscall.Umask(0o077)
	cfg, err := r.LoadConfig()
	if err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	s, err := store.Open(r.Store(), store.DirBlobs{Dir: r.Blobs()})
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if s.Setting(ctx, "tz") == "" || s.Setting(ctx, "tz") != cfg.TZ {
		s.SetSetting(ctx, "tz", cfg.TZ)
	}
	clk := clock.New(cfg.ClockSource)
	secrets := &config.Secrets{Path: r.Secrets()}
	counter := models.NewCounter()
	counter.LoadState([]byte(s.Setting(ctx, "counter")))
	reg := tools.NewRegistry(loop.CoreTools(s)...)
	lp := &loop.Loop{S: s, Clock: clk, P: core.Defaults, Tools: reg, Counter: counter, Secrets: secrets, Log: logger}
	if err := lp.Init(ctx); err != nil {
		return err
	}
	srv := &api.Server{S: s, Clock: clk, Log: logger}
	ms := &api.ModelSetup{A: srv, Secrets: secrets}
	srv.Hooks.Wake = lp.Wake
	srv.Hooks.Setup = func(w http.ResponseWriter, req *http.Request, card string) {
		if !ms.Handle(w, req) {
			http.Error(w, "", http.StatusNotImplemented)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 30 * time.Second}
	go hs.Serve(ln)
	b, _ := json.Marshal(map[string]string{"addr": ln.Addr().String(), "tailnet": ""})
	tmp := r.APIFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.APIFile()); err != nil {
		return err
	}
	logger.Printf("serving on %s", ln.Addr())
	go lp.Run(ctx)
	ops.Notify("READY=1")
	stop := make(chan struct{})
	go ops.Heartbeat(func() bool { return s.DB.PingContext(ctx) == nil }, stop)
	<-ctx.Done()
	close(stop)
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	hs.Shutdown(sctx)
	os.Remove(r.APIFile())
	return nil
}

// runner is the tools user's service: it keeps the socket the server hands calls over.
// shortcut: it only holds the socket until n.tools fills in the sandboxed runs.
func runner(args []string) error {
	r, err := ops.FromExecutable()
	if err != nil {
		return err
	}
	syscall.Umask(0o007)
	os.Remove(r.RunnerSock())
	ln, err := net.Listen("unix", r.RunnerSock())
	if err != nil {
		return err
	}
	os.Chmod(r.RunnerSock(), 0o660)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return nil
		}
		c.Close()
	}
}

// replay --verify rebuilds every sealed turn and heard row from its spelling and checks
// each against the hash fixed at seal (TP-T14).
func replay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	verify := fs.Bool("verify", false, "rebuild every sealed turn and heard row and check its hash")
	path := fs.String("store", "", "a store file other than the root's (a restored copy)")
	fs.Parse(args)
	r, err := ops.FromExecutable()
	if err != nil {
		return err
	}
	p := r.Store()
	if *path != "" {
		p = *path
	}
	s, err := store.Open(p, store.DirBlobs{Dir: r.Blobs()})
	if err != nil {
		return err
	}
	defer s.Close()
	if !*verify {
		return fmt.Errorf("replay: say what to replay (--verify)")
	}
	t, h, verr := s.Verify(context.Background())
	out := map[string]any{"turns": t, "heard": h, "ok": verr == nil}
	if verr != nil {
		out["error"] = verr.Error()
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	if verr != nil {
		return verr
	}
	return nil
}
