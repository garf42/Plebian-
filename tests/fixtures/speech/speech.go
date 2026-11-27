// Package speech makes synthetic two-voice speech: espeak-ng speaks each line
// in one of the two voices of tests/probes/speech/probe_speech.py, the result
// is resampled to 16 kHz mono PCM the way the probe resamples it, and Encode
// turns it into Opus frames as the Omi encodes them.
package speech

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os/exec"
)

// Line is one spoken line followed by a pause.
type Line struct {
	Speaker string `json:"speaker"` // "a" or "b"
	Text    string `json:"text"`
	PauseMs int    `json:"pause_ms"`
}

// Segment is where a line landed in the audio.
type Segment struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
}

// Voices are the probe's two espeak-ng voices; the probe's -s 150 rate applies to both.
var Voices = map[string][]string{
	"a": {"-v", "en-us+f3", "-p", "70"},
	"b": {"-v", "en-us+m3", "-p", "30"},
}

// say runs espeak-ng and returns its PCM16 samples and their rate.
func say(speaker, text string) ([]int16, int, error) {
	v, ok := Voices[speaker]
	if !ok {
		return nil, 0, fmt.Errorf("speaker %q: only a and b have voices", speaker)
	}
	args := append(append([]string{}, v...), "-s", "150", "--stdout", text)
	wav, err := exec.Command("espeak-ng", args...).Output()
	if err != nil {
		return nil, 0, fmt.Errorf("espeak-ng: %w", err)
	}
	return parseWAV(wav)
}

// parseWAV reads mono PCM16 WAV. espeak-ng writing to a pipe cannot seek back
// to fill in sizes, so the data chunk runs to the end of the stream.
func parseWAV(b []byte) ([]int16, int, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV stream")
	}
	rate := 0
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:]))
		body := b[off+8:]
		switch id {
		case "fmt ":
			if len(body) < 16 || binary.LittleEndian.Uint16(body[0:]) != 1 || binary.LittleEndian.Uint16(body[2:]) != 1 ||
				binary.LittleEndian.Uint16(body[14:]) != 16 {
				return nil, 0, errors.New("WAV is not mono PCM16")
			}
			rate = int(binary.LittleEndian.Uint32(body[4:]))
		case "data":
			if rate == 0 {
				return nil, 0, errors.New("WAV data before fmt")
			}
			if size > len(body) {
				size = len(body)
			}
			pcm := make([]int16, size/2)
			_ = binary.Read(bytes.NewReader(body[:size/2*2]), binary.LittleEndian, pcm)
			return pcm, rate, nil
		}
		off += 8 + size + size%2
	}
	return nil, 0, errors.New("WAV has no data chunk")
}

// resample is the probe's np.interp over np.linspace(0, n-1, int(n*to/from)).
func resample(x []int16, from, to int) []int16 {
	n := len(x) * to / from
	out := make([]int16, n)
	for i := range out {
		pos := 0.0
		if n > 1 {
			pos = float64(i) * float64(len(x)-1) / float64(n-1)
		}
		j := int(pos)
		v := float64(x[j])
		if j+1 < len(x) {
			v += (pos - float64(j)) * float64(x[j+1]-x[j])
		}
		out[i] = int16(math.Round(v))
	}
	return out
}

// Synthesize speaks lines in order into 16 kHz mono PCM.
func Synthesize(lines []Line) ([]int16, []Segment, error) {
	var pcm []int16
	var segs []Segment
	ms := func(n int) int64 { return int64(n) * 1000 / SampleRate }
	for _, l := range lines {
		if l.PauseMs < 0 {
			return nil, nil, fmt.Errorf("pause_ms %d is negative", l.PauseMs)
		}
		x, rate, err := say(l.Speaker, l.Text)
		if err != nil {
			return nil, nil, err
		}
		start := len(pcm)
		pcm = append(pcm, resample(x, rate, SampleRate)...)
		segs = append(segs, Segment{l.Speaker, l.Text, ms(start), ms(len(pcm))})
		pcm = append(pcm, make([]int16, l.PauseMs*SampleRate/1000)...)
	}
	return pcm, segs, nil
}

// WAV wraps 16 kHz mono PCM16 in a WAV container.
func WAV(pcm []int16) []byte {
	var b bytes.Buffer
	n := uint32(len(pcm) * 2)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, 36+n)
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []uint32{16})
	_ = binary.Write(&b, binary.LittleEndian, []uint16{1, 1})
	_ = binary.Write(&b, binary.LittleEndian, []uint32{SampleRate, SampleRate * 2})
	_ = binary.Write(&b, binary.LittleEndian, []uint16{2, 16})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, n)
	_ = binary.Write(&b, binary.LittleEndian, pcm)
	return b.Bytes()
}

// Frames speaks lines and encodes them for codec c.
func Frames(lines []Line, c Codec) ([][]byte, []Segment, error) {
	pcm, segs, err := Synthesize(lines)
	if err != nil {
		return nil, nil, err
	}
	frames, err := Encode(pcm, c)
	return frames, segs, err
}
