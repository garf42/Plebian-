// The root: header, Today · Journal · Open, and Today's thread and composer.
import 'package:flutter/material.dart';

import '../api.dart';
import '../model.dart';
import '../strings.dart';
import 'admin.dart';
import 'common.dart';
import 'journal.dart';
import 'kit.dart';

class Shell extends StatelessWidget {
  const Shell({super.key});
  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    return Scaffold(
      body: Ground(
        child: Column(children: [
          const Header(),
          Expanded(child: IndexedStack(index: m.tab, children: const [TodayView(), JournalView(), OpenView()])),
          const _Tabs(),
        ]),
      ),
    );
  }
}

class _Tabs extends StatelessWidget {
  const _Tabs();
  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    Widget tab(int i, String label, IconData icon, [Widget? badge]) => Expanded(
          child: Act(
            label: label,
            selected: m.tab == i,
            onTap: () => m.setTab(i),
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 6),
              child: Stack(alignment: Alignment.topCenter, clipBehavior: Clip.none, children: [
                Column(mainAxisSize: MainAxisSize.min, children: [
                  Icon(icon, size: 22, color: m.tab == i ? C.forest : C.ink3),
                  const SizedBox(height: 3),
                  Text(label, style: T.body(11, color: m.tab == i ? C.forest : C.ink3, w: FontWeight.w500)),
                ]),
                if (m.tab == i) Positioned(top: -6, child: Container(width: 22, height: 2.5, color: C.forest)),
                if (badge != null) Positioned(top: 0, right: 0, left: 36, child: Center(child: badge)),
              ]),
            ),
          ),
        );
    return Container(
      decoration: const BoxDecoration(
        color: Color.fromRGBO(251, 250, 246, .92),
        border: Border(top: BorderSide(color: Color.fromRGBO(0, 0, 0, .06))),
      ),
      child: SafeArea(
        top: false,
        child: Row(children: [
          tab(0, S.today, Icons.chat_bubble_outline),
          tab(1, S.journal, Icons.menu_book_outlined),
          tab(2, S.open, Icons.schedule, Loaded('/v1/open', builder: (c, o) {
            final n = (o['questions'] as List? ?? const []).length + (o['drafts'] as List? ?? const []).length;
            return n == 0 ? const SizedBox.shrink() : _Badge(n);
          })),
        ]),
      ),
    );
  }
}

/// The count of what waits on the owner; bumps when it grows.
class _Badge extends StatelessWidget {
  const _Badge(this.n);
  final int n;
  @override
  Widget build(BuildContext context) => TweenAnimationBuilder<double>(
        key: ValueKey(n),
        tween: Tween(begin: 0, end: 1),
        duration: M.reduced(context) ? Duration.zero : M.bump,
        builder: (_, v, c) => Transform.scale(scale: 1 + .35 * (v < .4 ? v / .4 : (1 - v) / .6), child: c),
        child: Container(
          constraints: const BoxConstraints(minWidth: 16),
          padding: const EdgeInsets.symmetric(horizontal: 4),
          decoration: BoxDecoration(color: C.ore, borderRadius: BorderRadius.circular(8)),
          child: Text('$n', textAlign: TextAlign.center, style: T.body(10, color: Colors.white, w: FontWeight.w700)),
        ),
      );
}

class TodayView extends StatefulWidget {
  const TodayView({super.key});
  @override
  State<TodayView> createState() => _TodayViewState();
}

class _TodayViewState extends State<TodayView> {
  final _keys = <String, GlobalKey>{};

  GlobalKey _key(String id) => _keys.putIfAbsent(id, GlobalKey.new);

  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final focus = m.focus;
    if (focus != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final c = _keys[focus]?.currentContext;
        if (c != null) Scrollable.ensureVisible(c, duration: M.reduced(context) ? M.fade : M.lay, curve: M.ease);
        m.focus = null;
      });
    }
    return Column(children: [
      Expanded(
        // shortcut: the whole loaded thread is built (no lazy list) so a
        // notification can scroll to any message; page it when threads grow long.
        child: SingleChildScrollView(
          reverse: true,
          padding: const EdgeInsets.fromLTRB(12, 12, 12, 18),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Loaded('/v1/today', builder: (c, page) => TodayCard(page)),
            for (final e in m.thread)
              Padding(
                key: _key(e['id'] as String),
                padding: const EdgeInsets.only(top: 10),
                child: EventLine(e, onBrowser: () => Navigator.of(context).push(lay(const BrowserPage()))),
              ),
            for (final (text, status) in m.pending)
              Padding(
                padding: const EdgeInsets.only(top: 10),
                child: Semantics(label: status ?? '', child: OwnerBubble(text, status ?? '')),
              ),
          ]),
        ),
      ),
      const Composer(),
    ]);
  }
}

