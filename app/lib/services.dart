// What the app needs from the phone, behind small interfaces so tests run the
// same code with stand-ins: secure storage, the device key, notifications,
// the background service, and pairing.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:biometric_signature/biometric_signature.dart';
import 'package:flutter/services.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:just_audio/just_audio.dart';

import 'api.dart';
import 'link.dart';
import 'strings.dart';

/// Server address, token and paired Omi, kept in the phone's secure storage.
abstract class Vault {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
}

class SecureVault implements Vault {
  static const _s = FlutterSecureStorage();
  @override
  Future<String?> read(String key) => _s.read(key: key);
  @override
  Future<void> write(String key, String value) => _s.write(key: key, value: value);
}

class MemoryVault implements Vault {
  final m = <String, String>{};
  @override
  Future<String?> read(String key) async => m[key];
  @override
  Future<void> write(String key, String value) async => m[key] = value;
}

/// The device key: ECDSA P-256, made in the phone's secure hardware.
abstract class Signer {
  /// base64 SPKI DER of the public key (made on first call).
  Future<String> publicKey();

  /// base64 ASN.1 ECDSA-SHA256 over [body], behind the owner's fingerprint.
  Future<String> sign(Uint8List body);
}

/// Android Keystore (StrongBox when present); signing needs BiometricPrompt.
class KeystoreSigner implements Signer {
  final _b = BiometricSignature();
  @override
  Future<String> publicKey() async {
    final r = await _b.createKeys(
      config: CreateKeysConfig(
        signatureType: SignatureType.ecdsa,
        enforceBiometric: false,
        requireAuthentication: true,
        setInvalidatedByBiometricEnrollment: true,
        useDeviceCredentials: false,
        enableDecryption: false,
      ),
      keyFormat: KeyFormat.base64,
    );
    final k = r.publicKey;
    if (k == null) throw StateError(r.error ?? 'key');
    return k;
  }

  @override
  Future<String> sign(Uint8List body) async {
    final r = await _b.createSignatureFromBytes(payload: body, signatureFormat: SignatureFormat.base64);
    final s = r.signature;
    if (s == null) throw StateError(r.error ?? 'signature');
    return s;
  }
}

/// Phone notifications for messages the agent starts; tapping one opens Today
/// at that message.
class PhoneNotifier implements Notifier {
  PhoneNotifier(this.onOpen);
  final void Function(String eventId) onOpen;
  final _p = FlutterLocalNotificationsPlugin();
  int _n = 0;

  Future<void> init() async {
    await _p.initialize(
      settings: const InitializationSettings(android: AndroidInitializationSettings('@mipmap/ic_launcher')),
      onDidReceiveNotificationResponse: (r) {
        if (r.payload != null) onOpen(r.payload!);
      },
    );
    await _p.resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()?.requestNotificationsPermission();
    final launch = await _p.getNotificationAppLaunchDetails();
    final id = launch?.notificationResponse?.payload;
    if (launch?.didNotificationLaunchApp == true && id != null) onOpen(id);
  }

  @override
  Future<void> show(Json e) => _p.show(
        id: _n++,
        title: S.plebian,
        body: e['text'] as String? ?? '',
        payload: e['id'] as String,
        notificationDetails: const NotificationDetails(
          android: AndroidNotificationDetails('messages', S.messages, importance: Importance.high, priority: Priority.high),
        ),
      );
}

/// The foreground service (type connectedDevice) that keeps this engine — and
/// with it the Omi link and the server link — alive with the app closed.
abstract final class LinkService {
  static const _ch = MethodChannel('org.plebian.app/service');
  static Future<void> start() async {
    try {
      await _ch.invokeMethod<void>('start');
    } on MissingPluginException {
      // Tests and hosts without the Android service.
    }
  }
}

// --- Pairing ----------------------------------------------------------------------

