// Package omi simulates the Omi wearable at the BLE GATT level, built from
// the firmware source (omi/firmware/omi, MIT): its services and
// characteristics, its audio notifications fragmented to the negotiated MTU,
// its button and haptic, its clock, and its offline storage in the on-device
// layout with the storage protocol its firmware revision speaks.
package omi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"plebian/tests/fixtures/speech"
)

// Event is one thing the device sends the phone.
type Event struct {
	Op    string `json:"op"` // connected, disconnected, notify
	Char  string `json:"char,omitempty"`
	Value []byte `json:"value,omitempty"` // base64 in JSON
}

// Write is one write the phone made, as received.
type Write struct {
	Time  time.Time `json:"time"`
	Char  string    `json:"char"`
	Value []byte    `json:"value"`
	Note  string    `json:"note,omitempty"`  // decoded meaning: "haptic 100 ms", "time sync 1764...", "storage ..."
	Error string    `json:"error,omitempty"` // the ATT error answered, if any
}

// Config is the device as it ships.
type Config struct {
	Firmware    string       // DIS firmware revision; selects the storage protocol
	Codec       speech.Codec // audio codec the device encodes with
	Serial      string       // DIS serial: 16 uppercase hex, the nRF FICR device ID
	Battery     int          // percent
	PacketStart uint16       // packet counter at boot (the firmware starts at 0)
	// FrameInterval paces Talk; zero means as fast as the phone reads, otherwise
	// the codec's frame duration plays it in real time.
	FrameInterval time.Duration
}

// DefaultConfig is the current consumer Omi: omi.conf's DIS strings and
// config.h's codec 21.
// gap: the serial (any 16 uppercase hex, unit_id.h's shape) and the 100 % battery are picked; each unit reads its own.
func DefaultConfig() Config {
	return Config{Firmware: "3.0.24", Codec: speech.Codec21, Serial: "5045424C49414E31", Battery: 100,
		FrameInterval: time.Duration(speech.Codec21.FrameMs()) * time.Millisecond}
}

type sink struct {
	ch   chan Event
	done chan struct{}
}

// Device is one simulated Omi.
type Device struct {
	cfg  Config
	boot time.Time

	mu         sync.Mutex
	talkMu     sync.Mutex
	phone      *sink
	inRange    bool
	mtu        int
	subs       map[string]bool
	packetNext uint16
	rtcValid   bool
	rtcBaseMs  int64
	rtcSetAt   time.Time
	battery    int
	writes     []Write
	store      store
	talking    bool
	queue      chan struct{} // closed when the last queued storage answer has gone out
}

// New returns a device in range with no phone connected.
func New(cfg Config) (*Device, error) {
	if cfg.Codec.FrameSamples == 0 {
		return nil, errors.New("config needs a codec")
	}
	d := &Device{cfg: cfg, boot: time.Now(), inRange: true, mtu: AttDefaultMTU, subs: map[string]bool{},
		packetNext: cfg.PacketStart, battery: cfg.Battery, queue: make(chan struct{})}
	close(d.queue)
	st, err := newStore(cfg.Firmware, d)
	if err != nil {
		return nil, err
	}
	d.store = st
	return d, nil
}

// version parses "a.b.c" into comparable parts.
func version(s string) [3]int {
	var v [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		v[i], _ = strconv.Atoi(p)
	}
	return v
}

func atLeast(fw string, a, b, c int) bool {
	v := version(fw)
	return v[0] > a || v[0] == a && (v[1] > b || v[1] == b && v[2] >= c)
}

func (d *Device) connectedLocked() bool { return d.phone != nil && d.inRange }

// deviceTimeLocked is the firmware's omi_record_timestamp: RTC seconds when
// the clock is set and plausible, else seconds since boot with bit 31 set.
func (d *Device) deviceTimeLocked() uint32 {
	if d.rtcValid {
		s := (d.rtcBaseMs + time.Since(d.rtcSetAt).Milliseconds()) / 1000
		if s >= tsMinUTC && s <= 0x7FFFFFFF {
			return uint32(s)
		}
	}
	return tsUptimeFlag | uint32(time.Since(d.boot).Milliseconds()/1000)&0x7FFFFFFF
}

// emit sends ev to the phone that was connected when it was produced.
func emit(s *sink, ev Event) {
	if s == nil {
		return
	}
	select {
	case s.ch <- ev:
	case <-s.done:
	}
}

// notifyLocked returns the sink to notify if the phone is connected and subscribed.
func (d *Device) notifyLocked(char string) *sink {
	if d.connectedLocked() && d.subs[char] {
		return d.phone
	}
	return nil
}

// Attach connects a phone. The device takes one phone at a time (src.omi).
func (d *Device) Attach() (<-chan Event, error) {
	d.mu.Lock()
	if d.phone != nil {
		d.mu.Unlock()
		return nil, errors.New("a phone is already connected")
	}
	s := &sink{ch: make(chan Event), done: make(chan struct{})}
	d.phone = s
	in := d.inRange
	if in {
		d.onConnectLocked()
	}
	d.mu.Unlock()
	if in {
		go emit(s, Event{Op: "connected"})
	}
	return s.ch, nil
}

