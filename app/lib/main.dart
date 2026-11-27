// Plebian on the phone. The engine running this outlives the screen: the
// Android foreground service keeps it (and the Omi and server links) alive
// with the app closed.
import 'dart:async';
import 'dart:io';

import 'package:material_ui/material_ui.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';
import 'device/transport.dart';
import 'link.dart';
import 'model.dart';
import 'services.dart';
import 'strings.dart';
import 'ui/admin.dart';
import 'ui/kit.dart';
import 'ui/shell.dart';

/// Set by the build to drive the Omi simulator instead of the radio.
const omiSim = String.fromEnvironment('OMI_SIM');

/// Everything the app takes from the phone; tests pass stand-ins.
class Boot {
  Boot({
    required this.vault,
    required this.signer,
    required this.dir,
    required this.notifier,
    required this.player,
    this.apiFor = _http,
    this.sim,
    this.scan,
    this.radio,
    this.service = LinkService.start,
  });
  final Vault vault;
  final Signer signer;
  final Future<Directory> Function() dir;
  final Notifier notifier;
  final Player player;
  final Api Function(Uri base, String token) apiFor;
  final Uri? sim;
  final Stream<(String, String)> Function()? scan;
  final DeviceTransport Function(String id)? radio;
  final Future<void> Function() service;

  static Api _http(Uri base, String token) => HttpApi(base, token);

  /// The paired app: the links started, the Omi resumed, the service up.
  Future<AppModel?> open() async {
    final server = await vault.read('server'), token = await vault.read('token');
    if (server == null || token == null) return null;
    final api = apiFor(Uri.parse(server), token);
    final link = Link(api: api, dir: await dir(), notifier: notifier);
    final omi = OmiPairing(link, vault, sim: sim, scan: scan, radio: radio);
    final m = AppModel(api: api, link: link, signer: signer, omi: omi, player: player, vault: vault);
    await m.load();
    await link.start();
    unawaited(omi.resume());
    await service();
    return m;
  }
}

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  late final PhoneNotifier notifier;
  AppModel? model;
  notifier = PhoneNotifier((id) => model?.openAt(id));
  runApp(PlebianApp(
    boot: Boot(
      vault: SecureVault(),
      signer: KeystoreSigner(),
      dir: getApplicationSupportDirectory,
      notifier: notifier,
      player: PhonePlayer(),
      sim: omiSim.isEmpty ? null : Uri.parse(omiSim),
    ),
    onOpen: (m) async {
      model = m;
      await notifier.init();
    },
  ));
}

class PlebianApp extends StatefulWidget {
  const PlebianApp({super.key, required this.boot, this.onOpen});
  final Boot boot;
  final Future<void> Function(AppModel)? onOpen;
  @override
  State<PlebianApp> createState() => _PlebianAppState();
}

class _PlebianAppState extends State<PlebianApp> {
  AppModel? _model;
  bool _ready = false;

  @override
  void initState() {
    super.initState();
    _start();
  }

  Future<void> _start() async {
    AppModel? m;
    try {
      m = await widget.boot.open();
    } catch (_) {}
    if (!mounted) return;
    setState(() {
      _model = m;
      _ready = true;
    });
    if (m != null) await widget.onOpen?.call(m);
  }

  @override
  Widget build(BuildContext context) {
    final m = _model;
    return MaterialApp(
      title: S.plebian,
      theme: theme(),
      home: m != null
          ? Scope(model: m, child: const Shell())
          : !_ready
              ? const Ground(child: SizedBox.expand())
              : Pairing(vault: widget.boot.vault, signer: widget.boot.signer, child: PairPage(onPaired: (_) => _start())),
    );
  }
}
