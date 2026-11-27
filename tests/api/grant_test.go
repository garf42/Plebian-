package api_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"plebian/server/api"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// D1 (n.api): a standing grant (any admin action) needs the paired device key's
// signature over its body; a bearer token alone, another key's signature or a replayed
// nonce changes nothing.
func TestGrantRequiresSignature(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{})
	var ran []string
	a := &api.Server{S: k.S, Clock: k.Clock, Hooks: api.Hooks{Admin: func(_ context.Context, _, action string, _ []byte) error {
		ran = append(ran, action)
		return nil
	}}}
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	ctx := context.Background()
	code, err := api.NewPairCode(ctx, k.S, "2026-11-27T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pb, _ := json.Marshal(map[string]string{"code": code, "public_key": base64.StdEncoding.EncodeToString(der), "name": "phone"})
	resp, err := http.Post(srv.URL+"/v1/pair", "application/json", bytes.NewReader(pb))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("pair: %v %v", err, resp)
	}
	var paired struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&paired)
	resp.Body.Close()
	grant := func(body []byte, signer *ecdsa.PrivateKey) int {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/admin/grant", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+paired.Token)
		if signer != nil {
			h := sha256.Sum256(body)
			sig, _ := ecdsa.SignASN1(rand.Reader, signer, h[:])
			req.Header.Set("X-Plebian-Signature", base64.StdEncoding.EncodeToString(sig))
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if c := grant([]byte(`{"nonce":"a","step":"post the weekly order"}`), nil); c == 200 || len(ran) > 0 {
		t.Fatalf("a grant with a bearer token alone answered %d and ran %v", c, ran)
	}
	if c := grant([]byte(`{"nonce":"b","step":"post the weekly order"}`), other); c == 200 || len(ran) > 0 {
		t.Fatalf("a grant signed by another key answered %d and ran %v", c, ran)
	}
	if c := grant([]byte(`{"nonce":"c","step":"post the weekly order"}`), key); c != 200 || len(ran) != 1 {
		t.Fatalf("a grant signed by the device's key answered %d and ran %v", c, ran)
	}
	if c := grant([]byte(`{"nonce":"c","step":"post the weekly order"}`), key); c == 200 || len(ran) != 1 {
		t.Fatalf("a replayed nonce answered %d and ran %v", c, ran)
	}
}
