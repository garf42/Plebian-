package acceptance

import (
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Trees that change on any running host, unrelated to what install writes.
var churn = []string{"/proc", "/sys", "/dev", "/run", "/tmp", "/var/tmp", "/var/log", "/var/cache", "/var/crash",
	"/var/lib/systemd/coredump", "/var/lib/apt", "/var/lib/dpkg", "/home", "/root", "/snap", "/mnt", "/media",
	"/var/lib/docker", "/var/lib/containerd", "/opt/hostedtoolcache", "/opt/runner", "/usr/share/rust", "/swapfile",
	"/var/lib/waagent", "/var/lib/cloud", "/var/lib/sudo", "/var/lib/private", "/var/lib/polkit-1"}

// What install may write outside its root: the two accounts and their linger flags.
var allowed = []string{"/etc/passwd", "/etc/group", "/etc/shadow", "/etc/gshadow", "/etc/passwd-", "/etc/group-", "/etc/shadow-", "/etc/gshadow-",
	"/etc/.pwd.lock", "/var/lib/systemd/linger/plebian", "/var/lib/systemd/linger/plebian-tools", "/var/lib/systemd/linger"}

func snapshot(skip string) map[string]string {
	out := map[string]string{}
	filepath.WalkDir("/", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		for _, c := range churn {
			if p == c || strings.HasPrefix(p, c+"/") {
				return fs.SkipDir
			}
		}
		if strings.HasPrefix(p, "/opt/plebian-acc-") || p == skip || strings.HasPrefix(p, skip+"/") {
			return fs.SkipDir
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			// A directory's mtime moves with any entry in it; its entries are compared themselves.
			out[p] = "dir " + fi.Mode().String()
			return nil
		}
		out[p] = fmt.Sprintf("%v %d %d", fi.Mode(), fi.Size(), fi.ModTime().UnixNano())
		return nil
	})
	return out
}

func diff(a, b map[string]string) []string {
	var out []string
	for p, v := range b {
		if a[p] != v {
			out = append(out, p)
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			out = append(out, p+" (removed)")
		}
	}
	sort.Strings(out)
	return out
}

func outsideAllowed(paths []string) []string {
	var bad []string
	for _, p := range paths {
		q := strings.TrimSuffix(p, " (removed)")
		ok := false
		for _, a := range allowed {
			if q == a {
				ok = true
			}
		}
		if !ok {
			bad = append(bad, p)
		}
	}
	return bad
}

// listeners returns the listening addresses of processes owned by the plebian accounts.
func listeners() []string {
	out, _ := sudo("ss", "-Hltnup").Output()
	var addrs []string
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) < 6 || !strings.Contains(ln, "pid=") {
			continue
		}
		for _, part := range strings.Split(ln, "pid=")[1:] {
			pid, _ := strconv.Atoi(strings.SplitN(part, ",", 2)[0])
			st, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			for _, s := range strings.Split(string(st), "\n") {
				if strings.HasPrefix(s, "Uid:") {
					uid := strings.Fields(s)[1]
					for _, u := range []string{"plebian", "plebian-tools"} {
						if id, _ := exec.Command("id", "-u", u).Output(); strings.TrimSpace(string(id)) == uid {
							addrs = append(addrs, f[4])
						}
					}
				}
			}
		}
	}
	return addrs
}

type canary struct {
	port int
	dir  string
	unit string
}

func startCanary(t testing.TB) canary {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	c := canary{port: port, dir: "/srv/plebian-canary", unit: "plebian-canary-acc"}
	runOut(t, sudo("mkdir", "-p", c.dir))
	runOut(t, sudo("sh", "-c", "echo canary > "+c.dir+"/index.html"))
	runOut(t, sudo("systemd-run", "--unit", c.unit, "python3", "-m", "http.server", "--bind", "127.0.0.1", strconv.Itoa(port), "--directory", c.dir))
	t.Cleanup(func() {
		sudo("systemctl", "stop", c.unit).Run()
		sudo("rm", "-rf", c.dir).Run()
	})
	Until(10*time.Second, func() bool { return c.alive() })
	return c
}

func (c canary) alive() bool {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", c.port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)) == "canary"
}

type a01 struct {
	canary                canary
	before, during, after map[string]string
	listen                []string
	canaryDuring          bool
	canaryAfter           bool
	rootLeft              bool
	accountsLeft          []string
	version               bool
}

func init() {
	var st *a01
	register(Scenario{
		ID: "A01",
		Setup: func(t testing.TB) *H {
			st = &a01{canary: startCanary(t)}
			st.before = snapshot(st.canary.dir)
			return &H{T: t}
		},
		Drive: func(h *H) {
			full := Install(h.T)
			*h = *full
			st.version = h.Do("GET", "/v1/version", nil, nil, false) == 200
			st.during = snapshot(st.canary.dir)
			st.listen = listeners()
			st.canaryDuring = st.canary.alive()
			runOut(h.T, sudo(filepath.Join(h.Root, "bin", "plebian"), "uninstall", "--root", h.Root))
			time.Sleep(time.Second)
			st.after = snapshot(st.canary.dir)
			st.canaryAfter = st.canary.alive()
			_, err := os.Stat(h.Root)
			st.rootLeft = err == nil
			for _, u := range []string{"plebian", "plebian-tools"} {
				if exec.Command("id", u).Run() == nil {
					st.accountsLeft = append(st.accountsLeft, u)
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("installed_and_serving", st.version && h.Root != "", "the installed server did not answer /v1/version")
			c.Check("listens", len(st.listen) > 0, "no listening socket found for the plebian accounts")
			for _, a := range st.listen {
				host, _, _ := net.SplitHostPort(a)
				c.Check("listens_localhost_only", host == "127.0.0.1" || host == "::1" || host == "[::1]", "listens on %s", a)
			}
			if st.during != nil {
				bad := outsideAllowed(diff(st.before, st.during))
				c.Check("writes_only_root_accounts_linger", len(bad) == 0, "install wrote outside its root: %v", bad)
				c.Check("canary_untouched_during", st.canaryDuring, "the canary service stopped answering on its port")
			}
			if st.after != nil {
				bad := outsideAllowed(diff(st.before, st.after))
				c.Check("uninstall_restores", len(bad) == 0, "after uninstall, changed: %v", bad)
				c.Check("canary_untouched_after", st.canaryAfter, "the canary stopped answering after uninstall")
				c.Check("root_removed", !st.rootLeft, "the root is still there")
				c.Check("accounts_removed", len(st.accountsLeft) == 0, "accounts left: %v", st.accountsLeft)
			} else {
				c.Check("uninstall_restores", false, "not uninstalled")
			}
		},
	})
}

func TestA01(t *testing.T) { Run(t, "A01") }
