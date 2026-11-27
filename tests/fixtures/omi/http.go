package omi

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"plebian/tests/fixtures"
	"plebian/tests/fixtures/speech"
)

// Handler serves the device over loopback HTTP and a WebSocket; the shape is
// written up where cmd/fixture starts it ("fixture omi").
func Handler(d *Device) http.Handler {
	mux := http.NewServeMux()
	fail := func(w http.ResponseWriter, err error) {
		fixtures.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	}
	type phoneReq struct {
		Char  string `json:"char"`
		Value []byte `json:"value"`
		MTU   int    `json:"mtu"`
	}
	phone := func(f func(w http.ResponseWriter, q phoneReq)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var q phoneReq
			if fixtures.ReadJSON(w, r, &q) {
				q.Char = strings.ToLower(q.Char)
				f(w, q)
			}
		}
	}
	mux.HandleFunc("GET /phone/events", func(w http.ResponseWriter, r *http.Request) { serveEvents(d, w, r) })
	mux.HandleFunc("POST /phone/read", phone(func(w http.ResponseWriter, q phoneReq) {
		if v, err := d.Read(q.Char); err != nil {
			fail(w, err)
		} else {
			fixtures.WriteJSON(w, 200, map[string][]byte{"value": v})
		}
	}))
	mux.HandleFunc("POST /phone/write", phone(func(w http.ResponseWriter, q phoneReq) {
		if err := d.Write(q.Char, q.Value); err != nil {
			fail(w, err)
		} else {
			fixtures.WriteJSON(w, 200, map[string]bool{"ok": true})
		}
	}))
	mux.HandleFunc("POST /phone/subscribe", phone(func(w http.ResponseWriter, q phoneReq) {
		if err := d.Subscribe(q.Char); err != nil {
			fail(w, err)
		} else {
			fixtures.WriteJSON(w, 200, map[string]bool{"ok": true})
		}
	}))
	mux.HandleFunc("POST /phone/mtu", phone(func(w http.ResponseWriter, q phoneReq) {
		fixtures.WriteJSON(w, 200, map[string]int{"mtu": d.SetMTU(q.MTU)})
	}))

	mux.HandleFunc("POST /control/talk", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Lines []speech.Line `json:"lines"`
		}
		if !fixtures.ReadJSON(w, r, &q) {
			return
		}
		frames, segs, err := speech.Frames(q.Lines, d.cfg.Codec)
		if err != nil {
			fixtures.WriteJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		go d.Talk(frames)
		fixtures.WriteJSON(w, 200, map[string]any{"frames": len(frames),
			"duration_ms": len(frames) * d.cfg.Codec.FrameMs(), "segments": segs})
	})
	mux.HandleFunc("POST /control/double_tap", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, map[string]bool{"heard": d.DoubleTap()})
	})
	mux.HandleFunc("POST /control/range", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			In *bool `json:"in"`
		}
		if !fixtures.ReadJSON(w, r, &q) {
			return
		}
		if q.In == nil {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "in is required"})
			return
		}
		d.SetRange(*q.In)
		fixtures.WriteJSON(w, 200, d.State())
	})
	mux.HandleFunc("POST /control/clock", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			DeviceUnixMs *int64 `json:"device_unix_ms"`
		}
		if !fixtures.ReadJSON(w, r, &q) {
			return
		}
		if q.DeviceUnixMs == nil {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "device_unix_ms is required"})
			return
		}
		d.SetClock(*q.DeviceUnixMs)
		fixtures.WriteJSON(w, 200, d.State())
	})
	mux.HandleFunc("POST /control/battery", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Level *int `json:"level"`
		}
		if !fixtures.ReadJSON(w, r, &q) {
			return
		}
		if q.Level == nil || *q.Level < 0 || *q.Level > 100 {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "level is a percentage"})
			return
		}
		d.SetBattery(*q.Level)
		fixtures.WriteJSON(w, 200, d.State())
	})
	mux.HandleFunc("GET /control/writes", func(w http.ResponseWriter, r *http.Request) { fixtures.WriteJSON(w, 200, d.Writes()) })
	mux.HandleFunc("GET /control/storage", func(w http.ResponseWriter, r *http.Request) { fixtures.WriteJSON(w, 200, d.Storage()) })
	mux.HandleFunc("GET /control/state", func(w http.ResponseWriter, r *http.Request) { fixtures.WriteJSON(w, 200, d.State()) })
	return mux
}

// The WebSocket below is the least of RFC 6455 this face needs: the server
// sends text frames, and from the phone it takes only close and ping.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11" // RFC 6455 1.3

type wsConn struct {
	mu   sync.Mutex
	conn net.Conn
	rw   *bufio.ReadWriter
}

// frame writes one unmasked server frame (RFC 6455 5.2).
func (c *wsConn) frame(op byte, p []byte) error {
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, byte(n))
	case n < 1<<16:
		h = binary.BigEndian.AppendUint16(append(h, 126), uint16(n))
	default:
		h = binary.BigEndian.AppendUint64(append(h, 127), uint64(n))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.rw.Write(append(h, p...)); err != nil {
		return err
	}
	return c.rw.Flush()
}

// next reads one phone frame: masked, and here only a control frame, whose
// payload RFC 6455 5.5 caps at 125 bytes.
func (c *wsConn) next() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.rw, h[:]); err != nil {
		return 0, nil, err
	}
	op, n := h[0]&0x0F, int(h[1]&0x7F)
	if op < 8 || n > 125 || h[1]&0x80 == 0 {
		return op, nil, errors.New("only masked control frames are taken from the phone")
	}
	var mask [4]byte
	p := make([]byte, n)
	if _, err := io.ReadFull(c.rw, mask[:]); err != nil {
		return 0, nil, err
	}
	if _, err := io.ReadFull(c.rw, p); err != nil {
		return 0, nil, err
	}
	for i := range p {
		p[i] ^= mask[i%4]
	}
	return op, p, nil
}

func serveEvents(d *Device, w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" {
		http.Error(w, "a WebSocket upgrade is required", http.StatusBadRequest)
		return
	}
	events, err := d.Attach()
	if err != nil {
		fixtures.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	defer d.Detach()
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	sum := sha1.Sum([]byte(key + wsGUID))
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
	if rw.Flush() != nil {
		return
	}
	c := &wsConn{conn: conn, rw: rw}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			op, p, err := c.next()
			switch {
			case err != nil:
				_ = c.frame(8, binary.BigEndian.AppendUint16(nil, 1003)) // 1003: unsupported data
				return
			case op == 8:
				_ = c.frame(8, p)
				return
			case op == 9:
				_ = c.frame(10, p)
			}
		}
	}()
	for {
		select {
		case ev := <-events:
			b, _ := json.Marshal(ev)
			if c.frame(1, b) != nil {
				return
			}
		case <-closed:
			return
		}
	}
}
