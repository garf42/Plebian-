// n.app D5 kill test: no hints, captions, instructions or empty-state
// sentences anywhere. Three checks: every string in S is a label, name, time,
// count or status; no screen source carries a visible string literal of its
// own; and every screen rendered over an empty store shows nothing but S
// strings, numbers and separators.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/strings.dart';
import 'package:plebian/ui/admin.dart';
import 'package:plebian/ui/journal.dart';
import 'package:plebian/ui/shell.dart';

import '../support/fakes.dart';
import '../support/fixtures.dart';

/// Words that only instructions, hints and empty-state sentences need.
final explaining = RegExp(
    r"\b(here|will|yet|nothing|no|none|empty|you can|try|please|tap to|press to|to see|appears?|start by|lift|swipe|click|enter|type|add a|ask about|waiting for|there are|there is|don't|isn't)\b",
    caseSensitive: false);

/// Top-level string literals in Dart source: (offset, the text outside any
/// interpolation). Comments are skipped; interpolations are skipped whole.
List<(int, String)> literals(String s) {
  final out = <(int, String)>[];
  var i = 0;
  int skipString(int i, StringBuffer? text) {
    final raw = i > 0 && s[i - 1] == 'r';
    final q = s[i];
    i++;
    while (i < s.length && s[i] != q) {
      if (!raw && s[i] == r'\') {
        i += 2;
      } else if (!raw && s[i] == r'$' && i + 1 < s.length && s[i + 1] == '{') {
        var depth = 1;
        i += 2;
        while (i < s.length && depth > 0) {
          if (s[i] == "'" || s[i] == '"') {
            i = skipString(i, null);
            continue;
          }
          if (s[i] == '{') depth++;
          if (s[i] == '}') depth--;
          i++;
        }
      } else if (!raw && s[i] == r'$') {
        i++;
        while (i < s.length && RegExp(r'\w').hasMatch(s[i])) {
          i++;
        }
      } else {
        text?.write(s[i]);
        i++;
      }
    }
    return i + 1;
  }

  while (i < s.length) {
    if (s.startsWith('//', i)) {
      i = s.indexOf('\n', i);
      if (i < 0) break;
    } else if (s[i] == "'" || s[i] == '"') {
      final text = StringBuffer();
      final start = i;
      i = skipString(i, text);
      out.add((start, '$text'));
    } else {
      i++;
    }
  }
  return out;
}

void main() {
  test('every app string is a label, name, time, count or status', () {
    for (final s in S.all) {
      expect(RegExp(r'[.!?…:]\s*$').hasMatch(s), isFalse, reason: 'a sentence: "$s"');
      expect(s.trim().split(RegExp(r'\s+')).length, lessThanOrEqualTo(4), reason: 'too long for a label: "$s"');
      expect(explaining.hasMatch(s), isFalse, reason: 'explains: "$s"');
    }
  });

  test('no screen carries a visible string literal of its own', () {
    const contexts = ['Text(', 'TextSpan(text:', 'labelText:', 'hintText:', 'helperText:', 'label:', 'tooltip:', 'semanticsLabel:', 'title:'];
    final offenders = <String>[];
    for (final f in [...Directory('lib/ui').listSync().whereType<File>(), File('lib/main.dart')]) {
      final src = f.readAsStringSync();
      for (final (start, text) in literals(src)) {
        final before = src.substring(0, start).trimRight();
        if (contexts.any(before.endsWith) && RegExp('[A-Za-z]').hasMatch(text)) offenders.add('${f.path}: "$text"');
      }
    }
    expect(offenders, isEmpty);
  });

  final screens = <String, Widget Function()>{
    'Today': () => const Shell(),
    'Journal': () => const Scaffold(body: JournalView()),
    'Open': () => const Scaffold(body: OpenView()),
    'Day page': () => const DayPage('2026-11-15'),
    'Omi': () => const OmiPage(),
    'This week': () => const WeekPage(),
    'Setup: Mail': () => const SetupForm('mail'),
    'Pair': () => PairPage(onPaired: (_) {}),
    'Browser': () => const BrowserPage(),
  };
  final known = [...S.all]..sort((a, b) => b.length - a.length);

  for (final MapEntry(key: name, value: screen) in screens.entries) {
    testWidgets('$name over an empty store shows only labels, counts and statuses', (t) async {
      phone(t);
      final m = await model(FakeApi(fixtures(empty: true)));
      await t.pumpWidget(host(m, screen()));
      await t.pumpAndSettle();
      for (final w in t.widgetList<Text>(find.byType(Text))) {
        var rest = (w.data ?? w.textSpan?.toPlainText() ?? '').toLowerCase();
        for (final k in known) {
          rest = rest.replaceAll(k.toLowerCase(), ' ');
        }
        rest = rest.replaceAll(RegExp(r'[\d\s·:%–\-↳/]'), '');
        expect(rest, isEmpty, reason: '$name shows "${w.data}"');
      }
    });
  }
}
