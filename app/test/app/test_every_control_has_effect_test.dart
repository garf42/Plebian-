// n.app D1 kill test: every control on every screen does something. Each
// control (an Act: the app's only control type) is pressed on a fresh screen
// against the fake server; afterwards a request went out, a page was laid or
// lifted, a socket frame was sent, the composer quoted something, or what the
// screen shows changed. Re-pressing the selected segment is exempt.
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/model.dart';
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
  'Setup: Backups': () => const SetupForm('backups'),
  'Omi pairing': () => const OmiScanPage(),
  'Pair': () => PairPage(onPaired: (_) {}),
  'Browser': () => const BrowserPage(),
};

Future<(FakeApi, AppModel, NavLog)> mount(WidgetTester t, Widget Function() screen) async {
  phone(t);
  final api = FakeApi();
  final m = await model(api);
  m.composer.text = 'in my own words';
  final nav = NavLog();
  await t.pumpWidget(const SizedBox()); // a fresh tree: no state carried over
  await t.pumpWidget(host(m, screen(), nav: nav));
  await t.pumpAndSettle();
  return (api, m, nav);
}

String signature(WidgetTester t, FakeApi api, AppModel m, NavLog nav) => [
      api.calls.length,
      nav.events,
      m.quote?.id,
      m.tab,
      m.link.outbox.items.length,
      api.socks.values.fold<int>(0, (a, s) => a + s.sent.length),
      for (final w in t.allWidgets)
        switch (w) {
          Text(:final data) => data,
          Act(:final label, :final selected) => '$label/$selected',
          EditableText(:final controller) => controller.text,
          _ => null,
        },
    ].join('|');

void main() {
  for (final MapEntry(key: name, value: screen) in screens.entries) {
    testWidgets('$name: every control has an effect', (t) async {
      var (_, _, _) = await mount(t, screen);
      final n = find.byType(Act).evaluate().length;
      expect(n, greaterThan(0), reason: '$name has controls');
      for (var i = 0; i < n; i++) {
        final (api, m, nav) = await mount(t, screen);
        final act = t.widget<Act>(find.byType(Act).at(i));
        if (act.selected == true) continue;
        final before = signature(t, api, m, nav);
        act.onTap();
        await t.pumpAndSettle();
        expect(signature(t, api, m, nav), isNot(before), reason: '$name: control "${act.label}" changed nothing');
      }
    });

    testWidgets('$name: every long-press makes a tap', (t) async {
      var (_, _, _) = await mount(t, screen);
      final n = find.byType(Act).evaluate().length;
      for (var i = 0; i < n; i++) {
        final (api, _, _) = await mount(t, screen);
        final act = t.widget<Act>(find.byType(Act).at(i));
        if (act.onLongPress == null) continue;
        act.onLongPress!();
        await t.pumpAndSettle();
        expect(api.sent('/v1/taps'), hasLength(1), reason: '$name: long-press on "${act.label}" made no tap');
        expect((api.sent('/v1/taps').single.body as Map)['how'], 'app');
      }
    });
  }
}
