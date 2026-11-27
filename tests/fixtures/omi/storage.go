package omi

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// store is the device's offline storage and the protocol its firmware speaks
// on the storage characteristics. Every method runs with the device locked;
// command's returned func runs after, unlocked, on the storage queue.
type store interface {
	name() string
	frame(f []byte) // one codec frame recorded while not streaming
	flush()         // write out a part-filled record
	connected()     // a link came up
	disconnected()  // the link went down
	status() []byte // the StorageRead value
	command(v []byte) (note string, after func())
	describe() any
}

// newStore picks the protocol by firmware revision as Omi's app does
// (app/lib/services/devices/ring_protocol.dart isRingBufferFirmware: 3.0.20 and
// later; app/lib/services/wals/storage_sync.dart: LittleFS multi-file
// 3.0.17-3.0.19; earlier: one length-listed stream, with timestamp markers
// from 3.0.16, sdcard_wal_sync.dart _timestampMarkerMinVersion).
func newStore(fw string, d *Device) (store, error) {
	switch {
	case atLeast(fw, 3, 0, 20):
		var id [8]byte
		if _, err := rand.Read(id[:]); err != nil { // ring_generate_id draws a random id too
			return nil, err
		}
		return &ring{d: d, id: binary.BigEndian.Uint64(id[:]) | 1,
			capacity: uint32((maxStorage/sectorBytes - rawMetaSect) / rawBatchSect * recordsPerBat)}, nil
	case atLeast(fw, 3, 0, 17):
		return &files{d: d}, nil
	}
	return &legacy{d: d, markers: atLeast(fw, 3, 0, 16)}, nil
}

// gap: the card's size is taken as sd_card.h MAX_STORAGE_BYTES; the firmware sizes the ring from the card it mounts.

// packer is omi_live_pack (custody.c): entries [size][frame] packed into a
// 440-byte block whose last byte always stays zero; an entry that does not
// fit closes the block, and a block filled exactly closes at once.
type packer struct {
	buf [recordAudio]byte
	off int
}

func (p *packer) add(entry []byte) []byte {
	limit := recordAudio - 1
	if len(entry) > limit {
		return nil // omi_live_pack_frame refuses it
	}
	var out []byte
	if p.off+len(entry) > limit && p.off > 0 {
		out = p.take()
	}
	p.off += copy(p.buf[p.off:], entry)
	if p.off == limit && out == nil {
		out = p.take()
	}
	return out
}

// take returns the part-filled block zero-padded, or nil when empty.
func (p *packer) take() []byte {
	if p.off == 0 {
		return nil
	}
	b := append([]byte{}, p.buf[:]...)
	clear(b[p.off:])
	p.buf, p.off = [recordAudio]byte{}, 0
	return b
}

func entry(f []byte) []byte { return append([]byte{byte(len(f))}, f...) }

// record is a ring record (sd_card.c process_write_data_req): BE32 timestamp, then the block.
func (d *Device) record(block []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, d.deviceTimeLocked()), block...)
}

// xfer is one storage read in flight; stop ends it at the next notification.
type xfer struct {
	gen  int
	busy bool
}

func (x *xfer) stop() { x.gen++; x.busy = false }

// sendLocked returns a func emitting notes on StorageData to the phone
// connected now, one by one while that link and this transfer last.
func (d *Device) sendLocked(x *xfer, notes [][]byte) func() {
	s, mine := d.notifyLocked(StorageData), 0
	if x != nil && s != nil {
		x.busy, mine = true, x.gen
	}
	return func() {
		defer func() {
			if x != nil {
				d.mu.Lock()
				if x.gen == mine {
					x.busy = false
				}
				d.mu.Unlock()
			}
		}()
		for _, n := range notes {
			d.mu.Lock()
			ok := s != nil && d.notifyLocked(StorageData) == s && (x == nil || x.gen == mine)
			d.mu.Unlock()
			if !ok {
				return
			}
			emit(s, Event{Op: "notify", Char: StorageData, Value: n})
		}
	}
}

