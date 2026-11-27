package ical

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const event = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Plebian//Fixture//EN\r\nBEGIN:VEVENT\r\nUID:gravel-1@fixture\r\nDTSTAMP:20261120T090000Z\r\nDTSTART:20261203T150000Z\r\nDTEND:20261203T160000Z\r\nSUMMARY:Gravel delivery\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func get(t *testing.T, url string, hdr map[string]string) *http.Response {
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestConditionalGet(t *testing.T) {
	f := New()
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	if resp, _ := http.Post(srv.URL+"/control/body", "text/calendar", strings.NewReader(event)); resp.StatusCode != 200 {
		t.Fatal("set body")
	}
	r1 := get(t, srv.URL+"/calendar.ics", nil)
	b, _ := io.ReadAll(r1.Body)
	etag, lm := r1.Header.Get("ETag"), r1.Header.Get("Last-Modified")
	if r1.StatusCode != 200 || string(b) != event || etag == "" || lm == "" {
		t.Fatalf("first: %d %q %q", r1.StatusCode, etag, lm)
	}
	if r := get(t, srv.URL+"/calendar.ics", map[string]string{"If-None-Match": etag}); r.StatusCode != 304 {
		t.Fatalf("If-None-Match: %d", r.StatusCode)
	}
	if r := get(t, srv.URL+"/calendar.ics", map[string]string{"If-Modified-Since": lm}); r.StatusCode != 304 {
		t.Fatalf("If-Modified-Since: %d", r.StatusCode)
	}
	if r := get(t, srv.URL+"/calendar.ics", map[string]string{"If-None-Match": `"other"`}); r.StatusCode != 200 {
		t.Fatalf("stale ETag: %d", r.StatusCode)
	}
	if s := f.Stats(); s != (Stats{Fetches: 4, OK: 2, NotModified: 2}) {
		t.Fatalf("stats %+v", s)
	}
}
