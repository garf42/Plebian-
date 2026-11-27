// Journal (Days, Taps, Search), Open, the Day page and the Tap screen.
import 'dart:async';
import 'dart:typed_data';

import 'package:material_ui/material_ui.dart';

import '../api.dart';
import '../model.dart';
import '../services.dart';
import '../strings.dart';
import 'common.dart';
import 'kit.dart';

class JournalView extends StatefulWidget {
  const JournalView({super.key});
  @override
  State<JournalView> createState() => _JournalViewState();
}

class _JournalViewState extends State<JournalView> {
  bool _taps = false;
  String _q = '';
  final _search = TextEditingController();

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  void _show({required bool taps}) => setState(() {
        _taps = taps;
        _q = '';
        _search.clear();
      });

  Widget _seg(String label, bool on, VoidCallback tap) => Expanded(
        child: Act(
          label: label,
          selected: on,
          onTap: tap,
          child: Container(
            padding: const EdgeInsets.symmetric(vertical: 7),
            alignment: Alignment.center,
            decoration: BoxDecoration(color: on ? Colors.white : Colors.transparent, borderRadius: BorderRadius.circular(8)),
            child: Text(label, style: T.body(12.5, color: on ? C.ink : C.ink2, w: FontWeight.w600)),
          ),
        ),
      );

  @override
  Widget build(BuildContext context) {
    final body = _q.isNotEmpty
        ? Loaded('/v1/search', query: {'q': _q}, builder: (c, r) => _Results(r))
        : _taps
            ? Loaded('/v1/taps', builder: (c, r) => Column(children: [for (final t in (r['taps'] as List).cast<Json>()) TapRow(t, withDay: true)]))
            : Loaded('/v1/days', builder: (c, r) => Column(children: [for (final d in (r['days'] as List).cast<Json>()) _DayCard(d)]));
    return ListView(padding: const EdgeInsets.fromLTRB(12, 12, 12, 18), children: [
      Container(
        padding: const EdgeInsets.all(3),
        decoration: BoxDecoration(
            color: const Color.fromRGBO(255, 255, 255, .65), border: Border.all(color: const Color.fromRGBO(0, 0, 0, .06)), borderRadius: BorderRadius.circular(10)),
        child: Row(children: [
          _seg(S.days, !_taps && _q.isEmpty, () => _show(taps: false)),
          _seg(S.taps, _taps && _q.isEmpty, () => _show(taps: true)),
        ]),
      ),
      const SizedBox(height: 10),
      TextField(
        controller: _search,
        textInputAction: TextInputAction.search,
        onSubmitted: (v) => setState(() => _q = v.trim()),
        style: T.body(13),
        decoration: InputDecoration(
          isDense: true,
          labelText: S.search,
          floatingLabelBehavior: FloatingLabelBehavior.never,
          prefixIcon: const Icon(Icons.search, size: 18, color: C.ink3),
          filled: true,
          fillColor: const Color.fromRGBO(255, 255, 255, .7),
          border: OutlineInputBorder(borderRadius: BorderRadius.circular(10), borderSide: const BorderSide(color: Color.fromRGBO(0, 0, 0, .06))),
        ),
      ),
      const SizedBox(height: 10),
      body,
    ]);
  }
}

class _DayCard extends StatelessWidget {
  const _DayCard(this.d);
  final Json d;
  @override
  Widget build(BuildContext context) {
    final date = d['date'] as String?;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Act(
        label: '${weekday(date)} ${monthDay(date)}',
        onTap: () => Navigator.of(context).push(lay(DayPage(date ?? ''))),
        child: PaperCard(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Wrap(crossAxisAlignment: WrapCrossAlignment.end, spacing: 8, children: [
              Text(weekday(date), style: T.display(18)),
              Text(monthDay(date), style: T.meta),
            ]),
            const SizedBox(height: 7),
            Wrap(spacing: 6, runSpacing: 6, children: [
              Tag('${d['heard'] ?? 0} ${S.heard}'),
              if ((d['taps'] ?? 0) != 0) Tag('${d['taps']} ${d['taps'] == 1 ? S.tap : S.tapsCount}', ore: true),
              if ((d['messages'] ?? 0) != 0) Tag('${d['messages']} ${S.messages}'),
              if ((d['open'] ?? 0) != 0) Tag('${d['open']} ${S.openCount}'),
            ]),
          ]),
        ),
      ),
    );
  }
}