// ---- ring buffer, firmware 3.0.20 and later (storage.c, sd_card.c, custody.c) ----

// Storage statuses and notification opcodes (storage.c).
const (
	stOK         = 0
	stInvalid    = 6
	stNotReady   = 9
	stOutOfRng   = 10
	stIDMismatch = 11

	ntAck       = 0x01
	ntInfo      = 0x02
	ntData      = 0x03
	ntDone      = 0x04
	ntReadBegin = 0x05
)

type rec struct {
	seq  uint64
	data []byte // recordBytes
}

type ring struct {
	d                        *Device
	id                       uint64
	readSeq, writeSeq, dropd uint64
	capacity                 uint32
	recs                     []rec
	pack                     packer
	caps                     byte
	x                        xfer
}

func (r *ring) name() string { return "ring" }

func (r *ring) put(block []byte) {
	if block == nil {
		return
	}
	// shortcut: a full ring keeps writing; upgrade when a scenario records past the card's capacity
	r.recs = append(r.recs, rec{r.writeSeq, r.d.record(block)})
	r.writeSeq++
}

func (r *ring) frame(f []byte) { r.put(r.pack.add(entry(f))) }
func (r *ring) flush()         { r.put(r.pack.take()) }
func (r *ring) connected()     { r.flush(); r.caps = 0 } // storage_connection_changed clears granted caps
func (r *ring) disconnected()  { r.x.stop() }

func (r *ring) status() []byte {
	unread := r.writeSeq - r.readSeq
	free := (uint64(r.capacity) - unread) * recordBytes
	b := u32le(uint32(min(unread*recordBytes, 0xFFFFFFFF)))
	b = append(b, u32le(uint32(min(unread, 0xFFFFFFFF)))...)
	b = append(b, u32le(uint32(min(free, 0xFFFFFFFF)))...)
	valid := uint32(0)
	if r.d.rtcValid {
		valid = 1
	}
	return append(b, u32le(valid)...)
}

func (r *ring) ack(status byte, extra ...byte) func() {
	return r.d.sendLocked(nil, [][]byte{append([]byte{ntAck, status}, extra...)})
}

func be64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }

