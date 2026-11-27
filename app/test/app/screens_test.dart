// Every screen against the fake server's fixture JSON: what it shows comes from
// the API (MR-1: the same store rendered twice gives identical trees), and
// raising the system font scale clips nothing and keeps every control reachable (MR-2).
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/strings.dart';
import 'package:plebian/ui/admin.dart';
import 'package:plebian/ui/journal.dart';
import 'package:plebian/ui/kit.dart';
import 'package:plebian/ui/shell.dart';

import '../support/fakes.dart';

final screens = <String, Widget Function()>{
  'Today': () => const Shell(),
  'Journal': () => const Scaffold(body: JournalView()),
  'Open': () => const Scaffold(body: OpenView()),
  'Day page': () => const DayPage('2026-11-15'),
  'Tap': () => const TapPage('t1'),
  'Omi': () => const OmiPage(),
  'This week': () => const WeekPage(),
  'Setup: Model': () => const SetupForm('model'),
  'Pair': () => PairPage(onPaired: (_) {}),
  'Browser': () => const BrowserPage(),
};

String tree(WidgetTester t) => [
      for (final w in t.allWidgets)
        switch (w) {
          Text(:final data) => 'T:$data',
          Act(:final label) => 'A:$label',
          _ => w.runtimeType.toString(),
        },
    ].join('\n');

void main() {
  testWidgets('Today shows the page, the thread with stamps and bubbles, and the composer', (t) async {
    phone(t);
    final m = await model(FakeApi());
    await t.pumpWidget(host(m, const Shell()));
    await t.pumpAndSettle();
    expect(find.text('Sunday'), findsOneWidget);
    expect(find.text('NOV 15 · LIVE · 1 TAPS'), findsOneWidget);
    expect(find.text('Morning. 28 orders wait on your word.'), findsOneWidget);
    expect(find.textContaining('${S.plebian}${S.dot}'), findsWidgets);
    expect(find.text('hold them till monday'), findsOneWidget);
    expect(find.text('Who is the second voice at 2:09?'), findsOneWidget);
    expect(find.bySemanticsLabel(S.tapped), findsWidgets, reason: 'the tap dot on the ribbon');
    expect(find.bySemanticsLabel(S.send), findsOneWidget);
  });

  testWidgets('a notification opens Today at its message', (t) async {
    phone(t);
    final m = await model(FakeApi());
    m.setTab(1);
    await t.pumpWidget(host(m, const Shell()));
    await t.pumpAndSettle();
    m.openAt('1');
    await t.pumpAndSettle();
    expect(m.tab, 0);
    expect(m.focus, isNull);
    expect(t.getRect(find.text('Morning. 28 orders wait on your word.')).top, greaterThan(0));
  });

  for (final MapEntry(key: name, value: screen) in screens.entries) {
    testWidgets('MR-1 $name: the same store renders the same tree', (t) async {
      phone(t);
      await t.pumpWidget(host(await model(FakeApi()), screen()));
      await t.pumpAndSettle();
      final first = tree(t);
      await t.pumpWidget(const SizedBox());
      await t.pumpWidget(host(await model(FakeApi()), screen()));
      await t.pumpAndSettle();
      expect(tree(t), first);
    });

    testWidgets('MR-2 $name: at twice the font size nothing clips and every control is reachable', (t) async {
      phone(t, textScale: 2);
      final m = await model(FakeApi());
      await t.pumpWidget(host(m, screen()));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull, reason: 'no overflow');
      for (final e in find.byType(Act).evaluate().toList()) {
        await t.ensureVisible(find.byWidget(e.widget));
        await t.pumpAndSettle();
        final box = e.renderObject! as RenderBox;
        expect(box.hasSize && box.size.width > 0 && box.size.height > 0, isTrue, reason: '${(e.widget as Act).label} has no area');
      }
      expect(t.takeException(), isNull);
    });
  }
}
