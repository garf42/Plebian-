// Fixture JSON in the shapes of server/api/API.md: one Sunday, a thread with a
// message each way, a draft, a question and a browser step, two heard rows,
// taps, Open, search, This week and Setup.
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

String sha256Hex(List<int> b) => sha256.convert(b).toString();

/// A P-256 public key (SPKI DER, base64) made for these tests; its private half was discarded.
const testPublicKey =
    'MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEuh+k8BmgtxYGhek1GRBsluFxxYzxMUozgX5A5BzYoaEzsQYNYL0tLuURM7elCc6Ahv9tJ83KJ2t7zcF0bmphIw==';

/// Two Ogg pages: OpusHead, OpusTags, then three audio packets (RFC 3533 framing).
final oggFixture = Uint8List.fromList([
  ..._page([19]), ...List.filled(19, 1),
  ..._page([10]), ...List.filled(10, 2),
  ..._page([40, 80, 20]), ...List.filled(140, 3),
]);

List<int> _page(List<int> laces) => [0x4F, 0x67, 0x67, 0x53, 0, 0, ...List.filled(8, 0), ...List.filled(12, 0), laces.length, ...laces];

const cut = '2026-11-15T04:00:00-05:00';

Map<String, Object?> heardRow(String id, {bool settled = false, String speaker = 'dale'}) => {
      'id': id,
      'source': 'talk',
      'at': '2026-11-15T14:08:00-05:00',
      'end': '2026-11-15T14:10:00-05:00',
      'settled': settled,
      'blob': 'b1ob',
      'lines': [
        {'speaker': speaker, 'text': 'The twenty foot culvert pipe three forty', 'at': '2026-11-15T14:09:00-05:00'},
        {'speaker': 'you', 'text': 'fifty should do it', 'at': '2026-11-15T14:09:30-05:00'},
      ],
    };

final thread = [
  {'id': '1', 'at': '2026-11-15T07:02:00-05:00', 'type': 'message', 'from': 'agent', 'text': 'Morning. 28 orders wait on your word.', 'notify': true},
  {'id': '2', 'at': '2026-11-15T07:10:00-05:00', 'type': 'message', 'from': 'owner', 'text': 'hold them till monday', 'notify': false, 'client_msg_id': 'c1'},
  {
    'id': '3', 'at': '2026-11-15T07:11:00-05:00', 'type': 'draft', 'draft_id': 'd1', 'step': 'mail.send',
    'shown': 'To: dale@example.com\nSubject: culvert\n\nTwenty foot, please.',
    'record': {'as_drafted': 6, 'changed': 1, 'turned_down': 0}, 'state': 'waiting',
  },
  {'id': '4', 'at': '2026-11-15T07:12:00-05:00', 'type': 'question', 'question_id': 'q1', 'text': 'Who is the second voice at 2:09?', 'state': 'open'},
  {'id': '5', 'at': '2026-11-15T07:13:00-05:00', 'type': 'step', 'tool': 'browser', 'summary': 'example.com/order', 'state': 'held', 'browser': true},
];

Map<String, Object?> tap(String id, String at, {String how = 'omi'}) =>
    {'id': id, 'at': at, 'how': how, 'title': 'Dale quoted the culvert pipe', 'note': '', 'stretch_id': 'h1'};

Map<String, Object?> page({bool empty = false}) => {
      'date': '2026-11-15',
      'cut': cut,
      'entry': empty
          ? []
          : [
              {'id': 'e1', 'text': 'Dale quoted the culvert pipe.', 'held': true, 'heard_by': 'dale'},
              {'id': 'e2', 'text': 'You held the orders till Monday.', 'held': false, 'heard_by': null},
            ],
      'thread': empty ? [] : thread,
      'taps': empty ? [] : [tap('t1', '2026-11-15T14:09:00-05:00')],
      'heard': empty ? [] : [heardRow('h1'), heardRow('h2', settled: true, speaker: 'marcus')],
      'ribbon': {
        'hours': empty ? [] : [for (var h = 6; h < 23; h++) {'hour': h, 'seconds': (h * 137) % 3600}],
        'taps': empty ? [] : ['2026-11-15T14:09:00-05:00'],
      },
    };

