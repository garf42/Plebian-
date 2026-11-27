package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"plebian/server/config"
	"plebian/server/models"
)

// ModelSetup handles the Model card: an endpoint answers a listing, the owner picks.
type ModelSetup struct {
	A       *Server
	Secrets *config.Secrets
	HTTP    *http.Client
}

func (m *ModelSetup) routes(ctx context.Context) []models.Route {
	var rs []models.Route
	json.Unmarshal([]byte(m.A.S.Setting(ctx, "routes")), &rs)
	return rs
}

func (m *ModelSetup) providers(ctx context.Context) map[string]providerEntry {
	ps := map[string]providerEntry{}
	json.Unmarshal([]byte(m.A.S.Setting(ctx, "providers")), &ps)
	return ps
}

type providerEntry struct {
	Provider models.Provider    `json:"provider"`
	Models   []models.ModelInfo `json:"models"`
}

func (m *ModelSetup) Handle(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	switch r.PathValue("card") {
	case "model":
		var req struct {
			BaseURL string `json:"base_url"`
			APIKey  string `json:"api_key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return true
		}
		dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		p, ms, err := models.Detect(dctx, req.BaseURL, req.APIKey, m.HTTP)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return true
		}
		if err := m.Secrets.Set("provider:"+p.ID, req.APIKey); err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return true
		}
		ps := m.providers(ctx)
		ps[p.ID] = providerEntry{Provider: p, Models: ms}
		b, _ := json.Marshal(ps)
		m.A.S.SetSetting(ctx, "providers", string(b))
		writeJSON(w, map[string]any{"provider_id": p.ID, "format": p.Format, "models": ms})
	case "model/select":
		var req struct {
			ProviderID string `json:"provider_id"`
			Model      string `json:"model"`
			Role       string `json:"role"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return true
		}
		pe, ok := m.providers(ctx)[req.ProviderID]
		if !ok {
			http.Error(w, "unknown provider", http.StatusBadRequest)
			return true
		}
		var mi *models.ModelInfo
		for i := range pe.Models {
			if pe.Models[i].ID == req.Model {
				mi = &pe.Models[i]
			}
		}
		if mi == nil {
			http.Error(w, "model not in the provider's listing", http.StatusBadRequest)
			return true
		}
		rt := models.Route{Provider: pe.Provider, Model: *mi}
		rs := m.routes(ctx)
		switch req.Role {
		case "", "main":
			if len(rs) == 0 {
				rs = []models.Route{rt}
			} else {
				rs[0] = rt
			}
		case "fallback":
			rs = append(rs, rt)
		default:
			b, _ := json.Marshal(rt)
			m.A.S.SetSetting(ctx, "route:"+req.Role, string(b))
			writeJSON(w, map[string]bool{"ok": true})
			return true
		}
		b, _ := json.Marshal(rs)
		m.A.S.SetSetting(ctx, "routes", string(b))
		if m.A.Hooks.Wake != nil {
			m.A.Hooks.Wake()
		}
		writeJSON(w, map[string]bool{"ok": true})
	case "search":
		// The results page search reads; empty returns to the keyless default.
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return true
		}
		if req.URL != "" && !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			http.Error(w, "the results URL must be http(s)", http.StatusBadRequest)
			return true
		}
		m.A.S.SetSetting(ctx, "search_url", req.URL)
		writeJSON(w, map[string]bool{"ok": true})
	default:
		return false
	}
	return true
}
