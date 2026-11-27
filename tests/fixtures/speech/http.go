package speech

import (
	"encoding/json"
	"net/http"
)

// Handler serves POST /frames {"lines": [Line], "codec": 20|21} → {"frames": [bytes],
// "segments": [Segment], "frame_ms": n}: two-voice speech as the Omi encodes it.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /frames", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Lines []Line `json:"lines"`
			Codec int    `json:"codec"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c, err := CodecByID(req.Codec)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		frames, segs, err := Frames(req.Lines, c)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"frames": frames, "segments": segs, "frame_ms": c.FrameMs()})
	})
	return mux
}