func (r *ring) command(v []byte) (string, func()) {
	n := len(v)
	switch v[0] {
	case 0x03: // CMD_STOP_SYNC: no answer
		r.x.stop()
		return "storage stop", nil
	case 0x10: // CMD_RING_INFO
		if r.d.mtu < infoMinMTU {
			// shortcut: the firmware waits up to 5 s for the MTU before NOT_READY; answering at once ends the same way sooner
			return "storage info (MTU too small)", r.ack(stNotReady)
		}
		b := []byte{ntInfo}
		b = binary.BigEndian.AppendUint64(b, r.readSeq)
		b = binary.BigEndian.AppendUint64(b, r.writeSeq)
		b = binary.BigEndian.AppendUint32(b, r.capacity)
		b = binary.BigEndian.AppendUint64(b, r.dropd)
		b = binary.BigEndian.AppendUint16(b, recordBytes)
		b = append(b, custodyCaps, custodyVer)
		b = binary.BigEndian.AppendUint64(b, r.id)
		return "storage info", r.d.sendLocked(nil, [][]byte{b})
	case 0x13: // CMD_RING_CLEAR
		r.x.stop()
		var id [8]byte
		_, _ = rand.Read(id[:])
		r.id, r.readSeq, r.writeSeq, r.dropd, r.recs = binary.BigEndian.Uint64(id[:])|1, 0, 0, 0, nil
		return "storage clear", r.ack(stOK)
	case 0x11: // CMD_RING_READ start_seq BE64 [count BE32]
		if n != 9 && n != 13 {
			return "storage read (bad length)", r.ack(stInvalid)
		}
		if r.x.busy {
			return "storage read (busy)", r.ack(stNotReady)
		}
		start, want := be64(v[1:]), uint64(0)
		if n == 13 {
			want = uint64(binary.BigEndian.Uint32(v[9:]))
		}
		if start < r.readSeq || start > r.writeSeq {
			return fmt.Sprintf("storage read %d (out of range)", start), r.ack(stOutOfRng)
		}
		avail := r.writeSeq - start
		if want == 0 || want > avail {
			want = avail
		}
		return fmt.Sprintf("storage read %d+%d", start, want), r.d.sendLocked(&r.x, r.readNotes(start, want))
	case 0x12, 0x15: // CMD_RING_ADVANCE seq BE64 / CMD_RING_ADVANCE_ID ring_id BE64 seq BE64
		check := v[0] == 0x15
		if !check && n != 9 || check && n != 17 {
			return "storage advance (bad length)", r.ack(stInvalid)
		}
		seq, reqID := be64(v[1:]), uint64(0)
		if check {
			reqID, seq = seq, be64(v[9:])
		}
		// omi_advance_eval (custody.c)
		switch {
		case check && reqID != r.id:
			return fmt.Sprintf("storage advance %d (ring id mismatch)", seq), r.ack(stIDMismatch)
		case seq > r.writeSeq:
			return fmt.Sprintf("storage advance %d (out of range)", seq), r.ack(stOutOfRng)
		case seq > r.readSeq:
			r.readSeq = seq
			for len(r.recs) > 0 && r.recs[0].seq < seq {
				r.recs = r.recs[1:]
			}
		}
		return fmt.Sprintf("storage advance %d", seq), r.ack(stOK)
	case 0x14: // CMD_CUSTODY_ENABLE version caps
		if n != 3 || v[1] != custodyVer {
			return "storage custody (invalid)", r.ack(stInvalid)
		}
		// gap: live persistence (custody LIVE_PERSIST, LIVE_MARK 0x06) is not modelled, so the simulator never grants it; the firmware grants every supported cap asked for.
		r.caps = v[2] & custodyCaps &^ capLive
		return fmt.Sprintf("storage custody caps %#x", r.caps), r.ack(stOK, r.caps)
	}
	return fmt.Sprintf("storage %#x (unknown)", v[0]), r.ack(stInvalid)
}

// readNotes is write_to_gatt: READ_BEGIN, then records read 36 at a time,
// each batch sent as DATA notifications of (MTU-3)-1 bytes, then DONE.
func (r *ring) readNotes(start, count uint64) [][]byte {
	begin := binary.BigEndian.AppendUint64([]byte{ntReadBegin}, start)
	notes := [][]byte{binary.BigEndian.AppendUint32(begin, uint32(count))}
	chunk := r.d.mtu - attHeader - 1 // get_ble_data_chunk_size
	if r.d.mtu <= attHeader+1 {
		chunk = 20 - 1
	}
	// shortcut: the chunk size is fixed at the MTU when the read starts; the firmware rereads it per batch
	var batch []byte
	sent := uint64(0)
	for _, rc := range r.recs {
		if rc.seq < start || sent == count {
			continue
		}
		batch = append(batch, rc.data...)
		sent++
		if sent%chunkRecords == 0 || sent == count {
			for off := 0; off < len(batch); off += chunk {
				notes = append(notes, append([]byte{ntData}, batch[off:min(off+chunk, len(batch))]...))
			}
			batch = nil
		}
	}
	return append(notes, binary.BigEndian.AppendUint64([]byte{ntDone, stOK}, start+sent))
}

func (r *ring) describe() any {
	type out struct {
		Seq    uint64 `json:"seq"`
		Time   uint32 `json:"timestamp"`
		Uptime bool   `json:"uptime"`
		Frames int    `json:"frames"`
		Bytes  []byte `json:"bytes"`
	}
	var recs []out
	for _, rc := range r.recs {
		ts := binary.BigEndian.Uint32(rc.data)
		recs = append(recs, out{rc.seq, ts &^ tsUptimeFlag, ts&tsUptimeFlag != 0, len(Unpack(rc.data[4:])), rc.data})
	}
	return map[string]any{"protocol": "ring", "ring_id": r.id, "read_seq": r.readSeq, "write_seq": r.writeSeq,
		"capacity": r.capacity, "records": recs}
}

