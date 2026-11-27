package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"plebian/server/core"
)

// Blobs holds originals (audio, messages as received) addressed by sha256.
type Blobs interface {
	// Put stores b and returns its sha256; it returns only once the bytes are durable.
	Put(ctx context.Context, b []byte) (string, error)
	Get(ctx context.Context, sha string) ([]byte, error)
	Has(ctx context.Context, sha string) (bool, error)
}

// DirBlobs keeps blobs as files under a directory: <dir>/<ab>/<sha>.
type DirBlobs struct{ Dir string }

func validSha(sha string) error {
	if len(sha) != 64 {
		return errors.New("blobs: bad sha256")
	}
	if _, err := hex.DecodeString(sha); err != nil {
		return errors.New("blobs: bad sha256")
	}
	return nil
}

func (d DirBlobs) path(sha string) string { return filepath.Join(d.Dir, sha[:2], sha) }

func (d DirBlobs) Put(_ context.Context, b []byte) (string, error) {
	h := sha256.Sum256(b)
	sha := hex.EncodeToString(h[:])
	p := d.path(sha)
	if _, err := os.Stat(p); err == nil {
		return sha, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", err
	}
	dir, err := os.Open(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	defer dir.Close()
	return sha, dir.Sync()
}

func (d DirBlobs) Get(_ context.Context, sha string) ([]byte, error) {
	if err := validSha(sha); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(d.path(sha))
	if err != nil {
		return nil, err
	}
	if h := sha256.Sum256(b); hex.EncodeToString(h[:]) != sha {
		return nil, fmt.Errorf("blobs: %s does not match its bytes", sha)
	}
	return b, nil
}

func (d DirBlobs) Has(_ context.Context, sha string) (bool, error) {
	if err := validSha(sha); err != nil {
		return false, err
	}
	_, err := os.Stat(d.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// ReadAllLimit reads r up to limit bytes; more is an error, so nothing is silently cut.
func ReadAllLimit(r io.Reader, limit int64) ([]byte, error) {
	var b bytes.Buffer
	n, err := io.Copy(&b, io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, errors.New("too large")
	}
	return b.Bytes(), nil
}

// HeardRow is one stretch of talk, message or calendar entry as it landed.
type HeardRow struct {
	Source string // talk | mail | text | calendar
	At     string // RFC 3339 wall time of its start
	End    string
	Bytes  []byte // exact bytes: lines per speaker or sender (see underlay)
	Blob   string // sha256 of the original
	Ext    string // source key
	Covers int64  // earlier row this one is a newer reading of, or 0
	Landed string
}

// AddHeard writes a heard row once, durably, and returns its id.
func (s *Store) AddHeard(ctx context.Context, r HeardRow) (int64, error) {
	h := core.Hash(r.Bytes)
	sp, err := s.Speller().Spell("heard", r.Bytes)
	if err != nil {
		return 0, err
	}
	var covers any
	if r.Covers != 0 {
		covers = r.Covers
	}
	var tick int64
	s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(tick),0)+1 FROM heard WHERE source=?`, r.Source).Scan(&tick)
	res, err := s.Durable(ctx, `INSERT INTO heard(source, at, end_at, spelling, sha, blob, ext, covers, tick, landed_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.Source, r.At, r.End, sp, h[:], r.Blob, r.Ext, covers, tick, r.Landed)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// List returns every blob's sha256 in the directory.
func (d DirBlobs) List() ([]string, error) {
	var out []string
	err := filepath.WalkDir(d.Dir, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if !e.IsDir() && validSha(e.Name()) == nil {
			out = append(out, e.Name())
		}
		return nil
	})
	return out, err
}

// Remove deletes one blob from the directory (once it lives elsewhere).
func (d DirBlobs) Remove(sha string) error {
	if err := validSha(sha); err != nil {
		return err
	}
	return os.Remove(d.path(sha))
}
