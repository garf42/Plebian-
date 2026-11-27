// Package api is n.api: the app's only surface (API.md). An unpaired client receives
// nothing and changes nothing; admin actions need a paired device key's signature.
package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"plebian/server/clock"
	"plebian/server/store"
)

// Commit, Version and Source are set at build time (-ldflags -X); Source is the
// repository the release was built in (its releases hold the browser bundle).
var (
	Commit  = "unknown"
	Version = "dev"
	Source  = ""
)

// Hooks are what other nodes plug in; each is optional.
type Hooks struct {
	Wake       func()                                                              // the loop looks for inbounds
	AudioLive  func(w http.ResponseWriter, r *http.Request, device string)         // /v1/audio/live
	AudioFile  func(w http.ResponseWriter, r *http.Request, device string)         // /v1/audio/files
	OmiState   func(ctx context.Context, device string, body []byte) error         // /v1/omi/state
	Admin      func(ctx context.Context, device, action string, body []byte) error // signed actions
	Setup      func(w http.ResponseWriter, r *http.Request, card string)           // /v1/setup/{card}
	Browser    func(w http.ResponseWriter, r *http.Request)                        // /v1/browser/live
	Week       func(ctx context.Context) (any, error)
	SetupCards func(ctx context.Context) (any, error)
}

type Server struct {
	DataDir string // the data drive's directory, for space and days left
	S       *store.Store
	Clock   clock.Clock
	Hooks   Hooks
	Log     *log.Logger
}

const maxBody = 1 << 20

func (a *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"commit": Commit, "version": Version})
	})
	mux.HandleFunc("POST /v1/pair", a.pair)
	auth := func(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			dev, ok := a.device(r)
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r, dev)
		}
	}
	mux.HandleFunc("POST /v1/messages", auth(a.postMessage))
	mux.HandleFunc("GET /v1/thread", auth(a.thread))
	mux.HandleFunc("GET /v1/events", auth(a.events))
	mux.HandleFunc("GET /v1/days", auth(a.days))
	mux.HandleFunc("GET /v1/days/{date}", auth(a.day))
	mux.HandleFunc("GET /v1/today", auth(a.today))
	mux.HandleFunc("GET /v1/taps", auth(a.taps))
	mux.HandleFunc("GET /v1/taps/{id}", auth(a.tap))
	mux.HandleFunc("POST /v1/taps", auth(a.postTap))
	mux.HandleFunc("PATCH /v1/taps/{id}", auth(a.patchTap))
	mux.HandleFunc("GET /v1/open", auth(a.open))
	mux.HandleFunc("GET /v1/search", auth(a.search))
	mux.HandleFunc("GET /v1/blobs/{sha}", auth(a.blob))
	mux.HandleFunc("GET /v1/audio/live", auth(func(w http.ResponseWriter, r *http.Request, d string) { a.hook(a.Hooks.AudioLive, w, r, d) }))
	mux.HandleFunc("POST /v1/audio/files", auth(func(w http.ResponseWriter, r *http.Request, d string) { a.hook(a.Hooks.AudioFile, w, r, d) }))
	mux.HandleFunc("POST /v1/omi/state", auth(a.omiState))
	mux.HandleFunc("GET /v1/week", auth(a.week))
	mux.HandleFunc("GET /v1/setup", auth(a.setupCards))
	mux.HandleFunc("POST /v1/setup/{card...}", auth(func(w http.ResponseWriter, r *http.Request, d string) {
		if a.Hooks.Setup == nil {
			http.Error(w, "", http.StatusNotImplemented)
			return
		}
		a.Hooks.Setup(w, r, r.PathValue("card"))
	}))
	mux.HandleFunc("POST /v1/admin/{action}", auth(a.admin))
	mux.HandleFunc("GET /v1/browser/live", auth(func(w http.ResponseWriter, r *http.Request, d string) {
		if a.Hooks.Browser == nil {
			http.Error(w, "", http.StatusNotImplemented)
			return
		}
		a.Hooks.Browser(w, r)
	}))
	return mux
}

