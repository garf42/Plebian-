// The one seam between the phone's radio (or the Omi simulator) and the Omi
// protocol. Everything above it carries raw characteristic bytes only.
//
// Shape after Omi's DeviceTransport
// (app/lib/services/devices/transports/device_transport.dart, MIT License,
// Copyright (c) 2024 Based Hardware Contributors), trimmed to what Plebian uses.
import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:universal_ble/universal_ble.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'omi_protocol.dart';

abstract class DeviceTransport {
  Future<void> connect();
  Future<void> disconnect();

  /// true on connect, false on loss.
  Stream<bool> get connection;

  /// Subscribes and yields every notification's raw bytes.
  Stream<List<int>> notifications(String service, String characteristic);
  Future<List<int>> read(String service, String characteristic);
  Future<void> write(String service, String characteristic, List<int> value);
  Future<void> requestMtu(int mtu);
}

/// The Omi simulator fixture over WebSocket: JSON text frames carrying raw
/// characteristic bytes as base64.
class SimTransport implements DeviceTransport {
  SimTransport(this.uri);
  final Uri uri;
  WebSocketChannel? _ws;
  final _state = StreamController<bool>.broadcast();
  final _notify = StreamController<(String, List<int>)>.broadcast();
  final _reads = <int, Completer<List<int>>>{};
  int _id = 0;

  @override
  Stream<bool> get connection => _state.stream;

  @override
  Future<void> connect() async {
    final ws = WebSocketChannel.connect(uri);
    await ws.ready;
    _ws = ws;
    final up = Completer<void>();
    ws.stream.listen((raw) {
      final m = jsonDecode(raw as String) as Map<String, dynamic>;
      switch (m['op']) {
        case 'connected':
          if (!up.isCompleted) up.complete();
          _state.add(true);
        case 'disconnected':
          _state.add(false);
        case 'notify':
          _notify.add(((m['char'] as String).toLowerCase(), base64Decode(m['value'] as String)));
        case 'read_result':
          _reads.remove(m['id'])?.complete(base64Decode(m['value'] as String));
      }
    }, onDone: () {
      _state.add(false);
      for (final c in _reads.values) {
        c.completeError(StateError('closed'));
      }
      _reads.clear();
    });
    await up.future;
  }

  @override
  Future<void> disconnect() async => _ws?.sink.close();

  void _send(Map<String, Object> m) => _ws!.sink.add(jsonEncode(m));

  @override
  Stream<List<int>> notifications(String service, String characteristic) {
    final c = characteristic.toLowerCase();
    _send({'op': 'subscribe', 'char': c});
    return _notify.stream.where((n) => n.$1 == c).map((n) => n.$2);
  }

  @override
  Future<List<int>> read(String service, String characteristic) {
    final id = ++_id;
    final c = Completer<List<int>>();
    _reads[id] = c;
    _send({'op': 'read', 'id': id, 'char': characteristic.toLowerCase()});
    return c.future;
  }

  @override
  Future<void> write(String service, String characteristic, List<int> value) async =>
      _send({'op': 'write', 'char': characteristic.toLowerCase(), 'value': base64Encode(value)});

  @override
  Future<void> requestMtu(int mtu) async => _send({'op': 'mtu', 'value': mtu});
}

/// The Omi over Bluetooth LE on the phone.
class BleTransport implements DeviceTransport {
  BleTransport(this.deviceId);
  final String deviceId;

  /// Omi pendants advertising the audio service.
  static Stream<BleDevice> scan() {
    UniversalBle.requestPermissions();
    UniversalBle.startScan(scanFilter: ScanFilter(withServices: [omiService]));
    return UniversalBle.scanStream;
  }

  static Future<void> stopScan() => UniversalBle.stopScan();

  @override
  Stream<bool> get connection => UniversalBle.connectionStream(deviceId);

  @override
  Future<void> connect() async {
    await UniversalBle.connect(deviceId);
    await UniversalBle.discoverServices(deviceId);
  }

  @override
  Future<void> disconnect() => UniversalBle.disconnect(deviceId);

  @override
  Stream<List<int>> notifications(String service, String characteristic) {
    UniversalBle.subscribeNotifications(deviceId, service, characteristic);
    return UniversalBle.characteristicValueStream(deviceId, characteristic);
  }

  @override
  Future<List<int>> read(String service, String characteristic) =>
      UniversalBle.read(deviceId, service, characteristic);

  @override
  Future<void> write(String service, String characteristic, List<int> value) =>
      UniversalBle.write(deviceId, service, characteristic, Uint8List.fromList(value));

  @override
  Future<void> requestMtu(int mtu) => UniversalBle.requestMtu(deviceId, mtu);
}
