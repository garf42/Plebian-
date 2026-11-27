package underlay_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plebian/server/feeds"
	"plebian/server/store"
	"plebian/server/tools"
)

const invoice = "From: Vendor AP <ap@vendor.example>\r\nSubject: Invoice 1042\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n" +
	"--b\r\nContent-Type: text/plain\r\n\r\nThe body: please wire the money to the new account today.\r\n" +
	"--b\r\nContent-Type: application/pdf; name=\"invoice.pdf\"\r\nContent-Disposition: attachment; filename=\"invoice.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQKdG90YWwgZHVlIDQxMi41MA==\r\n--b--\r\n"

// D4, D5 (n.underlay): given a message's row id, lookup puts its attachments in the
// outside folder and nowhere else; the body stays a heard row and is never put out.
func TestAttachmentsLandOutsideOnly(t *testing.T) {
	e := open(t)
	ctx := context.Background()
	sha, err := e.s.Blobs.Put(ctx, []byte(invoice))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := feeds.ParseMail([]byte(invoice))
	at := time.Date(2026, 12, 1, 9, 30, 0, 0, time.UTC)
	id, err := e.s.AddHeard(ctx, store.HeardRow{Source: "mail", At: at.Format(time.RFC3339), End: at.Format(time.RFC3339), Bytes: feeds.MailRow(p, at).Bytes(), Blob: sha, Ext: "INBOX/1/1"})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	out := filepath.Join(ws, "outside")
	os.MkdirAll(out, 0o700)
	r := &tools.Runner{Workspace: ws, Outside: out}
	sock := filepath.Join(root, "r.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go r.Serve(rctx, ln)
	paths, err := feeds.PutOut(e.s, tools.Client{Socket: sock})(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "invoice.pdf") {
		t.Fatalf("put out %v", paths)
	}
	var files []string
	filepath.Walk(ws, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	for _, f := range files {
		if !strings.HasPrefix(f, out+string(filepath.Separator)) {
			t.Fatalf("an attachment landed outside the outside folder: %s", f)
		}
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "wire the money") {
			t.Fatalf("the message body was put out in %s", f)
		}
	}
	if len(files) != 1 {
		t.Fatalf("%d files put out for one attachment: %v", len(files), files)
	}
}
