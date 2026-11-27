package acceptance

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A18: A05 passes with audio and originals in an S3-compatible bucket; nothing else changes.
func init() {
	type a18 struct {
		a05
		setup     int
		putSeen   bool
		original  []byte
		localLeft string
	}
	var st *a18
	register(Scenario{
		ID: "A18",
		Setup: func(t testing.TB) *H {
			st = &a18{}
			h := InstallSpeech(t)
			h.Fixtures["speech"] = StartFixture(t, "speech")
			h.Fixtures["s3"] = StartFixture(t, "s3")
			return h
		},
		Drive: func(h *H) {
			b := h.Fixtures["s3"]
			st.setup = h.Do("POST", "/v1/setup/storage", map[string]any{"bucket": map[string]string{
				"endpoint": "http://" + b.Addr, "bucket": "owner-bucket", "key": fmt.Sprint(b.Info["user"]), "secret": fmt.Sprint(b.Info["key"]), "region": "us-east-1"}}, nil, true)
			driveA05(h, &st.a05)
			if len(st.rows) == 0 {
				return
			}
			sha := st.rows[0].Blob
			for _, r := range Requests(h.T, b) {
				if r.Method == "PUT" && strings.HasSuffix(r.URL, "/"+sha) {
					st.putSeen = true
				}
			}
			req, _ := http.NewRequest("GET", "http://"+h.Addr+"/v1/blobs/"+sha, nil)
			req.Header.Set("Authorization", "Bearer "+h.Token)
			if resp, err := h.http.Do(req); err == nil {
				buf := new(bytes.Buffer)
				buf.ReadFrom(resp.Body)
				resp.Body.Close()
				st.original = buf.Bytes()
			}
			out, _ := sudo("find", filepath.Join(h.Root, "data", "blobs"), "-type", "f").Output()
			st.localLeft = strings.TrimSpace(string(out))
		},
		Assert: func(h *H, c *C) {
			c.Check("bucket_set", st.setup == 200, "storage setup answered %d", st.setup)
			assertA05(c, &st.a05)
			c.Check("audio_in_bucket", st.putSeen, "the bucket received no PUT for the stretch's original")
			c.Check("original_reads_back", bytes.HasPrefix(st.original, []byte("OggS")), "the original read back as %d bytes", len(st.original))
			c.Check("nothing_left_on_disk", st.putSeen && st.localLeft == "", "the bucket took the original %v; originals left in the root: %s", st.putSeen, st.localLeft)
		},
	})
}

func TestA18(t *testing.T) { Run(t, "A18") }
