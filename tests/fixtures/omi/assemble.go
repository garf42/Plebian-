package omi

import "encoding/binary"

// Frame is one codec frame put back together from audio notifications; Packet
// is the packet number of its first fragment.
type Frame struct {
	Packet uint16
	Data   []byte
}

// Assembler rebuilds frames from audio notifications ([packet LE16][index]
// [payload]) for tests that drive the device without the app: index 0 opens a
// frame, each next index on the next packet number (wrapping) extends it, and
// a gap drops the part-built frame.
type Assembler struct {
	frames []Frame
	cur    *Frame
	last   uint16
	idx    byte
}

// Add takes one notification.
func (a *Assembler) Add(n []byte) {
	if len(n) < fragmentHeader {
		return
	}
	num, idx, p := binary.LittleEndian.Uint16(n), n[2], n[fragmentHeader:]
	switch {
	case idx == 0:
		a.close()
		a.cur = &Frame{Packet: num, Data: append([]byte{}, p...)}
	case a.cur != nil && num == a.last+1 && idx == a.idx+1:
		a.cur.Data = append(a.cur.Data, p...)
	default:
		a.cur = nil
		return
	}
	a.last, a.idx = num, idx
}

func (a *Assembler) close() {
	if a.cur != nil {
		a.frames = append(a.frames, *a.cur)
		a.cur = nil
	}
}

// Frames returns every frame so far, closing the one being built.
func (a *Assembler) Frames() []Frame {
	a.close()
	return a.frames
}
