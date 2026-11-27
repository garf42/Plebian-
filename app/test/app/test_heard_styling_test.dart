// n.app D3 kill test: heard lines are not styled like settled lines. Until
// settled a heard line is lighter (ink_2), sits on a dashed rule and is read
// "heard · speaker"; settling inks the rule solid over the settle motion.
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/ui/common.dart';
import 'package:plebian/ui/kit.dart';

import '../support/fakes.dart';
import '../support/fixtures.dart';

HeardRule rule(WidgetTester t, String text) => t
    .widget<CustomPaint>(find.ancestor(of: find.text(text), matching: find.byWidgetPredicate((w) => w is CustomPaint && w.foregroundPainter is HeardRule)).first)
    .foregroundPainter! as HeardRule;

Color colorOf(WidgetTester t, String text) => t.widget<Text>(find.text(text).first).style!.color!;

void main() {
  testWidgets('unsettled heard lines: lighter, dashed, spoken as heard; settled ones are ink', (t) async {
    phone(t);
    final m = await model(FakeApi());
    final heard = heardRow('h1'), settled = heardRow('h2', settled: true, speaker: 'marcus');
    await t.pumpWidget(host(m, Scaffold(body: Column(children: [HeardRowView(heard), HeardRowView(settled)]))));
    await t.pumpAndSettle();
    const line = 'The twenty foot culvert pipe three forty';
    final texts = find.text(line);
    expect(texts, findsNWidgets(2));
    final unsettledText = t.widget<Text>(texts.at(0)), settledText = t.widget<Text>(texts.at(1));
    expect(unsettledText.style!.color, C.ink2);
    expect(settledText.style!.color, C.ink);
    final rules = t.widgetList<CustomPaint>(find.byWidgetPredicate((w) => w is CustomPaint && w.foregroundPainter is HeardRule)).map((p) => p.foregroundPainter! as HeardRule).toList();
    expect(rules.first.settled, isFalse, reason: 'a heard line sits on the dashed rule');
    expect(rules.last.settled, isTrue);
    expect(find.text('heard · dale'), findsWidgets);
    expect(find.bySemanticsLabel(RegExp('^heard · dale')), findsWidgets);
  });

  testWidgets('settling inks the dashed rule solid over 450 ms (the settle motion)', (t) async {
    phone(t);
    final m = await model(FakeApi());
    Widget line(bool s) => host(m, Scaffold(body: HeardText(text: 'fifty should do it', speaker: 'you', settled: s)));
    await t.pumpWidget(line(false));
    expect(rule(t, 'fifty should do it').settled, isFalse);
    await t.pumpWidget(line(true));
    await t.pump(const Duration(milliseconds: 225));
    final mid = rule(t, 'fifty should do it');
    expect(mid.settled, isTrue);
    expect(mid.ink, inExclusiveRange(0, 1));
    await t.pump(const Duration(milliseconds: 300));
    expect(rule(t, 'fifty should do it').ink, 1);
    expect(colorOf(t, 'fifty should do it'), C.ink);
  });

  testWidgets('a held entry line is heard-styled until settled', (t) async {
    phone(t);
    final m = await model(FakeApi());
    await t.pumpWidget(host(m, Scaffold(body: Column(children: [EntryLine((page()['entry'] as List)[0] as Map<String, dynamic>), EntryLine((page()['entry'] as List)[1] as Map<String, dynamic>)]))));
    await t.pumpAndSettle();
    expect(colorOf(t, 'Dale quoted the culvert pipe.'), C.ink2);
    expect(colorOf(t, 'You held the orders till Monday.'), C.ink);
  });
}
