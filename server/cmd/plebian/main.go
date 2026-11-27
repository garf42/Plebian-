// Command plebian is the one program: install, update, uninstall, serve, pair, backup,
// restore, replay, and the tools user's runner.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"

	"plebian/server/api"
	"plebian/server/assemble"
	"plebian/server/audio"
	"plebian/server/clock"
	"plebian/server/config"
	"plebian/server/core"
	"plebian/server/feeds"
	"plebian/server/grammar"
	"plebian/server/loop"
	"plebian/server/models"
	"plebian/server/ops"
	"plebian/server/recall"
	"plebian/server/speech"
	"plebian/server/store"
	"plebian/server/tools"
	"plebian/server/underlay"
	"plebian/server/watch"
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
	case "restore":
		fs := flag.NewFlagSet("restore", flag.ExitOnError)
		root := fs.String("root", "/opt/plebian", "the installed root to restore into")
		from := fs.String("from", "", "the restic repository holding the backup")
		keyFile := fs.String("key-file", "", "a file holding the backup key the owner kept (read from stdin when empty)")
		fs.Parse(args)
		var kb []byte
		if *keyFile != "" {
			kb, err = os.ReadFile(*keyFile)
		} else {
			kb, err = io.ReadAll(io.LimitReader(os.Stdin, 4096))
		}
		if err == nil {
			err = ops.Restore(ops.Root(*root), *from, strings.TrimSpace(string(kb)))
		}
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
	control := fs.String("control-url", "", "a Headscale server's URL (empty = Tailscale)")
	speechFrom := fs.String("speech-from", "", "a directory holding the speech model files instead of downloading them; none skips them")
	fs.Parse(args)
	if _, err := time.LoadLocation(*tz); err != nil {
		return fmt.Errorf("--tz: %v", err)
	}
	r := ops.Root(*root)
	if err := ops.Install(ops.InstallOptions{Root: r, Config: ops.Config{TZ: *tz, ClockSource: *clk, TextsSource: *texts, Tailnet: !*noTailnet, AuthKeyFile: *authKey, ControlURL: *control}, Out: os.Stdout, SpeechFrom: *speechFrom}); err != nil {
		return err
	}
	fmt.Println("Plebian is installed in", r)
	if !*noTailnet {
		if !waitTailnet(r) {
			fmt.Println("Once this box is on your tailnet, run: sudo", r.Bin(), "pair")
			return nil
		}
	}
	return pairAs(r)
}

