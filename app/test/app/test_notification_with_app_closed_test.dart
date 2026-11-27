// n.app D4 kill test: a message the agent starts raises a phone notification
// from the background link itself, with no screen built at all (the app
// closed); a reply (notify false) raises none.
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:plebian/link.dart';

import '../support/fakes.dart';

void main() {
  test('no screen, an agent-started message: one notification, carrying its event id', () async {
    final api = FakeApi();
    final notifier = FakeNotifier();
    final link = Link(api: api, dir: Directory.systemTemp.createTempSync('plebian'), notifier: notifier);
    await link.start();
    final events = api.socks['/v1/events']!;
    events.push(jsonEncode({'id': '41', 'at': '2026-11-15T08:00:00Z', 'type': 'message', 'from': 'agent', 'text': 'reply', 'notify': false}));
    events.push(jsonEncode({'id': '42', 'at': '2026-11-15T09:00:00Z', 'type': 'message', 'from': 'agent', 'text': 'Dale is at the gate', 'notify': true}));
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(notifier.shown.map((e) => e['id']), ['42']);
    expect(link.lastId, '42', reason: 'the link resumes after it, so nothing is notified twice');
  });
}