// Detach drops the phone's link.
func (d *Device) Detach() {
	d.mu.Lock()
	if d.phone != nil {
		close(d.phone.done)
		d.phone = nil
		d.onDisconnectLocked()
	}
	d.mu.Unlock()
}

func (d *Device) onConnectLocked() {
	d.mtu = AttDefaultMTU
	d.store.connected()
}

// onDisconnectLocked forgets what a link held: subscriptions, MTU, a running transfer.
func (d *Device) onDisconnectLocked() {
	d.subs = map[string]bool{}
	d.mtu = AttDefaultMTU
	d.store.disconnected()
}

// SetRange moves the device in or out of the phone's range.
func (d *Device) SetRange(in bool) {
	d.mu.Lock()
	was := d.connectedLocked()
	d.inRange = in
	now := d.connectedLocked()
	s := d.phone
	switch {
	case was && !now:
		d.onDisconnectLocked()
	case !was && now:
		d.onConnectLocked()
	}
	d.mu.Unlock()
	if was && !now {
		emit(s, Event{Op: "disconnected"})
	} else if !was && now {
		emit(s, Event{Op: "connected"})
	}
}

// SetMTU is the phone's ATT MTU exchange: the result is the smaller of both sides'.
func (d *Device) SetMTU(n int) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.connectedLocked() {
		return d.mtu
	}
	d.mtu = min(max(n, AttDefaultMTU), DeviceMaxMTU)
	return d.mtu
}

// SetClock sets the device's RTC to ms now (a clock that is off by some skew).
func (d *Device) SetClock(ms int64) {
	d.mu.Lock()
	d.rtcValid, d.rtcBaseMs, d.rtcSetAt = true, ms, time.Now()
	d.mu.Unlock()
}

// SetBattery changes the battery level and notifies a subscribed phone.
func (d *Device) SetBattery(level int) {
	d.mu.Lock()
	d.battery = level
	s := d.notifyLocked(BatteryLevel)
	d.mu.Unlock()
	emit(s, Event{Op: "notify", Char: BatteryLevel, Value: []byte{byte(level)}})
}

// Subscribe writes the characteristic's CCC to notify.
func (d *Device) Subscribe(char string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.connectedLocked() {
		return errors.New(ErrNotConnected)
	}
	if !notifiable[char] {
		return errors.New(ErrNotNotifiable)
	}
	d.subs[char] = true
	return nil
}

func u32le(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// Read answers a characteristic read.
func (d *Device) Read(char string) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.connectedLocked() {
		return nil, errors.New(ErrNotConnected)
	}
	switch char {
	case AudioData:
		return []byte{}, nil
	case AudioCodec:
		return []byte{byte(d.cfg.Codec.ID)}, nil
	case Features:
		return u32le(features), nil
	case TimeSyncRead:
		t := d.deviceTimeLocked()
		if t&tsUptimeFlag != 0 {
			t = 0 // get_utc_time with no RTC set
		}
		return u32le(t), nil
	case Button:
		return make([]byte, 8), nil // final_button_state: int[2], zero until a press
	case StorageRead:
		return d.store.status(), nil
	case BatteryLevel:
		return []byte{byte(d.battery)}, nil
	case ModelNumber:
		return []byte("Omi CV 1"), nil
	case SerialNumber:
		return []byte(d.cfg.Serial), nil
	case FirmwareRev:
		return []byte(d.cfg.Firmware), nil
	case HardwareRev:
		return []byte("5.0"), nil
	case Manufacturer:
		return []byte("Based Hardware"), nil
	case TimeSyncWrite, Haptic, StorageData:
		return nil, errors.New(ErrReadNotPermitted)
	}
	return nil, errors.New(ErrNotFound)
}

// Write handles a characteristic write and records it.
func (d *Device) Write(char string, value []byte) error {
	d.mu.Lock()
	w := Write{Time: time.Now().UTC(), Char: char, Value: append([]byte{}, value...)}
	var after func()
	err := func() error {
		if !d.connectedLocked() {
			return errors.New(ErrNotConnected)
		}
		switch char {
		case TimeSyncWrite:
			if len(value) != 4 {
				return errors.New(ErrInvalidLength)
			}
			s := binary.LittleEndian.Uint32(value)
			d.rtcValid, d.rtcBaseMs, d.rtcSetAt = true, int64(s)*1000, time.Now()
			w.Note = fmt.Sprintf("time sync %d", s)
		case Haptic:
			if len(value) < 1 {
				return errors.New(ErrInvalidLength)
			}
			ms := map[byte]int{1: 100, 2: 300, 3: 500}[value[0]] // haptic.c
			if ms == 0 {
				w.Note = fmt.Sprintf("haptic %d ignored", value[0])
			} else {
				w.Note = fmt.Sprintf("haptic %d ms", ms)
			}
		case StorageData:
			if len(value) < 1 {
				return errors.New(ErrInvalidLength)
			}
			w.Note, after = d.store.command(value)
		default:
			if _, err := d.readableLocked(char); err == nil {
				return errors.New(ErrWriteNotPermitted)
			}
			return errors.New(ErrNotFound)
		}
		return nil
	}()
	if err != nil {
		w.Error = err.Error()
	}
	d.writes = append(d.writes, w)
	if after != nil {
		// Storage answers go out in command order, as the firmware's storage thread drains its queue.
		// shortcut: a command sent during a read is answered after the read; the firmware answers it between chunks; upgrade when the app interleaves them
		prev, done := d.queue, make(chan struct{})
		d.queue = done
		go func() { <-prev; after(); close(done) }()
	}
	d.mu.Unlock()
	return err
}

