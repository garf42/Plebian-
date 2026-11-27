// Omi, This week (brief, health, spend, Setup cards), the Setup forms,
// pairing, and the browser live view.
import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../api.dart';
import '../model.dart';
import '../services.dart';
import '../strings.dart';
import 'common.dart';
import 'kit.dart';

class _Row extends StatelessWidget {
  const _Row(this.k, this.v);
  final String k, v;
  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          SizedBox(width: 96, child: Text(k.toUpperCase(), style: T.kicker)),
          Expanded(child: Text(v, style: T.line)),
        ]),
      );
}

/// The Omi as the server last heard of it.
class OmiPage extends StatelessWidget {
  const OmiPage({super.key});
  @override
  Widget build(BuildContext context) {
    final o = AppModel.of(context).omiState ?? const {};
    final state = switch (o['state']) { 'live' => S.live, 'syncing' => S.syncing, 'out_of_range' => S.outOfRange, _ => '' };
    return PageFrame(
      title: S.omi,
      child: Cards(children: [
        PaperCard(
          child: Column(children: [
            _Row(S.state, state),
            _Row(S.battery, o['battery'] == null ? '' : '${o['battery']}%'),
            _Row(S.firmware, '${o['firmware'] ?? ''}'),
            _Row(S.waiting, '${o['waiting_files'] ?? ''}'),
          ]),
        ),
      ]),
    );
  }
}

/// This week: the brief by lane, health, spend, and the Setup cards.
class WeekPage extends StatelessWidget {
  const WeekPage({super.key});

  static const _lanes = {'breakage': S.breakage, 'security': S.security, 'ideas': S.ideas, 'standing': S.standing};

  Future<void> _act(BuildContext context, String path, Json body) async {
    final m = AppModel.of(context);
    try {
      await signed(m.api, m.signer, path, body);
    } catch (_) {}
    m.refresh();
  }

  @override
  Widget build(BuildContext context) => PageFrame(
        title: S.week,
        child: Loaded('/v1/week', builder: (c, w) {
          final brief = (w['brief'] as List? ?? const []).cast<Json>();
          final health = (w['health'] as Json?) ?? const {};
          final spend = (w['spend'] as List? ?? const []).cast<Json>();
          return Cards(children: [
            PaperCard(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                for (final MapEntry(key: lane, value: name) in _lanes.entries) ...[
                  Padding(
                    padding: const EdgeInsets.only(top: 6, bottom: 2),
                    child: Text('$name  ${brief.where((b) => b['lane'] == lane).length}', style: T.display(15)),
                  ),
                  for (final b in brief.where((b) => b['lane'] == lane))
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      decoration: const BoxDecoration(border: Border(top: BorderSide(color: C.rule))),
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                        Text(b['text'] as String? ?? '', style: T.body(13)),
                        if ((b['replay'] ?? '') != '') Text('${S.replay}${S.dot}${b['replay']}', style: T.body(12, color: C.ink3)),
                        const SizedBox(height: 5),
                        Wrap(spacing: 6, crossAxisAlignment: WrapCrossAlignment.center, children: [
                          if (lane == 'standing')
                            Pill(S.grant, primary: true, onTap: () => _act(c, '/v1/admin/grant', {'step': b['id']}))
                          else
                            Pill(S.approve, primary: true, onTap: () => _act(c, '/v1/admin/approve', {'item': b['id']})),
                          if (b['state'] == 'ready') Pill(S.deploy, onTap: () => _act(c, '/v1/admin/deploy', {'candidate': b['id']})),
                          Tag('${b['state'] ?? ''}'),
                        ]),
                      ]),
                    ),
                ],
              ]),
            ),
            PaperCard(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(S.health.toUpperCase(), style: T.kicker),
                for (final e in health.entries) _Row(e.key, '${e.value}'),
              ]),
            ),
            PaperCard(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(S.spend.toUpperCase(), style: T.kicker),
                for (final s in spend)
                  _Row('${s['day'] ?? ''}',
                      '${s['model'] ?? ''}${S.dot}${s['in'] ?? 0} ${S.tokensIn}${S.dot}${s['out'] ?? 0} ${S.tokensOut}${S.dot}${s['cached'] ?? 0} ${S.cached}'),
              ]),
            ),
            Text(S.setup.toUpperCase(), style: T.kicker),
            Loaded('/v1/setup', builder: (c, s) => Column(children: [for (final card in (s['cards'] as List).cast<Json>()) SetupCard(card)])),
          ]);
        }),
      );
}