/// The page on top of Today: the day's name, ribbon and entry.
class TodayCard extends StatelessWidget {
  const TodayCard(this.page, {super.key});
  final Json page;
  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final date = page['date'] as String?;
    final taps = (page['taps'] as List? ?? const []).length;
    return PaperCard(
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Act(
          label: weekday(date),
          onTap: () => Navigator.of(context).push(lay(DayPage(date ?? ''))),
          child: Wrap(crossAxisAlignment: WrapCrossAlignment.end, spacing: 8, children: [
            Text(weekday(date), style: T.dayName),
            Text('${monthDay(date)}${S.dot}${S.live}${S.dot}$taps ${S.tapsCount}'.toUpperCase(), style: T.meta),
          ]),
        ),
        const SizedBox(height: 8),
        Ribbon(ribbon: page['ribbon'] as Json?, cut: when(page['cut']), fresh: m.freshTaps),
        for (final l in (page['entry'] as List? ?? const []).cast<Json>()) EntryLine(l),
      ]),
    );
  }
}

class Composer extends StatelessWidget {
  const Composer({super.key});
  @override
  Widget build(BuildContext context) {
    final m = AppModel.of(context);
    final q = m.quote;
    return Container(
      padding: const EdgeInsets.fromLTRB(10, 8, 10, 10),
      decoration: const BoxDecoration(
        color: Color.fromRGBO(251, 250, 246, .94),
        border: Border(top: BorderSide(color: Color.fromRGBO(0, 0, 0, .05))),
      ),
      child: Stack(clipBehavior: Clip.none, children: [
        Positioned(left: 4, right: 4, top: -8, child: Presence(working: m.working)),
        Column(mainAxisSize: MainAxisSize.min, children: [
          if (q != null)
            Container(
              margin: const EdgeInsets.fromLTRB(2, 0, 2, 6),
              padding: const EdgeInsets.fromLTRB(9, 6, 4, 6),
              decoration: BoxDecoration(
                color: C.paper2,
                borderRadius: BorderRadius.circular(4),
                border: const Border(left: BorderSide(color: C.steel, width: 2)),
              ),
              child: Row(children: [
                const Text('↳ ', style: TextStyle(color: C.steel)),
                Expanded(child: Text(q.text, maxLines: 2, overflow: TextOverflow.ellipsis, style: T.body(12, color: C.ink2))),
                Act(label: S.removeQuote, onTap: m.clearQuote, child: const Padding(padding: EdgeInsets.all(4), child: Icon(Icons.close, size: 16, color: C.ink3))),
              ]),
            ),
          Container(
            padding: const EdgeInsets.fromLTRB(13, 2, 6, 2),
            decoration: BoxDecoration(color: Colors.white, border: Border.all(color: C.rule), borderRadius: BorderRadius.circular(22)),
            child: Row(children: [
              Expanded(
                child: TextField(
                  controller: m.composer,
                  focusNode: m.composerFocus,
                  minLines: 1,
                  maxLines: 5,
                  textCapitalization: TextCapitalization.sentences,
                  style: T.text,
                  decoration: InputDecoration(
                    border: InputBorder.none,
                    labelText: S.message,
                    labelStyle: T.text.copyWith(color: C.ink3),
                    floatingLabelBehavior: FloatingLabelBehavior.never,
                  ),
                ),
              ),
              Act(
                label: S.send,
                onTap: m.send,
                child: Container(
                  width: 32,
                  height: 32,
                  decoration: const BoxDecoration(color: C.forest, shape: BoxShape.circle),
                  child: const Icon(Icons.arrow_upward, size: 18, color: Colors.white),
                ),
              ),
            ]),
          ),
        ]),
      ]),
    );
  }
}

/// While the agent works, the ground's gradient drifts along the composer's top edge.
class Presence extends StatefulWidget {
  const Presence({super.key, required this.working});
  final bool working;
  @override
  State<Presence> createState() => _PresenceState();
}

class _PresenceState extends State<Presence> with SingleTickerProviderStateMixin {
  late final _drift = AnimationController(vsync: this, duration: M.presence);

  @override
  void dispose() {
    _drift.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final reduced = M.reduced(context);
    widget.working && !reduced ? _drift.repeat() : _drift.stop();
    return AnimatedOpacity(
      opacity: widget.working ? 1 : 0,
      duration: M.fade,
      child: AnimatedBuilder(
        animation: _drift,
        builder: (_, _) {
          final s = -1.2 + 2.4 * _drift.value;
          return Container(
            height: 2,
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(2),
              gradient: LinearGradient(
                begin: Alignment(s - 1, 0),
                end: Alignment(s + 1, 0),
                colors: const [Colors.transparent, C.dawn, C.steel, C.forest, Colors.transparent],
              ),
            ),
          );
        },
      ),
    );
  }
}
