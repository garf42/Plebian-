// What the screens share: the thread as the event stream delivers it, the
// composer and its quote, the Omi chip, presence, and a revision that makes
// every loaded projection refetch when something lands.
import 'dart:async';

import 'package:flutter/widgets.dart';

import 'api.dart';
import 'device/transport.dart';
import 'link.dart';
import 'services.dart';

class Quote {
  const Quote(this.kind, this.id, this.text);
  final String kind; // heard | draft | question | tap | line
  final String id, text;
  Json get json => {'kind': kind, 'id': id};
}

/// Pairs and reconnects the Omi; the simulator stands in for the radio when
/// the build sets OMI_SIM.
class OmiPairing {
  OmiPairing(this.link, this.vault,
      {this.sim, Stream<(String, String)> Function()? scan, DeviceTransport Function(String id)? radio, Future<void> Function()? stopScan})
      : scan = scan ?? (() => BleTransport.scan().map((d) => (d.deviceId, d.name ?? d.deviceId))),
        radio = radio ?? BleTransport.new,
        stopScan = stopScan ?? BleTransport.stopScan;
  final Link link;
  final Vault vault;
  final Uri? sim;
  final Stream<(String, String)> Function() scan;
  final DeviceTransport Function(String id) radio;
  final Future<void> Function() stopScan;

  Future<void> resume() async {
    final id = await vault.read('omi');
    if (sim != null) return attachSim();
    if (id != null) await _attach(radio(id));
  }

  Future<void> attachSim() => _attach(SimTransport(sim!));

  Future<void> choose(String id) async {
    await stopScan().catchError((_) {});
    await vault.write('omi', id);
    await _attach(radio(id));
  }

  Future<void> _attach(DeviceTransport t) async {
    try {
      await link.attachOmi(t);
    } catch (_) {
      // Out of range: the chip says so from the server's omi state.
    }
  }
}

class AppModel extends ChangeNotifier {
  AppModel({required this.api, required this.link, required this.signer, required this.omi, required this.player, required this.vault});
  final Api api;
  final Link link;
  final Signer signer;
  final OmiPairing omi;
  final Player player;
  final Vault vault;

  final composer = TextEditingController();
  final composerFocus = FocusNode();
  Quote? quote;
  final thread = <Json>[];
  final _awaiting = <Json>[];
  final freshTaps = <String>{};
  Json? omiState;
  bool working = false;
  int revision = 0;
  int tab = 0;
  String? focus;
  final _subs = <StreamSubscription>[];

  static AppModel of(BuildContext c) => maybeOf(c)!;
  static AppModel? maybeOf(BuildContext c) => c.dependOnInheritedWidgetOfExactType<Scope>()?.notifier;

  Future<void> load() async {
    _subs
      ..add(link.events.listen(on))
      ..add(link.changed.listen((_) => notifyListeners()))
      ..add(link.sent.stream.listen((s) {
        if (s.$1['path'] == '/v1/messages') _awaiting.add({...s.$1['body'] as Json, 'id': (s.$2 as Json?)?['id']});
      }));
    try {
      final ev = ((await api.get('/v1/thread'))['events'] as List).cast<Json>();
      for (final e in ev) {
        _upsert(e);
      }
    } catch (_) {}
    notifyListeners();
  }

  @override
  void dispose() {
    for (final s in _subs) {
      s.cancel();
    }
    composer.dispose();
    composerFocus.dispose();
    super.dispose();
  }

  static String? _merge(Json e) => switch (e['type']) {
        'draft' => 'd:${e['draft_id']}',
        'question' => 'q:${e['question_id']}',
        _ => null,
      };

  void _upsert(Json e) {
    if (thread.any((x) => x['id'] == e['id'])) return;
    final k = _merge(e);
    final i = k == null ? -1 : thread.indexWhere((x) => _merge(x) == k);
    i >= 0 ? thread[i] = e : thread.add(e);
  }

  /// One event from the server's stream.
  void on(Json e) {
    switch (e['type']) {
      case 'message' || 'draft' || 'question' || 'step':
        _upsert(e);
        if (e['type'] == 'message' && e['from'] == 'owner') {
          _awaiting.removeWhere((a) => a['client_msg_id'] == e['client_msg_id']);
        }
      case 'tap':
        freshTaps.add(e['at'] as String);
      case 'omi':
        omiState = e;
      case 'presence':
        working = e['working'] == true;
    }
    revision++;
    notifyListeners();
  }

  /// Owner messages not yet echoed by the stream: (text, status).
  List<(String, String?)> get pending => [
        for (final a in _awaiting) (a['text'] as String, null),
        for (final e in link.outbox.items)
          if (e['path'] == '/v1/messages') ((e['body'] as Json)['text'] as String, 'queued'),
        for (final e in link.outbox.refused)
          if (e['path'] == '/v1/messages') ((e['body'] as Json)['text'] as String, 'refused'),
      ];

  /// Tapping an item quotes it into the composer on Today; the owner answers
  /// in their own words.
  void quoteItem(String kind, String id, String text) {
    quote = Quote(kind, id, text);
    tab = 0;
    notifyListeners();
    composerFocus.requestFocus();
  }

  /// Refetch every loaded projection (after an action of the owner's).
  void refresh() {
    revision++;
    notifyListeners();
  }

  void setTab(int i) {
    tab = i;
    notifyListeners();
  }

  void clearQuote() {
    quote = null;
    notifyListeners();
  }

  Future<void> send() async {
    final text = composer.text.trim();
    if (text.isEmpty) return;
    final q = quote;
    composer.clear();
    quote = null;
    notifyListeners();
    await link.outbox.message(text, q?.json);
  }

  /// Long-press on any line: a tap at that line's time.
  Future<void> makeTap(String at, Json? line) =>
      link.outbox.add('POST', '/v1/taps', {'at': at, 'how': 'app', 'line': line});

  /// A notification was tapped: Today, at that message.
  void openAt(String eventId) {
    tab = 0;
    focus = eventId;
    notifyListeners();
  }
}

class Scope extends InheritedNotifier<AppModel> {
  const Scope({super.key, required AppModel model, required super.child}) : super(notifier: model);
}
