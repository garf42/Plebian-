package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WatchdogSec is how long systemd waits for a heartbeat before restarting the server.
// The server beats at half of it (sd_notify convention). Gap: picked value.
const WatchdogSec = 60

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return nil
}

func uidOf(name string) (int, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

// ensureUser creates a system account whose home is inside the root; useradd writes
// only the account files (no home is created, no mail spool for system accounts).
func ensureUser(name, home string, groups ...string) error {
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	args := []string{"--system", "--user-group", "--no-create-home", "--home-dir", home, "--shell", "/usr/sbin/nologin"}
	if len(groups) > 0 {
		args = append(args, "--groups", strings.Join(groups, ","))
	}
	return run("useradd", append(args, name)...)
}

// userCtl runs systemctl --user for an account through its own user manager.
func userCtl(name string, args ...string) error {
	uid, _, err := uidOf(name)
	if err != nil {
		return err
	}
	rt := fmt.Sprintf("/run/user/%d", uid)
	for i := 0; i < 300; i++ { // the user manager starts once linger is on
		if _, err := os.Stat(rt + "/bus"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	full := append([]string{"-u", name, "--", "env", "XDG_RUNTIME_DIR=" + rt, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + rt + "/bus", "systemctl", "--user"}, args...)
	return run("runuser", full...)
}

func unit(desc, exec string, root Root, notify bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\n\n[Service]\n", desc)
	if notify {
		fmt.Fprintf(&b, "Type=notify\nNotifyAccess=main\nWatchdogSec=%d\n", WatchdogSec)
	}
	fmt.Fprintf(&b, "Environment=PLEBIAN_ROOT=%s\nEnvironment=LD_LIBRARY_PATH=%s\nExecStart=%s\nRestart=always\nUMask=0077\n\n[Install]\nWantedBy=default.target\n", root, root.P("lib"), exec)
	return b.String()
}

func writeFile(path string, b []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, mode); err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// InstallOptions are install's flags.
type InstallOptions struct {
	Root       Root
	Config     Config
	Out        io.Writer
	SpeechFrom string // a directory holding the speech model files; empty = download
	// BrowserFrom is a directory holding chromium/, node/ and mcp/ (Playwright MCP's
	// node_modules); empty fetches BrowserURL's bundle; none leaves the browser out.
	BrowserFrom string
	BrowserURL  string // this release's browser bundle; empty when the build names no release
}

// swapBrowser puts a bundle's chromium/, node/ and mcp/ in place whole: each is laid
// down as <name>.new (moved when it was staged on this filesystem, copied otherwise),
// and only when all three are complete do they replace the old ones, so a failed copy
// leaves the old browser as it was and no directory of an earlier bundle survives.
func swapBrowser(r Root, from string, move bool, uid, gid int) error {
	names := []string{"chromium", "node", "mcp"}
	clean := func() {
		for _, d := range names {
			os.RemoveAll(r.P("browser", d+".new"))
		}
	}
	clean()
	for _, d := range names {
		src, dst := filepath.Join(from, d), r.P("browser", d+".new")
		if move && os.Rename(src, dst) == nil {
			filepath.Walk(dst, func(p string, _ os.FileInfo, _ error) error { return os.Lchown(p, uid, gid) })
			continue
		}
		if err := copyTree(src, dst, uid, gid); err != nil {
			clean()
			return err
		}
	}
	for _, d := range names {
		cur := r.P("browser", d)
		os.RemoveAll(cur + ".old")
		if err := os.Rename(cur, cur+".old"); err != nil && !os.IsNotExist(err) {
			clean()
			return err
		}
		if err := os.Rename(cur+".new", cur); err != nil {
			os.Rename(cur+".old", cur)
			clean()
			return err
		}
		os.RemoveAll(cur + ".old")
	}
	return nil
}

// copyTree copies a directory, modes kept, owned by uid:gid.
func copyTree(src, dst string, uid, gid int) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		switch {
		case fi.IsDir():
			if err := os.MkdirAll(t, fi.Mode().Perm()|0o700); err != nil {
				return err
			}
		case fi.Mode()&os.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			os.Remove(t)
			if err := os.Symlink(l, t); err != nil {
				return err
			}
		default:
			if err := copyFile(p, t, fi.Mode().Perm()); err != nil {
				return err
			}
		}
		return os.Lchown(t, uid, gid)
	})
}