func (a *Server) hook(h func(http.ResponseWriter, *http.Request, string), w http.ResponseWriter, r *http.Request, d string) {
	if h == nil {
		http.Error(w, "", http.StatusNotImplemented)
		return
	}
	h(w, r, d)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (a *Server) now() string { return a.Clock.Now().UTC().Format(time.RFC3339Nano) }

func tokenHash(t string) []byte { h := sha256.Sum256([]byte(t)); return h[:] }

// device returns the paired device a bearer token belongs to.
func (a *Server) device(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || tok == "" {
		// WebSocket clients that can't set headers pass the token as a query parameter.
		tok = r.URL.Query().Get("token")
		if tok == "" {
			return "", false
		}
	}
	var id string
	if err := a.S.DB.QueryRowContext(r.Context(), `SELECT id FROM devices WHERE token_hash=?`, tokenHash(tok)).Scan(&id); err != nil {
		return "", false
	}
	return id, true
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "=")
}

// NewPairCode makes a one-time pairing code: 128 random bits (the 128-bit security level).
func NewPairCode(ctx context.Context, s *store.Store, at string) (string, error) {
	code := random(16)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO pair_codes(code_hash, created_at) VALUES(?,?)`, tokenHash(code), at)
	return code, err
}

func (a *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code      string `json:"code"`
		PublicKey string `json:"public_key"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil || req.Code == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	der, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		http.Error(w, "bad public key", http.StatusBadRequest)
		return
	}
	pk, err := x509.ParsePKIXPublicKey(der)
	if _, ok := pk.(*ecdsa.PublicKey); err != nil || !ok {
		http.Error(w, "bad public key", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tx, err := a.S.DB.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE pair_codes SET used_at=? WHERE code_hash=? AND used_at IS NULL`, a.now(), tokenHash(strings.ToUpper(strings.TrimSpace(req.Code))))
	if n, _ := res.RowsAffected(); err != nil || n != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	id, tok := random(10), random(32)
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, name, public_key, token_hash, paired_at) VALUES(?,?,?,?,?)`, id, req.Name, der, tokenHash(tok), a.now()); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"device_id": id, "token": tok})
}