Map<String, Object?> fixtures({bool empty = false}) => {
      'GET /v1/version': {'commit': 'abc123', 'version': 'v0.0.1'},
      'GET /v1/thread': {'events': empty ? [] : thread},
      'GET /v1/today': page(empty: empty),
      'GET /v1/days/2026-11-15': page(empty: empty),
      'GET /v1/days': {
        'days': empty
            ? []
            : [
                {'date': '2026-11-15', 'heard': 2, 'taps': 1, 'messages': 2, 'open': 2},
                {'date': '2026-11-14', 'heard': 5, 'taps': 0, 'messages': 0, 'open': 0},
              ],
      },
      'GET /v1/taps': {'taps': empty ? [] : [tap('t1', '2026-11-15T14:09:00-05:00'), tap('t2', '2026-11-14T09:31:00-05:00', how: 'app')]},
      'GET /v1/taps/t1': {...tap('t1', '2026-11-15T14:09:00-05:00'), 'stretch': heardRow('h1')},
      'POST /v1/taps': tap('t9', '2026-11-15T15:00:00-05:00', how: 'app'),
      'GET /v1/open': {
        'questions': empty ? [] : [{'id': 'q1', 'kind': 'question', 'text': 'Who is the second voice at 2:09?', 'at': '2026-11-15T07:12:00-05:00'}],
        'drafts': empty
            ? []
            : [{'id': 'd1', 'kind': 'draft', 'text': 'Mail to Dale', 'at': '2026-11-15T07:11:00-05:00', 'shown': 'Twenty foot, please.'}],
        'orders': empty ? [] : [{'id': 'o1', 'kind': 'order', 'text': 'When Dale brings up gravel, ask about the pipe', 'at': '2026-11-10T09:00:00-05:00'}],
      },
      'GET /v1/search': {
        'thread': {'count': 0, 'first': null, 'last': null, 'hits': []},
        'heard': {
          'count': 1, 'first': '2026-11-15T14:09:00-05:00', 'last': '2026-11-15T14:09:00-05:00',
          'hits': [{'id': 'h1', 'kind': 'heard', 'text': 'culvert pipe three forty', 'at': '2026-11-15T14:09:00-05:00', 'who': 'dale'}],
        },
      },
      'GET /v1/week': {
        'brief': empty
            ? []
            : [
                {'id': 'b1', 'lane': 'breakage', 'text': 'The transcriber labels speakers differently.', 'replay': '3 pages differ', 'state': 'ready'},
                {'id': 's1', 'lane': 'standing', 'text': 'mail.send to Dale', 'replay': '', 'state': 'offered'},
              ],
        'health': empty ? {} : {'mail': '2026-11-15T07:00:00Z', 'disk': '412 days'},
        'spend': empty ? [] : [{'model': 'm1', 'day': '2026-11-15', 'in': 1200, 'out': 300, 'cached': 800}],
      },
      'GET /v1/setup': {
        'cards': [
          for (final (id, req) in const [
            ('server', true), ('pair', true), ('model', true), ('omi', false), ('mail', false), ('calendar', false),
            ('texts', false), ('storage', false), ('backups', false), ('updates', false),
          ])
            {'id': id, 'status': empty ? '' : 'connected', 'required': req},
        ],
      },
      'POST /v1/setup/model': {'provider_id': 'p1', 'format': 'anthropic', 'models': [{'id': 'm1', 'context': 200000}]},
      'POST /v1/setup/backups': {'key': 'k-once'},
      'POST /v1/setup/texts': {'emoji': '🦊'},
      'POST /v1/messages': {'id': '77'},
      'POST /v1/pair': {'device_id': 'dev1', 'token': 'tok1'},
    };