class TapRow extends StatelessWidget {
  const TapRow(this.t, {super.key, this.withDay = false});
  final Json t;
  final bool withDay;
  @override
  Widget build(BuildContext context) {
    final at = when(t['at']);
    final day = at == null ? '' : S.weekdays[at.weekday - 1].substring(0, 3);
    final title = t['title'] as String? ?? '';
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Act(
        label: '${S.tapped}${S.dot}${clock(t['at'])}${S.dot}$title',
        onTap: () => Navigator.of(context).push(lay(TapPage(t['id'] as String))),
        child: PaperCard(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            const Padding(padding: EdgeInsets.only(top: 5, right: 9), child: TapDot()),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text((withDay ? '$day${S.dot}${clock(t['at'])}' : clock(t['at'])).toUpperCase(), style: T.meta),
                Text(title, style: T.line),
              ]),
            ),
          ]),
        ),
      ),
    );
  }
}

/// Search over both bodies, counted per body: the thread and the heard day.
class _Results extends StatelessWidget {
  const _Results(this.r);
  final Json r;
  @override
  Widget build(BuildContext context) {
    Widget body(String name, Json? b) {
      final hits = (b?['hits'] as List? ?? const []).cast<Json>();
      final span = [clock(b?['first']), clock(b?['last'])].where((s) => s.isNotEmpty).join(' – ');
      return PaperCard(
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('$name${S.dot}${b?['count'] ?? 0}${span.isEmpty ? '' : '${S.dot}$span'}'.toUpperCase(), style: T.kicker),
          for (final h in hits)
            Act(
              label: h['text'] as String? ?? '',
              onTap: () {
                final t = when(h['at']);
                if (t != null) Navigator.of(context).push(lay(DayPage(ymd(t.subtract(const Duration(hours: 4))))));
              },
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 5),
                child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  SizedBox(width: 62, child: Text(clock(h['at']), style: T.mono(10))),
                  Expanded(
                    child: Text.rich(TextSpan(children: [
                      if (h['who'] != null) TextSpan(text: '${h['who']} ', style: T.mono(10, color: C.teal)),
                      TextSpan(text: h['text'] as String? ?? '', style: T.line),
                    ])),
                  ),
                ]),
              ),
            ),
        ]),
      );
    }

    return Column(children: [body(S.withPlebian, r['thread'] as Json?), const SizedBox(height: 10), body(S.heardTitle, r['heard'] as Json?)]);
  }
}

/// What waits on the owner. Tapping an item quotes it; only the owner's words close one.
class OpenView extends StatelessWidget {
  const OpenView({super.key});
  @override
  Widget build(BuildContext context) => Loaded('/v1/open', builder: (c, o) {
        Widget section(String name, String key, String kind) {
          final items = (o[key] as List? ?? const []).cast<Json>();
          return PaperCard(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('$name${S.dot}${items.length}'.toUpperCase(), style: T.kicker),
              for (final it in items)
                Act(
                  label: it['text'] as String? ?? '',
                  onTap: () => quoteAndGo(c, kind, it['id'] as String, it['text'] as String? ?? ''),
                  child: Container(
                    width: double.infinity,
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: C.rule))),
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Text(it['text'] as String? ?? '', style: T.line),
                      if (it['shown'] != null) Padding(padding: const EdgeInsets.only(top: 4), child: Text(it['shown'] as String, style: T.mono(11.5, color: C.ink2))),
                      Text(clock(it['at']), style: T.mono(10)),
                    ]),
                  ),
                ),
            ]),
          );
        }

        return Cards(children: [
          section(S.questions, 'questions', 'question'),
          section(S.drafts, 'drafts', 'draft'),
          // Kept orders are quoted as lines: the quote kinds have no order kind.
          section(S.orders, 'orders', 'line'),
        ]);
      });
}

/// A day: entry on top, With Plebian and its taps, the heard day beneath (peek).
class DayPage extends StatelessWidget {
  const DayPage(this.date, {super.key});
  final String date;
  @override
  Widget build(BuildContext context) => PageFrame(
        title: weekday(date),
        child: Loaded('/v1/days/$date', builder: (c, p) => _Page(p)),
      );
}

