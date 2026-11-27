// The Omi above the transport: connect sequence, audio frames, buttons,
// battery, firmware revision, time sync, haptic and offline storage sync.
// The same code runs on the phone (BleTransport) and in tests (SimTransport,
// in-memory transports).
//
// Connect sequence, storage flows and timeouts ported from Omi
// (https://github.com/BasedHardware/omi, commit f2f8bbc4), MIT License,
// Copyright (c) 2024 Based Hardware Contributors:
//   app/lib/services/devices/connectors/omi_connection.dart
//   app/lib/services/wals/sdcard_wal_sync.dart   (legacy read/clear)
//   app/lib/services/wals/storage_sync.dart      (multi-file read/delete)
//   app/lib/services/wals/ring_storage_sync.dart (ring read/advance)
// Change: a stored read is deleted only with the server's sha256 of exactly
// those bytes (Plebian's rule); there is no upload to Omi's servers.
import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

import 'omi_protocol.dart';
import 'transport.dart';

/// Omi's own waits: 5 s for a command's answer (omi_connection.dart), 15 s of
/// silence ends a stalled read (sdcard_wal_sync.dart).
const _answerWait = Duration(seconds: 5);
const _stallWait = Duration(seconds: 15);

/// A recording run inside a stored read: the Opus frames as the Omi stored
/// them, each prefixed by its length (u16 LE) — the body /v1/audio/files
/// takes — and the device time (ms) of its first frame.
class StoredRun {
  StoredRun(this.deviceMs, this.body);
  final int deviceMs;
  final Uint8List body;
  late final String sha256Hex = sha256.convert(body).toString();
}

/// One stored stretch as read from the Omi, split into runs wherever the
/// device clock steps more than [ringGapBreakS] between blocks.
class StoredRead {
  StoredRead(this.file, this.layout, this.runs, this.next);
  final StoredFile file;
  final String layout; // legacy | files | ring
  final List<StoredRun> runs;
  final int next; // ring: the sequence after this read
}

/// A device-clock step between consecutive stored blocks above this is a real
/// recording gap (Omi ring_storage_sync.dart ringRecordGapBreakSeconds).
const ringGapBreakS = 60;

/// Frames per second by codec (Omi bt_device.dart getFramesPerSecond).
int framesPerSecond(int codec) => codec == codecOpusFS320 ? 50 : 100;

/// Packs blocks `(device seconds or 0 when unknown, frames)` into runs.
List<StoredRun> packRuns(List<(int, List<List<int>>)> blocks, {required int fallbackS, required int codec}) {
  final runs = <(int, BytesBuilder)>[];
  var last = 0;
  for (final (ts, frames) in blocks) {
    if (frames.isEmpty) continue;
    if (runs.isEmpty || (ts != 0 && last != 0 && (ts - last).abs() > ringGapBreakS)) {
      runs.add((ts != 0 ? ts : fallbackS, BytesBuilder()));
    }
    if (ts != 0) last = ts;
    for (final f in frames) {
      runs.last.$2
        ..add([f.length & 0xFF, f.length >> 8])
        ..add(f);
    }
  }
  return [for (final (s, b) in runs) StoredRun(s * 1000, b.takeBytes())];
}

class OmiDevice {
  OmiDevice(this.t, {int Function()? nowMs}) : nowMs = nowMs ?? (() => DateTime.now().millisecondsSinceEpoch);
  final DeviceTransport t;
  final int Function() nowMs;

  final _frames = StreamController<AudioFrame>.broadcast();
  final _buttons = StreamController<int>.broadcast();
  final _battery = StreamController<int>.broadcast();
  Stream<AudioFrame> get frames => _frames.stream;
  Stream<int> get buttons => _buttons.stream;
  Stream<int> get batteryLevels => _battery.stream;

  int codec = codecOpus;
  String firmware = '';
  int battery = -1;
  int clockOffsetMs = 0;
  late Stream<List<int>> _storage;
  final _subs = <StreamSubscription>[];

