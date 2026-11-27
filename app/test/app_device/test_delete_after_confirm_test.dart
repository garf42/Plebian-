// n.app_device D1 kill test: a stored stretch is deleted from the Omi only
// after the server answers the sha256 of exactly the bytes uploaded.
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:plebian/device/omi_device.dart';
import 'package:plebian/device/omi_protocol.dart';
import 'package:plebian/link.dart';

import '../support/fakes.dart';
import '../support/fixtures.dart';
import '../support/mem_transport.dart';

const deleteCmd = 'device write 18'; // 0x12: ring advance, or multi-file delete

/// Ring firmware with [records] stored records, timestamps from [ts0] a second apart.
MemTransport ringOmi({int records = 3, int ts0 = 1763220000}) {
  final t = MemTransport(firmware: '3.0.20');
  var read = 100;
  final write = 100 + records;
  List<int> info() => (ByteData(32)
        ..setUint8(0, notifyInfo)
        ..setUint64(1, read)
        ..setUint64(9, write)
        ..setUint32(17, 4096)
        ..setUint16(29, ringRecord))
      .buffer
      .asUint8List();
  t.onStorage = (cmd) {
    switch (cmd[0]) {
      case cmdRingInfo:
        t.notify(storageData, info());
      case cmdRingRead:
        final bytes = <int>[for (var r = 0; r < write - read; r++) ...record(ts0 + r, r)];
        for (var o = 0; o < bytes.length; o += 300) {
          t.notify(storageData, [notifyData, ...bytes.sublist(o, o + 300 < bytes.length ? o + 300 : bytes.length)]);
        }
        t.notify(storageData, (ByteData(10)
              ..setUint8(0, notifyDone)
              ..setUint64(2, write))
            .buffer
            .asUint8List());
      case cmdRingAdvance:
        read = ByteData.sublistView(Uint8List.fromList(cmd)).getUint64(1);
        t.notify(storageData, [notifyAck, 0]);
    }
  };
  return t;
}

/// One 444-byte ring record: timestamp, then frames packed `[size][frame]` and zero padding.
List<int> record(int ts, int seed) {
  final p = <int>[];
  for (var f = 0; f < 3; f++) {
    p..add(40)..addAll(List.filled(40, (seed * 3 + f) & 0xFF));
  }
  return [...(ByteData(4)..setUint32(0, ts)).buffer.asUint8List(), ...p, ...List.filled(440 - p.length, 0)];
}

Future<(MemTransport, FakeApi, Link)> run(MemTransport t, FakeApi api) async {
  final link = Link(api: api, dir: Directory.systemTemp.createTempSync('plebian'), notifier: FakeNotifier());
  final d = OmiDevice(t);
  await d.start();
  link.omi = d;
  link.omiConnected = true;
  await link.syncStorage(d);
  return (t, api, link);
}

void main() {
  test('ring: the advance is written only after the server acknowledges every run', () async {
    final t = ringOmi();
    final api = FakeApi();
    api.onFile = (b) {
      t.log.add('server ack');
      return {'sha256': sha256Hex(b)};
    };
    await run(t, api);
    expect(api.sent('/v1/audio/files'), isNotEmpty);
    final files = api.sent('/v1/audio/files');
    expect(files.first.headers['X-Omi-File-Timestamp'], '${1763220000 * 1000}');
    expect(t.log, contains(deleteCmd));
    expect(t.log.indexOf('server ack'), lessThan(t.log.indexOf(deleteCmd)));
  });

  test('ring: no answer from the server, no delete', () async {
    final t = ringOmi();
    await run(t, FakeApi()..down = true);
    expect(t.log, isNot(contains(deleteCmd)));
  });

  test('ring: a hash that is not the bytes sent, no delete', () async {
    final t = ringOmi();
    await run(t, FakeApi()..onFile = (_) => {'sha256': '0' * 64});
    expect(t.log, isNot(contains(deleteCmd)));
  });

  test('ring: a clock step over 60 s splits the read into two uploads at their own times', () async {
    final t = ringOmi(records: 2);
    final api = FakeApi();
    t.onStorage = (cmd) {
      if (cmd[0] == cmdRingRead) {
        t.notify(storageData, [notifyData, ...record(1000, 0), ...record(1000 + 61, 1)]);
        t.notify(storageData, (ByteData(10)
              ..setUint8(0, notifyDone)
              ..setUint64(2, 102))
            .buffer
            .asUint8List());
      } else if (cmd[0] == cmdRingInfo) {
        t.notify(storageData, (ByteData(32)
              ..setUint8(0, notifyInfo)
              ..setUint64(1, 100)
              ..setUint64(9, 102))
            .buffer
            .asUint8List());
      } else if (cmd[0] == cmdRingAdvance) {
        t.notify(storageData, [notifyAck, 0]);
        t.onStorage = (c) {
          if (c[0] == cmdRingInfo) t.notify(storageData, (ByteData(32)..setUint8(0, notifyInfo)).buffer.asUint8List());
        };
      }
    };
    await run(t, api);
    expect([for (final c in api.sent('/v1/audio/files')) c.headers['X-Omi-File-Timestamp']], ['1000000', '1061000']);
  });

  test('multi-file: delete only after the acknowledgement', () async {
    final t = MemTransport(firmware: '2.0.0')..storageList = [...u32le(880), ...u32le(1)];
    var files = 1;
    t.onStorage = (cmd) {
      switch (cmd[0]) {
        case cmdListFiles:
          t.notify(storageData, files == 0 ? [0] : [1, 0x69, 0x18, 0x6E, 0x20, 0, 0, 0x03, 0x70]);
        case cmdReadFile:
          t.notify(storageData, [0]);
          t.notify(storageData, record(1763220000, 1));
          t.notify(storageData, record(1763220001, 2));
        case cmdDeleteFile:
          files = 0;
          t.notify(storageData, [0]);
      }
    };
    final api = FakeApi();
    api.onFile = (b) {
      t.log.add('server ack');
      return {'sha256': sha256Hex(b)};
    };
    await run(t, api);
    expect(t.log, contains(deleteCmd));
    expect(t.log.indexOf('server ack'), lessThan(t.log.indexOf(deleteCmd)));
  });
}