const _names = {
  'server': S.server, 'pair': S.pair, 'model': S.model, 'omi': S.omi, 'mail': S.mail, 'calendar': S.calendar,
  'texts': S.texts, 'storage': S.storage, 'backups': S.backups, 'updates': S.updates,
};

/// The Setup forms: the fields each card posts and the endpoint it posts to.
const _forms = <String, (String, List<(String, String, bool)>)>{
  'model': ('/v1/setup/model', [('base_url', S.endpoint, false), ('api_key', S.key, true)]),
  'mail': ('/v1/setup/mail', [('imap', S.imap, false), ('smtp', S.smtp, false), ('user', S.user, false), ('password', S.password, true)]),
  'calendar': ('/v1/setup/calendar', [('url', S.url, true)]),
  'storage': ('/v1/setup/storage', [('endpoint', S.endpoint, false), ('bucket', S.bucket, false), ('key', S.key, false), ('secret', S.secret, true), ('region', S.region, false)]),
  'backups': ('/v1/setup/backups', [('second_disk', S.secondDisk, false), ('offsite', S.offsite, false)]),
  'updates': ('/v1/setup/updates', [('github_token', S.githubToken, true), ('mechanic', S.mechanic, false)]),
};

/// One Setup card: name, status, Required, and its one action.
class SetupCard extends StatefulWidget {
  const SetupCard(this.card, {super.key});
  final Json card;
  @override
  State<SetupCard> createState() => _SetupCardState();
}

class _SetupCardState extends State<SetupCard> {
  String? _got; // what the action returned: a commit, an emoji

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final id = widget.card['id'] as String;
    final (String label, VoidCallback act) = switch (id) {
      'server' => (S.check, () async {
          try {
            final v = await m.api.get('/v1/version') as Json;
            setState(() => _got = '${S.version} ${v['version']}${S.dot}${S.commit} ${v['commit']}');
          } catch (_) {
            setState(() => _got = S.unreachable);
          }
        }),
      'pair' => (S.pair, () => Navigator.of(context).push(lay(PairPage(onPaired: (_) => Navigator.of(context).pop())))),
      'omi' => (S.pair, () => Navigator.of(context).push(lay(const OmiScanPage()))),
      'texts' => (S.connect, () async {
          try {
            final r = await m.api.send('POST', '/v1/setup/texts', json: <String, dynamic>{}) as Json?;
            setState(() => _got = '${S.emoji} ${r?['emoji'] ?? ''}');
          } catch (_) {
            setState(() => _got = S.unreachable);
          }
          m.refresh();
        }),
      _ => (S.connect, () => Navigator.of(context).push(lay(SetupForm(id)))),
    };
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: PaperCard(
        child: Row(children: [
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Wrap(spacing: 8, crossAxisAlignment: WrapCrossAlignment.center, children: [
                Text(_names[id] ?? id, style: T.display(17)),
                if (widget.card['required'] == true) Tag(S.required),
              ]),
              Text('${widget.card['status'] ?? ''}', style: T.meta),
              if (_got != null) SelectableText(_got!, style: T.line),
            ]),
          ),
          Pill(label, primary: true, onTap: act),
        ]),
      ),
    );
  }
}

class SetupForm extends StatefulWidget {
  const SetupForm(this.id, {super.key});
  final String id;
  @override
  State<SetupForm> createState() => _SetupFormState();
}

class _SetupFormState extends State<SetupForm> {
  late final (String, List<(String, String, bool)>) _form = _forms[widget.id]!;
  late final _c = {for (final f in _form.$2) f.$1: TextEditingController()};
  Json? _res;
  String _role = 'main';
  String? _status;

