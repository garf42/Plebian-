// The app's halves of A05 and A06 at the device seam, against the Omi
// simulator (tests/fixtures/omi) and a stand-in server. The app runs its own
// code from Boot down: Link, OmiDevice, the protocol, SimTransport.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:plebian/main.dart';
import 'package:plebian/model.dart';
import 'package:plebian/services.dart';

import 'fake_server.dart';
import 'fakes.dart';

/// A paired app on [server] whose Omi is the simulator at [omi].
Boot seamBoot(FakeServer server, Uri omi) {
  final vault = MemoryVault()
    ..m['server'] = server.base.toString()
    ..m['token'] = 'tok'
    ..m['omi'] = 'sim';
  return Boot(
    vault: vault,
    signer: FakeSigner(),
    dir: () async => Directory.systemTemp.createTempSync('plebian'),
    notifier: FakeNotifier(),
    player: FakePlayer(),
    sim: omi,
    service: () async {},
  );
}

Future<bool> _poll(Future<bool> Function() test, {Duration within = const Duration(seconds: 30)}) async {
  final end = DateTime.now().add(within);
  while (!await test()) {
    if (DateTime.now().isAfter(end)) return false;
    await Future<void>.delayed(const Duration(milliseconds: 200));
  }
  return true;
}

Future<void> a05DoubleTap(FakeServer server, OmiControl omi) async {
  expect(await _poll(() async => ((await omi.state())['subscribed'] as List? ?? const []).contains('23ba7925-0000-1000-7450-346eac492e92')), isTrue,
      reason: 'the app subscribes to the button');
  expect(await omi.doubleTap(), {'heard': true});
  expect(await server.until(() => server.sent('/v1/taps').isNotEmpty), isTrue, reason: 'a double-tap posts a tap');
  expect(server.sent('/v1/taps').single.json['how'], 'omi');
  expect(await _poll(() async => (await omi.writes()).any((w) => w['note'] == 'haptic 100 ms')), isTrue, reason: 'and buzzes 100 ms');
}

Future<void> a06OutOfRange(FakeServer server, OmiControl omi, AppModel m) async {
  final taps = server.sent('/v1/taps').length;
  final haptics = (await omi.writes()).where((w) => '${w['note']}'.startsWith('haptic')).length;
  await omi.range(false);
  expect(await _poll(() async => !m.link.omiConnected), isTrue);
  expect(await omi.doubleTap(), {'heard': false});
  final talk = await omi.talk([
    {'speaker': 'a', 'text': 'the twenty foot culvert pipe is three forty', 'pause_ms': 0},
  ]) as Map;
  await Future<void>.delayed(Duration(milliseconds: (talk['duration_ms'] as num).toInt() + 500));
  final stored = await omi.storage();
  expect((stored['write_seq'] as num) > (stored['read_seq'] as num), isTrue, reason: 'out of range, the Omi records to storage');
  await omi.range(true);
  expect(await server.until(() => server.sent('/v1/audio/files').isNotEmpty, within: const Duration(seconds: 60)), isTrue,
      reason: 'back in range, the stored audio syncs');
  final f = server.sent('/v1/audio/files').first;
  expect(int.parse(f.headers['x-omi-file-timestamp']!), greaterThan(0));
  expect(await _poll(() async {
    final s = await omi.storage();
    return s['read_seq'] == s['write_seq'];
  }), isTrue, reason: 'deleted from the Omi once the server answered its hash');
  expect(server.sent('/v1/taps').length, taps, reason: 'a double-tap out of range writes no tap');
  expect((await omi.writes()).where((w) => '${w['note']}'.startsWith('haptic')).length, haptics);
}
