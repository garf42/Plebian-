// A stand-in for the Plebian server over real HTTP (dart:io only), for tests
// that run the whole app on a device: it answers the projections with empty
// pages, keeps the event and live-audio sockets open, acknowledges offline
// files with their sha256, and records every request it receives.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

class Got {
  Got(this.method, this.path, this.headers, this.body);
  final String method, path;
  final Map<String, String> headers;
  final Uint8List body;
  Map<String, dynamic> get json => jsonDecode(utf8.decode(body)) as Map<String, dynamic>;
}

class FakeServer {
  FakeServer._(this._s);
  final HttpServer _s;
  final got = <Got>[];
  int liveFrames = 0;
  final _changed = StreamController<void>.broadcast();

  static Future<FakeServer> start() async {
    final f = FakeServer._(await HttpServer.bind(InternetAddress.loopbackIPv4, 0));
    f._s.listen(f._handle);
    return f;
  }

  Uri get base => Uri.parse('http://127.0.0.1:${_s.port}');
  Future<void> close() => _s.close(force: true);

  List<Got> sent(String path) => [for (final g in got) if (g.path == path) g];

  /// Waits until [test] holds over what has arrived, or [within] passes.
  Future<bool> until(bool Function() test, {Duration within = const Duration(seconds: 30)}) async {
    final end = DateTime.now().add(within);
    while (!test()) {
      if (DateTime.now().isAfter(end)) return false;
      await Future.any([_changed.stream.first, Future<void>.delayed(const Duration(milliseconds: 200))]);
    }
    return true;
  }

  Future<void> _handle(HttpRequest r) async {
    final path = r.uri.path;
    if (WebSocketTransformer.isUpgradeRequest(r)) {
      final ws = await WebSocketTransformer.upgrade(r);
      ws.listen((m) {
        if (path == '/v1/audio/live' && m is List<int>) liveFrames++;
        _changed.add(null);
      });
      return;
    }
    final body = Uint8List.fromList(await r.fold<List<int>>([], (a, b) => a..addAll(b)));
    final headers = <String, String>{};
    r.headers.forEach((k, v) => headers[k.toLowerCase()] = v.join(','));
    got.add(Got(r.method, path, headers, body));
    _changed.add(null);
    final Object answer = switch ((r.method, path)) {
      ('POST', '/v1/audio/files') => {'sha256': sha256.convert(body).toString()},
      ('POST', '/v1/messages') => {'id': '${got.length}'},
      ('GET', '/v1/thread') => {'events': []},
      ('GET', '/v1/open') => {'questions': [], 'drafts': [], 'orders': []},
      ('GET', '/v1/today') => {'date': '2026-11-27', 'cut': '2026-11-27T04:00:00Z', 'entry': [], 'thread': [], 'taps': [], 'heard': [], 'ribbon': {'hours': [], 'taps': []}},
      _ => {'ok': true},
    };
    r.response.headers.contentType = ContentType.json;
    r.response.write(jsonEncode(answer));
    await r.response.close();
  }
}

/// The Omi simulator's control face (tests/fixtures/omi).
class OmiControl {
  OmiControl(this.base);
  final Uri base;
  final _c = HttpClient();

  Future<dynamic> _call(String method, String path, [Object? body]) async {
    final q = await _c.openUrl(method, base.replace(path: path));
    if (body != null) {
      q.headers.contentType = ContentType.json;
      q.write(jsonEncode(body));
    }
    final r = await q.close();
    final text = await r.transform(utf8.decoder).join();
    return text.isEmpty ? null : jsonDecode(text);
  }

  Future<dynamic> doubleTap() => _call('POST', '/control/double_tap');
  Future<dynamic> range(bool inRange) => _call('POST', '/control/range', {'in': inRange});
  Future<dynamic> talk(List<Map<String, Object>> lines) => _call('POST', '/control/talk', {'lines': lines});
  Future<dynamic> clock(int deviceUnixMs) => _call('POST', '/control/clock', {'device_unix_ms': deviceUnixMs});
  Future<Map<String, dynamic>> state() async => await _call('GET', '/control/state') as Map<String, dynamic>;
  Future<Map<String, dynamic>> storage() async => await _call('GET', '/control/storage') as Map<String, dynamic>;
  Future<List<dynamic>> writes() async {
    final w = await _call('GET', '/control/writes');
    return w is List ? w : (w as Map)['writes'] as List? ?? const [];
  }
}