// Unpack is the app's parser for a packed block (ring_protocol.dart
// parseAudioPayload): zero bytes are padding; a frame that would reach the
// block's end is not read. 0xFF entries are legacy timestamp markers.
func Unpack(block []byte) [][]byte {
	var frames [][]byte
	for off := 0; off < len(block)-1; {
		size := int(block[off])
		switch {
		case size == 0:
			off++
			continue
		case size == 0xFF && off+5 <= len(block):
			off += 5
			continue
		case off+1+size >= len(block):
			return frames
		}
		frames = append(frames, block[off+1:off+1+size])
		off += 1 + size
	}
	return frames
}

// ---- multi-file, firmware 3.0.17-3.0.19 (LittleFS) ----

// gap: the 3.0.17-3.0.19 and pre-3.0.17 firmware sources are not in the clone; their wire shapes follow Omi's app parsers (storage_sync.dart, sdcard_wal_sync.dart, omi_connection.dart), their on-device layout reuses the ring's 440-byte packed blocks, and their error status bytes are taken from what those parsers treat as failure.

type file struct {
	ts   uint32
	recs [][]byte // recordBytes each
}

type files struct {
	d    *Device
	list []*file
	open *file
	pack packer
	x    xfer
}

func (f *files) name() string { return "multi-file" }

func (f *files) put(block []byte) {
	if block != nil {
		f.open.recs = append(f.open.recs, f.d.record(block))
	}
}

func (f *files) frame(fr []byte) {
	if f.open == nil {
		f.open = &file{ts: f.d.deviceTimeLocked()}
		f.list = append(f.list, f.open)
	}
	f.put(f.pack.add(entry(fr)))
}

func (f *files) flush() {
	if f.open != nil {
		f.put(f.pack.take())
	}
}

func (f *files) connected()    { f.flush(); f.open = nil } // the next offline stretch starts a new file
func (f *files) disconnected() { f.x.stop() }

func (fl *file) size() uint32 { return uint32(len(fl.recs) * recordAudio) } // the app counts 440 per block

func (f *files) status() []byte {
	total := uint32(0)
	for _, fl := range f.list {
		total += fl.size()
	}
	return append(u32le(total), u32le(uint32(len(f.list)))...)
}

func (f *files) one(b byte) func() { return f.d.sendLocked(nil, [][]byte{{b}}) }

func (f *files) command(v []byte) (string, func()) {
	switch v[0] {
	case 0x03:
		f.x.stop()
		return "storage stop", nil
	case 0x10: // CMD_LIST_FILES: [count][ts BE32][size BE32]...
		// shortcut: lists only what one notification carries; upgrade past that many files
		room := (f.d.mtu - attHeader - 1) / 8
		b := []byte{byte(min(len(f.list), room))}
		for _, fl := range f.list[:min(len(f.list), room)] {
			b = binary.BigEndian.AppendUint32(b, fl.ts)
			b = binary.BigEndian.AppendUint32(b, fl.size())
		}
		return "storage list files", f.d.sendLocked(nil, [][]byte{b})
	case 0x11: // CMD_READ_FILE index offset BE32: [0], blocks [ts BE32][440], then [100]
		if len(v) < 6 || int(v[1]) >= len(f.list) {
			return "storage read file (bad)", f.one(3)
		}
		fl := f.list[v[1]]
		if len(fl.recs) == 0 {
			return fmt.Sprintf("storage read file %d (empty)", v[1]), f.one(4)
		}
		if f.d.mtu-attHeader < recordBytes {
			return fmt.Sprintf("storage read file %d (MTU too small)", v[1]), f.one(3)
		}
		notes := [][]byte{{0}}
		for _, rc := range fl.recs[min(int(binary.BigEndian.Uint32(v[2:]))/recordAudio, len(fl.recs)):] {
			notes = append(notes, rc)
		}
		return fmt.Sprintf("storage read file %d", v[1]), f.d.sendLocked(&f.x, append(notes, []byte{100}))
	case 0x12: // CMD_DELETE_FILE index: [0] on success
		if len(v) < 2 || int(v[1]) >= len(f.list) {
			return "storage delete file (bad)", f.one(1)
		}
		if f.list[v[1]] == f.open {
			f.open = nil
		}
		f.list = append(f.list[:v[1]], f.list[v[1]+1:]...)
		return fmt.Sprintf("storage delete file %d", v[1]), f.one(0)
	}
	return fmt.Sprintf("storage %#x (unknown)", v[0]), f.one(stInvalid)
}