  Future<void> start() async {
    await t.connect();
    firmware = utf8.decode(await _tryRead(infoService, firmwareRevision), allowMalformed: true).replaceAll('\u0000', '').trim();
    final c = await _tryRead(omiService, audioCodec);
    if (c.isNotEmpty) codec = c[0];
    final b = await _tryRead(batteryService, batteryLevel);
    if (b.isNotEmpty) battery = b[0];
    final assembler = FrameAssembler(_frames.add);
    _subs
      ..add(t.notifications(audioData).listen((v) => assembler.add(v, nowMs())))
      ..add(t.notifications(buttonTrigger).listen((v) => _buttons.add(parseButton(v))))
      ..add(t.notifications(batteryLevel).listen((v) {
        if (v.isNotEmpty) _battery.add(battery = v[0]);
      }));
    _storage = t.notifications(storageData).asBroadcastStream();
    _subs.add(_storage.listen((_) {}));
    await link();
  }

  /// What every new link needs: the MTU exchange, the CCC writes (a link
  /// forgets both) and the time sync.
  Future<void> link() async {
    await t.requestMtu(maxMtu);
    for (final (s, c) in const [(omiService, audioData), (buttonService, buttonTrigger), (batteryService, batteryLevel), (storageService, storageData)]) {
      try {
        await t.subscribe(s, c);
      } catch (_) {}
    }
    await syncTime();
  }

  Future<void> stop() async {
    for (final s in _subs) {
      await s.cancel();
    }
    _subs.clear();
    await t.disconnect();
  }

  Future<List<int>> _tryRead(String s, String c) async {
    try {
      return await t.read(s, c);
    } catch (_) {
      return const [];
    }
  }

  /// Time sync at connect: write UTC seconds, read the device clock back.
  Future<void> syncTime() async {
    try {
      await t.write(timeService, timeWrite, u32le(nowMs() ~/ 1000));
      final dev = await t.read(timeService, timeRead);
      if (dev.length >= 4) clockOffsetMs = nowMs() - readU32le(dev) * 1000;
    } catch (_) {
      clockOffsetMs = 0;
    }
  }

  Future<void> haptic(int level) => t.write(hapticService, hapticWrite, [level & 0xFF]);

  // --- Offline storage ------------------------------------------------------

  String? _layout;
  RingInfo? _ring;
  List<int> _legacyList = const [];

  Future<String> layout() async {
    if (_layout != null) return _layout!;
    if (isRingFirmware(firmware)) return _layout = 'ring';
    final list = parseStorageList(await _tryRead(storageService, storageControl));
    if (list.length >= 2 && list[1] <= legacyOffsetFloor && await _listFiles() != null) return _layout = 'files';
    return _layout = 'legacy';
  }

  /// Stored stretches waiting on the Omi, in the order they can be deleted.
  Future<List<StoredFile>> waiting() async {
    switch (await layout()) {
      case 'ring':
        final info = _ring = await _command([cmdRingInfo], parseRingInfo);
        if (info == null || info.unread <= 0) return [];
        return [StoredFile(info.readSeq, 0, min(info.unread, ringReadWindow))];
      case 'files':
        // Firmware re-indexes after a delete: highest index first keeps the rest valid.
        return (await _listFiles() ?? [])..sort((a, b) => b.index.compareTo(a.index));
      default:
        _legacyList = parseStorageList(await _tryRead(storageService, storageControl));
        if (_legacyList.isEmpty || _legacyList[0] <= 0) return [];
        final offset = _legacyList.length < 2 || _legacyList[1] > _legacyList[0] ? 0 : _legacyList[1];
        // Field 2, where firmware 3.0.16+ lists it, is the recording's start (sdcard_wal_sync.dart).
        return [StoredFile(1, _legacyList.length >= 3 ? _legacyList[2] : 0, _legacyList[0] - offset)];
    }
  }

  Future<List<StoredFile>?> _listFiles() => _command([cmdListFiles], parseFileList);

  /// Writes a storage command and returns the first notification [parse] accepts.
  Future<T?> _command<T>(List<int> cmd, T? Function(List<int>) parse) async {
    final got = Completer<T?>();
    final sub = _storage.listen((v) {
      final r = parse(v);
      if (r != null && !got.isCompleted) got.complete(r);
    });
    try {
      await t.write(storageService, storageData, cmd);
      return await got.future.timeout(_answerWait, onTimeout: () => null);
    } finally {
      await sub.cancel();
    }
  }

