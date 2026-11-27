package ops

import (
	"archive/tar"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SpeechFile is one model download, pinned by checksum.
type SpeechFile struct {
	Name, URL, SHA256 string
	Tar               bool
}

const sherpaReleases = "https://github.com/k2-fsa/sherpa-onnx/releases/download/"

// SpeechFiles are the speech models (D6), from sherpa-onnx's releases.
var SpeechFiles = []SpeechFile{
	{"sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8.tar.bz2", sherpaReleases + "asr-models/sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8.tar.bz2", "6fc4426b54aa549a0019b8a75b8dea59164e71790a37ac56a045ad57f915f714", true},
	{"sherpa-onnx-pyannote-segmentation-3-0.tar.bz2", sherpaReleases + "speaker-segmentation-models/sherpa-onnx-pyannote-segmentation-3-0.tar.bz2", "24615ee884c897d9d2ba09bb4d30da6bb1b15e685065962db5b02e76e4996488", true},
	{"nemo_en_titanet_small.onnx", sherpaReleases + "speaker-recongition-models/nemo_en_titanet_small.onnx", "ad4a1802485d8b34c722d2a9d04249662f2ece5d28a7a039063ca22f515a789e", false},
	{"silero_vad.onnx", sherpaReleases + "asr-models/silero_vad.onnx", "9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6", false},
}

// SpeechLicenses is recorded beside the models.
const SpeechLicenses = `Speech models, downloaded at install from sherpa-onnx releases (github.com/k2-fsa/sherpa-onnx):
- Parakeet-TDT 0.6B v2 (NVIDIA), int8 export by sherpa-onnx: CC-BY-4.0. https://huggingface.co/nvidia/parakeet-tdt-0.6b-v2
- pyannote segmentation 3.0 (pyannote.audio): MIT. https://huggingface.co/pyannote/segmentation-3.0
- TitaNet-small speaker embeddings (NVIDIA NeMo): CC-BY-4.0. https://catalog.ngc.nvidia.com/orgs/nvidia/teams/nemo/models/titanet_small
- Silero VAD (Silero Team): MIT. https://github.com/snakers4/silero-vad
`

func fetch(f SpeechFile, from, dst string) error {
	var r io.ReadCloser
	if from != "" {
		fh, err := os.Open(filepath.Join(from, f.Name))
		if err != nil {
			return err
		}
		r = fh
	} else {
		c := http.Client{Timeout: 30 * time.Minute}
		resp, err := c.Get(f.URL)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return fmt.Errorf("%s: %s", f.URL, resp.Status)
		}
		r = resp.Body
	}
	defer r.Close()
	tmp, err := os.CreateTemp(dst, ".dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != f.SHA256 {
		return fmt.Errorf("%s: checksum %s, pinned %s", f.Name, got, f.SHA256)
	}
	if !f.Tar {
		return os.Rename(tmp.Name(), filepath.Join(dst, f.Name))
	}
	in, err := os.Open(tmp.Name())
	if err != nil {
		return err
	}
	defer in.Close()
	tr := tar.NewReader(bzip2.NewReader(in))
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dst, filepath.Clean("/"+hd.Name))
		if !strings.HasPrefix(p, filepath.Clean(dst)+"/") {
			return fmt.Errorf("%s: entry %q leaves the speech directory", f.Name, hd.Name)
		}
		switch hd.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(p), 0o755)
			out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

// FetchSpeech puts the speech models in dir, from the releases or from a directory
// holding the same files (checked against the same pins).
func FetchSpeech(dir, from string) error {
	for _, f := range SpeechFiles {
		if err := fetch(f, from, dir); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "LICENSES.txt"), []byte(SpeechLicenses), 0o644)
}
