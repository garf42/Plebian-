package omi

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"plebian/tests/fixtures/speech"
)

var script = []speech.Line{
	{Speaker: "a", Text: "so about that gravel delivery I can bring it wednesday", PauseMs: 800},
	{Speaker: "b", Text: "fifteen yards should do it", PauseMs: 0},
}

func frames(t *testing.T, c speech.Codec) [][]byte {
	t.Helper()
	f, _, err := speech.Frames(script, c)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// phone collects everything the device sends.
type phone struct {
	mu  sync.Mutex
	evs []Event
}

func attach(t *testing.T, d *Device) *phone {
	t.Helper()
	ch, err := d.Attach()
	if err != nil {
		t.Fatal(err)
	}
	p := &phone{}
	go func() {
		for ev := range ch {
			p.mu.Lock()
			p.evs = append(p.evs, ev)
			p.mu.Unlock()
		}
	}()
	p.wait(t, func(evs []Event) bool { return len(evs) > 0 && evs[0].Op == "connected" })
	return p
}

func (p *phone) wait(t *testing.T, done func([]Event) bool) []Event {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		p.mu.Lock()
		evs := append([]Event{}, p.evs...)
		p.mu.Unlock()
		if done(evs) {
			return evs
		}
	}
	t.Fatal("timed out waiting for the device")
	return nil
}

// on returns the notification values on char from index from.
func on(evs []Event, char string, from int) [][]byte {
	var out [][]byte
	for _, e := range evs[min(from, len(evs)):] {
		if e.Op == "notify" && e.Char == char {
			out = append(out, e.Value)
		}
	}
	return out
}