// Install lays the root down, creates the accounts, enables their linger flags and starts
// their user services. Run as root.
func Install(o InstallOptions) (err error) {
	if os.Geteuid() != 0 {
		return errors.New("install needs root (it creates two system accounts)")
	}
	r := o.Root
	if !filepath.IsAbs(string(r)) {
		return errors.New("--root must be absolute")
	}
	fresh := true
	if fi, err := os.Stat(string(r)); err == nil && fi.IsDir() {
		if _, err := os.Stat(r.Bin()); err != nil {
			if ents, _ := os.ReadDir(string(r)); len(ents) > 0 {
				return fmt.Errorf("%s exists and is not a Plebian root; choose another --root", r)
			}
		} else {
			fresh = false // an existing root: its store is never rolled back
		}
	}
	_, hadServer := user.Lookup(ServerUser)
	_, hadTools := user.Lookup(ToolsUser)
	fresh = fresh && hadServer != nil && hadTools != nil
	// A fresh install that fails leaves nothing behind: the box is as it was (A01).
	defer func() {
		if err == nil || !fresh {
			return
		}
		if uid, _, e := uidOf(ServerUser); e == nil {
			if out, e := exec.Command("journalctl", fmt.Sprintf("_UID=%d", uid), "-n", "40", "--no-pager", "-o", "cat").Output(); e == nil && len(out) > 0 {
				err = fmt.Errorf("%w\nserver log:\n%s", err, strings.TrimSpace(string(out)))
			}
		}
		if e := Uninstall(r); e != nil {
			err = fmt.Errorf("%w\nrolling back: %v", err, e)
		}
	}()
	// The server joins the tools group so it can reach the runner's socket; the tools
	// user joins no group of the server's, so the store and secrets stay out of its reach.
	if err := ensureUser(ToolsUser, r.ToolsHome()); err != nil {
		return err
	}
	if err := ensureUser(ServerUser, r.ServerHome(), ToolsUser); err != nil {
		return err
	}
	suid, sgid, _ := uidOf(ServerUser)
	tuid, tgid, _ := uidOf(ToolsUser)
	dirs := []struct {
		path     string
		uid, gid int
		mode     os.FileMode
	}{
		{string(r), 0, 0, 0o755},
		{r.P("bin"), suid, sgid, 0o755}, // the server's own: an approved update replaces its program
		{r.P("lib"), suid, sgid, 0o755},
		{r.P("data"), suid, sgid, 0o700},
		{r.Blobs(), suid, sgid, 0o700},
		{r.P("secrets"), suid, sgid, 0o700},
		{r.Run(), suid, sgid, 0o755},
		{r.P("run", "runner"), tuid, tgid, 0o750}, // the runner's socket: the tools user's; the server reaches it through the tools group
		{r.P("home"), 0, 0, 0o755},
		{r.ServerHome(), suid, sgid, 0o700},
		{r.ToolsHome(), tuid, tgid, 0o700},
		{r.Workspace(), tuid, tgid, 0o700},
		{r.Outside(), tuid, tgid, 0o700},
		{r.Tailnet(), suid, sgid, 0o700},
		{r.Browser(), suid, sgid, 0o700},
		{r.Speech(), suid, sgid, 0o755},
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d.path, d.mode); err != nil {
			return err
		}
		if err := os.Chmod(d.path, d.mode); err != nil {
			return err
		}
		if err := os.Chown(d.path, d.uid, d.gid); err != nil {
			return err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if exe != r.Bin() {
		if err := copyFile(exe, r.Bin(), 0o755); err != nil {
			return err
		}
	}
	// The sandbox's bubblewrap ships beside the program in the release.
	// Programs shipped beside it in the release: the sandbox and the backup tool.
	for _, name := range []string{"bwrap", "restic"} {
		if src := filepath.Join(filepath.Dir(exe), name); src != r.P("bin", name) {
			if _, err := os.Stat(src); err == nil {
				if err := copyFile(src, r.P("bin", name), 0o755); err != nil {
					return err
				}
			}
		}
	}
	// Shared libraries shipped beside the program in the release (speech).
	if libs, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(exe)), "lib", "*")); len(libs) > 0 {
		for _, l := range libs {
			dst := r.P("lib", filepath.Base(l))
			if l != dst {
				if err := copyFile(l, dst, 0o644); err != nil {
					return err
				}
			}
		}
	}
	browserFrom, fetched := o.BrowserFrom, false
	switch browserFrom {
	case "none":
		browserFrom = ""
	case "":
		if o.BrowserURL == "" {
			fmt.Fprintln(o.Out, "browser not installed: this build names no release to fetch it from (install --browser-from <dir>)")
			break
		}
		// Staged in a directory only root can write, on the root's filesystem: the libraries
		// are checked from it before anything is handed to the server's account.
		stage := r.P(".browser-stage")
		os.RemoveAll(stage)
		defer os.RemoveAll(stage)
		if err := os.Mkdir(stage, 0o700); err != nil {
			return fmt.Errorf("browser: %w", err)
		}
		if d, err := FetchBrowser(context.Background(), o.BrowserURL, stage); err != nil {
			fmt.Fprintln(o.Out, "browser not installed:", err)
		} else {
			browserFrom, fetched = d, true
		}
	}
	if o.SpeechFrom == "none" {
		fmt.Fprintln(o.Out, "speech models skipped (--speech-from none)")
	} else if err := FetchSpeech(r.Speech(), o.SpeechFrom); err != nil {
		// Install goes on; This week shows speech as missing until a later install fetches it.
		fmt.Fprintln(o.Out, "speech models not installed:", err)
	}
	// What the box's own system must provide (Chromium's libraries, host programs the
	// tools use) is installed through its package manager; without it the rest still runs.
	if err := EnsureHost(o.Out, browserFrom); err != nil {
		fmt.Fprintln(o.Out, "the box's system packages:", err)
	}
	swapped := false
	if browserFrom != "" {
		// The browser directory is the server account's: Chromium keeps its profile there.
		if err := os.MkdirAll(r.P("browser", "profile"), 0o700); err != nil {
			return fmt.Errorf("browser: %w", err)
		}
		for _, d := range []string{r.P("browser"), r.P("browser", "profile")} {
			if err := os.Chown(d, suid, sgid); err != nil {
				return fmt.Errorf("browser: %w", err)
			}
		}
		if err := swapBrowser(r, browserFrom, fetched, suid, sgid); err != nil {
			return fmt.Errorf("browser: %w", err)
		}
		swapped = true
	}
	cb, _ := json.MarshalIndent(o.Config, "", " ")
	if err := writeFile(r.Config(), cb, 0o644, 0, 0); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(r.ServerHome(), ".config/systemd/user/plebian.service"),
		[]byte(unit("Plebian server", r.Bin()+" serve", r, true)), 0o644, suid, sgid); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(r.ToolsHome(), ".config/systemd/user/plebian-runner.service"),
		[]byte(unit("Plebian tools runner", r.Bin()+" runner", r, false)), 0o644, tuid, tgid); err != nil {
		return err
	}
	for _, d := range []struct {
		dir      string
		uid, gid int
	}{{r.ServerHome(), suid, sgid}, {r.ToolsHome(), tuid, tgid}} {
		filepath.Walk(d.dir, func(p string, _ os.FileInfo, _ error) error { os.Lchown(p, d.uid, d.gid); return nil })
	}
	read, write := []string{r.Bin(), r.P("lib"), r.Config()}, []string{r.ServerHome()}
	if _, err := os.Stat(r.Speech()); err == nil {
		read = append(read, r.Speech())
	}
	if browserFrom != "" {
		read, write = append(read, r.Browser()), append(write, r.P("browser", "profile"))
	}
	for _, a := range []struct {
		user        string
		read, write []string
	}{
		{ServerUser, read, write},
		{ToolsUser, []string{r.Bin(), r.P("lib")}, []string{r.Workspace(), r.Outside(), r.P("run", "runner")}},
	} {
		if _, err := os.Stat(r.P("bin", "bwrap")); err == nil && a.user == ToolsUser {
			a.read = append(a.read, r.P("bin", "bwrap"))
		}
		if err := run("runuser", "-u", a.user, "--", "env", "LD_LIBRARY_PATH="+r.P("lib"), r.Bin(), "access", "--read", strings.Join(a.read, ","), "--write", strings.Join(a.write, ",")); err != nil {
			return err
		}
	}
	for _, u := range []string{ServerUser, ToolsUser} {
		if err := run("loginctl", "enable-linger", u); err != nil {
			return err
		}
	}
	if err := userCtl(ToolsUser, "daemon-reload"); err != nil {
		return err
	}
	if err := userCtl(ToolsUser, "enable", "--now", "plebian-runner.service"); err != nil {
		return err
	}
	if err := userCtl(ServerUser, "daemon-reload"); err != nil {
		return err
	}
	if err := userCtl(ServerUser, "enable", "--now", "plebian.service"); err != nil {
		return err
	}
	if swapped && !fresh {
		// A running server holds the browser paths it found at start; it takes the new ones.
		os.Remove(r.APIFile())
		if err := userCtl(ServerUser, "restart", "plebian.service"); err != nil {
			return err
		}
	}
	return waitAPI(r, 60*time.Second)
}

