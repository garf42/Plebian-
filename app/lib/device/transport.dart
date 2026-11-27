// The one seam between the phone's radio (or the Omi simulator) and the Omi
// protocol. Everything above it carries raw characteristic bytes only.
//
// Shape after Omi's DeviceTransport
// (app/lib/services/devices/transports/device_transport.dart, MIT License,
// Copyright (c) 2024 Based Hardware Contributors), trimmed to what Plebian uses.
import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:universal_ble/universal_ble.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'omi_protocol.dart';

abstract class DeviceTransport {
  Future<void> connect();
  Future<void> disconnect();

  /// true on connect, false on loss.
  Stream<bool> get connection;

  /// The CCC write: notifications on [characteristic] start once this completes.
  Future<void> subscribe(String service, String characteristic);

  /// Every notification's raw bytes on [characteristic].
  Stream<List<int>> notifications(String characteristic);
  Future<List<int>> read(String service, String characteristic);
  Future<void> write(String service, String characteristic, List<int> value);
  Future<void> requestMtu(int mtu);
}

/// The Omi simulator (tests/fixtures/omi, `fixture omi`) standing in for the
/// radio: GET /phone/events is a WebSocket whose opening connects and whose
/// frames carry connected, disconnected and notify; reads, writes, CCC
/// subscriptions and the MTU exchange are POSTs. Values are raw bytes as
/// base64, so every byte above this transport is the phone's own code.
class SimTransport implements DeviceTransport {
  SimTransport(this.base, [http.Client? client]) : _c = client ?? http.Client();
  final Uri base;
  final http.Client _c;
  WebSocketChannel? _ws;
  final _state = StreamController<bool>.broadcast();
  final _notify = StreamController<(String, List<int>)>.broadcast();
  Completer<void>? _up;

  @override
  Stream<bool> get connection => _state.stream;

  @override
  Future<void> connect() async {
    if (_ws != null) return _up!.future;
    final up = _up = Completer<void>();
    final ws = _ws = WebSocketChannel.connect(base.replace(scheme: base.scheme == 'https' ? 'wss' : 'ws', path: '/phone/events'));
    await ws.ready;
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
      }
    }, onDone: () {
      _ws = null;
      _state.add(false);
    });
    return up.future;
  }

  @override
  Future<void> disconnect() async {
    await _ws?.sink.close();
    _ws = null;
  }

  Future<Map<String, dynamic>> _post(String path, Map<String, Object> body) async {
    final r = await _c.post(base.replace(path: path), headers: {'Content-Type': 'application/json'}, body: jsonEncode(body));
    if (r.statusCode != 200) throw StateError('${r.statusCode} ${r.body}');
    return jsonDecode(r.body) as Map<String, dynamic>;
  }

  @override
  Future<void> subscribe(String service, String characteristic) => _post('/phone/subscribe', {'char': characteristic.toLowerCase()});

  @override
  Stream<List<int>> notifications(String characteristic) {
    final c = characteristic.toLowerCase();
    return _notify.stream.where((n) => n.$1 == c).map((n) => n.$2);
  }

  @override
  Future<List<int>> read(String service, String characteristic) async =>
      base64Decode((await _post('/phone/read', {'char': characteristic.toLowerCase()}))['value'] as String? ?? '');

  @override
  Future<void> write(String service, String characteristic, List<int> value) =>
      _post('/phone/write', {'char': characteristic.toLowerCase(), 'value': base64Encode(value)});

  @override
  Future<void> requestMtu(int mtu) => _post('/phone/mtu', {'mtu': mtu});
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

  bool _known = false;

  /// After the first link, Android's autoConnect waits for the Omi to come back in range.
  @override
  Future<void> connect() async {
    await UniversalBle.connect(deviceId, autoConnect: _known);
    _known = true;
    await UniversalBle.discoverServices(deviceId);
  }

  @override
  Future<void> disconnect() => UniversalBle.disconnect(deviceId);

  @override
  Future<void> subscribe(String service, String characteristic) =>
      UniversalBle.subscribeNotifications(deviceId, service, characteristic);

  @override
  Stream<List<int>> notifications(String characteristic) => UniversalBle.characteristicValueStream(deviceId, characteristic);

  @override
  Future<List<int>> read(String service, String characteristic) =>
      UniversalBle.read(deviceId, service, characteristic);

  @override
  Future<void> write(String service, String characteristic, List<int> value) =>
      UniversalBle.write(deviceId, service, characteristic, Uint8List.fromList(value));

  @override
  Future<void> requestMtu(int mtu) => UniversalBle.requestMtu(deviceId, mtu);
}
