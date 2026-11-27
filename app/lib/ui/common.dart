// Pieces several screens share: loading a projection, the header, pushed
// pages, and the lines of the thread, the entry and the heard day.
import 'package:flutter/material.dart';

import '../api.dart';
import '../model.dart';
import '../strings.dart';
import 'admin.dart';
import 'kit.dart';

/// Loads [path] and rebuilds with what the server returns; refetches whenever
/// something lands on the event stream.
class Loaded extends StatefulWidget {
  const Loaded(this.path, {super.key, this.query, required this.builder});
  final String path;
  final Map<String, String>? query;
  final Widget Function(BuildContext, Json) builder;
  @override
  State<Loaded> createState() => _LoadedState();
}

class _LoadedState extends State<Loaded> {
  Json? _data;
  bool _failed = false;
  int _rev = -1;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final m = AppModel.of(context);
    if (m.revision != _rev) {
      _rev = m.revision;
      _fetch(m);
    }
  }

  @override
  void didUpdateWidget(Loaded old) {
    super.didUpdateWidget(old);
    if (old.path != widget.path || old.query?.toString() != widget.query?.toString()) _fetch(AppModel.of(context));
  }

  Future<void> _fetch(AppModel m) async {
    try {
      final d = await m.api.get(widget.path, widget.query) as Json;
      if (mounted) setState(() => (_data, _failed) = (d, false));
    } catch (_) {
      if (mounted) setState(() => _failed = true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final d = _data;
    if (d != null) return widget.builder(context, d);
    return _failed ? Padding(padding: const EdgeInsets.all(12), child: Text(S.unreachable, style: T.meta)) : const SizedBox.shrink();
  }
}

/// Wordmark (root) or back + title, the Omi chip, and the gear to This week.
class Header extends StatelessWidget {
  const Header({super.key, this.title});
  final String? title;

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final state = m.omiState?['state'] as String?;
    final status = switch (state) { 'live' => S.live, 'syncing' => S.syncing, 'out_of_range' => S.outOfRange, _ => null };
    return DecoratedBox(
      decoration: const BoxDecoration(gradient: C.ground),
      child: SafeArea(
        bottom: false,
        child: Container(
          constraints: const BoxConstraints(minHeight: 50),
          padding: const EdgeInsets.fromLTRB(14, 4, 12, 4),
          child: Row(children: [
            if (title == null)
              Semantics(label: S.plebian, image: true, child: Image.asset('assets/brand/wordmark.png', height: 23, excludeFromSemantics: true))
            else ...[
              Act(label: S.back, onTap: () => Navigator.of(context).pop(), child: const _Round(Icons.chevron_left)),
              const SizedBox(width: 8),
              Flexible(child: Text(title!, style: T.title.copyWith(color: Colors.white), overflow: TextOverflow.ellipsis)),
            ],
            const Spacer(),
            Act(
              label: status == null ? S.omi : '${S.omi}${S.dot}$status',
              onTap: () => Navigator.of(context).push(lay(const OmiPage())),
              child: _OmiChip(status: status, syncing: state == 'syncing'),
            ),
            if (title == null) ...[
              const SizedBox(width: 8),
              Act(label: S.week, onTap: () => Navigator.of(context).push(lay(const WeekPage())), child: const _Round(Icons.tune)),
            ],
          ]),
        ),
      ),
    );
  }
}

class _Round extends StatelessWidget {
  const _Round(this.icon);
  final IconData icon;
  @override
  Widget build(BuildContext context) => Container(
        width: 28,
        height: 28,
        decoration: const BoxDecoration(color: Color.fromRGBO(255, 255, 255, .14), shape: BoxShape.circle),
        child: Icon(icon, color: Colors.white, size: 18),
      );
}

class _OmiChip extends StatefulWidget {
  const _OmiChip({required this.status, required this.syncing});
  final String? status;
  final bool syncing;
  @override
  State<_OmiChip> createState() => _OmiChipState();
}

class _OmiChipState extends State<_OmiChip> with SingleTickerProviderStateMixin {
  late final _pulse = AnimationController(vsync: this, duration: M.pulse);

