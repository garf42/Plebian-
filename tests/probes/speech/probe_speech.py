"""Probe: offline speech pipeline at Plebian's dimensions on this host.
Two synthetic voices (espeak-ng) hold a short exchange with pauses; diarization
(pyannote-3.0 segmentation + speaker embeddings) splits speakers; Parakeet TDT
int8 transcribes each turn. Prints word error rate and real-time factors.
Run twice; the transcript and labels must match."""
import json, time, sys, numpy as np, sherpa_onnx as so
M = sys.argv[1]
LINES = [("a", "the twenty foot culvert pipe is three forty and the thirty foot is four ninety"),
         ("b", "have them dump the gravel down by the culvert not at the top of the drive"),
         ("a", "so about that gravel delivery I can bring it wednesday"),
         ("b", "fifteen yards should do it")]
import subprocess, io, wave
VOICE = {"a": ["-v", "en-us+f3", "-p", "70"], "b": ["-v", "en-us+m3", "-p", "30"]}
def say(who, text):
    wav = subprocess.run(["espeak-ng", *VOICE[who], "-s", "150", "--stdout", text], capture_output=True, check=True).stdout
    w = wave.open(io.BytesIO(wav)); fr = w.getframerate(); x = np.frombuffer(w.readframes(w.getnframes()), np.int16).astype(np.float32) / 32768
    idx = np.linspace(0, len(x) - 1, int(len(x) * 16000 / fr)); return np.interp(idx, np.arange(len(x)), x).astype(np.float32)
sr = 16000; chunks = []; truth = []
for who, text in LINES:
    chunks += [say(who, text), np.zeros(int(0.8 * sr), np.float32)]; truth.append((who, text))
audio = np.concatenate(chunks); dur = len(audio) / sr
dz = so.OfflineSpeakerDiarization(so.OfflineSpeakerDiarizationConfig(
    segmentation=so.OfflineSpeakerSegmentationModelConfig(pyannote=so.OfflineSpeakerSegmentationPyannoteModelConfig(model=f"{M}/sherpa-onnx-pyannote-segmentation-3-0/model.onnx")),
    embedding=so.SpeakerEmbeddingExtractorConfig(model=f"{M}/nemo_en_titanet_small.onnx"),
    clustering=so.FastClusteringConfig(num_clusters=-1, threshold=0.7)))
t0 = time.time(); segs = dz.process(audio).sort_by_start_time(); t_dz = time.time() - t0
P = f"{M}/sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8"
asr = so.OfflineRecognizer.from_transducer(encoder=f"{P}/encoder.int8.onnx", decoder=f"{P}/decoder.int8.onnx", joiner=f"{P}/joiner.int8.onnx",
                                           tokens=f"{P}/tokens.txt", model_type="nemo_transducer", num_threads=2)
t0 = time.time(); out = []
for g in segs:
    st = asr.create_stream(); st.accept_waveform(sr, audio[int(g.start * sr):int(g.end * sr)]); asr.decode_stream(st)
    out.append((g.speaker, round(g.start, 1), st.result.text.strip()))
t_asr = time.time() - t0
import re
norm = lambda s: re.sub(r"[^a-z0-9 ]", "", s.lower()).split()
ref = sum((norm(t) for _, t in truth), []); hyp = sum((norm(t) for _, _, t in out), [])
import difflib
sm = difflib.SequenceMatcher(a=ref, b=hyp); errs = sum(max(i2 - i1, j2 - j1) for tag, i1, i2, j1, j2 in sm.get_opcodes() if tag != "equal")
print(json.dumps({"audio_s": round(dur, 1), "speakers_found": len({s for s, _, _ in out}), "turns": out,
                  "wer": round(errs / len(ref), 3), "rtf_asr": round(t_asr / dur, 3), "rtf_diarize": round(t_dz / dur, 3)}))
