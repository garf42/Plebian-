package ops

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"plebian/server/clock"
	"plebian/server/config"
	"plebian/server/store"
)

const backupSecret = "backups"

// BackupConfig is kept in the secrets file: the owner's key and the restic repositories.
type BackupConfig struct {
	Key   string   `json:"key"`
	Repos []string `json:"repos"`
}

// Backup is the nightly backup: a consistent snapshot of the store (VACUUM INTO, so no
// write-ahead log can be left behind) and the blob store, sent by restic to every
// repository, encrypted with the owner's key.
type Backup struct {
	S       *store.Store
	Root    Root
	Secrets *config.Secrets
	Clock   clock.Clock
	Loc     func(ctx context.Context) *time.Location
	Log     *log.Logger
}

func (b *Backup) restic() string { return b.Root.P("bin", "restic") }

func (b *Backup) env(key, repo string) []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=" + b.Root.ServerHome(), "RESTIC_PASSWORD=" + key, "RESTIC_REPOSITORY=" + repo,
		"RESTIC_CACHE_DIR=" + filepath.Join(b.Root.ServerHome(), ".cache", "restic")}
}

func runRestic(bin string, env []string, args ...string) ([]byte, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("restic %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func (b *Backup) config() (BackupConfig, bool) {
	var c BackupConfig
	err := json.Unmarshal([]byte(b.Secrets.Get(backupSecret)), &c)
	return c, err == nil && c.Key != "" && len(c.Repos) > 0
}

// Setup makes the owner's key, initializes each repository and runs the first backup.
// The key is returned once, for the owner to keep.
func (b *Backup) Setup(ctx context.Context, secondDisk, offsite string) (string, error) {
	var repos []string
	if secondDisk != "" {
		if !filepath.IsAbs(secondDisk) {
			return "", errors.New("the second disk must be an absolute path")
		}
		repos = append(repos, secondDisk)
	}
	if offsite != "" {
		repos = append(repos, offsite)
	}
	if len(repos) == 0 {
		return "", errors.New("a second disk or an offsite repository is required")
	}
	k := make([]byte, 32)
	rand.Read(k)
	key := base64.RawURLEncoding.EncodeToString(k)
	for _, r := range repos {
		if _, err := runRestic(b.restic(), b.env(key, r), "init"); err != nil {
			return "", err
		}
	}
	c, _ := json.Marshal(BackupConfig{Key: key, Repos: repos})
	if err := b.Secrets.Set(backupSecret, string(c)); err != nil {
		return "", err
	}
	if err := b.Once(ctx); err != nil {
		return "", err
	}
	return key, nil
}

func (b *Backup) stage() string { return b.Root.P("run", "backup") }

// Once backs up now to every repository.
func (b *Backup) Once(ctx context.Context) error {
	c, ok := b.config()
	if !ok {
		return errors.New("backups are not set up")
	}
	st := b.stage()
	os.RemoveAll(st)
	if err := os.MkdirAll(st, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(st)
	snap := filepath.Join(st, "store.db")
	if _, err := b.S.DB.ExecContext(ctx, `VACUUM INTO ?`, snap); err != nil {
		return fmt.Errorf("store snapshot: %w", err)
	}
	paths := []string{st}
	if fi, err := os.Stat(b.Root.Blobs()); err == nil && fi.IsDir() {
		paths = append(paths, b.Root.Blobs())
	}
	var errs []string
	for _, r := range c.Repos {
		if _, err := runRestic(b.restic(), b.env(c.Key, r), append([]string{"backup", "--quiet", "--tag", "plebian"}, paths...)...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	status := "last backup " + b.Clock.Now().UTC().Format(time.RFC3339)
	if len(errs) > 0 {
		status = "failed " + b.Clock.Now().UTC().Format(time.RFC3339) + ": " + strings.Join(errs, "; ")
	}
	b.S.SetSetting(ctx, "backups_status", status)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Run backs up each night at the page cut (4 am), when the day is whole.
func (b *Backup) Run(ctx context.Context) {
	for ctx.Err() == nil {
		loc := b.Loc(ctx)
		now := b.Clock.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, loc)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		select {
		case <-ctx.Done():
			return
		case <-b.Clock.After(next):
		}
		if _, ok := b.config(); !ok {
			continue
		}
		if err := b.Once(ctx); err != nil && b.Log != nil {
			b.Log.Printf("backup: %v", err)
		}
	}
}

// Restore puts the latest snapshot of repo into an installed root, the server stopped
// while its store and blobs are replaced. Run as root.
func Restore(r Root, repo, key string) error {
	if os.Geteuid() != 0 {
		return errors.New("restore needs root")
	}
	suid, sgid, err := uidOf(ServerUser)
	if err != nil {
		return err
	}
	if err := userCtl(ServerUser, "stop", "plebian.service"); err != nil {
		return err
	}
	if err := RestoreFiles(r, repo, key, suid, sgid); err != nil {
		return err
	}
	os.Remove(r.APIFile())
	if err := userCtl(ServerUser, "start", "plebian.service"); err != nil {
		return err
	}
	return waitAPI(r, 60*time.Second)
}

// RestoreFiles replaces a root's store and blobs with the latest snapshot in repo.
func RestoreFiles(r Root, repo, key string, uid, gid int) error {
	bin := r.P("bin", "restic")
	env := []string{"PATH=/usr/bin:/bin", "HOME=/root", "RESTIC_PASSWORD=" + key, "RESTIC_REPOSITORY=" + repo, "RESTIC_CACHE_DIR=" + r.P("run", "restic-cache")}
	defer os.RemoveAll(r.P("run", "restic-cache"))
	out, err := runRestic(bin, env, "snapshots", "latest", "--tag", "plebian", "--json")
	if err != nil {
		return err
	}
	var snaps []struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(out, &snaps); err != nil || len(snaps) == 0 {
		return fmt.Errorf("no Plebian snapshot in %s", repo)
	}
	os.MkdirAll(r.Run(), 0o755)
	tmp, err := os.MkdirTemp(r.Run(), "restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := runRestic(bin, env, "restore", "latest", "--tag", "plebian", "--target", tmp); err != nil {
		return err
	}
	var storeFile, blobs string
	for _, p := range snaps[0].Paths {
		full := filepath.Join(tmp, p)
		if _, err := os.Stat(filepath.Join(full, "store.db")); err == nil {
			storeFile = filepath.Join(full, "store.db")
		} else if fi, err := os.Stat(full); err == nil && fi.IsDir() {
			blobs = full
		}
	}
	if storeFile == "" {
		return errors.New("the snapshot holds no store")
	}
	for _, f := range []string{r.Store(), r.Store() + "-wal", r.Store() + "-shm"} {
		os.Remove(f)
	}
	if err := copyFile(storeFile, r.Store(), 0o600); err != nil {
		return err
	}
	os.Chown(r.Store(), uid, gid)
	if blobs != "" {
		os.RemoveAll(r.Blobs())
		if err := os.Rename(blobs, r.Blobs()); err != nil {
			return err
		}
		filepath.Walk(r.Blobs(), func(p string, _ os.FileInfo, _ error) error { os.Lchown(p, uid, gid); return nil })
	}
	return nil
}
