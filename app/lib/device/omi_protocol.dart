// Omi BLE protocol: UUIDs, frame reassembly, buttons, codec ids and the three
// offline-storage layouts.
//
// Ported from Omi (https://github.com/BasedHardware/omi, commit f2f8bbc4),
// MIT License, Copyright (c) 2024 Based Hardware Contributors:
//   app/lib/services/devices/models.dart            (UUIDs)
//   app/lib/utils/audio/wav_bytes.dart              (storeFramePacket)
//   app/lib/services/devices/connectors/omi_connection.dart (storage list,
//     multi-file list, haptic, time sync, codec ids)
//   app/lib/services/devices/ring_protocol.dart     (ring-buffer storage)
//   omi/firmware/omi/src/lib/core/button.c          (button codes)
// Changes: the packet counter compares modulo 2^16 (Omi's `index !=
// lastPacketIndex + 1` drops the frame after 65535 wraps to 0); frames carry
// the packet number of their first fragment and their arrival time; no
// decoding, no logging, no analytics.
import 'dart:typed_data';

const omiService = '19b10000-e8f2-537e-4f6c-d104768a1214';
const audioData = '19b10001-e8f2-537e-4f6c-d104768a1214';
const audioCodec = '19b10002-e8f2-537e-4f6c-d104768a1214';
const buttonService = '23ba7924-0000-1000-7450-346eac492e92';
const buttonTrigger = '23ba7925-0000-1000-7450-346eac492e92';
const storageService = '30295780-4301-eabd-2904-2849adfeae43';
const storageData = '30295781-4301-eabd-2904-2849adfeae43';
const storageControl = '30295782-4301-eabd-2904-2849adfeae43';
const timeService = '19b10030-e8f2-537e-4f6c-d104768a1214';
const timeWrite = '19b10031-e8f2-537e-4f6c-d104768a1214';
const timeRead = '19b10032-e8f2-537e-4f6c-d104768a1214';
const hapticService = 'cab1ab95-2ea5-4f4d-bb56-874b72cfc984';
const hapticWrite = 'cab1ab96-2ea5-4f4d-bb56-874b72cfc984';
const batteryService = '0000180f-0000-1000-8000-00805f9b34fb';
const batteryLevel = '00002a19-0000-1000-8000-00805f9b34fb';
const infoService = '0000180a-0000-1000-8000-00805f9b34fb';
const firmwareRevision = '00002a26-0000-1000-8000-00805f9b34fb';

/// The largest ATT MTU (Bluetooth Core spec; firmware MAX_POSSIBLE_MTU 517).
const maxMtu = 517;

/// Codec ids on the codec characteristic (omi_connection.dart).
const codecPcm16 = 0, codecPcm8 = 1, codecMulaw16 = 10, codecMulaw8 = 11;
const codecOpus = 20, codecOpusFS320 = 21;

/// Button codes (firmware button.c).
const buttonSingle = 1, buttonDouble = 2, buttonLong = 3, buttonPress = 4, buttonRelease = 5;

/// Haptic levels (firmware haptic.c): 1 = 100 ms, 2 = 300 ms, 3 = 500 ms.
const hapticShort = 1;

/// One codec frame, reassembled from its BLE fragments.
class AudioFrame {
  AudioFrame(this.packet, this.arrivalMs, this.bytes);
  final int packet; // u16 packet number of the frame's first fragment
  final int arrivalMs; // unix ms when the first fragment arrived
  final Uint8List bytes;
}

/// Reassembles notifications `[packet u16 LE][index u8][payload]` into frames.
/// A frame is complete when the next frame's index-0 fragment arrives; a gap in
/// packet numbers or indices drops the frame in flight, as Omi's app does.
class FrameAssembler {
  FrameAssembler(this.onFrame);
  final void Function(AudioFrame) onFrame;
  int _last = -1, _lastIndex = -1, _startPacket = 0, _startMs = 0;
  BytesBuilder _pending = BytesBuilder();