class _Page extends StatefulWidget {
  const _Page(this.p);
  final Json p;
  @override
  State<_Page> createState() => _PageState();
}

class _PageState extends State<_Page> {
  bool _peek = false;
  double _h = 0;
  final _top = GlobalKey();

  void _measure() => WidgetsBinding.instance.addPostFrameCallback((_) {
        final h = _top.currentContext?.size?.height ?? 0;
        if (mounted && h != _h) setState(() => _h = h);
      });

  @override
  Widget build(BuildContext context) {
    _measure();
    final p = widget.p;
    final heard = (p['heard'] as List? ?? const []).cast<Json>();
    final thread = (p['thread'] as List? ?? const []).cast<Json>();
    final taps = (p['taps'] as List? ?? const []).cast<Json>();
    final entry = (p['entry'] as List? ?? const []).cast<Json>();
    final reduced = M.reduced(context);
    final drop = _peek && _h > 46 ? _h - 46 : 0.0;
    return Cards(children: [
      PaperCard(
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('${monthDay(p['date'])}${S.dot}${heard.length} ${S.heard}'.toUpperCase(), style: T.meta),
          const SizedBox(height: 8),
          Ribbon(ribbon: p['ribbon'] as Json?, cut: when(p['cut']), big: true, fresh: AppModel.of(context).freshTaps),
          if (entry.isNotEmpty) ...[Kicker(S.entry), for (final l in entry) EntryLine(l)],
        ]),
      ),
      ClipRRect(
        borderRadius: BorderRadius.circular(14),
        child: Stack(children: [
          Positioned.fill(
            child: Container(
              decoration: BoxDecoration(
                  color: C.heardUnder, border: Border.all(color: C.rule2), borderRadius: BorderRadius.circular(14)),
              child: ListView(padding: const EdgeInsets.all(12), children: [
                Text(S.heardTitle.toUpperCase(), style: T.kicker),
                for (final r in heard) HeardRowView(r),
              ]),
            ),
          ),
          AnimatedContainer(
            duration: reduced ? M.fade : M.peek,
            curve: M.ease,
            transform: Matrix4.translationValues(0, drop, 0)..rotateZ(_peek && !reduced ? .6 * 3.14159265 / 180 : 0),
            child: Container(
              key: _top,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(color: C.paper, borderRadius: BorderRadius.circular(14), boxShadow: C.shadow),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(S.withPlebian.toUpperCase(), style: T.kicker),
                for (final e in thread)
                  if (e['type'] == 'message')
                    Act(
                      label: e['text'] as String? ?? '',
                      onTap: () => quoteAndGo(context, 'line', e['id'] as String, e['text'] as String? ?? ''),
                      onLongPress: () => AppModel.of(context).makeTap(e['at'] as String, {'kind': 'event', 'id': e['id']}),
                      child: Container(
                        width: double.infinity,
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: C.rule))),
                        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                          Text('${clock(e['at'])}${S.dot}${e['from'] == 'owner' ? S.you : S.plebian}', style: T.mono(10)),
                          Text(e['text'] as String? ?? '', style: T.body(13)),
                        ]),
                      ),
                    ),
                Kicker(S.taps),
                for (final t in taps) TapRow(t),
                const SizedBox(height: 6),
                Pill(S.heardTitle, selected: _peek, onTap: () => setState(() => _peek = !_peek)),
              ]),
            ),
          ),
        ]),
      ),
    ]);
  }
}

/// One tap: the dot, day and time, how it was made, its line, the stretch's
/// audio, the verbatim talk around it, and the owner's note.
class TapPage extends StatelessWidget {
  const TapPage(this.id, {super.key});
  final String id;
  @override
  Widget build(BuildContext context) => PageFrame(title: S.tap, child: Loaded('/v1/taps/$id', builder: (c, t) => _Tap(t)));
}

class _Tap extends StatefulWidget {
  const _Tap(this.t);
  final Json t;
  @override
  State<_Tap> createState() => _TapState();
}