  @override
  void dispose() {
    _pulse.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    widget.syncing && !M.reduced(context) ? _pulse.repeat() : _pulse.stop();
    final dot = widget.syncing ? const Color(0xFFF2D49B) : const Color(0xFFCFE3D2);
    return Container(
      padding: const EdgeInsets.fromLTRB(7, 4, 9, 4),
      decoration: BoxDecoration(
        color: const Color.fromRGBO(255, 255, 255, .15),
        border: Border.all(color: const Color.fromRGBO(255, 255, 255, .22)),
        borderRadius: BorderRadius.circular(99),
      ),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        AnimatedBuilder(
          animation: _pulse,
          builder: (_, _) => Container(
            width: 7,
            height: 7,
            decoration: BoxDecoration(color: dot, shape: BoxShape.circle, boxShadow: [
              BoxShadow(color: dot.withValues(alpha: .3 * (1 - _pulse.value)), spreadRadius: 2 + 3 * _pulse.value),
            ]),
          ),
        ),
        const SizedBox(width: 6),
        Text(widget.status == null ? S.omi : '${S.omi}${S.dot}${widget.status}', style: T.body(11, color: Colors.white, w: FontWeight.w500)),
      ]),
    );
  }
}

/// A pushed page: header with back and title over the ground.
class PageFrame extends StatelessWidget {
  const PageFrame({super.key, required this.title, required this.child});
  final String title;
  final Widget child;
  @override
  Widget build(BuildContext context) => Scaffold(
        body: Ground(child: Column(children: [Header(title: title), Expanded(child: child)])),
      );
}

/// A scrolling column of cards with the screen's padding.
class Cards extends StatelessWidget {
  const Cards({super.key, required this.children});
  final List<Widget> children;
  @override
  Widget build(BuildContext context) => ListView(
        padding: const EdgeInsets.fromLTRB(12, 12, 12, 18),
        children: [for (final c in children) Padding(padding: const EdgeInsets.only(bottom: 10), child: c)],
      );
}

/// Quote into the composer on Today, leaving any pushed page.
void quoteAndGo(BuildContext context, String kind, String id, String text) {
  AppModel.of(context).quoteItem(kind, id, text);
  Navigator.of(context).popUntil((r) => r.isFirst);
}

/// One heard row (talk, mail, text, calendar): each line lighter on its
/// dashed rule until the row is settled. Tap quotes; long-press taps.
class HeardRowView extends StatelessWidget {
  const HeardRowView(this.row, {super.key, this.highlightAt});
  final Json row;
  final DateTime? highlightAt;

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final settled = row['settled'] == true;
    final lines = (row['lines'] as List? ?? const []).cast<Json>();
    var here = -1;
    if (highlightAt != null && lines.isNotEmpty) {
      var best = 1 << 62;
      for (var i = 0; i < lines.length; i++) {
        final d = (when(lines[i]['at'])?.difference(highlightAt!).inMilliseconds.abs()) ?? best;
        if (d < best) (best, here) = (d, i);
      }
    }
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      for (final (i, l) in lines.indexed)
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 3),
          child: Act(
            label: '${S.heard}${S.dot}${l['speaker'] ?? ''}${S.dot}${l['text']}',
            onTap: () => quoteAndGo(context, 'heard', row['id'] as String, l['text'] as String? ?? ''),
            onLongPress: () => m.makeTap(l['at'] as String? ?? row['at'] as String, {'kind': 'heard', 'id': row['id']}),
            child: Container(
              width: double.infinity,
              padding: i == here ? const EdgeInsets.symmetric(horizontal: 6, vertical: 4) : EdgeInsets.zero,
              decoration: i == here ? BoxDecoration(color: C.oreTint, borderRadius: BorderRadius.circular(6)) : null,
              child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                SizedBox(width: 62, child: Text(clock(l['at']), style: T.mono(10, color: C.ink3))),
                Expanded(child: HeardText(text: l['text'] as String? ?? '', speaker: l['speaker'] as String?, settled: settled)),
              ]),
            ),
          ),
        ),
    ]);
  }
}

/// A line of the night's entry: held lines sit heard-styled until settled.
class EntryLine extends StatelessWidget {
  const EntryLine(this.line, {super.key});
  final Json line;
  @override
  Widget build(BuildContext context) {
    final text = line['text'] as String? ?? '';
    return Padding(
      padding: const EdgeInsets.only(top: 9),
      child: Act(
        label: text,
        onTap: () => quoteAndGo(context, 'line', line['id'] as String, text),
        child: SizedBox(
          width: double.infinity,
          child: line['held'] == true
              ? HeardText(text: text, speaker: line['heard_by'] as String?, settled: false)
              : HeardText(text: text, speaker: null, settled: true),
        ),
      ),
    );
  }
}

