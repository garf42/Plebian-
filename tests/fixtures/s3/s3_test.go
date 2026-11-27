package s3

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPutGetWithKey(t *testing.T) {
	b := New("AKFIXTURE", "secret")
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()
	do := func(method, path, key string, body []byte) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		now := time.Now().UTC()
		req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+key+"/"+now.Format("20060102")+
			"/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=00")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	audio := []byte("OggS fixture audio bytes")
	if r := do("PUT", "/plebian/audio/1.ogg", "AKFIXTURE", audio); r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("put: %d %s", r.StatusCode, b)
	}
	r := do("GET", "/plebian/audio/1.ogg", "AKFIXTURE", nil)
	got, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !bytes.Equal(got, audio) {
		t.Fatalf("get: %d %q", r.StatusCode, got)
	}
	if r := do("GET", "/plebian/audio/1.ogg", "AKOTHER", nil); r.StatusCode != 403 {
		t.Fatalf("other key: %d", r.StatusCode)
	}
	if reqs := b.Requests(); len(reqs) != 3 || !bytes.Equal(reqs[0].Body, audio) {
		t.Fatalf("log %d", len(reqs))
	}
}
