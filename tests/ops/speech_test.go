package ops_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"plebian/server/ops"
)

// TestSpeechFileReadableByServer: install runs as root and the server's account loads
// the models, so a fetched single-file model is readable by others.
func TestSpeechFileReadableByServer(t *testing.T) {
	from, dst := t.TempDir(), t.TempDir()
	body := []byte("not a real model")
	os.WriteFile(filepath.Join(from, "m.onnx"), body, 0o644)
	sum := sha256.Sum256(body)
	if err := ops.FetchFile(ops.SpeechFile{Name: "m.onnx", SHA256: hex.EncodeToString(sum[:])}, from, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dst, "m.onnx"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o004 == 0 {
		t.Fatalf("the fetched model is %v: the server's account cannot read it", fi.Mode().Perm())
	}
}