// waitTailnet prints the tailnet sign-in link and waits for the server to join.
func waitTailnet(r ops.Root) bool {
	shown := ""
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		var a struct{ Tailnet, Login string }
		if b, err := os.ReadFile(r.APIFile()); err == nil {
			json.Unmarshal(b, &a)
		}
		if a.Tailnet != "" {
			fmt.Println("On your tailnet as", a.Tailnet)
			return true
		}
		if a.Login != "" && a.Login != shown {
			fmt.Println("Sign this box into your tailnet:", a.Login)
			shown = a.Login
		}
		time.Sleep(time.Second)
	}
	return false
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
	dirBlobs := store.DirBlobs{Dir: r.Blobs()}
	blobs := store.NewSwitchBlobs(dirBlobs)
	bucketSecrets := &config.Secrets{Path: r.Secrets()}
	var bucket store.Bucket
	if json.Unmarshal([]byte(bucketSecrets.Get("bucket")), &bucket) == nil && bucket.Bucket != "" {
		blobs.Use(store.S3Blobs{B: bucket, HTTP: &http.Client{Timeout: 5 * time.Minute}})
	}
	s, err := store.Open(r.Store(), blobs)
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if s.Setting(ctx, "tz") == "" || s.Setting(ctx, "tz") != cfg.TZ {
		s.SetSetting(ctx, "tz", cfg.TZ)
	}
	gr, err := grammar.Open(ctx, s)
	if err != nil {
		return err
	}
	s.SetSpeller(gr)
	clk := clock.New(cfg.ClockSource)
	secrets := &config.Secrets{Path: r.Secrets()}
	counter := models.NewCounter()
	counter.LoadState([]byte(s.Setting(ctx, "counter")))
	an := &audio.Node{S: s, Clock: clk, NewDecoder: newDecoder, FrameOf: speech.FrameSamples, Log: logger,
		Gap: func(context.Context) time.Duration { return core.Defaults.UsualGap }}
	if eng, err := speech.Load(speech.Models{Dir: r.Speech()}); err != nil {
		logger.Printf("speech: %v", err)
	} else {
		an.E = engine{eng}
	}
	rc := &recall.R{S: s, G: gr, P: core.Defaults}
	hc := tools.HTTPClient()
	runner := tools.Client{Socket: r.RunnerSock()}
	var lp *loop.Loop
	var un *underlay.U
	var ask func(ctx context.Context, text string) error
	usual := func(ctx context.Context) time.Duration { return lp.Usual(ctx) }
	lander := &feeds.Lander{S: s, Clock: clk, Land: func(ctx context.Context, id int64) {
		if err := un.Land(ctx, id); err != nil {
			logger.Printf("landing %d: %v", id, err)
		}
	}}
	feedWatch := func(name string) *watch.Watch {
		return &watch.Watch{S: s, Clock: clk, Feed: name, Ask: func(ctx context.Context, t string) error { return ask(ctx, t) }}
	}
	mailFeed := &feeds.Mail{L: lander, Watch: feedWatch("mail"), Secrets: secrets, Usual: usual, Log: logger}
	calFeed := &feeds.Calendar{L: lander, Watch: feedWatch("calendar"), Secrets: secrets, HTTP: hc, Usual: usual, Log: logger,
		Loc: func(ctx context.Context) *time.Location { return api.Location(ctx, s) }}
	textsFeed := &feeds.Texts{L: lander, Watch: feedWatch("texts"), Secrets: secrets, HTTP: hc, Usual: usual, Log: logger}
	reg := tools.NewRegistry(append(loop.CoreTools(s, an.Name, feeds.PutOut(s, runner)),
		feeds.SendTool{M: mailFeed, Clock: clk},
		feeds.TextTool{T: textsFeed},
		tools.Shell{Write: false, Runner: runner, Secrets: secrets},
		tools.Shell{Write: true, Runner: runner, Secrets: secrets},
		tools.Fetch{HTTP: hc},
		tools.Search{HTTP: hc, URL: func() string { return s.Setting(context.Background(), "search_url") }},
	)...)
	lp = &loop.Loop{S: s, Clock: clk, P: core.Defaults, Tools: reg, Counter: counter, Secrets: secrets, Log: logger,
		Index: func(ctx context.Context, n int64, sh core.Sheet, heard bool) error {
			ix, err := gr.Index(ctx, n, sh, heard, core.Defaults)
			if err != nil {
				return err
			}
			return rc.Cover(ctx, ix.Fragments, n)
		},
		A:     &assemble.Assembler{S: s, G: gr, R: rc, P: core.Defaults, Counter: counter},
		R:     rc,
		Sleep: gr.Sleep}
	if err := lp.Init(ctx); err != nil {
		return err
	}
	un = &underlay.U{S: s, G: gr, P: core.Defaults,
		Info: func(ctx context.Context, sym int64) float64 { return rc.Info(ctx, sym, grammar.Thread) },
		Open: func(ctx context.Context, body string) error { return lp.Enqueue(ctx, "landing", body) }}
	lp.Heard = un.Lit
	an.Land = func(ctx context.Context, id int64) {
		if err := un.Land(ctx, id); err != nil {
			logger.Printf("landing %d: %v", id, err)
		}
	}
	ask = func(ctx context.Context, text string) error { return lp.Enqueue(ctx, "question", text) }
	an.Ask = ask
	an.Watch = &watch.Watch{S: s, Clock: clk, Feed: "omi", Ask: ask}
	go an.Watch.Run(ctx)
	for _, w := range []*watch.Watch{mailFeed.Watch, calFeed.Watch, textsFeed.Watch} {
		go w.Run(ctx)
	}
	go mailFeed.Run(ctx)
	go calFeed.Run(ctx)
	go textsFeed.Run(ctx)
	fs := &feeds.Setup{Mail: mailFeed, Cal: calFeed, Texts: textsFeed}
	bk := &ops.Backup{S: s, Root: r, Secrets: secrets, Clock: clk, Log: logger, Loc: func(ctx context.Context) *time.Location { return api.Location(ctx, s) }}
	go bk.Run(ctx)
	srv := &api.Server{S: s, Clock: clk, Log: logger}
	ms := &api.ModelSetup{A: srv, Secrets: secrets}
	srv.Hooks.Wake = lp.Wake
	srv.Hooks.Admin = func(ctx context.Context, dev, action string, body []byte) error {
		var a struct {
			Step  string `json:"step"`
			Nonce string `json:"nonce"`
		}
		if err := json.Unmarshal(body, &a); err != nil {
			return err
		}
		switch action {
		case "grant":
			return lp.Grant(ctx, a.Step, dev+":"+a.Nonce)
		}
		return fmt.Errorf("unknown admin action %q", action)
	}
	srv.Hooks.AudioLive = an.LiveHandler
	srv.Hooks.AudioFile = an.FileHandler
	srv.Hooks.OmiState = func(ctx context.Context, _ string, b []byte) error { return an.OmiState(ctx, b) }
	srv.Hooks.Setup = func(w http.ResponseWriter, req *http.Request, card string) {
		if card == "storage" {
			var b struct {
				Bucket *store.Bucket `json:"bucket"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<20)).Decode(&b); err != nil || b.Bucket == nil || b.Bucket.Endpoint == "" || b.Bucket.Bucket == "" {
				http.Error(w, "a bucket (endpoint, bucket, key, secret, region) is required", http.StatusBadRequest)
				return
			}
			s3 := store.S3Blobs{B: *b.Bucket, HTTP: &http.Client{Timeout: 5 * time.Minute}}
			// The bucket is checked with a write and a read before anything moves.
			probe := []byte("plebian bucket check")
			if sha, err := s3.Put(req.Context(), probe); err != nil {
				http.Error(w, "the bucket refused a write: "+err.Error(), http.StatusBadGateway)
				return
			} else if got, err := s3.Get(req.Context(), sha); err != nil || string(got) != string(probe) {
				http.Error(w, "the bucket did not return what was written", http.StatusBadGateway)
				return
			}
			bb, _ := json.Marshal(b.Bucket)
			if err := secrets.Set("bucket", string(bb)); err != nil {
				http.Error(w, "", http.StatusInternalServerError)
				return
			}
			blobs.Use(s3) // new originals go to the bucket from here on
			n, err := store.MoveToBucket(req.Context(), dirBlobs, s3)
			if err != nil {
				http.Error(w, fmt.Sprintf("moved %d originals, then: %v", n, err), http.StatusBadGateway)
				return
			}
			s.SetSetting(req.Context(), "bucket", b.Bucket.Endpoint+"/"+b.Bucket.Bucket)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "moved": n})
			return
		}
		if card == "backups" {
			var b struct {
				SecondDisk string `json:"second_disk"`
				Offsite    string `json:"offsite"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<20)).Decode(&b); err != nil {
				http.Error(w, "", http.StatusBadRequest)
				return
			}
			key, err := bk.Setup(req.Context(), b.SecondDisk, b.Offsite)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"key": key}) // shown once; the owner keeps it
			return
		}
		if !ms.Handle(w, req) && !fs.Handle(w, req) {
			http.Error(w, "", http.StatusNotImplemented)
		}
	}
	srv.Hooks.SetupCards = func(ctx context.Context) (any, error) {
		return map[string]any{"cards": append(ms.Cards(ctx), fs.Cards(ctx)...)}, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 30 * time.Second}
	go hs.Serve(ln)
	var apiMu sync.Mutex
	apiState := map[string]string{"addr": ln.Addr().String(), "tailnet": "", "login": ""}
	writeAPI := func(k, v string) error {
		apiMu.Lock()
		defer apiMu.Unlock()
		apiState[k] = v
		b, _ := json.Marshal(apiState)
		tmp := r.APIFile() + ".tmp"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, r.APIFile())
	}
	if err := writeAPI("addr", ln.Addr().String()); err != nil {
		return err
	}
	logger.Printf("serving on %s", ln.Addr())
	if cfg.Tailnet {
		key := ""
		if cfg.AuthKeyFile != "" {
			if b, err := os.ReadFile(cfg.AuthKeyFile); err == nil {
				key = strings.TrimSpace(string(b))
			}
		}
		tn := &api.Tailnet{Dir: r.Tailnet(), Hostname: "plebian", AuthKey: key, ControlURL: cfg.ControlURL, Log: logger,
			OnLogin: func(u string) { writeAPI("login", u) },
			OnUp:    func(a string) { writeAPI("login", ""); writeAPI("tailnet", a) }}
		go func() {
			if err := tn.Serve(ctx, ln.Addr().(*net.TCPAddr).Port, srv.Handler()); err != nil && ctx.Err() == nil {
				logger.Printf("tailnet: %v", err)
			}
		}()
	}
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

// runner is the tools user's service: it keeps the socket the server hands calls over
// and runs each inside bubblewrap (the release's own bwrap, else the host's).
func runner(args []string) error {
	r, err := ops.FromExecutable()
	if err != nil {
		return err
	}
	syscall.Umask(0o007)
	bw := r.P("bin", "bwrap")
	if _, err := os.Stat(bw); err != nil {
		bw = "/usr/bin/bwrap"
	}
	os.Remove(r.RunnerSock())
	ln, err := net.Listen("unix", r.RunnerSock())
	if err != nil {
		return err
	}
	os.Chmod(r.RunnerSock(), 0o660)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return (&tools.Runner{Workspace: r.Workspace(), Outside: r.Outside(), Bwrap: bw}).Serve(ctx, ln)
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
	gr, err := grammar.Open(context.Background(), s)
	if err != nil {
		return err
	}
	s.SetSpeller(gr)
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