class _TapState extends State<_Tap> {
  late final _note = TextEditingController(text: widget.t['note'] as String? ?? '');

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final t = widget.t;
    final at = when(t['at']);
    final stretch = t['stretch'] as Json?;
    final title = t['title'] as String? ?? '';
    return Cards(children: [
      PaperCard(
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            const TapDot(),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                  '${at == null ? '' : S.weekdays[at.weekday - 1]}${S.dot}${clock(t['at'])}${S.dot}${t['how'] == 'omi' ? S.tappedOnOmi : S.pressedInApp}'
                      .toUpperCase(),
                  style: T.meta),
            ),
          ]),
          const SizedBox(height: 6),
          Act(
            label: '${S.quote}${S.dot}$title',
            onTap: () => quoteAndGo(context, 'tap', t['id'] as String, title),
            child: SizedBox(width: double.infinity, child: Text(title, style: T.body(15))),
          ),
          if (stretch?['blob'] is String) StretchPlayer(stretch!['blob'] as String),
        ]),
      ),
      if (stretch != null)
        PaperCard(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(S.heardTitle.toUpperCase(), style: T.kicker),
            HeardRowView(stretch, highlightAt: at),
          ]),
        ),
      PaperCard(
        child: Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
          Expanded(
            child: TextField(
              controller: _note,
              minLines: 1,
              maxLines: 4,
              style: T.line,
              decoration: InputDecoration(labelText: S.note, labelStyle: T.meta, border: InputBorder.none),
            ),
          ),
          Pill(S.save, primary: true, onTap: () => m.link.outbox.add('PATCH', '/v1/taps/${t['id']}', {'note': _note.text})),
        ]),
      ),
    ]);
  }
}

/// The stretch's audio: a waveform of its Opus packet sizes and a playhead.
class StretchPlayer extends StatefulWidget {
  const StretchPlayer(this.sha, {super.key});
  final String sha;
  @override
  State<StretchPlayer> createState() => _StretchPlayerState();
}

class _StretchPlayerState extends State<StretchPlayer> {
  Uint8List? _ogg;
  List<int> _sizes = const [];
  bool _playing = false;
  double _at = 0;
  StreamSubscription? _sub;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_ogg != null) return;
    final m = AppModel.of(context);
    _sub ??= m.player.progress.listen((v) => mounted ? setState(() => _at = v) : null);
    m.api.bytes('/v1/blobs/${widget.sha}').then((b) {
      if (mounted) {
        setState(() {
          _ogg = b;
          _sizes = oggPacketSizes(b);
        });
      }
    }, onError: (_) {});
  }

  @override
  void dispose() {
    _sub?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Row(children: [
        Act(
          label: _playing ? S.pause : S.play,
          onTap: () {
            final ogg = _ogg;
            setState(() => _playing = !_playing);
            if (!_playing) {
              m.player.pause();
            } else if (ogg != null) {
              m.player.play(ogg);
            }
          },
          child: Container(
            width: 34,
            height: 34,
            decoration: const BoxDecoration(color: C.forest, shape: BoxShape.circle),
            child: Icon(_playing ? Icons.pause : Icons.play_arrow, color: Colors.white, size: 20),
          ),
        ),
        const SizedBox(width: 9),
        Expanded(child: SizedBox(height: 26, child: CustomPaint(painter: _Wave(_sizes, _at)))),
      ]),
    );
  }
}

class _Wave extends CustomPainter {
  _Wave(this.sizes, this.at);
  final List<int> sizes;
  final double at;
  @override
  void paint(Canvas canvas, Size s) {
    const bars = 60; // the mockup's waveform: 60 bars
    final p = Paint()..color = C.ink3;
    if (sizes.isNotEmpty) {
      final per = (sizes.length / bars).ceil();
      final buckets = [for (var i = 0; i < sizes.length; i += per) sizes.skip(i).take(per).reduce((a, b) => a + b) / per];
      final top = buckets.reduce((a, b) => a > b ? a : b);
      final w = s.width / buckets.length;
      for (var i = 0; i < buckets.length; i++) {
        final h = top == 0 ? 1.0 : (buckets[i] / top) * s.height;
        canvas.drawRect(Rect.fromLTWH(i * w, (s.height - h) / 2, w * .6, h), p);
      }
    }
    canvas.drawRect(Rect.fromLTWH(at * s.width, 0, 2, s.height), Paint()..color = C.ore);
  }

  @override
  bool shouldRepaint(_Wave o) => o.sizes != sizes || o.at != at;
}
