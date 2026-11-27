// n.app D2 kill test: nothing in the conversation sends fixed words. Pressing
// any draft, question, heard line, tap or line only quotes it into the
// composer; the one message that goes out is the owner's own typing.
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/model.dart';
import 'package:plebian/strings.dart';
import 'package:plebian/ui/journal.dart';
import 'package:plebian/ui/kit.dart';
import 'package:plebian/ui/shell.dart';

import '../support/fakes.dart';

final conversation = <String, Widget Function()>{
  'Today': () => const Shell(),
  'Open': () => const Scaffold(body: OpenView()),
  'Day page': () => const DayPage('2026-11-15'),
  'Tap': () => const TapPage('t1'),
};

Future<(FakeApi, AppModel)> mount(WidgetTester t, Widget Function() screen) async {
  phone(t);
  final api = FakeApi();
  final m = await model(api);
  await t.pumpWidget(const SizedBox());
  await t.pumpWidget(host(m, screen()));
  await t.pumpAndSettle();
  return (api, m);
}

void main() {
  for (final MapEntry(key: name, value: screen) in conversation.entries) {
    testWidgets('$name: no control sends words the owner did not type', (t) async {
      var (_, _) = await mount(t, screen);
      final n = find.byType(Act).evaluate().length;
      for (var i = 0; i < n; i++) {
        final (api, m) = await mount(t, screen);
        final act = t.widget<Act>(find.byType(Act).at(i));
        act.onTap();
        await t.pumpAndSettle();
        expect(api.sent('/v1/messages'), isEmpty, reason: '$name: "${act.label}" sent a message the owner did not type');
        expect(m.link.outbox.items.where((e) => e['path'] == '/v1/messages'), isEmpty,
            reason: '$name: "${act.label}" queued a message the owner did not type');
        expect(m.composer.text, isEmpty, reason: '$name: "${act.label}" put words in the owner\'s mouth');
      }
    });
  }

  testWidgets('no reply buttons: drafts and questions carry no yes, no, send or approve', (t) async {
    await mount(t, conversation['Today']!);
    final canned = RegExp(r'^(yes|no|ok|okay|send|approve|decline|confirm|cancel|reply)$', caseSensitive: false);
    for (final act in t.widgetList<Act>(find.byType(Act))) {
      expect(canned.hasMatch(act.label) && act.label != S.send, isFalse, reason: 'a canned reply "${act.label}"');
    }
    expect(find.bySemanticsLabel(S.send), findsOneWidget, reason: 'one Send: the composer\'s');
  });

  testWidgets('a quoted draft goes out with the owner\'s own words and the quote', (t) async {
    final (api, m) = await mount(t, conversation['Today']!);
    t.widget<Act>(find.byWidgetPredicate((w) => w is Act && w.label.startsWith(S.draft))).onTap();
    await t.pumpAndSettle();
    expect(m.quote?.kind, 'draft');
    await t.enterText(find.byType(TextField), 'send it but say thirty foot');
    t.widget<Act>(find.byWidgetPredicate((w) => w is Act && w.label == S.send)).onTap();
    await t.pumpAndSettle();
    final sent = api.sent('/v1/messages').single.body as Map;
    expect(sent['text'], 'send it but say thirty foot');
    expect(sent['quote'], {'kind': 'draft', 'id': 'd1'});
  });
}