func device(t *testing.T, fw string, c speech.Codec, packetStart uint16) *Device {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Firmware, cfg.Codec, cfg.PacketStart, cfg.FrameInterval = fw, c, packetStart, 0
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The notifications reassemble into exactly the frames the speech generator
// made, across a wrap of the u16 packet number, at the smallest MTU the
// firmware streams at (fragmenting) and at its largest (one frame each).
func TestAudioReassemblesAcrossWrapAndMTUs(t *testing.T) {
	for _, c := range []speech.Codec{speech.Codec21, speech.Codec20} {
		want := frames(t, c)
		for _, mtu := range []int{MinimalPacketSize, DeviceMaxMTU} {
			d := device(t, "3.0.24", c, 0xFFFF-10)
			p := attach(t, d)
			if got := d.SetMTU(517); got != DeviceMaxMTU {
				t.Fatalf("MTU exchange gave %d", got)
			}
			d.SetMTU(mtu)
			must(t, d.Subscribe(AudioData))
			d.Talk(want)
			per, count := mtu-attHeader-fragmentHeader, 0
			for _, f := range want {
				count += (len(f) + per - 1) / per
			}
			notes := on(p.wait(t, func(evs []Event) bool { return len(on(evs, AudioData, 0)) == count }), AudioData, 0)
			var a Assembler
			wrapped, split := false, false
			for i, n := range notes {
				a.Add(n)
				if i > 0 && binary.LittleEndian.Uint16(n) == 0 && binary.LittleEndian.Uint16(notes[i-1]) == 0xFFFF {
					wrapped = true
				}
				if len(n) > mtu-attHeader {
					t.Fatalf("notification of %d bytes over MTU %d", len(n), mtu)
				}
				split = split || n[2] > 0
			}
			got := a.Frames()
			if len(got) != len(want) {
				t.Fatalf("codec %d MTU %d: %d frames back, want %d", c.ID, mtu, len(got), len(want))
			}
			for i := range want {
				if !bytes.Equal(got[i].Data, want[i]) {
					t.Fatalf("codec %d MTU %d: frame %d differs", c.ID, mtu, i)
				}
			}
			if !wrapped {
				t.Fatalf("codec %d MTU %d: packet numbers never wrapped", c.ID, mtu)
			}
			maxFrame := 0
			for _, f := range want {
				maxFrame = max(maxFrame, len(f))
			}
			if wantSplit := maxFrame > mtu-attHeader-fragmentHeader; split != wantSplit {
				t.Fatalf("codec %d MTU %d: fragmented=%v, want %v", c.ID, mtu, split, wantSplit)
			}
			if st := d.Storage().(map[string]any); st["write_seq"].(uint64) != 0 {
				t.Fatalf("live audio also went to storage: %v", st["write_seq"])
			}
		}
	}
}

func TestBelowMinimalMTUAudioGoesToStorage(t *testing.T) {
	d := device(t, "3.0.24", speech.Codec21, 0)
	p := attach(t, d)
	must(t, d.Subscribe(AudioData)) // MTU stays at the ATT default, 23
	d.Talk(frames(t, speech.Codec21))
	if n := on(p.wait(t, func([]Event) bool { return true }), AudioData, 0); len(n) != 0 {
		t.Fatalf("%d audio notifications at MTU 23", len(n))
	}
	if st := d.Storage().(map[string]any); st["write_seq"].(uint64) == 0 {
		t.Fatal("nothing stored")
	}
}

func TestButtonHapticTimeSync(t *testing.T) {
	d := device(t, "3.0.24", speech.Codec21, 0)
	p := attach(t, d)
	must(t, d.Subscribe(Button))
	if !d.DoubleTap() {
		t.Fatal("tap not heard")
	}
	evs := p.wait(t, func(evs []Event) bool { return len(on(evs, Button, 0)) == 1 })
	if v := on(evs, Button, 0)[0]; !bytes.Equal(v, []byte{DoubleTap, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("button value %v", v)
	}
	for _, level := range []byte{1, 2, 3} {
		must(t, d.Write(Haptic, []byte{level}))
	}
	sync := uint32(1764000000)
	must(t, d.Write(TimeSyncWrite, binary.LittleEndian.AppendUint32(nil, sync)))
	if err := d.Write(TimeSyncWrite, []byte{1, 2, 3}); err == nil || err.Error() != ErrInvalidLength {
		t.Fatalf("3-byte time sync: %v", err)
	}
	if err := d.Write(Button, []byte{1}); err == nil || err.Error() != ErrWriteNotPermitted {
		t.Fatalf("write to button: %v", err)
	}
	var notes []string
	for _, w := range d.Writes() {
		notes = append(notes, w.Note)
	}
	if got := strings.Join(notes[:4], "|"); got != "haptic 100 ms|haptic 300 ms|haptic 500 ms|time sync 1764000000" {
		t.Fatalf("writes %q", got)
	}
	v, err := d.Read(TimeSyncRead)
	if got := binary.LittleEndian.Uint32(v); err != nil || got < sync || got > sync+1 {
		t.Fatalf("device time %d", got)
	}
	if fw, _ := d.Read(FirmwareRev); string(fw) != "3.0.24" {
		t.Fatalf("firmware %q", fw)
	}
	d.SetRange(false)
	p.wait(t, func(evs []Event) bool { return evs[len(evs)-1].Op == "disconnected" })
	if d.DoubleTap() {
		t.Fatal("a tap out of range reached the phone")
	}
}

// Offline audio lands in ring records stamped by the device clock and syncs
// back byte for byte over the 3.0.20+ ring protocol.
func TestRingOfflineSync(t *testing.T) {
	want := frames(t, speech.Codec21)
	d := device(t, "3.0.24", speech.Codec21, 0)
	p := attach(t, d)
	sync := uint32(1764000000)
	must(t, d.Write(TimeSyncWrite, binary.LittleEndian.AppendUint32(nil, sync)))
	d.SetRange(false)
	d.Talk(want)
	d.SetRange(true)
	d.SetMTU(DeviceMaxMTU)
	must(t, d.Subscribe(StorageData))
	status, _ := d.Read(StorageRead)
	n := binary.LittleEndian.Uint32(status[4:])
	if n == 0 || binary.LittleEndian.Uint32(status[0:]) != n*recordBytes || binary.LittleEndian.Uint32(status[12:]) != 1 {
		t.Fatalf("status %v", status)
	}
	mark := len(p.wait(t, func([]Event) bool { return true }))
	must(t, d.Write(StorageData, []byte{0x10}))
	evs := p.wait(t, func(evs []Event) bool { return len(on(evs, StorageData, mark)) == 1 })
	info := on(evs, StorageData, mark)[0]
	if len(info) != 41 || info[0] != ntInfo || be64(info[9:]) != uint64(n) || binary.BigEndian.Uint16(info[29:]) != recordBytes {
		t.Fatalf("info %v", info)
	}
	ringID := be64(info[33:])
	mark = len(evs)
	must(t, d.Write(StorageData, binary.BigEndian.AppendUint64([]byte{0x11}, 0)))
	evs = p.wait(t, func(evs []Event) bool {
		ns := on(evs, StorageData, mark)
		return len(ns) > 0 && ns[len(ns)-1][0] == ntDone
	})
	ns := on(evs, StorageData, mark)
	if ns[0][0] != ntReadBegin || binary.BigEndian.Uint32(ns[0][9:]) != n {
		t.Fatalf("read begin %v", ns[0])
	}
	var raw []byte
	for _, x := range ns[1 : len(ns)-1] {
		if x[0] != ntData || len(x) > DeviceMaxMTU-attHeader {
			t.Fatalf("data note %d bytes", len(x))
		}
		raw = append(raw, x[1:]...)
	}
	if done := ns[len(ns)-1]; done[1] != stOK || be64(done[2:]) != uint64(n) || len(raw) != int(n)*recordBytes {
		t.Fatalf("done %v with %d bytes", done, len(raw))
	}
	var got [][]byte
	for off := 0; off < len(raw); off += recordBytes {
		ts := binary.BigEndian.Uint32(raw[off:])
		if ts&tsUptimeFlag != 0 || ts < sync || ts > sync+5 {
			t.Fatalf("record timestamp %d", ts)
		}
		got = append(got, Unpack(raw[off+4:off+recordBytes])...)
	}
	if len(got) != len(want) {
		t.Fatalf("%d frames back, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("frame %d differs", i)
		}
	}
	mark = len(evs)
	must(t, d.Write(StorageData, binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64([]byte{0x15}, ringID+1), uint64(n))))
	must(t, d.Write(StorageData, binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64([]byte{0x15}, ringID), uint64(n))))
	evs = p.wait(t, func(evs []Event) bool { return len(on(evs, StorageData, mark)) == 2 })
	if acks := on(evs, StorageData, mark); acks[0][1] != stIDMismatch || acks[1][1] != stOK {
		t.Fatalf("advance acks %v", acks)
	}
	if status, _ = d.Read(StorageRead); binary.LittleEndian.Uint32(status[4:]) != 0 {
		t.Fatalf("unread after advance: %v", status)
	}
}

