package models_test

import (
	"context"
	"strings"
	"testing"

	"plebian/server/models"
)

// D3 (n.models): each model's tokens are counted by that model's own reports, and a
// count never falls short of what the model reported for the same bytes: before any
// report a byte is a token (a token covers at least one byte); after one, the model's
// highest tokens per byte. One model's reports never lower another's count.
func TestCountsNeverShort(t *testing.T) {
	ctx := context.Background()
	p := &models.Provider{ID: "local", Format: "openai"} // no counter of its own
	req := func(model string) models.Request {
		return models.Request{Model: model, MaxTokens: 1, Messages: []models.Message{{Role: "user", Content: []models.Block{{Type: "text", Text: strings.Repeat("gravel by the culvert ", 200)}}}}}
	}
	body, _ := p.Body(req("dense"))
	c := models.NewCounter()
	if n, _ := c.Count(ctx, p, req("dense")); n < len(body) {
		t.Fatalf("before any report a %d-byte request counted %d tokens: a token covers at least one byte", len(body), n)
	}
	// Two models with different tokenizers report on the same bytes.
	c.Observe("dense", len(body), 0, models.Usage{In: len(body) / 2})
	c.Observe("sparse", len(body), 0, models.Usage{In: len(body) / 5})
	if n, _ := c.Count(ctx, p, req("dense")); n < len(body)/2 {
		t.Fatalf("the dense model reported %d tokens for these bytes; counted %d", len(body)/2, n)
	}
	if n, _ := c.Count(ctx, p, req("sparse")); n < len(body)/5 {
		t.Fatalf("the sparse model reported %d tokens for these bytes; counted %d", len(body)/5, n)
	}
	// A later, lower report for the dense model does not lower its count.
	c.Observe("dense", len(body), 0, models.Usage{In: len(body) / 4})
	if n, _ := c.Count(ctx, p, req("dense")); n < len(body)/2 {
		t.Fatalf("after a lower report the dense model counted %d, below its highest %d", n, len(body)/2)
	}
}