func (f *files) describe() any {
	type out struct {
		Timestamp uint32   `json:"timestamp"`
		Size      uint32   `json:"size"`
		Records   [][]byte `json:"records"`
	}
	var list []out
	for _, fl := range f.list {
		list = append(list, out{fl.ts, fl.size(), fl.recs})
	}
	return map[string]any{"protocol": "multi-file", "files": list}
}

// ---- one length-listed stream, firmware before 3.0.17 ----

type legacy struct {
	d         *Device
	markers   bool // 3.0.16: a 0xFF + LE32 epoch marker opens each recording
	data      []byte
	startTs   uint32
	recording bool
	pack      packer
	x         xfer
}

func (l *legacy) name() string { return "legacy" }

func (l *legacy) put(block []byte) { l.data = append(l.data, block...) }

func (l *legacy) frame(f []byte) {
	if !l.recording {
		l.recording = true
		ts := l.d.deviceTimeLocked()
		if l.startTs == 0 {
			l.startTs = ts
		}
		if l.markers && ts&tsUptimeFlag == 0 {
			l.put(l.pack.add(append([]byte{0xFF}, u32le(ts)...)))
		}
	}
	l.put(l.pack.add(entry(f)))
}

func (l *legacy) flush()        { l.put(l.pack.take()) }
func (l *legacy) connected()    { l.flush(); l.recording = false }
func (l *legacy) disconnected() { l.x.stop() }

// status is the length list: [total bytes, read offset, recording start (3.0.16)], LE32 each.
func (l *legacy) status() []byte {
	b := append(u32le(uint32(len(l.data))), u32le(0)...) // shortcut: the read offset stays 0; the app reads from it and clears after
	if l.markers {
		b = append(b, u32le(l.startTs)...)
	}
	return b
}

func (l *legacy) command(v []byte) (string, func()) {
	switch v[0] {
	case 0x03:
		l.x.stop()
		return "storage stop", nil
	case 0x00: // read: [0x00][file][offset BE32] -> 440-byte blocks, then [100]
		if len(l.data) == 0 {
			return "storage read (empty)", l.d.sendLocked(nil, [][]byte{{4}})
		}
		if len(v) < 6 || l.d.mtu-attHeader < recordAudio {
			return "storage read (bad)", l.d.sendLocked(nil, [][]byte{{3}})
		}
		var notes [][]byte
		for off := int(binary.BigEndian.Uint32(v[2:])) / recordAudio * recordAudio; off < len(l.data); off += recordAudio {
			notes = append(notes, l.data[off:off+recordAudio])
		}
		return "storage read", l.d.sendLocked(&l.x, append(notes, []byte{100}))
	case 0x01: // clear the stream
		l.x.stop()
		l.data, l.startTs = nil, 0
		return "storage delete", nil
	}
	return fmt.Sprintf("storage %#x (unknown)", v[0]), l.d.sendLocked(nil, [][]byte{{stInvalid}})
}

func (l *legacy) describe() any {
	return map[string]any{"protocol": "legacy", "bytes": len(l.data), "start": l.startTs, "data": l.data}
}
