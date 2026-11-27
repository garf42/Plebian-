// The server API (server/api/API.md) as the app sees it: JSON over HTTP,
// three WebSockets, bearer auth. Screens render only what these calls return.
import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:web_socket_channel/io.dart';

typedef Json = Map<String, dynamic>;

class ApiError implements Exception {
  ApiError(this.status);
  final int status;
  @override
  String toString() => 'ApiError($status)';
}

/// One WebSocket: text frames are Strings, binary frames are Uint8List.
abstract class Sock {
  Stream<dynamic> get messages;
  void add(Object data);
  Future<void> close();
}

abstract class Api {
  Uri get base;
  Future<dynamic> get(String path, [Map<String, String>? query]);

  /// JSON body, or [raw] bytes sent exactly (signed admin actions, offline files).
  Future<dynamic> send(String method, String path, {Object? json, Uint8List? raw, Map<String, String> headers});
  Future<Uint8List> bytes(String path);
  Sock socket(String path, [Map<String, String>? query]);
}

class HttpApi implements Api {
  HttpApi(this.base, this.token, [http.Client? client]) : _c = client ?? http.Client();
  @override
  final Uri base;
  final String? token;
  final http.Client _c;

  Map<String, String> get _auth => token == null ? {} : {'Authorization': 'Bearer $token'};
  Uri _u(String path, [Map<String, String>? q]) => base.replace(path: path, queryParameters: q);

  dynamic _decode(http.Response r) {
    if (r.statusCode >= 400) throw ApiError(r.statusCode);
    return r.body.isEmpty ? null : jsonDecode(r.body);
  }

  @override
  Future<dynamic> get(String path, [Map<String, String>? query]) async => _decode(await _c.get(_u(path, query), headers: _auth));

  @override
  Future<dynamic> send(String method, String path, {Object? json, Uint8List? raw, Map<String, String> headers = const {}}) async {
    final req = http.Request(method, _u(path))
      ..headers.addAll(_auth)
      ..headers.addAll(headers);
    if (raw != null) {
      req.bodyBytes = raw;
    } else {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(json);
    }
    return _decode(await http.Response.fromStream(await _c.send(req)));
  }

  @override
  Future<Uint8List> bytes(String path) async {
    final r = await _c.get(_u(path), headers: _auth);
    if (r.statusCode >= 400) throw ApiError(r.statusCode);
    return r.bodyBytes;
  }

  @override
  Sock socket(String path, [Map<String, String>? query]) {
    final u = _u(path, query);
    return _WsSock(IOWebSocketChannel.connect(u.replace(scheme: u.scheme == 'https' ? 'wss' : 'ws'), headers: _auth));
  }
}

class _WsSock implements Sock {
  _WsSock(this.ch);
  final IOWebSocketChannel ch;
  @override
  Stream<dynamic> get messages => ch.stream;
  @override
  void add(Object data) => ch.sink.add(data);
  @override
  Future<void> close() async => ch.sink.close();
}
