// The phone's two links, held by the background service with the app closed:
// the server link (event stream, outbox, live audio, offline files) and the
// Omi link (through OmiDevice). Nothing here depends on a screen.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'api.dart';
import 'device/omi_device.dart';
import 'device/omi_protocol.dart';
import 'device/transport.dart';

/// Retry delays after RFC 6298 (TCP retransmission): start at 1 s, double,
/// cap at 60 s (the smallest maximum the RFC allows).
class Backoff {
  Duration _d = const Duration(seconds: 1);
  Duration next() {
    final d = _d;
    _d = Duration(seconds: min(_d.inSeconds * 2, 60));
    return d;
  }

  void reset() => _d = const Duration(seconds: 1);
}

/// Raises a phone notification for a message the agent started.
abstract class Notifier {
  Future<void> show(Json event);
}

String _rand() => List.generate(16, (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0')).join();

/// Owner messages, taps and notes, persisted in order and sent in order
/// whenever the server answers.
class Outbox {
  Outbox(this.file, this.api, {this.onChange, this.onSent}) {
    if (file.existsSync()) {
      for (final l in file.readAsLinesSync()) {
        if (l.isNotEmpty) items.add(jsonDecode(l) as Json);
      }
    }
  }
  final File file;
  final Api api;
  final void Function()? onChange;
  final void Function(Json item, dynamic response)? onSent;
  final items = <Json>[];
  final refused = <Json>[];
  final _backoff = Backoff();
  bool _running = false;
  Timer? _retry;

  void _save() => file.writeAsStringSync(items.map((e) => '${jsonEncode(e)}\n').join());

  Future<void> add(String method, String path, Json body) async {
    items.add({'method': method, 'path': path, 'body': body});
    _save();
    onChange?.call();
    unawaited(drain());
  }

  /// Sends a message typed by the owner. [quote] is what they tapped, if anything.
  Future<void> message(String text, Json? quote) => add('POST', '/v1/messages', {
        'client_msg_id': _rand(),
        'text': text,
        'sent_at': DateTime.now().toUtc().toIso8601String(),
        'quote': quote,
      });

  Future<void> drain() async {
    if (_running) return;
    _running = true;
    _retry?.cancel();
    try {
      while (items.isNotEmpty) {
        final e = items.first;
        try {
          final res = await api.send(e['method'] as String, e['path'] as String, json: e['body']);
          onSent?.call(e, res);
        } on ApiError catch (err) {
          final s = err.status;
          if (s == 401 || s == 408 || s == 429 || s >= 500) return _later();
          refused.add(e); // refused for good: kept visible, never silent
        } catch (_) {
          return _later();
        }
        items.removeAt(0);
        _save();
        _backoff.reset();
        onChange?.call();
      }
    } finally {
      _running = false;
    }
  }

  void _later() {
    _retry?.cancel();
    _retry = Timer(_backoff.next(), drain);
  }
}

/// Live Omi audio to /v1/audio/live: each frame is spooled to disk first and
/// sent from the spool in order, so frames heard while the server is
/// unreachable go out, in order, when it returns.
class AudioUplink {
  AudioUplink(this.api, this.spool);
  final Api api;
  final File spool;
  int codec = codecOpus, offsetMs = 0;
  int _read = 0;
  Sock? _sock;
  bool _running = false;
  final _backoff = Backoff();
  Timer? _retry;

  /// The wire frame: u16 LE packet number, i64 LE arrival unix ms, payload.
  static Uint8List wire(AudioFrame f) {
    final b = BytesBuilder()
      ..add((ByteData(10)
            ..setUint16(0, f.packet, Endian.little)
            ..setInt64(2, f.arrivalMs, Endian.little))
          .buffer
          .asUint8List())
      ..add(f.bytes);
    return b.takeBytes();
  }

  void add(AudioFrame f) {
    final w = wire(f);
    spool.writeAsBytesSync([...u32le(w.length), ...w], mode: FileMode.append, flush: true);
    unawaited(drain());
  }

  /// Opens a new socket for a new device session (codec or clock may differ).
  void restart() {
    _sock?.close();
    _sock = null;
  }

  Future<void> drain() async {
    if (_running) return;
    _running = true;
    try {
      while (true) {
        final data = spool.existsSync() ? spool.readAsBytesSync() : Uint8List(0);
        if (_read >= data.length) {
          if (_read > 0) spool.writeAsBytesSync(const [], flush: true);
          _read = 0;
          return;
        }
        final sock = _sock ??= _open();
        while (_read + 4 <= data.length) {
          final n = readU32le(data.sublist(_read, _read + 4));
          sock.add(Uint8List.sublistView(data, _read + 4, _read + 4 + n));
          _read += 4 + n;
        }
        await Future<void>.delayed(Duration.zero);
      }
    } catch (_) {
      // shortcut: the API has no per-frame ack, so frames already handed to a
      // socket that then fails are counted as sent; upgrade when it acks.
      _sock = null;
      _retry?.cancel();
      _retry = Timer(_backoff.next(), drain);
    } finally {
      _running = false;
    }
  }

  Sock _open() {
    final s = api.socket('/v1/audio/live', {'codec': '$codec', 'offset_ms': '$offsetMs'});
    s.messages.listen((_) => _backoff.reset(), onError: (_) => _sock = null, onDone: () {
      if (identical(_sock, s)) _sock = null;
    }, cancelOnError: true);
    return s;
  }
}

/// The server link and the Omi link together.
class Link {
  Link({required this.api, required this.dir, required this.notifier}) {
    outbox = Outbox(File('${dir.path}/outbox.jsonl'), api, onChange: () => _changed.add(null), onSent: (e, r) => sent.add((e, r)));
    uplink = AudioUplink(api, File('${dir.path}/audio.spool'));
    final last = File('${dir.path}/events.last');
    lastId = last.existsSync() ? last.readAsStringSync() : null;
  }
  final Api api;
  final Directory dir;
  final Notifier notifier;
  late final Outbox outbox;
  late final AudioUplink uplink;
  String? lastId;

  final _events = StreamController<Json>.broadcast();
  final _changed = StreamController<void>.broadcast();
  final sent = StreamController<(Json, dynamic)>.broadcast();
  Stream<Json> get events => _events.stream;
  Stream<void> get changed => _changed.stream;

  bool reachable = false;
  bool _closed = false;
  Sock? _sock;

  // Omi state, as posted to /v1/omi/state.
  OmiDevice? omi;
  bool omiConnected = false;
  int waitingFiles = 0;

  Future<void> start() async {
    if (lastId == null) {
      try {
        final ev = ((await api.get('/v1/thread'))['events'] as List).cast<Json>();
        _setLast(ev.isEmpty ? '' : ev.last['id'] as String);
      } catch (_) {}
    }
    unawaited(_eventsLoop());
    unawaited(outbox.drain());
  }

  Future<void> close() async {
    _closed = true;
    await _sock?.close();
  }

  void _setLast(String id) {
    lastId = id;
    File('${dir.path}/events.last').writeAsStringSync(id, flush: true);
  }

  Future<void> _eventsLoop() async {
    final backoff = Backoff();
    while (!_closed) {
      try {
        final s = _sock = api.socket('/v1/events', lastId == null || lastId!.isEmpty ? null : {'after': lastId!});
        await for (final raw in s.messages) {
          if (!reachable) _back();
          backoff.reset();
          final e = jsonDecode(raw as String) as Json;
          _setLast(e['id'] as String);
          if (e['type'] == 'message' && e['notify'] == true) await notifier.show(e);
          _events.add(e);
        }
      } catch (_) {}
      reachable = false;
      _changed.add(null);
      if (_closed) return;
      await Future<void>.delayed(backoff.next());
    }
  }

  /// The server answers again: send what waited, in order.
  void _back() {
    reachable = true;
    _changed.add(null);
    unawaited(outbox.drain());
    unawaited(uplink.drain());
    final d = omi;
    if (d != null && omiConnected) unawaited(syncStorage(d));
  }

  // --- The Omi -------------------------------------------------------------

  /// Connects the Omi through [t] and keeps it streaming to the server.
  Future<OmiDevice> attachOmi(DeviceTransport t) async {
    final d = omi = OmiDevice(t);
    var lost = false;
    t.connection.listen((up) async {
      omiConnected = up;
      if (!up && !lost) {
        lost = true;
        unawaited(postOmiState());
        unawaited(_reconnect(t));
      } else if (up && lost) {
        // A new link: MTU, subscriptions and the clock again, then what was recorded meanwhile.
        lost = false;
        await d.link();
        uplink
          ..offsetMs = d.clockOffsetMs
          ..restart();
        await postOmiState();
        unawaited(syncStorage(d));
      }
    });
    await d.start();
    omiConnected = true;
    uplink
      ..codec = d.codec
      ..offsetMs = d.clockOffsetMs
      ..restart();
    d.frames.listen(uplink.add);
    d.buttons.listen((code) async {
      if (code != buttonDouble || !omiConnected) return;
      await outbox.add('POST', '/v1/taps', {'at': DateTime.now().toUtc().toIso8601String(), 'how': 'omi', 'line': null});
      await d.haptic(hapticShort);
    });
    d.batteryLevels.listen((_) => postOmiState());
    await postOmiState();
    unawaited(syncStorage(d));
    return d;
  }

  /// Asks the transport for the link again until the Omi is back in range.
  Future<void> _reconnect(DeviceTransport t) async {
    final backoff = Backoff();
    while (!omiConnected && !_closed) {
      try {
        await t.connect();
        return;
      } catch (_) {
        await Future<void>.delayed(backoff.next());
      }
    }
  }

  Future<void> postOmiState() async {
    final d = omi;
    if (d == null) return;
    try {
      await api.send('POST', '/v1/omi/state', json: {
        'connected': omiConnected,
        'battery': d.battery,
        'firmware': d.firmware,
        'clock_offset_ms': d.clockOffsetMs,
        'waiting_files': waitingFiles,
      });
    } catch (_) {}
  }

  bool _syncing = false;

  /// Moves every stored stretch from the Omi to the server; each is deleted
  /// from the Omi only on the server's sha256 of exactly the bytes read.
  Future<void> syncStorage(OmiDevice d) async {
    if (_syncing) return;
    _syncing = true;
    try {
      while (omiConnected) {
        final files = await d.waiting();
        waitingFiles = files.length;
        await postOmiState();
        if (files.isEmpty) return;
        for (final f in files) {
          final r = await d.read(f);
          final acks = <String>[];
          for (final run in r.runs) {
            final res = await api.send('POST', '/v1/audio/files', raw: run.body, headers: {
              'X-Omi-File-Index': '${f.index}',
              'X-Omi-File-Timestamp': '${run.deviceMs}',
              'X-Omi-Clock-Offset-Ms': '${d.clockOffsetMs}',
              'X-Omi-Codec': '${d.codec}',
            });
            final sha = (res as Json?)?['sha256'];
            if (sha is! String) return;
            acks.add(sha);
          }
          if (!await d.delete(r, acks)) return;
        }
      }
    } catch (_) {
      // Unreachable server or a stalled read: the stretch stays on the Omi.
    } finally {
      _syncing = false;
    }
  }
}
