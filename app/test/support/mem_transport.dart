// An Omi in memory behind the device seam: tests push raw characteristic
// bytes in and read what the phone wrote. Everything above it is app code.
import 'dart:async';

import 'package:plebian/device/omi_protocol.dart';
import 'package:plebian/device/transport.dart';

class MemTransport implements DeviceTransport {
  MemTransport({this.firmware = '3.0.20', this.codec = codecOpus, this.battery = 80});
  String firmware;
  int codec, battery;
  int deviceClockS = 0;
  final writes = <(String, List<int>)>[];
  final _state = StreamController<bool>.broadcast();
  final _notify = StreamController<(String, List<int>)>.broadcast();

  /// Answers a write on the storage data characteristic with notifications.
  void Function(List<int> cmd)? onStorage;
  List<int> storageList = const [];

  /// Every event in order across writes and the test's own markers.
  final log = <String>[];

  @override
  Stream<bool> get connection => _state.stream;

  void setConnected(bool up) => _state.add(up);

  @override
  Future<void> connect() async {}

  @override
  Future<void> disconnect() async => _state.add(false);

  final subscribed = <String>[];

  @override
  Future<void> subscribe(String service, String characteristic) async => subscribed.add(characteristic);

  @override
  Stream<List<int>> notifications(String characteristic) =>
      _notify.stream.where((n) => n.$1 == characteristic).map((n) => n.$2);

  void notify(String characteristic, List<int> value) => _notify.add((characteristic, value));

  @override
  Future<List<int>> read(String service, String c) async => switch (c) {
        firmwareRevision => firmware.codeUnits,
        audioCodec => [codec],
        batteryLevel => [battery],
        timeRead => u32le(deviceClockS),
        storageControl => storageList,
        _ => throw StateError('no characteristic $c'),
      };

  @override
  Future<void> write(String service, String c, List<int> value) async {
    writes.add((c, List.of(value)));
    if (c == timeWrite) deviceClockS = readU32le(value);
    if (c == storageData) {
      log.add('device write ${value.first}');
      scheduleMicrotask(() => onStorage?.call(value));
    }
  }

  @override
  Future<void> requestMtu(int mtu) async {}

  List<List<int>> writesTo(String c) => [for (final w in writes) if (w.$1 == c) w.$2];
}

/// Splits [frames] into `[packet u16 LE][index][payload]` notifications for
/// an ATT MTU of [mtu] (payload = mtu - 3 ATT - 3 header), as the firmware
/// pusher does, numbering packets from [first] (wrapping at 2^16).
List<List<int>> fragment(List<List<int>> frames, {required int mtu, int first = 0}) {
  final out = <List<int>>[];
  var packet = first;
  final size = mtu - 6;
  for (final f in frames) {
    var index = 0;
    for (var o = 0; o < f.length; o += size) {
      final end = o + size < f.length ? o + size : f.length;
      out.add([packet & 0xFF, (packet >> 8) & 0xFF, index, ...f.sublist(o, end)]);
      packet = (packet + 1) & 0xFFFF;
      index++;
    }
  }
  return out;
}
