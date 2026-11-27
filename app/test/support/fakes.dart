// A fake server answering fixture JSON, and the app model wired to it, so
// widget tests drive the real screens and see every request they make.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/api.dart';
import 'package:plebian/device/transport.dart';
import 'package:plebian/link.dart';
import 'package:plebian/model.dart';
import 'package:plebian/services.dart';
import 'package:plebian/ui/admin.dart';
import 'package:plebian/ui/kit.dart';

import 'fixtures.dart';
import 'mem_transport.dart';

class Call {
  Call(this.method, this.path, this.body, this.headers);
  final String method, path;
  final Object? body;
  final Map<String, String> headers;
  @override
  String toString() => '$method $path ${body is Uint8List ? '<${(body as Uint8List).length} bytes>' : jsonEncode(body)}';
}

class FakeSock implements Sock {
  final _in = StreamController<dynamic>();
  final sent = <Object>[];
  @override
  Stream<dynamic> get messages => _in.stream;
  @override
  void add(Object data) => sent.add(data);
  void push(Object data) => _in.add(data);
  @override
  Future<void> close() async {}
}

class FakeApi implements Api {
  FakeApi([Map<String, Object?>? routes]) : routes = routes ?? fixtures();
  final Map<String, Object?> routes;
  final calls = <Call>[];
  final socks = <String, FakeSock>{};
  bool down = false;

  /// Answers `/v1/audio/files` (default: the sha256 of the body, as the server does).
  Object? Function(Uint8List body)? onFile;

  @override
  Uri get base => Uri.parse('http://127.0.0.1:1');

  @override
  Future<dynamic> get(String path, [Map<String, String>? query]) async {
    if (down) throw const SocketException('down');
    final r = routes['GET $path'];
    if (r == null) throw ApiError(404);
    return jsonDecode(jsonEncode(r));
  }

  @override
  Future<dynamic> send(String method, String path, {Object? json, Uint8List? raw, Map<String, String> headers = const {}}) async {
    if (down) throw const SocketException('down');
    calls.add(Call(method, path, raw ?? json, headers));
    if (path == '/v1/audio/files') return (onFile ?? (b) => {'sha256': sha256Hex(b)})(raw!);
    final r = routes['$method $path'];
    return r == null ? {'ok': true} : jsonDecode(jsonEncode(r));
  }

  @override
  Future<Uint8List> bytes(String path) async => Uint8List.fromList(oggFixture);

  @override
  Sock socket(String path, [Map<String, String>? query]) => socks[path] = FakeSock();

  List<Call> sent(String path) => [for (final c in calls) if (c.path == path) c];
}

class FakeNotifier implements Notifier {
  final shown = <Json>[];
  @override
  Future<void> show(Json event) async => shown.add(event);
}

class FakeSigner implements Signer {
  final signedBodies = <Uint8List>[];
  @override
  Future<String> publicKey() async => testPublicKey;
  @override
  Future<String> sign(Uint8List body) async {
    signedBodies.add(body);
    return 'c2lnbmVk';
  }
}

class FakePlayer implements Player {
  final played = <int>[];
  @override
  Future<void> play(Uint8List ogg) async => played.add(ogg.length);
  @override
  Future<void> pause() async {}
  @override
  Stream<double> get progress => const Stream.empty();
}

/// A paired app model on [api], with no real I/O but the outbox file.
Future<AppModel> model(FakeApi api, {Notifier? notifier, DeviceTransport Function(String)? radio, Stream<(String, String)> Function()? scan}) async {
  final dir = Directory.systemTemp.createTempSync('plebian');
  final link = Link(api: api, dir: dir, notifier: notifier ?? FakeNotifier());
  final vault = MemoryVault();
  final m = AppModel(
    api: api,
    link: link,
    signer: FakeSigner(),
    omi: OmiPairing(link, vault,
        scan: scan ?? () => Stream.value(('AA:BB', 'Omi')), radio: radio ?? (_) => MemTransport(firmware: '1.0.0'), stopScan: () async {}),
    player: FakePlayer(),
    vault: vault,
  );
  await m.load();
  await link.start();
  return m;
}

/// Records pushes and pops.
class NavLog extends NavigatorObserver {
  int events = 0;
  @override
  void didPush(Route route, Route? previousRoute) => events++;
  @override
  void didPop(Route route, Route? previousRoute) => events++;
}

Widget host(AppModel m, Widget screen, {NavLog? nav}) => Scope(
      model: m,
      child: Pairing(
        vault: m.vault,
        signer: m.signer,
        child: MaterialApp(theme: theme(), navigatorObservers: [?nav], home: screen),
      ),
    );

/// A phone-sized screen at [textScale].
void phone(WidgetTester t, {double textScale = 1}) {
  t.view.physicalSize = const Size(1080, 2400);
  t.view.devicePixelRatio = 2.625;
  t.platformDispatcher.textScaleFactorTestValue = textScale;
  addTearDown(t.view.reset);
  addTearDown(t.platformDispatcher.clearTextScaleFactorTestValue);
}