/// One thread event: a message, a draft, a question or a step.
class EventLine extends StatelessWidget {
  const EventLine(this.e, {super.key, this.onBrowser});
  final Json e;
  final VoidCallback? onBrowser;

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final id = e['id'] as String;
    void tap() => m.makeTap(e['at'] as String, {'kind': 'event', 'id': id});
    switch (e['type']) {
      case 'message':
        final text = e['text'] as String? ?? '';
        final mine = e['from'] == 'owner';
        return Act(
          label: text,
          onTap: () => quoteAndGo(context, 'line', id, text),
          onLongPress: tap,
          child: mine ? OwnerBubble(text, clock(e['at'])) : _AgentText(text, clock(e['at'])),
        );
      case 'draft':
        final rec = e['record'] as Json? ?? const {};
        final shown = e['shown'] as String? ?? '';
        return Act(
          label: '${S.draft}${S.dot}$shown',
          onTap: () => quoteAndGo(context, 'draft', e['draft_id'] as String, shown),
          onLongPress: tap,
          child: PaperCard(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('${S.draft}${S.dot}${e['step'] ?? ''}'.toUpperCase(), style: T.kicker),
              const SizedBox(height: 6),
              Text(shown, style: T.line),
              const SizedBox(height: 6),
              Text(
                  '${rec['as_drafted'] ?? 0} ${S.asDrafted}${S.dot}${rec['changed'] ?? 0} ${S.changed}${S.dot}'
                  '${rec['turned_down'] ?? 0} ${S.turnedDown}${S.dot}${e['state'] ?? ''}',
                  style: T.meta),
            ]),
          ),
        );
      case 'question':
        final text = e['text'] as String? ?? '';
        return Act(
          label: '${S.question}${S.dot}$text',
          onTap: () => quoteAndGo(context, 'question', e['question_id'] as String, text),
          onLongPress: tap,
          child: PaperCard(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('${S.question}${S.dot}${e['state'] ?? ''}'.toUpperCase(), style: T.kicker),
              const SizedBox(height: 6),
              Text(text, style: T.line),
            ]),
          ),
        );
      case 'step':
        final text = '${e['tool'] ?? ''}${S.dot}${e['summary'] ?? ''}${S.dot}${e['state'] ?? ''}';
        return Act(
          label: e['browser'] == true ? '${S.browser}${S.dot}$text' : text,
          onTap: e['browser'] == true && onBrowser != null ? onBrowser! : () => quoteAndGo(context, 'line', id, text),
          onLongPress: tap,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 2),
            child: Row(children: [
              if (e['browser'] == true) const Padding(padding: EdgeInsets.only(right: 6), child: Icon(Icons.public, size: 14, color: C.steel)),
              Expanded(child: Text(text, style: T.meta)),
            ]),
          ),
        );
    }
    return const SizedBox.shrink();
  }
}

class OwnerBubble extends StatelessWidget {
  const OwnerBubble(this.text, this.time, {super.key});
  final String text, time;
  @override
  Widget build(BuildContext context) => Align(
        alignment: Alignment.centerRight,
        child: FractionallySizedBox(
          widthFactor: .86,
          alignment: Alignment.centerRight,
          child: Align(
            alignment: Alignment.centerRight,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 8),
              decoration: const BoxDecoration(
                color: C.steel,
                borderRadius: BorderRadius.only(
                    topLeft: Radius.circular(16), topRight: Radius.circular(16), bottomRight: Radius.circular(4), bottomLeft: Radius.circular(16)),
              ),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(text, style: T.text.copyWith(color: Colors.white)),
                if (time.isNotEmpty) Text(time, style: T.mono(9.5, color: Colors.white.withValues(alpha: .7))),
              ]),
            ),
          ),
        ),
      );
}

class _AgentText extends StatelessWidget {
  const _AgentText(this.text, this.time);
  final String text, time;
  @override
  Widget build(BuildContext context) => Align(
        alignment: Alignment.centerLeft,
        child: FractionallySizedBox(
          widthFactor: .86,
          child: Padding(
            padding: const EdgeInsets.all(2),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(text, style: T.text),
              Text('${S.plebian}${S.dot}$time', style: T.mono(9.5)),
            ]),
          ),
        ),
      );
}