func (d *Device) readableLocked(char string) (bool, error) {
	switch char {
	case AudioData, AudioCodec, Features, TimeSyncRead, Button, StorageRead, BatteryLevel,
		ModelNumber, SerialNumber, FirmwareRev, HardwareRev, Manufacturer:
		return true, nil
	}
	return false, errors.New(ErrNotFound)
}

// Writes returns every write the phone made.
func (d *Device) Writes() []Write {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Write{}, d.writes...)
}

// Storage describes what the device holds offline.
func (d *Device) Storage() any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.store.describe()
}

// fragmentsLocked splits one frame as push_to_gatt does: payload MTU-6 per
// notification, each headed by the packet number (LE16, one per notification,
// wrapping) and the fragment's index within the frame.
func (d *Device) fragmentsLocked(frame []byte) [][]byte {
	per := d.mtu - attHeader - fragmentHeader
	var out [][]byte
	for off, idx := 0, 0; off < len(frame); idx++ {
		n := min(per, len(frame)-off)
		p := make([]byte, 0, fragmentHeader+n)
		p = binary.LittleEndian.AppendUint16(p, d.packetNext)
		p = append(p, byte(idx))
		out = append(out, append(p, frame[off:off+n]...))
		d.packetNext++
		off += n
	}
	return out
}

// DoubleTap presses the button twice; a connected, subscribed phone hears it.
func (d *Device) DoubleTap() bool { return d.button(DoubleTap) }

func (d *Device) button(code int32) bool {
	d.mu.Lock()
	s := d.notifyLocked(Button)
	d.mu.Unlock()
	v := make([]byte, 8) // int final_button_state[2], little-endian
	binary.LittleEndian.PutUint32(v, uint32(code))
	emit(s, Event{Op: "notify", Char: Button, Value: v})
	return s != nil
}

// Talk plays frames as the microphone would: each goes to the phone as
// audio notifications while it is connected, subscribed and the MTU is at
// least MinimalPacketSize (the pusher's test), and into offline storage
// otherwise. It returns when the last frame is placed.
func (d *Device) Talk(frames [][]byte) {
	d.talkMu.Lock()
	defer d.talkMu.Unlock()
	d.mu.Lock()
	d.talking = true
	d.mu.Unlock()
	next := time.Now()
	for _, f := range frames {
		d.mu.Lock()
		var s *sink
		var notes [][]byte
		if d.connectedLocked() && d.mtu >= MinimalPacketSize && d.subs[AudioData] {
			s, notes = d.phone, d.fragmentsLocked(f)
			// shortcut: the firmware drains a part-filled record after 1 s idle; draining at the first live frame lands the same record a moment sooner
			d.store.flush()
		} else {
			d.store.frame(f)
		}
		d.mu.Unlock()
		for _, n := range notes {
			emit(s, Event{Op: "notify", Char: AudioData, Value: n})
		}
		if d.cfg.FrameInterval > 0 {
			next = next.Add(d.cfg.FrameInterval)
			time.Sleep(time.Until(next))
		}
	}
	d.mu.Lock()
	d.store.flush() // the firmware's 1 s idle drain (STORAGE_PACK_IDLE_FLUSH_MS), taken when the talk ends
	d.talking = false
	d.mu.Unlock()
}

// State is a summary for scenarios.
type State struct {
	Connected  bool     `json:"connected"`
	InRange    bool     `json:"in_range"`
	MTU        int      `json:"mtu"`
	Subscribed []string `json:"subscribed"`
	PacketNext uint16   `json:"packet_next"`
	Firmware   string   `json:"firmware"`
	Codec      int      `json:"codec"`
	Protocol   string   `json:"protocol"`
	Talking    bool     `json:"talking"`
	ClockSet   bool     `json:"clock_set"`
}

// State reports the device's state.
func (d *Device) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := State{Connected: d.connectedLocked(), InRange: d.inRange, MTU: d.mtu, PacketNext: d.packetNext,
		Firmware: d.cfg.Firmware, Codec: d.cfg.Codec.ID, Protocol: d.store.name(), Talking: d.talking, ClockSet: d.rtcValid}
	for c := range d.subs {
		st.Subscribed = append(st.Subscribed, c)
	}
	return st
}
