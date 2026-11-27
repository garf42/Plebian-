// A05/A06 app halves against the real Omi simulator, on the host VM. Runs when
// OMI_FIXTURE names a running `fixture omi` (http://127.0.0.1:PORT); the same
// scenario runs on the emulator in integration_test/device_seam_test.dart.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import '../support/fake_server.dart';
import '../support/seam_scenario.dart';

void main() {
  final fixture = Platform.environment['OMI_FIXTURE'] ?? '';
  test('double-tap in range, nothing out of range, stored audio lands late', () async {
    final server = await FakeServer.start();
    addTearDown(server.close);
    final omi = OmiControl(Uri.parse(fixture));
    final m = (await seamBoot(server, Uri.parse(fixture)).open())!;
    addTearDown(m.link.close);
    await a05DoubleTap(server, omi);
    await a06OutOfRange(server, omi, m);
  }, skip: fixture.isEmpty ? 'OMI_FIXTURE is not set' : false, timeout: const Timeout(Duration(minutes: 3)));
}
