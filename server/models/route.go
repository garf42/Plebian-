package models

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"
)

// Route is one entry of the owner's ordered provider list: a provider and a model.
type Route struct {
	Provider Provider  `json:"provider"`
	Model    ModelInfo `json:"model"`
}

// Sleeper waits on the server's clock.
type Sleeper interface {
	Now() time.Time
	After(t time.Time) <-chan time.Time
}

// SendWithin sends r, retrying as the provider's Retry-After directs while the total wait
// stays within window (the owner's usual time to answer, D19). Past it, it returns the
// last error so the next provider can take the turn.
func SendWithin(ctx context.Context, p *Provider, r Request, window time.Duration, clk Sleeper, onText func(string)) (Response, error) {
	start := clk.Now()
	for {
		resp, err := p.Send(ctx, r, onText)
		if err == nil {
			return resp, nil
		}
		var me *Error
		if !errors.As(err, &me) || (me.Class != "retry" && me.Class != "broken" && me.Class != "network") {
			return Response{}, err
		}
		wait := me.RetryAfter
		if wait <= 0 {
			// No Retry-After: the provider gave no wait to honor, so the next provider takes the turn.
			return Response{}, err
		}
		if clk.Now().Add(wait).Sub(start) > window {
			return Response{}, err
		}
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-clk.After(clk.Now().Add(wait)):
		}
	}
}

// Counter counts requests in each target model's tokens before they are sent: the
// provider's own counter where it has one, else the model's highest tokens per byte seen
// in its usage reports. Before any report the ratio is 1 token per byte, an upper bound
// (a token covers at least one byte), so a count never falls short.
type Counter struct {
	mu    sync.Mutex
	Ratio map[string]float64 `json:"ratio"` // model -> highest tokens/byte reported
	Limit map[string]int     `json:"limit"` // model -> context limit learned from errors or truncation
}

func NewCounter() *Counter {
	return &Counter{Ratio: map[string]float64{}, Limit: map[string]int{}}
}

func (c *Counter) MarshalState() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(c)
	return b
}

func (c *Counter) LoadState(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	json.Unmarshal(b, c)
	if c.Ratio == nil {
		c.Ratio = map[string]float64{}
	}
	if c.Limit == nil {
		c.Limit = map[string]int{}
	}
}

// Count returns the token count of r for p's model.
func (c *Counter) Count(ctx context.Context, p *Provider, r Request) (int, error) {
	if n, ok, err := p.Count(ctx, r); err == nil && ok {
		return n, nil
	}
	body, err := p.Body(r)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	ratio, ok := c.Ratio[r.Model]
	c.mu.Unlock()
	if !ok {
		ratio = 1
	}
	return int(math.Ceil(float64(len(body)) * ratio)), nil
}

// CountText counts a span of text for model by the same rule (for budgeting bands).
func (c *Counter) CountText(model string, b []byte) int {
	c.mu.Lock()
	ratio, ok := c.Ratio[model]
	c.mu.Unlock()
	if !ok {
		ratio = 1
	}
	return int(math.Ceil(float64(len(b)) * ratio))
}

// Observe records a usage report: the highest tokens per byte so far, and a context
// limit when the provider counted fewer input tokens than were sent. Within an exchange
// the request only grows, so a report below the previous request's in the same exchange
// (prevIn) means the runtime cut the request: what it kept is its limit.
func (c *Counter) Observe(model string, bodyBytes int, prevIn int, u Usage) {
	if bodyBytes <= 0 {
		return
	}
	in := u.In + u.CacheRead + u.CacheWrite
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := float64(in) / float64(bodyBytes); r > c.Ratio[model] {
		c.Ratio[model] = r
	}
	if prevIn > 0 && in > 0 && in < prevIn {
		if l, ok := c.Limit[model]; !ok || in < l {
			c.Limit[model] = in
		}
	}
}

// Overflowed records that a request of n tokens was refused as too long.
func (c *Counter) Overflowed(model string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.Limit[model]; !ok || n-1 < l {
		c.Limit[model] = n - 1
	}
}

// ContextLimit is the model's context limit: the listing's, else one learned.
func (c *Counter) ContextLimit(m ModelInfo) int {
	if m.Context > 0 {
		return m.Context
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Limit[m.ID]
}