func (a *Server) postMessage(w http.ResponseWriter, r *http.Request, dev string) {
	var m struct {
		ClientMsgID string          `json:"client_msg_id"`
		Text        string          `json:"text"`
		SentAt      string          `json:"sent_at"`
		Quote       json.RawMessage `json:"quote"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&m); err != nil || strings.TrimSpace(m.Text) == "" || m.ClientMsgID == "" {
		http.Error(w, "bad message", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse(time.RFC3339Nano, m.SentAt); err != nil {
		m.SentAt = a.now()
	}
	quote := ""
	if len(m.Quote) > 0 && string(m.Quote) != "null" {
		quote = string(m.Quote)
	}
	ctx := r.Context()
	res, err := a.S.DB.ExecContext(ctx, `INSERT INTO owner_messages(device, client_msg_id, text, quote, sent_at, received_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(device, client_msg_id) DO NOTHING`, dev, m.ClientMsgID, m.Text, quote, m.SentAt, a.now())
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	var id int64
	a.S.DB.QueryRowContext(ctx, `SELECT id FROM owner_messages WHERE device=? AND client_msg_id=?`, dev, m.ClientMsgID).Scan(&id)
	if n, _ := res.RowsAffected(); n == 1 {
		ev, _ := a.S.Emit(ctx, a.now(), "message", map[string]any{"from": "owner", "text": m.Text, "client_msg_id": m.ClientMsgID, "notify": false}, 0)
		a.S.DB.ExecContext(ctx, `UPDATE owner_messages SET event=? WHERE id=?`, ev, id)
		if a.Hooks.Wake != nil {
			a.Hooks.Wake()
		}
	}
	writeJSON(w, map[string]any{"id": fmt.Sprint(id)})
}

func (a *Server) admin(w http.ResponseWriter, r *http.Request, dev string) {
	body, err := store.ReadAllLimit(r.Body, maxBody)
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Plebian-Signature"))
	if err != nil || len(sig) == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var der []byte
	if err := a.S.DB.QueryRowContext(r.Context(), `SELECT public_key FROM devices WHERE id=?`, dev).Scan(&der); err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	pk, err := x509.ParsePKIXPublicKey(der)
	epk, ok := pk.(*ecdsa.PublicKey)
	if err != nil || !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	h := sha256.Sum256(body)
	if !ecdsa.VerifyASN1(epk, h[:], sig) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var v struct {
		Nonce string `json:"nonce"`
		At    string `json:"at"`
	}
	if json.Unmarshal(body, &v) != nil || v.Nonce == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if _, err := a.S.DB.ExecContext(r.Context(), `INSERT INTO nonces(device, nonce) VALUES(?,?)`, dev, v.Nonce); err != nil {
		w.WriteHeader(http.StatusForbidden) // a replayed nonce
		return
	}
	if a.Hooks.Admin == nil {
		http.Error(w, "", http.StatusNotImplemented)
		return
	}
	if err := a.Hooks.Admin(r.Context(), dev, r.PathValue("action"), body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *Server) omiState(w http.ResponseWriter, r *http.Request, dev string) {
	b, err := store.ReadAllLimit(r.Body, maxBody)
	if err != nil || !json.Valid(b) {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	if a.Hooks.OmiState != nil {
		if err := a.Hooks.OmiState(r.Context(), dev, b); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *Server) week(w http.ResponseWriter, r *http.Request, _ string) {
	if a.Hooks.Week == nil {
		writeJSON(w, map[string]any{"brief": []any{}, "health": a.health(r.Context()), "spend": a.spend(r.Context())})
		return
	}
	v, err := a.Hooks.Week(r.Context())
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, v)
}

// health is This week's health: each feed's last landing, the model's last answer and
// last failure, disk space and days left at the last week's rate, and the last backup.
func (a *Server) health(ctx context.Context) map[string]any {
	feeds := []map[string]any{}
	if rows, err := a.S.DB.QueryContext(ctx, `SELECT feed, COALESCE(last_landing,'') FROM feeds ORDER BY feed`); err == nil {
		for rows.Next() {
			var f, l string
			rows.Scan(&f, &l)
			feeds = append(feeds, map[string]any{"feed": f, "last_landing": l})
		}
		rows.Close()
	}
	var lastReply, lastFail, failWhy string
	a.S.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(at),'') FROM turns`).Scan(&lastReply)
	a.S.DB.QueryRowContext(ctx, `SELECT at, body FROM events WHERE type='step' AND json_extract(body,'$.tool')='model' AND json_extract(body,'$.state')='failed' ORDER BY id DESC LIMIT 1`).Scan(&lastFail, &failWhy)
	out := map[string]any{"feeds": feeds, "model": map[string]any{"last_answer": lastReply, "last_failure": lastFail},
		"backup": a.S.Setting(ctx, "backups_status"), "restore_drill": a.S.Setting(ctx, "restore_drill")}
	if a.DataDir != "" {
		var fs syscall.Statfs_t
		if syscall.Statfs(a.DataDir, &fs) == nil {
			free := int64(fs.Bavail) * int64(fs.Bsize)
			week := a.Clock.Now().Add(-7 * 24 * time.Hour)
			var grew int64
			filepath.Walk(a.DataDir, func(_ string, fi os.FileInfo, err error) error {
				if err == nil && !fi.IsDir() && fi.ModTime().After(week) {
					grew += fi.Size()
				}
				return nil
			})
			disk := map[string]any{"free_bytes": free, "written_last_7_days": grew}
			if grew > 0 {
				disk["days_left"] = free / (grew/7 + 1)
			}
			out["disk"] = disk
		}
	}
	return out
}

func (a *Server) spend(ctx context.Context) []map[string]any {
	rows, err := a.S.DB.QueryContext(ctx, `SELECT model, day, tokens_in, tokens_out, tokens_cached FROM spend ORDER BY day DESC, model`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var m, d string
		var i, o, c int
		rows.Scan(&m, &d, &i, &o, &c)
		out = append(out, map[string]any{"model": m, "day": d, "in": i, "out": o, "cached": c})
	}
	return out
}

func (a *Server) setupCards(w http.ResponseWriter, r *http.Request, _ string) {
	if a.Hooks.SetupCards == nil {
		writeJSON(w, map[string]any{"cards": []any{}})
		return
	}
	v, err := a.Hooks.SetupCards(r.Context())
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, v)
}

func (a *Server) blob(w http.ResponseWriter, r *http.Request, _ string) {
	b, err := a.S.Blobs.Get(r.Context(), r.PathValue("sha"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, strings.NewReader(string(b)))
}

var errNotFound = errors.New("not found")

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
