package store_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"plebian/server/store"
	"plebian/tests/fixtures/s3"
)

// n.store: a blob read returns exactly the bytes written under its hash, from either
// backend; originals moved to a bucket leave the directory and still read.
func TestBucketBlobs(t *testing.T) {
	ctx := context.Background()
	f := s3.New("AKFIXTURE", "fixture-secret")
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	b := store.S3Blobs{B: store.Bucket{Endpoint: srv.URL, Bucket: "owner-bucket", Key: "AKFIXTURE", Secret: "fixture-secret", Region: "us-east-1"}, HTTP: srv.Client()}
	dir := store.DirBlobs{Dir: t.TempDir()}
	old := []byte("audio recorded before the bucket")
	oldSha, _ := dir.Put(ctx, old)
	sw := store.NewSwitchBlobs(dir)
	sw.Use(b)
	n, err := store.MoveToBucket(ctx, dir, b)
	if err != nil || n != 1 {
		t.Fatalf("moved %d: %v", n, err)
	}
	if left, _ := dir.List(); len(left) != 0 {
		t.Fatalf("%d originals left in the directory", len(left))
	}
	data := bytes.Repeat([]byte("opus"), 1<<16)
	sha, err := sw.Put(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	for s, want := range map[string][]byte{sha: data, oldSha: old} {
		got, err := sw.Get(ctx, s)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("read %s: %v (equal %v)", s[:8], err, bytes.Equal(got, want))
		}
	}
	if ok, err := sw.Has(ctx, sha); err != nil || !ok {
		t.Fatalf("has: %v %v", ok, err)
	}
	if local, _ := dir.List(); len(local) != 0 {
		t.Fatal("a new original was written to the directory, not the bucket")
	}
}