  /// Reads one stored stretch; throws when the read stalls or the Omi refuses it.
  Future<StoredRead> read(StoredFile f) async {
    final lay = await layout();
    final blocks = <(int, List<List<int>>)>[];
    final ring = BytesBuilder();
    final done = Completer<int>(); // completes with the ring's next sequence, or 0
    var received = 0;
    Timer? stall;
    void arm() {
      stall?.cancel();
      stall = Timer(_stallWait, () {
        if (!done.isCompleted) done.completeError(TimeoutException('stalled'));
      });
    }

    final sub = _storage.listen((v) {
      if (done.isCompleted || v.isEmpty) return;
      arm();
      if (lay == 'ring') {
        if (v[0] == notifyData) {
          ring.add(v.sublist(1)); // DATA is not aligned to records
          final all = ring.takeBytes();
          var i = 0;
          for (; i + ringRecord <= all.length; i += ringRecord) {
            final r = all.sublist(i, i + ringRecord);
            blocks.add((ringRecordTimestamp(r), parseBlock(r.sublist(4))));
          }
          ring.add(all.sublist(i));
        } else if (parseRingDone(v) case (final status, final next)) {
          status == 0 ? done.complete(next) : done.completeError(StateError('ring status $status'));
        }
        return;
      }
      if (v.length == 1) {
        if (v[0] == statusOk) return;
        if (v[0] == statusEmpty || v[0] == statusEnd) return done.complete(0);
        return done.completeError(StateError('storage status ${v[0]}'));
      }
      if (lay == 'files') {
        if (v.length <= 4) return;
        blocks.add((readU32be(v), parseBlock(v.sublist(4))));
        received += v.length - 4;
        if (received >= f.size) done.complete(0);
      } else if (v.length == legacyPacket) {
        final n = v[3];
        if (n > 0 && 4 + n <= v.length) blocks.add((0, [v.sublist(4, 4 + n)]));
      } else {
        blocks.add((0, parseBlock(v)));
      }
    });
    try {
      arm();
      await t.write(storageService, storageData, switch (lay) {
        'ring' => ringRead(f.index, f.size),
        'files' => storageCommand(cmdReadFile, f.index, 0),
        _ => storageCommand(0, f.index, _legacyList.length < 2 ? 0 : _legacyList[1]),
      });
      final next = await done.future;
      final frames = blocks.fold<int>(0, (a, b) => a + b.$2.length);
      final start = f.timestamp != 0 ? f.timestamp : nowMs() ~/ 1000 - frames ~/ framesPerSecond(codec);
      return StoredRead(f, lay, packRuns(blocks, fallbackS: start, codec: codec), next);
    } catch (e) {
      await t.write(storageService, storageData, [cmdStop]);
      rethrow;
    } finally {
      stall?.cancel();
      await sub.cancel();
    }
  }

  /// Deletes a stored read from the Omi. [serverSha256] are the server's
  /// answers, one per run, for exactly the bytes of each run; anything else
  /// (a missing answer, a different hash, a read with no audio) refuses it.
  Future<bool> delete(StoredRead r, List<String> serverSha256) async {
    if (r.runs.isEmpty || serverSha256.length != r.runs.length) return false;
    for (var i = 0; i < r.runs.length; i++) {
      if (serverSha256[i] != r.runs[i].sha256Hex) throw StateError('server hash does not match the bytes read');
    }
    switch (r.layout) {
      case 'ring':
        final ring = _ring;
        final id = ring != null && ring.caps & capRingId != 0 ? ring.ringId : null;
        final ack = await _command(ringAdvance(r.next, id), (v) => v.length >= 2 && v[0] == notifyAck ? v[1] : null);
        return ack == 0;
      case 'files':
        final res = await _command([cmdDeleteFile, r.file.index & 0xFF], (v) => v.length == 1 ? v[0] : null);
        return res == 0;
      default:
        final before = _legacyList.isEmpty ? 0 : _legacyList[0];
        await t.write(storageService, storageData, storageCommand(1, r.file.index, 0));
        final after = parseStorageList(await _tryRead(storageService, storageControl));
        return after.isNotEmpty && after[0] < before;
    }
  }
}
