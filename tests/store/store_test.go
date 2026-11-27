package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/store"
)

var ctx = context.Background()

func open(t *testing.T, dir string) (*store.Store, *grammar.G) {
	s, err := store.Open(filepath.Join(dir, "data", "store.db"), store.DirBlobs{Dir: filepath.Join(dir, "data", "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := grammar.Open(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpeller(g)
	return s, g
}

func sheet(i int) core.Sheet {
	return core.Sheet{Opener: core.ByOwner, Parts: []core.Part{{Kind: core.PartOwner, Writer: core.Owner, At: "x", Text: []byte(fmt.Sprintf("turn %d: order the culvert pipe and gravel", i))}}}
}

// D1: the hash is over the rebuilt bytes, so a corrupted spelling never verifies.
func TestRebuildHash(t *testing.T) {
	s, _ := open(t, t.TempDir())
	for i := 0; i < 5; i++ {
		if _, err := s.Seal(ctx, sheet(i), store.TurnMeta{At: "x", Shown: "[]"}); err != nil {
			t.Fatal(err)
		}
	}
	if n, _, err := s.Verify(ctx); err != nil || n != 5 {
		t.Fatalf("verify %d %v", n, err)
	}
	// Swap one symbol in a spelling for another: the rebuilt bytes differ.
	var sp []byte
	s.DB.QueryRow(`SELECT spelling FROM turns WHERE id=3`).Scan(&sp)
	sp2 := append([]byte(nil), sp...)
	sp2[len(sp2)-1] ^= 0x01
	s.DB.Exec(`UPDATE turns SET spelling=? WHERE id=3`, sp2)
	if _, _, err := s.Verify(ctx); err == nil {
		t.Fatal("verify passed a corrupted store")
	}
	s2, _ := open(t, filepath.Dir(filepath.Dir(s.Path)))
	if _, err := s2.TurnBytes(ctx, 3); err == nil {
		t.Fatal("a corrupted spelling rebuilt and verified")
	} else if !errors.Is(err, store.ErrHash) && err.Error() == "" {
		t.Fatal(err)
	}
}

// D2: the store is the server user's alone: mode 0600 under a 0700 directory, whatever
// the umask, so the tools user's open fails with a permission error.
func TestToolsUserDenied(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	dir := t.TempDir()
	s, _ := open(t, dir)
	s.Seal(ctx, sheet(1), store.TurnMeta{At: "x", Shown: "[]"})
	for _, p := range []string{filepath.Join(dir, "data", "store.db"), filepath.Join(dir, "data", "store.db-wal")} {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is %v: readable beyond its owner", p, fi.Mode().Perm())
		}
	}
	fi, _ := os.Stat(filepath.Join(dir, "data"))
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("data directory is %v", fi.Mode().Perm())
	}
	// As another user (only possible as root here), opening fails.
	if os.Geteuid() == 0 {
		cmd := exec.Command("setpriv", "--reuid=65534", "--regid=65534", "--clear-groups", "cat", filepath.Join(dir, "data", "store.db"))
		if err := cmd.Run(); err == nil {
			t.Fatal("another user read the store")
		}
	}
}

// D3: a row is durable before the call it records runs: after kill -9 every turn the
// writer reported sealed is present and rebuilds.
func TestKill9Durable(t *testing.T) {
	if os.Getenv("STORE_WRITER") != "" {
		s, _ := open(t, os.Getenv("STORE_WRITER"))
		for i := 0; ; i++ {
			n, err := s.Seal(ctx, sheet(i), store.TurnMeta{At: "x", Shown: "[]"})
			if err != nil {
				os.Exit(2)
			}
			fmt.Printf("sealed %d\n", n)
			os.Stdout.Sync()
		}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestKill9Durable$")
	cmd.Env = append(os.Environ(), "STORE_WRITER="+dir)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var last int64
	buf := make([]byte, 1<<16)
	deadline := time.Now().Add(3 * time.Second)
	var acc []byte
	for time.Now().Before(deadline) && last < 200 {
		n, err := out.Read(buf)
		acc = append(acc, buf[:n]...)
		for {
			i := indexByte(acc, '\n')
			if i < 0 {
				break
			}
			var v int64
			fmt.Sscanf(string(acc[:i]), "sealed %d", &v)
			if v > last {
				last = v
			}
			acc = acc[i+1:]
		}
		if err != nil {
			break
		}
	}
	cmd.Process.Signal(syscall.SIGKILL)
	cmd.Wait()
	if last == 0 {
		t.Fatal("the writer sealed nothing")
	}
	s, _ := open(t, dir)
	for i := int64(1); i <= last; i++ {
		if _, err := s.TurnBytes(ctx, i); err != nil {
			t.Fatalf("turn %d (of %d reported sealed) lost after kill -9: %v", i, last, err)
		}
	}
	t.Logf("%s turns reported sealed, all present", strconv.FormatInt(last, 10))
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