/// `plebian://pair?addr=HOST:PORT&code=CODE` → (base URL, code); null otherwise.
(Uri, String)? parsePair(String s) {
  final u = Uri.tryParse(s.trim());
  if (u == null || u.scheme != 'plebian' || u.host != 'pair') return null;
  final addr = u.queryParameters['addr'] ?? '', code = u.queryParameters['code'] ?? '';
  final base = Uri.tryParse('http://$addr');
  if (code.isEmpty || base == null || base.host.isEmpty || !base.hasPort || base.path.isNotEmpty) return null;
  return (base, code);
}

/// Registers this phone's device key with the server and keeps the token.
Future<void> pair(Vault vault, Signer signer, Uri base, String code, {http.Client? client}) async {
  final api = HttpApi(base, null, client);
  final res = await api.send('POST', '/v1/pair', json: {
    'code': code,
    'public_key': await signer.publicKey(),
    'name': '${Platform.operatingSystem} ${Platform.localHostname}',
  }) as Json;
  await vault.write('server', base.toString());
  await vault.write('token', res['token'] as String);
  await vault.write('device_id', res['device_id'] as String);
}

/// A signed admin action: the body carries a fresh nonce and time, and the
/// signature covers exactly the bytes sent.
Future<dynamic> signed(Api api, Signer signer, String path, Json body) async {
  final nonce = List.generate(16, (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0')).join();
  final bytes = Uint8List.fromList(utf8.encode(jsonEncode({...body, 'nonce': nonce, 'at': DateTime.now().toUtc().toIso8601String()})));
  final sig = await signer.sign(bytes);
  return api.send('POST', path, raw: bytes, headers: {'X-Plebian-Signature': sig, 'Content-Type': 'application/json'});
}

/// Plays one stretch's original audio (Ogg Opus from /v1/blobs).
abstract class Player {
  Future<void> play(Uint8List ogg);
  Future<void> pause();
  Stream<double> get progress; // 0..1
}

class PhonePlayer implements Player {
  AudioPlayer? _p;
  Uint8List? _loaded;
  final _progress = StreamController<double>.broadcast();
  @override
  Stream<double> get progress => _progress.stream;

  @override
  Future<void> play(Uint8List ogg) async {
    final p = _p ??= AudioPlayer()
      ..positionStream.listen((pos) {
        final d = _p?.duration;
        if (d != null && d.inMilliseconds > 0) _progress.add(pos.inMilliseconds / d.inMilliseconds);
      });
    if (!identical(_loaded, ogg)) {
      await p.setAudioSource(_Bytes(ogg));
      _loaded = ogg;
    }
    await p.play();
  }

  @override
  Future<void> pause() async => _p?.pause();
}

class _Bytes extends StreamAudioSource {
  _Bytes(this.b);
  final Uint8List b;
  @override
  Future<StreamAudioResponse> request([int? start, int? end]) async {
    final s = start ?? 0, e = end ?? b.length;
    return StreamAudioResponse(
        sourceLength: b.length, contentLength: e - s, offset: s, stream: Stream.value(b.sublist(s, e)), contentType: 'audio/ogg');
  }
}

/// Packet sizes of an Ogg Opus stream, read from its page segment tables
/// (RFC 3533): a waveform of the stretch without decoding it. Opus is
/// variable-rate, so louder, busier speech makes bigger packets.
List<int> oggPacketSizes(Uint8List b) {
  final sizes = <int>[];
  var i = 0, run = 0;
  while (i + 27 <= b.length && b[i] == 0x4F && b[i + 1] == 0x67 && b[i + 2] == 0x67 && b[i + 3] == 0x53) {
    final n = b[i + 26];
    if (i + 27 + n > b.length) break;
    var body = 0;
    for (var k = 0; k < n; k++) {
      final lace = b[i + 27 + k];
      body += lace;
      run += lace;
      if (lace < 255) {
        sizes.add(run);
        run = 0;
      }
    }
    i += 27 + n + body;
  }
  return sizes.length > 2 ? sizes.sublist(2) : const []; // after OpusHead and OpusTags
}