func waitAPI(r Root, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(r.APIFile()); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("the server did not start; see: journalctl --user-unit plebian.service (as " + ServerUser + ")")
}

// Uninstall removes everything Install created.
func Uninstall(r Root) error {
	if os.Geteuid() != 0 {
		return errors.New("uninstall needs root")
	}
	var errs []string
	for _, x := range []struct{ u, svc string }{{ServerUser, "plebian.service"}, {ToolsUser, "plebian-runner.service"}} {
		if _, err := user.Lookup(x.u); err != nil {
			continue
		}
		userCtl(x.u, "disable", "--now", x.svc)
		if err := run("loginctl", "disable-linger", x.u); err != nil {
			errs = append(errs, err.Error())
		}
		run("loginctl", "terminate-user", x.u)
		uid, _, _ := uidOf(x.u)
		for i := 0; i < 100; i++ { // the user manager exits once linger is off
			if _, err := os.Stat(fmt.Sprintf("/run/user/%d", uid)); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		run("pkill", "-KILL", "-u", x.u)
		if err := run("userdel", x.u); err != nil {
			errs = append(errs, err.Error())
		}
		if _, err := user.LookupGroup(x.u); err == nil {
			run("groupdel", x.u)
		}
	}
	if _, err := os.Stat(r.Bin()); err == nil {
		if err := os.RemoveAll(string(r)); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