  @override
  void dispose() {
    for (final c in _c.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _submit(AppModel m) async {
    final v = {for (final e in _c.entries) e.key: e.value.text.trim()};
    final body = widget.id == 'storage' ? {'bucket': v.values.every((x) => x.isEmpty) ? null : v} : v;
    try {
      final r = await m.api.send('POST', _form.$1, json: body);
      setState(() => (_res, _status) = (r is Json ? r : null, null));
    } on ApiError catch (e) {
      setState(() => _status = '${S.refused} ${e.status}');
    } catch (_) {
      setState(() => _status = S.unreachable);
    }
    m.refresh();
  }

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final res = _res;
    final models = (res?['models'] as List? ?? const []).cast<Json>();
    return PageFrame(
      title: _names[widget.id] ?? widget.id,
      child: Cards(children: [
        PaperCard(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            for (final (key, label, secret) in _form.$2)
              TextField(controller: _c[key], obscureText: secret, style: T.line, decoration: InputDecoration(labelText: label)),
            const SizedBox(height: 10),
            Row(children: [
              Pill(widget.id == 'model' ? S.connect : S.save, primary: true, onTap: () => _submit(m)),
              const SizedBox(width: 8),
              if (_status != null) Text(_status!, style: T.meta),
            ]),
            if (res?['key'] != null) ...[Kicker(S.backupKey), SelectableText('${res!['key']}', style: T.mono(12, color: C.ink))],
          ]),
        ),
        if (models.isNotEmpty)
          PaperCard(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('${res?['format'] ?? ''}'.toUpperCase(), style: T.kicker),
              Wrap(spacing: 6, children: [
                for (final (r, l) in const [('main', S.roleMain), ('sub', S.roleSub), ('stand_in', S.roleStandIn), ('fallback', S.roleFallback)])
                  Pill(l, primary: _role == r, selected: _role == r, onTap: () => setState(() => _role = r)),
              ]),
              for (final mo in models)
                Act(
                  label: '${mo['id']}',
                  onTap: () async {
                    try {
                      await m.api.send('POST', '/v1/setup/model/select', json: {'provider_id': res?['provider_id'], 'model': mo['id'], 'role': _role});
                      setState(() => _status = '${mo['id']}${S.dot}$_role');
                    } catch (_) {
                      setState(() => _status = S.unreachable);
                    }
                    m.refresh();
                  },
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 7),
                    child: Row(children: [
                      Expanded(child: Text('${mo['id']}', style: T.line)),
                      Text('${mo['context'] ?? ''}', style: T.meta),
                    ]),
                  ),
                ),
            ]),
          ),
      ]),
    );
  }
}

/// Omi pendants in range; choosing one pairs it.
class OmiScanPage extends StatefulWidget {
  const OmiScanPage({super.key});
  @override
  State<OmiScanPage> createState() => _OmiScanPageState();
}

class _OmiScanPageState extends State<OmiScanPage> {
  final _found = <String, String>{};
  StreamSubscription? _sub;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final omi = AppModel.of(context).omi;
    if (_sub != null || omi.sim != null) return;
    _sub = omi.scan().listen((d) => setState(() => _found[d.$1] = d.$2), onError: (_) {});
  }

  @override
  void dispose() {
    _sub?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final omi = AppModel.of(context).omi;
    final entries = omi.sim != null ? {omi.sim.toString(): S.omi} : _found;
    return PageFrame(
      title: S.omi,
      child: Cards(children: [
        for (final MapEntry(key: id, value: name) in entries.entries)
          Act(
            label: name,
            onTap: () {
              omi.sim != null ? omi.attachSim() : omi.choose(id);
              Navigator.of(context).pop();
            },
            child: PaperCard(
              child: Row(children: [
                Expanded(child: Text(name, style: T.display(17))),
                Text(id, style: T.meta),
              ]),
            ),
          ),
      ]),
    );
  }
}

/// Pairing: scan the server's code, or type the same link into Code.
class PairPage extends StatefulWidget {
  const PairPage({super.key, required this.onPaired});
  final void Function(bool) onPaired;
  @override
  State<PairPage> createState() => _PairPageState();
}

class _PairPageState extends State<PairPage> {
  final _code = TextEditingController();
  String? _status;

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  Future<void> _pair(Pairing p) async {
    final parsed = parsePair(_code.text);
    if (parsed == null) return setState(() => _status = S.refused);
    try {
      await pair(p.vault, p.signer, parsed.$1, parsed.$2);
      widget.onPaired(true);
    } on ApiError catch (e) {
      setState(() => _status = '${S.refused} ${e.status}');
    } catch (_) {
      setState(() => _status = S.unreachable);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = Pairing.of(context);
    return PageFrame(
      title: S.pair,
      child: Cards(children: [
        PaperCard(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            TextField(controller: _code, style: T.line, decoration: InputDecoration(labelText: S.code)),
            const SizedBox(height: 10),
            Wrap(spacing: 8, crossAxisAlignment: WrapCrossAlignment.center, children: [
              Pill(S.pair, primary: true, onTap: () => _pair(p)),
              Pill(S.scan, onTap: () async {
                final v = await Navigator.of(context).push<String>(lay(const _ScanPage()));
                if (v != null) {
                  _code.text = v;
                  await _pair(p);
                }
              }),
              if (_status != null) Text(_status!, style: T.meta),
            ]),
          ]),
        ),
      ]),
    );
  }
}

