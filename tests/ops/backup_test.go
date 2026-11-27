package ops_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plebian/server/config"
	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/ops"
	"plebian/server/store"
	"plebian/tests/kit"
)

var ctx = context.Background()

func open(t *testing.T, r ops.Root) *store.Store {
	s, err := store.Open(r.Store(), store.DirBlobs{Dir: r.Blobs()})
	if err != nil {
		t.Fatal(err)
	}
	g, err := grammar.Open(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpeller(g)
	return s
}

func root(t *testing.T, restic string) ops.Root {
	r := ops.Root(t.TempDir())
	for _, d := range []string{"bin", "data/blobs", "run", "home/server", "secrets"} {
		os.MkdirAll(r.P(d), 0o700)
	}
	if err := os.Symlink(restic, r.P("bin", "restic")); err != nil {
		t.Fatal(err)
	}
	return r
}

// D2 (n.ops), A17: a backup restored to a fresh root holds every sealed turn, the last
// ones (still in the write-ahead log) included, and each rebuilds byte-exact.
func TestRestoreHoldsEveryTurn(t *testing.T) {
	restic := os.Getenv("PLEBIAN_RESTIC")
	if restic == "" {
		if p, err := exec.LookPath("restic"); err == nil {
			restic = p
		} else {
			t.Skip("restic is not installed (PLEBIAN_RESTIC)")
		}
	}
	a := root(t, restic)
	s := open(t, a)
	defer s.Close()
	var want int
	for i := 0; i < 30; i++ {
		sh := core.Sheet{Opener: core.ByOwner, Parts: []core.Part{{Kind: core.PartOwner, Writer: core.Owner, At: "2026-11-27T21:00:00Z", Text: []byte(fmt.Sprintf("note %d: the gravel comes Wednesday", i))}}}
		if _, err := s.Seal(ctx, sh, store.TurnMeta{At: "2026-11-27T21:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		want++
	}
	if _, err := s.Blobs.Put(ctx, []byte("an original")); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "second-disk")
	b := &ops.Backup{S: s, Root: a, Secrets: &config.Secrets{Path: a.Secrets() + "/secrets.json"}, Clock: kit.NewClock(time.Now())}
	key, err := b.Setup(ctx, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if d := s.Setting(ctx, "restore_drill"); !strings.HasPrefix(d, "ok ") {
		t.Fatalf("the restore drill after the backup: %q", d)
	}
	fresh := root(t, restic)
	if err := ops.RestoreFiles(fresh, repo, key, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	rs := open(t, fresh)
	defer rs.Close()
	turns, _, err := rs.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if turns != want {
		t.Fatalf("the restored store holds %d of %d sealed turns", turns, want)
	}
	blobs, _ := os.ReadDir(fresh.Blobs())
	if len(blobs) == 0 {
		t.Fatal("the restored root holds no originals")
	}
}