  void add(List<int> value, int arrivalMs) {
    if (value.length < 3) return;
    final packet = value[0] | (value[1] << 8);
    final index = value[2];
    final content = value.sublist(3);
    if (_last == -1) {
      if (index != 0) return;
      _begin(packet, arrivalMs, content);
      return;
    }
    if (packet != (_last + 1) & 0xFFFF || (index != 0 && index != _lastIndex + 1)) {
      _last = -1;
      _pending = BytesBuilder();
      if (index == 0) _begin(packet, arrivalMs, content);
      return;
    }
    if (index == 0) {
      onFrame(AudioFrame(_startPacket, _startMs, _pending.takeBytes()));
      _begin(packet, arrivalMs, content);
      return;
    }
    _pending.add(content);
    _last = packet;
    _lastIndex = index;
  }

  void _begin(int packet, int ms, List<int> content) {
    _pending = BytesBuilder()..add(content);
    _last = packet;
    _lastIndex = 0;
    _startPacket = packet;
    _startMs = ms;
  }
}

/// Button notifications carry `int[2]` little-endian; the first is the code.
int parseButton(List<int> v) {
  if (v.length >= 4) return ByteData.sublistView(Uint8List.fromList(v)).getInt32(0, Endian.little);
  return v.isEmpty ? 0 : v[0];
}

int _u32le(List<int> v, int i) => (v[i] | (v[i + 1] << 8) | (v[i + 2] << 16) | (v[i + 3] << 24)) & 0xFFFFFFFF;
int _u32be(List<int> v, int i) => ((v[i] << 24) | (v[i + 1] << 16) | (v[i + 2] << 8) | v[i + 3]) & 0xFFFFFFFF;

Uint8List u32le(int x) => (ByteData(4)..setUint32(0, x, Endian.little)).buffer.asUint8List();
int readU32le(List<int> v) => v.length < 4 ? 0 : _u32le(v, 0);
int readU32be(List<int> v) => v.length < 4 ? 0 : _u32be(v, 0);

/// A stored 440-byte block packs `[size u8][frame]...` with zero padding
/// (ring_protocol.dart parseAudioPayload). At the boundary the firmware writes
/// a trailing size byte without its frame; the bytes after it are stale.
List<List<int>> parseBlock(List<int> a) {
  final frames = <List<int>>[];
  var o = 0;
  while (o < a.length - 1) {
    final n = a[o];
    if (n == 0) {
      o++;
      continue;
    }
    if (o + 1 + n >= a.length) break;
    frames.add(a.sublist(o + 1, o + 1 + n));
    o += n + 1;
  }
  return frames;
}

/// Legacy firmware's per-packet layout `[index u16][internal u8][size u8][frame]`
/// is 83 bytes (sdcard_wal_sync.dart parseLegacyPacketFrame).
const legacyPacket = 83;

// --- Legacy storage: a list of u32 LE on the control characteristic -------

/// `[totalBytes, offset, ...]` on legacy firmware; `[totalBytes, fileCount]` on
/// multi-file firmware.
List<int> parseStorageList(List<int> v) =>
    [for (var i = 0; i + 4 <= v.length; i += 4) _u32le(v, i).toSigned(32)];

/// `[command, file, offset u32 BE]`: command 0 reads, 1 clears (legacy);
/// 0x11 reads one file (multi-file).
Uint8List storageCommand(int command, int file, int offset) =>
    Uint8List.fromList([command & 0xFF, file & 0xFF, (offset >> 24) & 0xFF, (offset >> 16) & 0xFF, (offset >> 8) & 0xFF, offset & 0xFF]);

/// Field[1] above this is a legacy byte offset, not a file count (Omi's own
/// heuristic in omi_connection.dart performGetStorageFileStats).
const legacyOffsetFloor = 1000;

