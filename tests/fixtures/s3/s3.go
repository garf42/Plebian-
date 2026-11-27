// Package s3 is an in-memory S3-compatible server (gofakes3) that accepts one
// access key and records every request it serves.
package s3

import (
	"net/http"
	"regexp"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"plebian/tests/fixtures"
)

// Bucket is the fixture's state.
type Bucket struct {
	AccessKey, SecretKey string
	log                  *fixtures.Log
}

// New returns a server accepting accessKey; buckets are created on first use.
func New(accessKey, secretKey string) *Bucket {
	return &Bucket{AccessKey: accessKey, SecretKey: secretKey, log: fixtures.NewLog("Authorization")}
}

// Requests returns every S3 request received, bodies exact.
func (b *Bucket) Requests() []fixtures.Request { return b.log.List() }

var credRe = regexp.MustCompile(`Credential=([^/,\s]+)/`)

// accessKey is the key ID a SigV4 request names, in its header or presigned query.
func accessKey(r *http.Request) string {
	if m := credRe.FindStringSubmatch(r.Header.Get("Authorization")); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`^([^/]+)/`).FindStringSubmatch(r.URL.Query().Get("X-Amz-Credential")); m != nil {
		return m[1]
	}
	return ""
}

// Handler serves S3 and the control API.
func (b *Bucket) Handler() http.Handler {
	s3 := gofakes3.New(s3mem.New(), gofakes3.WithAutoBucket(true)).Server()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /control/requests", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, b.log.List())
	})
	mux.Handle("/", b.log.Recording(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// shortcut: checks the access key ID a request names, not its SigV4 signature; upgrade when a scenario needs a wrong secret refused
		if accessKey(r) != b.AccessKey {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>InvalidAccessKeyId</Code>` +
				`<Message>The AWS Access Key Id you provided does not exist in our records.</Message></Error>`))
			return
		}
		s3.ServeHTTP(w, r)
	})))
	return mux
}
