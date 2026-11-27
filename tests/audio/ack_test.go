package audio_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"plebian/server/audio"
	"plebian/server/clock"
	"plebian/server/speech"
	"plebian/server/store"
)

type failing struct{ store.DirBlobs }

func (failing) Put(context.Context, []byte) (string, error) { return "", errors.New("disk full") }

// D4 (n.api): an offline file is answered with its sha256 only once its bytes are
// durable; a store that can't keep them answers no sha, so the Omi keeps the file.
func TestFileAckOnlyWhenDurable(t *testing.T) {
	var body []byte
	for _, f := range [][]byte{bytes.Repeat([]byte{1}, 40), bytes.Repeat([]byte{2}, 40)} {
		body = binary.LittleEndian.AppendUint16(body, uint16(len(f)))
		body = append(body, f...)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	for _, durable := range []bool{true, false} {
		dir := t.TempDir()
		var blobs store.Blobs = store.DirBlobs{Dir: filepath.Join(dir, "blobs")}
		if !durable {
			blobs = failing{}
		}
		s, err := store.Open(filepath.Join(dir, "store.db"), blobs)
		if err != nil {
			t.Fatal(err)
		}
		n := &audio.Node{S: s, Clock: clock.System{}, FrameOf: speech.FrameSamples}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.FileHandler(w, r, "dev") }))
		req, _ := http.NewRequest("POST", srv.URL, bytes.NewReader(body))
		req.Header.Set("X-Omi-Codec", "21")
		req.Header.Set("X-Omi-File-Timestamp", "1764216000000")
		req.Header.Set("X-Omi-Clock-Offset-Ms", "0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var r struct{ Sha256 string }
		json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		srv.Close()
		if durable {
			ok, _ := blobs.Has(context.Background(), want)
			if resp.StatusCode != 200 || r.Sha256 != want || !ok {
				t.Fatalf("durable: %d %q kept %v", resp.StatusCode, r.Sha256, ok)
			}
		} else if r.Sha256 != "" || resp.StatusCode == 200 {
			t.Fatalf("a file the store could not keep was answered %d %q", resp.StatusCode, r.Sha256)
		}
		s.Close()
	}
}
