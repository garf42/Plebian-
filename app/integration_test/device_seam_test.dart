// A05/A06, the app's halves, on the emulator: the whole app boots and holds
// the Omi simulator (tests/fixtures/omi) through SimTransport, the one stand-in
// below the device seam. Build with --dart-define=OMI_SIM=http://10.0.2.2:PORT.
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:plebian/main.dart';
import 'package:plebian/model.dart';
import 'package:plebian/ui/shell.dart';

import '../test/support/fake_server.dart';
import '../test/support/seam_scenario.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  const sim = String.fromEnvironment('OMI_SIM');

  testWidgets('double-tap ⇒ tap and 100 ms buzz; out of range ⇒ no tap; stored audio syncs late', (t) async {
    final server = await FakeServer.start();
    addTearDown(server.close);
    AppModel? m;
    await t.pumpWidget(PlebianApp(boot: seamBoot(server, Uri.parse(sim)), onOpen: (x) async => m = x));
    for (var i = 0; i < 100 && m == null; i++) {
      await t.pump(const Duration(milliseconds: 100));
    }
    expect(find.byType(Shell), findsOneWidget);
    final omi = OmiControl(Uri.parse(sim));
    await a05DoubleTap(server, omi);
    await a06OutOfRange(server, omi, m!);
  }, skip: sim.isEmpty);
}
