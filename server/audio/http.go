package audio

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"plebian/server/store"
)

// MaxFile bounds one offline upload: the Omi's 512 MB of storage.
const MaxFile = 512 << 20

// LiveHandler serves GET /v1/audio/live: binary frames in, speech minutes out.
func (n *Node) LiveHandler(w http.ResponseWriter, r *http.Request, _ string) {
	codec, err := strconv.Atoi(r.URL.Query().Get("codec"))
	if err != nil {
		http.Error(w, "codec", http.StatusBadRequest)
		return
	}
	if _, err := n.FrameOf(codec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := context.WithoutCancel(r.Context())
	out := make(chan []byte, 64)
	go func() {
		for b := range out {
			wctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			c.Write(wctx, websocket.MessageText, b)
			cancel()
		}
	}()
	defer close(out)
	err = n.Live(ctx, codec, func() (Frame, error) {
		typ, b, err := c.Read(r.Context())
		if err != nil {
			return Frame{}, io.EOF // a closed stream ends the stretch
		}
		if typ != websocket.MessageBinary {
			return n.skip()
		}
		return ReadFrame(b)
	}, func(minute string, seconds float64) {
		b, _ := json.Marshal(map[string]any{"speech": map[string]any{"minute": minute, "seconds": seconds}})
		select {
		case out <- b:
		default:
		}
	})
	if err != nil && n.Log != nil {
		n.Log.Printf("audio live: %v", err)
	}
	c.Close(websocket.StatusNormalClosure, "")
}

func (n *Node) skip() (Frame, error) { return Frame{}, errSkip }

var errSkip = &skipErr{}

type skipErr struct{}

func (*skipErr) Error() string { return "not a frame" }

// FileHandler serves POST /v1/audio/files; the answer comes only once the body is durable.
func (n *Node) FileHandler(w http.ResponseWriter, r *http.Request, _ string) {
	q := func(h string) (int64, error) { return strconv.ParseInt(r.Header.Get(h), 10, 64) }
	codec, err1 := q("X-Omi-Codec")
	ts, err2 := q("X-Omi-File-Timestamp")
	off, err3 := q("X-Omi-Clock-Offset-Ms")
	if err1 != nil || err2 != nil || err3 != nil {
		http.Error(w, "X-Omi-Codec, X-Omi-File-Timestamp and X-Omi-Clock-Offset-Ms are required", http.StatusBadRequest)
		return
	}
	body, err := store.ReadAllLimit(r.Body, MaxFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	sha, err := n.File(context.WithoutCancel(r.Context()), body, int(codec), ts, off)
	if sha == "" {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil && n.Log != nil {
		n.Log.Printf("audio file %s kept, not yet transcribed: %v", sha, err)
	}
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sha256": sha})
}
