// The device layer driven by raw notification bytes: MR-2 (another MTU ⇒
// identical frames), the double-tap (a tap and haptic level 1), no tap while
// out of range, time sync at connect, and frames forwarded in the live wire
// format.
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:plebian/device/omi_device.dart';
import 'package:plebian/device/omi_protocol.dart';
import 'package:plebian/link.dart';

import '../support/fakes.dart';
import '../support/mem_transport.dart';

final audio = [
  for (var i = 0; i < 6; i++) [for (var k = 0; k < 120 + 13 * i; k++) (i * 7 + k) & 0xFF],
];

Future<List<List<int>>> through(int mtu) async {
  final t = MemTransport();
  final d = OmiDevice(t);
  await d.start();
  final out = <List<int>>[];
  d.frames.listen((f) => out.add(f.bytes));
  for (final n in fragment([...audio, [0]], mtu: mtu)) {
    t.notify(audioData, n);
  }
  await Future<void>.delayed(Duration.zero);
  return out;
}

Future<(MemTransport, Link, FakeApi)> attached() async {
  final api = FakeApi();
  final link = Link(api: api, dir: Directory.systemTemp.createTempSync('plebian'), notifier: FakeNotifier());
  final t = MemTransport(firmware: '3.0.20')..onStorage = (_) {};
  await link.attachOmi(t);
  return (t, link, api);
}

void main() {
  test('MR-2: a different MTU gives identical frames', () async {
    final small = await through(23), big = await through(maxMtu);
    expect(small, audio);
    expect(big, small);
  });

  test('a double-tap while connected makes one tap (how omi) and writes haptic level 1', () async {
    final (t, _, api) = await attached();
    t.notify(buttonTrigger, [buttonDouble, 0, 0, 0, 0, 0, 0, 0]);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    final taps = api.sent('/v1/taps');
    expect(taps, hasLength(1));
    expect((taps.single.body as Map)['how'], 'omi');
    expect(t.writesTo(hapticWrite), [
      [hapticShort],
    ]);
  });

  test('a single tap, a long press and a double-tap out of range make no tap', () async {
    final (t, _, api) = await attached();
    t.notify(buttonTrigger, [buttonSingle, 0, 0, 0, 0, 0, 0, 0]);
    t.notify(buttonTrigger, [buttonLong, 0, 0, 0, 0, 0, 0, 0]);
    t.setConnected(false);
    await Future<void>.delayed(Duration.zero);
    t.notify(buttonTrigger, [buttonDouble, 0, 0, 0, 0, 0, 0, 0]);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(api.sent('/v1/taps'), isEmpty);
    expect(t.writesTo(hapticWrite), isEmpty);
  });

  test('time sync at connect writes UTC seconds; the device clock offset is read back', () async {
    final t = MemTransport();
    final d = OmiDevice(t, nowMs: () => 1763220000123);
    await d.start();
    expect(t.writesTo(timeWrite).single, u32le(1763220000));
    expect(d.clockOffsetMs, 123);
  });

  test('live frames go out as u16 packet, i64 arrival ms, payload', () {
    final w = AudioUplink.wire(AudioFrame(65535, 1763220000123, Uint8List.fromList([9, 8, 7])));
    final b = ByteData.sublistView(w);
    expect(b.getUint16(0, Endian.little), 65535);
    expect(b.getInt64(2, Endian.little), 1763220000123);
    expect(w.sublist(10), [9, 8, 7]);
  });
}