func TestMultiFileAndLengthListProtocols(t *testing.T) {
	want := frames(t, speech.Codec21)
	check := func(t *testing.T, got [][]byte) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%d frames back, want %d", len(got), len(want))
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("frame %d differs", i)
			}
		}
	}
	offline := func(t *testing.T, fw string) (*Device, *phone) {
		d := device(t, fw, speech.Codec21, 0)
		p := attach(t, d)
		must(t, d.Write(TimeSyncWrite, binary.LittleEndian.AppendUint32(nil, 1764000000)))
		d.SetRange(false)
		d.Talk(want)
		d.SetRange(true)
		d.SetMTU(DeviceMaxMTU)
		must(t, d.Subscribe(StorageData))
		return d, p
	}
	ends := func(b byte) func([]Event) bool {
		return func(evs []Event) bool { ns := on(evs, StorageData, 0); return len(ns) > 0 && ns[len(ns)-1][0] == b }
	}

	t.Run("3.0.18 multi-file", func(t *testing.T) {
		d, p := offline(t, "3.0.18")
		status, _ := d.Read(StorageRead)
		if binary.LittleEndian.Uint32(status[4:]) != 1 {
			t.Fatalf("status %v", status)
		}
		must(t, d.Write(StorageData, []byte{0x10}))
		list := on(p.wait(t, func(evs []Event) bool { return len(on(evs, StorageData, 0)) == 1 }), StorageData, 0)[0]
		if list[0] != 1 || binary.BigEndian.Uint32(list[1:]) < 1764000000 || binary.BigEndian.Uint32(list[5:]) != binary.LittleEndian.Uint32(status) {
			t.Fatalf("list %v", list)
		}
		must(t, d.Write(StorageData, []byte{0x11, 0, 0, 0, 0, 0}))
		ns := on(p.wait(t, ends(100)), StorageData, 0) // the list, [0], the records, [100]
		if ns[1][0] != 0 {
			t.Fatalf("read not acknowledged: %v", ns[1])
		}
		var got [][]byte
		for _, x := range ns[2 : len(ns)-1] {
			if len(x) != recordBytes || binary.BigEndian.Uint32(x) < 1764000000 {
				t.Fatalf("record of %d bytes", len(x))
			}
			got = append(got, Unpack(x[4:])...)
		}
		check(t, got)
		must(t, d.Write(StorageData, []byte{0x12, 0}))
		p.wait(t, func(evs []Event) bool {
			ns := on(evs, StorageData, 0)
			return ns[len(ns)-1][0] == 0 && len(ns[len(ns)-1]) == 1
		})
		if status, _ = d.Read(StorageRead); binary.LittleEndian.Uint32(status[4:]) != 0 {
			t.Fatalf("files after delete: %v", status)
		}
	})
	t.Run("3.0.16 length list", func(t *testing.T) {
		d, p := offline(t, "3.0.16")
		status, _ := d.Read(StorageRead)
		total := binary.LittleEndian.Uint32(status)
		if total == 0 || total%recordAudio != 0 || binary.LittleEndian.Uint32(status[8:]) < 1764000000 {
			t.Fatalf("status %v", status)
		}
		must(t, d.Write(StorageData, []byte{0x00, 1, 0, 0, 0, 0}))
		ns := on(p.wait(t, ends(100)), StorageData, 0)
		var got [][]byte
		for _, x := range ns[:len(ns)-1] {
			if len(x) != recordAudio {
				t.Fatalf("block of %d bytes", len(x))
			}
			got = append(got, Unpack(x)...)
		}
		check(t, got)
		if ns[0][0] != 0xFF || binary.LittleEndian.Uint32(ns[0][1:]) < 1764000000 {
			t.Fatalf("no timestamp marker opening the recording: %v", ns[0][:5])
		}
		must(t, d.Write(StorageData, []byte{0x01, 1, 0, 0, 0, 0}))
		if status, _ = d.Read(StorageRead); binary.LittleEndian.Uint32(status) != 0 {
			t.Fatal("stream not cleared")
		}
	})
}