/// What pairing needs, available before there is a server to talk to.
class Pairing extends InheritedWidget {
  const Pairing({super.key, required this.vault, required this.signer, required super.child});
  final Vault vault;
  final Signer signer;
  static Pairing of(BuildContext c) => c.dependOnInheritedWidgetOfExactType<Pairing>()!;
  @override
  bool updateShouldNotify(Pairing old) => false;
}

class _ScanPage extends StatelessWidget {
  const _ScanPage();
  @override
  Widget build(BuildContext context) {
    var done = false;
    return PageFrame(
      title: S.scan,
      child: MobileScanner(onDetect: (cap) {
        final v = cap.barcodes.isEmpty ? null : cap.barcodes.first.rawValue;
        if (v != null && !done && parsePair(v) != null) {
          done = true;
          Navigator.of(context).pop(v);
        }
      }),
    );
  }
}

/// The agent's browser, live: JPEG frames in; taps and keys out while the
/// owner holds the wheel. Nothing typed is shown or kept.
class BrowserPage extends StatefulWidget {
  const BrowserPage({super.key});
  @override
  State<BrowserPage> createState() => _BrowserPageState();
}

class _BrowserPageState extends State<BrowserPage> {
  Sock? _sock;
  Uint8List? _frame;
  Size? _size;
  bool _held = false;
  final _keys = TextEditingController();

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_sock != null) return;
    final s = _sock = AppModel.of(context).api.socket('/v1/browser/live');
    s.messages.listen((f) {
      if (f is List<int> && mounted) {
        final b = Uint8List.fromList(f);
        setState(() => (_frame, _size) = (b, jpegSize(b) ?? _size));
      }
    }, onError: (_) {});
  }

  @override
  void dispose() {
    if (_held) _send({'type': 'give'});
    _sock?.close();
    _keys.dispose();
    super.dispose();
  }

  void _send(Json m) => _sock?.add(jsonEncode(m));

  void _mouse(Offset local, Size box, String event) {
    final s = _size;
    if (!_held || s == null) return;
    _send({'type': 'mouse', 'x': (local.dx / box.width * s.width).round(), 'y': (local.dy / box.height * s.height).round(), 'event': event});
  }

  @override
  Widget build(BuildContext context) {
    final f = _frame, s = _size;
    return PageFrame(
      title: S.browser,
      child: Column(children: [
        Expanded(
          child: Center(
            child: f == null || s == null
                ? const SizedBox.shrink()
                : AspectRatio(
                    aspectRatio: s.width / s.height,
                    child: LayoutBuilder(
                      builder: (_, box) => Semantics(
                        label: S.browser,
                        image: true,
                        child: Listener(
                          onPointerDown: (e) => _mouse(e.localPosition, box.biggest, 'down'),
                          onPointerMove: (e) => _mouse(e.localPosition, box.biggest, 'move'),
                          onPointerUp: (e) => _mouse(e.localPosition, box.biggest, 'up'),
                          child: Image.memory(f, gaplessPlayback: true, fit: BoxFit.fill),
                        ),
                      ),
                    ),
                  ),
          ),
        ),
        Container(
          color: C.paper,
          padding: const EdgeInsets.all(10),
          child: SafeArea(
            top: false,
            child: Row(children: [
              Pill(_held ? S.give : S.take, primary: !_held, selected: _held, onTap: () {
                setState(() => _held = !_held);
                _send({'type': _held ? 'take' : 'give'});
              }),
              const SizedBox(width: 10),
              Expanded(
                child: TextField(
                  controller: _keys,
                  enabled: _held,
                  obscureText: true,
                  autocorrect: false,
                  enableSuggestions: false,
                  decoration: InputDecoration(labelText: S.keys, isDense: true),
                  onChanged: (v) {
                    if (v.isNotEmpty) _send({'type': 'key', 'text': v});
                    _keys.clear();
                  },
                  onSubmitted: (_) => _send({'type': 'key', 'text': '\n'}),
                ),
              ),
            ]),
          ),
        ),
      ]),
    );
  }
}

/// Width and height from a JPEG's start-of-frame marker (ITU T.81, B.2.2).
Size? jpegSize(Uint8List b) {
  var i = 2;
  while (i + 9 < b.length) {
    if (b[i] != 0xFF) return null;
    final marker = b[i + 1], len = (b[i + 2] << 8) | b[i + 3];
    if (marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC) {
      return Size(((b[i + 7] << 8) | b[i + 8]).toDouble(), ((b[i + 5] << 8) | b[i + 6]).toDouble());
    }
    i += 2 + len;
  }
  return null;
}