// --- Multi-file storage: 0x10 list, 0x11 read, 0x12 delete -----------------

class StoredFile {
  StoredFile(this.index, this.timestamp, this.size);
  final int index; // file index, or the ring start sequence
  final int timestamp; // device unix seconds
  final int size; // bytes, or ring records
}

const cmdListFiles = 0x10, cmdReadFile = 0x11, cmdDeleteFile = 0x12, cmdStop = 0x03;

/// `[count][ts u32 BE][size u32 BE]...`; null when it is not a list.
List<StoredFile>? parseFileList(List<int> v) {
  if (v.isEmpty) return null;
  final count = v[0];
  if (count == 0 && v.length == 1) return [];
  if (count == 0 || count > 128 || v.length < 1 + count * 8) return null;
  return [for (var i = 0; i < count; i++) StoredFile(i, _u32be(v, 1 + i * 8), _u32be(v, 5 + i * 8))];
}

/// Single-byte statuses on the storage data characteristic.
const statusOk = 0, statusBadFile = 3, statusEmpty = 4, statusEnd = 100;

// --- Ring-buffer storage (firmware 3.0.20+) --------------------------------

const ringRecord = 444; // [timestamp u32 BE][440-byte audio payload]
const notifyAck = 0x01, notifyInfo = 0x02, notifyData = 0x03, notifyDone = 0x04;
const cmdRingInfo = 0x10, cmdRingRead = 0x11, cmdRingAdvance = 0x12, cmdRingAdvanceId = 0x15;
const capRingId = 0x08;

/// Records per read (Omi ring_storage_sync.dart ringReadWindowRecords: about
/// 40 s of transfer and 1.2 MB in memory).
const ringReadWindow = 2700;

bool isRingFirmware(String? version) {
  if (version == null || version.isEmpty) return false;
  final p = version.split('.').map((s) => int.tryParse(s) ?? 0).toList();
  if (p.length < 3) return false;
  if (p[0] != 3) return p[0] > 3;
  if (p[1] > 0) return true;
  return p[2] >= 20;
}

class RingInfo {
  RingInfo(this.readSeq, this.writeSeq, this.caps, this.ringId);
  final int readSeq, writeSeq, caps;
  final int? ringId;
  int get unread => writeSeq - readSeq;
}

RingInfo? parseRingInfo(List<int> v) {
  if (v.length < 31 || v[0] != notifyInfo) return null;
  final b = ByteData.sublistView(Uint8List.fromList(v));
  return RingInfo(b.getUint64(1), b.getUint64(9), v.length >= 32 ? v[31] & 0x0F : 0,
      v.length >= 41 ? b.getUint64(33) : null);
}

/// NOTIFY_DONE `[0x04][status][next_seq u64 BE]` → (status, nextSeq).
(int, int)? parseRingDone(List<int> v) {
  if (v.length < 10 || v[0] != notifyDone) return null;
  return (v[1], ByteData.sublistView(Uint8List.fromList(v)).getUint64(2));
}

Uint8List ringRead(int startSeq, int count) => (ByteData(13)
      ..setUint8(0, cmdRingRead)
      ..setUint64(1, startSeq)
      ..setUint32(9, count))
    .buffer
    .asUint8List();

Uint8List ringAdvance(int nextSeq, int? ringId) => ringId == null
    ? (ByteData(9)
          ..setUint8(0, cmdRingAdvance)
          ..setUint64(1, nextSeq))
        .buffer
        .asUint8List()
    : (ByteData(17)
          ..setUint8(0, cmdRingAdvanceId)
          ..setUint64(1, ringId)
          ..setUint64(9, nextSeq))
        .buffer
        .asUint8List();

/// Bit 31 marks seconds since boot, not UTC (ring_protocol.dart).
int ringRecordTimestamp(List<int> r) {
  final raw = _u32be(r, 0);
  return raw & 0x80000000 != 0 ? 0 : raw;
}
