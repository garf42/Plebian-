package acceptance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	type a17 struct {
		pageA, pageB []byte
		key, status  string
		replay       map[string]any
		restoreErr   string
		restoreRan   bool
	}
	var st *a17
	register(Scenario{
		ID: "A17",
		Setup: func(t testing.TB) *H {
			st = &a17{}
			h := fresh(t)
			h.Model("anthropic", []map[string]any{{"match": "gravel", "reply": map[string]any{"text": "Wednesday, by the culvert."}}})
			return h
		},
		Drive: func(h *H) {
			h.Say("when is the gravel coming?")
			Until(60*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
			date := PageDate(h.Now())
			h.Do("GET", "/v1/days/"+date, nil, nil, true)
			st.pageA = append([]byte(nil), h.LastBody...)
			// The second disk: a directory the server's account can write, outside its root.
			disk := fmt.Sprintf("/var/tmp/plebian-backup-%d", time.Now().UnixNano())
			runOut(h.T, sudo("install", "-d", "-o", "plebian", "-g", "plebian", "-m", "0700", disk))
			h.T.Cleanup(func() { sudo("rm", "-rf", disk).Run() })
			var r struct{ Key string }
			if c := h.Do("POST", "/v1/setup/backups", map[string]string{"second_disk": disk}, &r, true); c != 200 {
				st.restoreErr = fmt.Sprintf("backup setup %d: %s", c, h.LastBody)
				return
			}
			st.key = r.Key
			var cards struct{ Cards []struct{ ID, Status string } }
			h.Do("GET", "/v1/setup", nil, &cards, true)
			for _, c := range cards.Cards {
				if c.ID == "backups" {
					st.status = c.Status
				}
			}
			// Gone: the first box. A fresh root, then the owner's key restores it.
			runOut(h.T, sudo(filepath.Join(h.Root, "bin", "plebian"), "uninstall", "--root", h.Root))
			h.Addr = "" // the first box is gone; the version check follows the restored one
			b := fresh(h.T)
			kf := filepath.Join(h.T.TempDir(), "key")
			os.WriteFile(kf, []byte(st.key+"\n"), 0o600)
			if out, err := sudo(filepath.Join(b.Root, "bin", "plebian"), "restore", "--root", b.Root, "--from", disk, "--key-file", kf).CombinedOutput(); err != nil {
				st.restoreErr = fmt.Sprintf("%v: %s", err, out)
				return
			}
			st.restoreRan = true
			ab, _ := os.ReadFile(filepath.Join(b.Root, "run", "api.json"))
			if ab == nil {
				ab, _ = sudo("cat", filepath.Join(b.Root, "run", "api.json")).Output()
			}
			if i := bytes.Index(ab, []byte(`"addr":"`)); i >= 0 {
				rest := ab[i+len(`"addr":"`):]
				b.Addr = string(rest[:bytes.IndexByte(rest, '"')])
			}
			b.Token = h.Token // the restored store knows the first box's devices
			h.Addr = b.Addr
			b.Do("GET", "/v1/days/"+date, nil, nil, true)
			st.pageB = append([]byte(nil), b.LastBody...)
			st.replay = b.Replay()
		},
		Assert: func(h *H, c *C) {
			c.Check("page_has_reply", bytes.Contains(st.pageA, []byte("Wednesday, by the culvert.")), "the page before the backup: %s", st.pageA)
			c.Check("backed_up", st.key != "" && strings.HasPrefix(st.status, "last backup"), "key given %v; backups card %q", st.key != "", st.status)
			c.Check("restored", st.restoreRan && st.restoreErr == "", "restore ran %v: %s", st.restoreRan, st.restoreErr)
			c.Check("page_byte_identical", len(st.pageA) > 0 && bytes.Equal(st.pageA, st.pageB), "restored page differs:\n%s\n%s", st.pageA, st.pageB)
			turns, _ := st.replay["turns"].(float64)
			c.Check("restored_turns_rebuild", st.replay["ok"] == true && turns >= 1, "replay on the restored root: %v", st.replay)
		},
	})
}

func TestA17(t *testing.T) { Run(t, "A17") }
