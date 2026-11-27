// n.app_device D3 kill test: the Omi simulator carries raw characteristic
// bytes only, so the phone's own parsing, reassembly and button code runs on
// what it sends. A stand-in for tests/fixtures/omi's phone face (the events
// WebSocket and the read/write/subscribe/mtu POSTs) sends MTU fragments; only
// the real assembler turns them into the frames. (The real simulator runs the
// same path in fixture_seam_test.dart and on the emulator.)
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:plebian/device/omi_device.dart';
import 'package:plebian/device/omi_protocol.dart';
import 'package:plebian/device/transport.dart';

import '../support/mem_transport.dart';

Future<(HttpServer, Completer<void>)> simulator(List<List<int>> audio, {int mtu = 64}) async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  final sent = Completer<void>();
  WebSocket? phone;
  final subs = <String>{};
  server.listen((r) async {
    if (r.uri.path == '/phone/events') {
      phone = await WebSocketTransformer.upgrade(r);
      phone!.add(jsonEncode({'op': 'connected'}));
      phone!.listen((_) {});
      return;
    }
    final q = jsonDecode(await utf8.decodeStream(r)) as Map<String, dynamic>;
    Object answer = {'ok': true};
    switch (r.uri.path) {
      case '/phone/read':
        answer = {
          'value': base64Encode(switch (q['char']) {
            firmwareRevision => utf8.encode('3.0.24'),
            audioCodec => [codecOpusFS320],
            batteryLevel => [77],
            timeRead => u32le(DateTime.now().millisecondsSinceEpoch ~/ 1000),
            _ => <int>[],
          }),
        };
      case '/phone/mtu':
        answer = {'mtu': mtu};
      case '/phone/subscribe':
        subs.add(q['char'] as String);
        if (subs.containsAll([audioData, buttonTrigger]) && !sent.isCompleted) {
          for (final n in fragment(audio, mtu: mtu)) {
            phone!.add(jsonEncode({'op': 'notify', 'char': audioData, 'value': base64Encode(n)}));
          }
          phone!.add(jsonEncode({'op': 'notify', 'char': buttonTrigger, 'value': base64Encode([buttonDouble, 0, 0, 0, 0, 0, 0, 0])}));
          sent.complete();
        }
    }
    r.response.write(jsonEncode(answer));
    await r.response.close();
  });
  return (server, sent);
}

void main() {
  final audio = [
    for (var i = 0; i < 4; i++) [for (var k = 0; k < 150; k++) (i * 17 + k) & 0xFF],
  ];

  test('fragments from the simulator come out as whole frames, and a double-tap as code 2', () async {
    final (server, sent) = await simulator(audio);
    addTearDown(() => server.close(force: true));
    final d = OmiDevice(SimTransport(Uri.parse('http://127.0.0.1:${server.port}')));
    final frames = <List<int>>[];
    final buttons = <int>[];
    d.frames.listen((f) => frames.add(f.bytes));
    d.buttons.listen(buttons.add);
    await d.start();
    await sent.future;
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(d.firmware, '3.0.24');
    expect(d.codec, codecOpusFS320);
    // The last frame completes when the next one starts; the first three are whole.
    expect(frames, audio.sublist(0, 3));
    expect(buttons, [buttonDouble]);
  });
}
