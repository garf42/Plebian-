// The walking skeleton on the emulator, against the server under test and the
// Omi simulator: pair, connect the Omi, send a message, see the agent's reply
// on Today, and see a heard row land. Build with
//   --dart-define=SERVER=http://10.0.2.2:PORT --dart-define=PAIR_CODE=CODE
//   --dart-define=OMI_SIM=http://10.0.2.2:PORT2
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:plebian/main.dart';
import 'package:plebian/model.dart';
import 'package:plebian/services.dart';
import 'package:plebian/strings.dart';
import 'package:plebian/ui/shell.dart';

import '../test/support/fake_server.dart';
import '../test/support/fakes.dart';

const server = String.fromEnvironment('SERVER');
const code = String.fromEnvironment('PAIR_CODE');
const sim = String.fromEnvironment('OMI_SIM');

Future<void> until(WidgetTester t, bool Function() test, {int seconds = 60}) async {
  for (var i = 0; i < seconds * 5 && !test(); i++) {
    await t.pump(const Duration(milliseconds: 200));
  }
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('pair, Omi, a message and its reply, a heard row', (t) async {
    AppModel? m;
    final boot = Boot(
      vault: MemoryVault(),
      // The emulator has no enrolled fingerprint; pairing reads only the
      // device key's public half, and no admin action runs here.
      signer: FakeSigner(),
      dir: () async => Directory.systemTemp.createTempSync('plebian'),
      notifier: FakeNotifier(),
      player: FakePlayer(),
      sim: Uri.parse(sim),
      service: () async {},
    );
    await t.pumpWidget(PlebianApp(boot: boot, onOpen: (x) async => m = x));
    await until(t, () => find.text(S.code).evaluate().isNotEmpty);
    await t.enterText(find.byType(TextField), 'plebian://pair?addr=${Uri.parse(server).authority}&code=$code');
    await t.tap(find.bySemanticsLabel(S.pair).first);
    await until(t, () => m != null);
    expect(find.byType(Shell), findsOneWidget);

    await until(t, () => m!.link.omiConnected, seconds: 30);
    await OmiControl(Uri.parse(sim)).talk([
      {'speaker': 'a', 'text': 'the gravel comes on wednesday', 'pause_ms': 400},
      {'speaker': 'b', 'text': 'down by the culvert, not the top of the drive', 'pause_ms': 0},
    ]);

    await t.enterText(find.byType(TextField).last, 'what did dale say about the gravel');
    await t.tap(find.bySemanticsLabel(S.send));
    await until(t, () => m!.thread.any((e) => e['type'] == 'message' && e['from'] == 'agent'));
    expect(m!.thread.where((e) => e['from'] == 'agent'), isNotEmpty, reason: 'the agent replied on Today');
    await t.pump();
    expect(find.textContaining('${S.plebian}${S.dot}'), findsWidgets);

    final page = '/v1/days/${(await m!.api.get('/v1/today'))['date']}';
    var heard = 0;
    for (var i = 0; i < 90 && heard == 0; i++) {
      heard = ((await m!.api.get(page))['heard'] as List).length;
      if (heard == 0) await t.pump(const Duration(seconds: 1));
    }
    expect(heard, greaterThan(0), reason: 'a heard row landed');
  }, skip: server.isEmpty || code.isEmpty || sim.isEmpty);
}
