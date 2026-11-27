// Package store is n.store: one SQLite file holding every TP-03 table, and a blob
// store addressed by sha256. A row is durable before the call it records runs.
package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"plebian/server/core"

	_ "modernc.org/sqlite"
)

// Speller turns a sheet's or heard row's exact bytes into its spelling and back.
// n.grammar provides it; the store keeps only the spelling and the hash (D5).
type Speller interface {
	Spell(body string, b []byte) ([]byte, error)
	Expand(body string, spelling []byte) ([]byte, error)
}

// Store is the open store.
type Store struct {
	DB      *sql.DB
	Path    string
	Blobs   Blobs
	Hub     Hub
	mu      sync.Mutex
	speller Speller
}

// Open opens or creates the store file with mode 0600 and applies migrations.
// synchronous=FULL in WAL mode makes each commit durable before Seal returns.
func Open(path string, blobs Blobs) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // shortcut: one connection serializes all access; upgrade to a reader pool if projections contend with turns
	s := &Store{DB: db, Path: path, Blobs: blobs, speller: Loose{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// SetSpeller installs the grammar's speller. Rows written under another speller keep
// rebuilding because every spelling carries its own format tag (see Loose).
func (s *Store) SetSpeller(sp Speller) { s.mu.Lock(); s.speller = sp; s.mu.Unlock() }

func (s *Store) Speller() Speller { s.mu.Lock(); defer s.mu.Unlock(); return s.speller }

func (s *Store) Close() error { return s.DB.Close() }

// TurnMeta is what a sealed turn records beside its bytes.
type TurnMeta struct {
	At            string // wall time of sealing (addressing only)
	Shown         string // JSON list of fragment ids the spotlight showed
	HeardOpened   bool
	OutsideInView bool
	Model         string
	RequestID     string
	Bands         string // JSON list of band shas the request used
}

// Seal writes sheet n durably and returns n. Its bytes never change afterwards.
func (s *Store) Seal(ctx context.Context, sh core.Sheet, m TurnMeta) (int64, error) {
	b := sh.Bytes()
	h := core.Hash(b)
	sp, err := s.Speller().Spell("thread", b)
	if err != nil {
		return 0, err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO turns(opener, spelling, sha, at, shown, heard_opened, outside_in_view, model, request_id, bands)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, string(sh.Opener), sp, h[:], m.At, m.Shown, m.HeardOpened, m.OutsideInView, m.Model, m.RequestID, m.Bands)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ErrHash means a row's spelling no longer rebuilds the bytes fixed at seal.
var ErrHash = errors.New("store: rebuilt bytes do not match the hash fixed at seal")

// TurnBytes rebuilds sealed turn n from its spelling alone and checks it against its hash.
func (s *Store) TurnBytes(ctx context.Context, n int64) ([]byte, error) {
	var sp, sha []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT spelling, sha FROM turns WHERE id=?`, n).Scan(&sp, &sha); err != nil {
		return nil, err
	}
	b, err := s.Speller().Expand("thread", sp)
	if err != nil {
		return nil, err
	}
	if h := core.Hash(b); hex.EncodeToString(h[:]) != hex.EncodeToString(sha) {
		return nil, fmt.Errorf("%w (turn %d)", ErrHash, n)
	}
	return b, nil
}

// Turn rebuilds and parses sealed turn n.
func (s *Store) Turn(ctx context.Context, n int64) (core.Sheet, error) {
	b, err := s.TurnBytes(ctx, n)
	if err != nil {
		return core.Sheet{}, err
	}
	return core.ParseSheet(b)
}

// HeardBytes rebuilds heard row id from its spelling and checks its hash.
func (s *Store) HeardBytes(ctx context.Context, id int64) ([]byte, error) {
	var sp, sha []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT spelling, sha FROM heard WHERE id=?`, id).Scan(&sp, &sha); err != nil {
		return nil, err
	}
	b, err := s.Speller().Expand("heard", sp)
	if err != nil {
		return nil, err
	}
	if h := core.Hash(b); string(h[:]) != string(sha) {
		return nil, fmt.Errorf("%w (heard %d)", ErrHash, id)
	}
	return b, nil
}

// Verify rebuilds every sealed turn and heard row and checks each hash (TP-T14).
func (s *Store) Verify(ctx context.Context) (turns, heard int, err error) {
	ids := func(q string) ([]int64, error) {
		rows, err := s.DB.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
	t, err := ids(`SELECT id FROM turns ORDER BY id`)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range t {
		if _, err := s.TurnBytes(ctx, id); err != nil {
			return turns, heard, err
		}
		turns++
	}
	h, err := ids(`SELECT id FROM heard ORDER BY id`)
	if err != nil {
		return turns, 0, err
	}
	for _, id := range h {
		if _, err := s.HeardBytes(ctx, id); err != nil {
			return turns, heard, err
		}
		heard++
	}
	return turns, heard, nil
}

// Snapshot writes a consistent copy of the store (write-ahead log included) to dst
// while the server keeps serving.
func (s *Store) Snapshot(ctx context.Context, dst string) error {
	os.Remove(dst)
	_, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, dst)
	if err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}

// Setting reads one setting; "" when unset.
func (s *Store) Setting(ctx context.Context, key string) string {
	var v string
	s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// Loose is the speller before the grammar runs: tag 0x00 then the bytes as written.
// Every speller's spelling starts with a format tag, so rows spelled under any of
// them keep rebuilding after another is installed.
type Loose struct{}

func (Loose) Spell(_ string, b []byte) ([]byte, error) { return append([]byte{0}, b...), nil }
func (Loose) Expand(_ string, sp []byte) ([]byte, error) {
	if len(sp) == 0 || sp[0] != 0 {
		return nil, errors.New("store: not a loose spelling")
	}
	return sp[1:], nil
}
