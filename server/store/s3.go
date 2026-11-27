package store

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bucket is an S3-compatible bucket's address and credentials.
type Bucket struct {
	Endpoint string `json:"endpoint"` // https://s3.example.com (path-style requests)
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	Secret   string `json:"secret"`
	Region   string `json:"region"`
}

// S3Blobs keeps blobs as objects <prefix><sha> in a bucket, signed with AWS SigV4.
// shortcut: path-style addressing only (every S3-compatible store serves it; AWS serves
// it for existing buckets); virtual-hosted addressing when a provider drops path-style.
type S3Blobs struct {
	B    Bucket
	HTTP *http.Client
}

const s3Prefix = "plebian/blobs/"

func hmacSHA(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func hexSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (s S3Blobs) do(ctx context.Context, method, sha string, body []byte) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(s.B.Endpoint, "/") + "/" + s.B.Bucket + "/" + s3Prefix + sha)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	region := s.B.Region
	if region == "" {
		region = "us-east-1"
	}
	now := time.Now().UTC()
	amzDate, day := now.Format("20060102T150405Z"), now.Format("20060102")
	payload := hexSHA(body)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payload)
	req.Header.Set("host", u.Host)
	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + strings.TrimSpace(req.Header.Get(n)) + "\n")
	}
	signed := strings.Join(names, ";")
	canon := strings.Join([]string{method, u.EscapedPath(), u.RawQuery, canonHeaders.String(), signed, payload}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA([]byte(canon))
	k := hmacSHA([]byte("AWS4"+s.B.Secret), day)
	k = hmacSHA(k, region)
	k = hmacSHA(k, "s3")
	k = hmacSHA(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(k, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", s.B.Key, scope, signed, sig))
	if method == "PUT" {
		req.ContentLength = int64(len(body))
	}
	return s.HTTP.Do(req)
}

func (s S3Blobs) Put(ctx context.Context, b []byte) (string, error) {
	sha := hexSHA(b)
	if ok, err := s.Has(ctx, sha); err == nil && ok {
		return sha, nil
	}
	resp, err := s.do(ctx, "PUT", sha, b)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bucket PUT answered %d", resp.StatusCode)
	}
	return sha, nil // S3 answers 200 only once the object is stored
}

var errNoBlob = errors.New("blobs: not found")

func (s S3Blobs) Get(ctx context.Context, sha string) ([]byte, error) {
	if err := validSha(sha); err != nil {
		return nil, err
	}
	resp, err := s.do(ctx, "GET", sha, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errNoBlob
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bucket GET answered %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if hexSHA(b) != sha {
		return nil, errors.New("blobs: the bucket returned other bytes than were written")
	}
	return b, nil
}

func (s S3Blobs) Has(ctx context.Context, sha string) (bool, error) {
	if err := validSha(sha); err != nil {
		return false, err
	}
	resp, err := s.do(ctx, "HEAD", sha, nil)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case 200:
		return true, nil
	case 404:
		return false, nil
	}
	return false, fmt.Errorf("bucket HEAD answered %d", resp.StatusCode)
}

// SwitchBlobs is the blob store a server holds: the directory, or a bucket once one is
// configured. New originals go to the current backend; a read that misses there falls
// back to the directory, so originals not yet moved still read.
type SwitchBlobs struct {
	mu  sync.RWMutex
	cur Blobs
	Dir DirBlobs
}

func NewSwitchBlobs(dir DirBlobs) *SwitchBlobs { return &SwitchBlobs{cur: dir, Dir: dir} }

func (s *SwitchBlobs) Use(b Blobs) { s.mu.Lock(); s.cur = b; s.mu.Unlock() }

func (s *SwitchBlobs) current() Blobs { s.mu.RLock(); defer s.mu.RUnlock(); return s.cur }

func (s *SwitchBlobs) Put(ctx context.Context, b []byte) (string, error) {
	return s.current().Put(ctx, b)
}

func (s *SwitchBlobs) Get(ctx context.Context, sha string) ([]byte, error) {
	c := s.current()
	b, err := c.Get(ctx, sha)
	if err != nil && c != Blobs(s.Dir) {
		if d, derr := s.Dir.Get(ctx, sha); derr == nil {
			return d, nil
		}
	}
	return b, err
}

func (s *SwitchBlobs) Has(ctx context.Context, sha string) (bool, error) {
	c := s.current()
	ok, err := c.Has(ctx, sha)
	if (err != nil || !ok) && c != Blobs(s.Dir) {
		return s.Dir.Has(ctx, sha)
	}
	return ok, err
}

// MoveToBucket copies every original in the directory to the bucket, checks each by its
// hash, then removes it from the directory: audio and originals live in the bucket.
func MoveToBucket(ctx context.Context, dir DirBlobs, b S3Blobs) (int, error) {
	shas, err := dir.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sha := range shas {
		data, err := dir.Get(ctx, sha)
		if err != nil {
			return n, err
		}
		if got, err := b.Put(ctx, data); err != nil || got != sha {
			return n, fmt.Errorf("moving %s: %v", sha, err)
		}
		if back, err := b.Get(ctx, sha); err != nil || !bytes.Equal(back, data) {
			return n, fmt.Errorf("the bucket did not return %s", sha)
		}
		if err := dir.Remove(sha); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
