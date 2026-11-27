package audio

import (
	"bytes"
	"encoding/binary"
)

// Ogg Opus (RFC 7845): the stretch's frames, byte for byte, in a container players open.

var crcTable = func() [256]uint32 {
	var t [256]uint32
	for i := range t {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

func oggCRC(b []byte) uint32 {
	var c uint32
	for _, x := range b {
		c = c<<8 ^ crcTable[byte(c>>24)^x]
	}
	return c
}

func page(w *bytes.Buffer, typ byte, granule int64, serial, seq uint32, packets [][]byte) {
	var seg []byte
	var data []byte
	for _, p := range packets {
		n := len(p)
		for n >= 255 {
			seg = append(seg, 255)
			n -= 255
		}
		seg = append(seg, byte(n))
		data = append(data, p...)
	}
	h := make([]byte, 27, 27+len(seg))
	copy(h, "OggS")
	h[5] = typ
	binary.LittleEndian.PutUint64(h[6:], uint64(granule))
	binary.LittleEndian.PutUint32(h[14:], serial)
	binary.LittleEndian.PutUint32(h[18:], seq)
	h[26] = byte(len(seg))
	h = append(h, seg...)
	full := append(h, data...)
	binary.LittleEndian.PutUint32(full[22:], oggCRC(full))
	w.Write(full)
}

// PreSkip is the decoder delay at 48 kHz: libopus's lookahead in its restricted low-delay
// mode (the Omi's) is Fs/400, 2.5 ms, which is 120 samples at 48 kHz.
const PreSkip = 120

// Ogg wraps Opus frames of frameSamples (at 16 kHz) as an Ogg Opus file. The serial is
// fixed by the caller so the same frames always give the same bytes.
func Ogg(frames [][]byte, frameSamples int, serial uint32) []byte {
	var w bytes.Buffer
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1 // version
	head[9] = 1 // channels
	binary.LittleEndian.PutUint16(head[10:], PreSkip)
	binary.LittleEndian.PutUint32(head[12:], 16000)
	page(&w, 0x02, 0, serial, 0, [][]byte{head})
	vendor := "plebian"
	tags := make([]byte, 8+4+len(vendor)+4)
	copy(tags, "OpusTags")
	binary.LittleEndian.PutUint32(tags[8:], uint32(len(vendor)))
	copy(tags[12:], vendor)
	page(&w, 0, 0, serial, 1, [][]byte{tags})
	per := int64(frameSamples) * 3 // granule counts 48 kHz samples
	seq := uint32(2)
	var granule int64
	for i := 0; i < len(frames); {
		var pk [][]byte
		segs := 0
		for i < len(frames) {
			n := len(frames[i])/255 + 1
			if segs+n > 255 {
				break
			}
			pk = append(pk, frames[i])
			segs += n
			granule += per
			i++
		}
		typ := byte(0)
		if i == len(frames) {
			typ = 0x04
		}
		page(&w, typ, granule, serial, seq, pk)
		seq++
	}
	if len(frames) == 0 {
		page(&w, 0x04, 0, serial, seq, nil)
	}
	return w.Bytes()
}