// wsDial is a minimal RFC 6455 client: it reads the server's text frames.
func wsDial(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	must(t, err)
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	_, err = io.WriteString(conn, "GET /phone/events HTTP/1.1\r\nHost: "+addr+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: "+base64.StdEncoding.EncodeToString(key)+"\r\n\r\n")
	must(t, err)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", err, resp)
	}
	return conn, br
}

func wsEvent(t *testing.T, br *bufio.Reader) Event {
	t.Helper()
	var h [2]byte
	_, err := io.ReadFull(br, h[:])
	must(t, err)
	n := int(h[1] & 0x7F)
	if n == 126 {
		var l [2]byte
		_, _ = io.ReadFull(br, l[:])
		n = int(binary.BigEndian.Uint16(l[:]))
	}
	p := make([]byte, n)
	_, err = io.ReadFull(br, p)
	must(t, err)
	var ev Event
	must(t, json.Unmarshal(p, &ev))
	return ev
}

func TestHTTPFace(t *testing.T) {
	d := device(t, "3.0.24", speech.Codec21, 0)
	srv := httptest.NewServer(Handler(d))
	defer srv.Close()
	post := func(path, body string) map[string]any {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		must(t, err)
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	conn, br := wsDial(t, strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()
	if ev := wsEvent(t, br); ev.Op != "connected" {
		t.Fatalf("first event %+v", ev)
	}
	if out := post("/phone/mtu", `{"mtu":517}`); out["mtu"].(float64) != DeviceMaxMTU {
		t.Fatalf("mtu %v", out)
	}
	post("/phone/subscribe", `{"char":"`+Button+`"}`)
	post("/control/double_tap", "")
	if ev := wsEvent(t, br); ev.Op != "notify" || ev.Char != Button || ev.Value[0] != DoubleTap {
		t.Fatalf("tap event %+v", ev)
	}
	post("/phone/write", `{"char":"`+Haptic+`","value":"AQ=="}`)
	out := post("/phone/read", `{"char":"`+AudioCodec+`"}`)
	if v, _ := base64.StdEncoding.DecodeString(out["value"].(string)); len(v) != 1 || v[0] != 21 {
		t.Fatalf("codec read %v", out)
	}
	if out := post("/phone/read", `{"char":"`+Haptic+`"}`); out["error"] != ErrReadNotPermitted {
		t.Fatalf("haptic read %v", out)
	}
	resp, _ := http.Get(srv.URL + "/control/writes")
	var writes []Write
	_ = json.NewDecoder(resp.Body).Decode(&writes)
	if len(writes) != 1 || writes[0].Note != "haptic 100 ms" {
		t.Fatalf("writes %+v", writes)
	}
	post("/control/range", `{"in":false}`)
	if ev := wsEvent(t, br); ev.Op != "disconnected" {
		t.Fatalf("out of range %+v", ev)
	}
	post("/control/range", `{"in":true}`)
	if ev := wsEvent(t, br); ev.Op != "connected" {
		t.Fatalf("back in range %+v", ev)
	}
	if _, err := d.Attach(); err == nil {
		t.Fatal("a second phone attached")
	}
}
