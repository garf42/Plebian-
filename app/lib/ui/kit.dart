// Look and motion from docs/design/tokens.json, and the few widgets every
// screen shares. Sizes in px are the tokens' and the mockup's.
import 'dart:math';
import 'dart:ui' as ui;

import 'package:material_ui/material_ui.dart';

import '../api.dart';
import '../strings.dart';

abstract final class C {
  static const dawn = Color(0xFF848DB3), slate = Color(0xFF5D739B), steel = Color(0xFF49637C);
  static const teal = Color(0xFF3B555A), forest = Color(0xFF2D4839);
  static const ink = Color(0xFF1E2A2D), ink2 = Color(0xFF47555A), ink3 = Color(0xFF7C878A), ink4 = Color(0xFFA9B1B2);
  static const paper = Color(0xFFFBFAF6), paper2 = Color(0xFFF3F1EA), paper3 = Color(0xFFE9E6DD);
  static const rule = Color(0xFFDCD8CD), rule2 = Color(0xFFC9C4B6);
  static const ore = Color(0xFFB9772E), ore2 = Color(0xFFE7C9A2), oreTint = Color(0xFFFBF3E8);
  static const ok = Color(0xFF3E7A55), warn = Color(0xFFA4552F);
  static const card = Color.fromRGBO(252, 251, 248, 0.90);
  static const heardUnder = Color.fromRGBO(237, 241, 240, 0.95);

  static const ground = LinearGradient(
    begin: Alignment(-0.82, -0.57), // 125deg
    end: Alignment(0.82, 0.57),
    colors: [dawn, slate, steel, teal, forest],
    stops: [0, 0.30, 0.55, 0.78, 1.0],
  );
  static const screen = LinearGradient(
    begin: Alignment(-0.17, -0.98), // 170deg
    end: Alignment(0.17, 0.98),
    colors: [Color(0xFFE8EBF1), Color(0xFFE5EBE8)],
  );
  static const shadow = [
    BoxShadow(color: Color.fromRGBO(30, 42, 45, .06), offset: Offset(0, 1), blurRadius: 2),
    BoxShadow(color: Color.fromRGBO(30, 42, 45, .06), offset: Offset(0, 6), blurRadius: 18),
  ];
}

abstract final class T {
  static const _display = 'Vollkorn', _body = 'Atkinson Hyperlegible Next', _mono = 'Atkinson Hyperlegible Mono';
  static TextStyle display(double size) => TextStyle(
      fontFamily: _display, fontStyle: FontStyle.italic, fontWeight: FontWeight.w600,
      fontVariations: const [FontVariation('wght', 600)], fontSize: size, color: C.ink, height: 1.1);
  static TextStyle body(double size, {Color color = C.ink, FontWeight w = FontWeight.w400}) => TextStyle(
      fontFamily: _body, fontSize: size, color: color, height: 1.45, fontWeight: w,
      fontVariations: [FontVariation('wght', w.value.toDouble())]);
  static TextStyle mono(double size, {Color color = C.ink3, double spacing = .02}) => TextStyle(
      fontFamily: _mono, fontSize: size, color: color, height: 1.3, letterSpacing: size * spacing,
      fontVariations: const [FontVariation('wght', 400)]);
  static final dayName = display(21), title = display(19);
  static final line = body(13.5), text = body(14);
  static final meta = mono(10.5), kicker = mono(9.5, spacing: .12);
}

/// Motion tokens (ms) and the reduced-motion rule: each motion becomes a 150 ms fade.
abstract final class M {
  static const ease = Cubic(.2, .8, .2, 1);
  static const lay = Duration(milliseconds: 340), out = Duration(milliseconds: 200);
  static const strike = Duration(milliseconds: 620), live = Duration(milliseconds: 500);
  static const peek = Duration(milliseconds: 420), presence = Duration(milliseconds: 1400);
  static const settle = Duration(milliseconds: 450), fade = Duration(milliseconds: 150);
  static const bump = Duration(milliseconds: 400), pulse = Duration(milliseconds: 1300);
  static bool reduced(BuildContext c) => MediaQuery.maybeDisableAnimationsOf(c) ?? false;
}

ThemeData theme() => ThemeData(
      useMaterial3: true,
      colorScheme: ColorScheme.fromSeed(seedColor: C.forest, surface: C.paper),
      fontFamily: 'Atkinson Hyperlegible Next',
      scaffoldBackgroundColor: Colors.transparent,
      textSelectionTheme: const TextSelectionThemeData(cursorColor: C.forest),
    );

// --- Times --------------------------------------------------------------------

DateTime? when(Object? s) => s is String ? DateTime.tryParse(s)?.toLocal() : null;

String clock(Object? at) {
  final t = when(at);
  if (t == null) return '';
  final h = t.hour % 12 == 0 ? 12 : t.hour % 12;
  return '$h:${t.minute.toString().padLeft(2, '0')}${t.hour < 12 ? S.am : S.pm}';
}

DateTime? dateOf(Object? ymd) => ymd is String ? DateTime.tryParse(ymd) : null;
String weekday(Object? ymd) => dateOf(ymd) == null ? '' : S.weekdays[dateOf(ymd)!.weekday - 1];
String monthDay(Object? ymd) => dateOf(ymd) == null ? '' : '${S.months[dateOf(ymd)!.month - 1]} ${dateOf(ymd)!.day}';
String ymd(DateTime t) => '${t.year}-${t.month.toString().padLeft(2, '0')}-${t.day.toString().padLeft(2, '0')}';

// --- Controls -------------------------------------------------------------------

/// Every control in the app: a spoken label and an effect are required, so a
/// control without either cannot be built.
class Act extends StatelessWidget {
  const Act({super.key, required this.label, required this.onTap, this.onLongPress, this.selected, required this.child});
  final String label;
  final VoidCallback onTap;
  final VoidCallback? onLongPress;
  final bool? selected;
  final Widget child;

  @override
  Widget build(BuildContext context) => Semantics(
        button: true,
        selected: selected,
        label: label,
        excludeSemantics: true,
        child: InkWell(onTap: onTap, onLongPress: onLongPress, borderRadius: BorderRadius.circular(8), child: child),
      );
}

/// A pill button (action color forest when primary).
class Pill extends StatelessWidget {
  const Pill(this.text, {super.key, required this.onTap, this.primary = false, this.selected});
  final String text;
  final VoidCallback onTap;
  final bool primary;
  final bool? selected;
  @override
  Widget build(BuildContext context) => Act(
        label: text,
        selected: selected,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 5),
          decoration: BoxDecoration(
            color: primary ? C.forest : Colors.white,
            border: Border.all(color: primary ? C.forest : C.rule2),
            borderRadius: BorderRadius.circular(99),
          ),
          child: Text(text, style: T.body(12, color: primary ? Colors.white : C.ink, w: FontWeight.w600)),
        ),
      );
}

class PaperCard extends StatelessWidget {
  const PaperCard({super.key, required this.child, this.padding = const EdgeInsets.all(12)});
  final Widget child;
  final EdgeInsets padding;
  @override
  Widget build(BuildContext context) => Container(
        width: double.infinity,
        padding: padding,
        decoration: BoxDecoration(
          color: C.card,
          border: Border.all(color: const Color.fromRGBO(255, 255, 255, .7)),
          borderRadius: BorderRadius.circular(14),
          boxShadow: C.shadow,
        ),
        child: child,
      );
}

class Kicker extends StatelessWidget {
  const Kicker(this.text, {super.key});
  final String text;
  @override
  Widget build(BuildContext context) =>
      Padding(padding: const EdgeInsets.only(top: 12, bottom: 2), child: Text(text.toUpperCase(), style: T.kicker));
}

class Tag extends StatelessWidget {
  const Tag(this.text, {super.key, this.ore = false});
  final String text;
  final bool ore;
  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 4),
        decoration: BoxDecoration(color: ore ? const Color(0xFFF6E6D2) : C.paper3, borderRadius: BorderRadius.circular(99)),
        child: Text(text, style: T.mono(10, color: ore ? const Color(0xFF7A4E1E) : C.ink2)),
      );
}

/// A tap is an ore dot, 7 px across, read aloud as "tapped".
class TapDot extends StatelessWidget {
  const TapDot({super.key});
  @override
  Widget build(BuildContext context) => Semantics(
        container: true,
        label: S.tapped,
        child: Container(width: 7, height: 7, decoration: const BoxDecoration(color: C.ore, shape: BoxShape.circle)),
      );
}

// --- Heard lines ------------------------------------------------------------------

/// Heard text: lighter, on a dashed rule, until settled; settling inks the rule
/// solid (the settle motion). Spoken as "heard · speaker".
class HeardText extends StatefulWidget {
  const HeardText({super.key, required this.text, required this.speaker, required this.settled, this.style});
  final String text;
  final String? speaker;
  final bool settled;
  final TextStyle? style;
  @override
  State<HeardText> createState() => _HeardTextState();
}

class _HeardTextState extends State<HeardText> with SingleTickerProviderStateMixin {
  late final _ink = AnimationController(vsync: this, duration: M.settle, value: widget.settled ? 1 : 0);

  @override
  void didUpdateWidget(HeardText old) {
    super.didUpdateWidget(old);
    if (widget.settled && !old.settled) {
      _ink.duration = M.reduced(context) ? M.fade : M.settle;
      _ink.forward(from: 0);
    } else if (!widget.settled) {
      _ink.value = 0;
    }
  }

  @override
  void dispose() {
    _ink.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final base = widget.style ?? T.line;
    return Semantics(
      label: widget.settled ? null : '${S.heard}${S.dot}${widget.speaker ?? ''}',
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        if (widget.speaker != null)
          Text('${S.heard}${S.dot}${widget.speaker}', style: T.mono(10, color: C.teal, spacing: .04)),
        AnimatedBuilder(
          animation: _ink,
          builder: (_, _) => CustomPaint(
            foregroundPainter: HeardRule(settled: widget.settled, ink: _ink.value),
            child: Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(widget.text, style: base.copyWith(color: widget.settled ? C.ink : C.ink2)),
            ),
          ),
        ),
      ]),
    );
  }
}

/// The rule under heard text: 1 px dashed ink_4 with a 6 px period, inked
/// solid from the left as [ink] goes 0 → 1 once settled.
class HeardRule extends CustomPainter {
  HeardRule({required this.settled, required this.ink});
  final bool settled;
  final double ink;
  @override
  void paint(Canvas canvas, Size size) {
    final y = size.height - .5;
    final dash = Paint()..color = C.ink4..strokeWidth = 1;
    final solid = Paint()..color = C.forest..strokeWidth = 1;
    final inked = settled ? size.width * ink : 0.0;
    if (settled && ink >= 1) return; // settled: the rule is gone into the ink of the text
    for (var x = inked; x < size.width; x += 6) {
      canvas.drawLine(Offset(x, y), Offset(min(x + 3.3, size.width), y), dash);
    }
    if (inked > 0) canvas.drawLine(Offset(0, y), Offset(inked, y), solid);
  }

  @override
  bool shouldRepaint(HeardRule o) => o.settled != settled || o.ink != ink;
}

// --- Ribbon -------------------------------------------------------------------------

/// The day by the hour from the cut (4 am): bars are the share of each hour
/// spoken (seconds / 3600), ore dots the taps at their minute. Bars grow when
/// speech lands (live); dots in [fresh] drop in (strike).
class Ribbon extends StatelessWidget {
  const Ribbon({super.key, required this.ribbon, required this.cut, this.big = false, this.fresh = const {}});
  final Json? ribbon;
  final DateTime? cut;
  final bool big;
  final Set<String> fresh;

  @override
  Widget build(BuildContext context) {
    final hours = List<double>.filled(24, 0);
    final start = cut?.hour ?? 4;
    for (final h in (ribbon?['hours'] as List? ?? const [])) {
      final hour = (h as Json)['hour'] as int;
      hours[(hour - start) % 24] = ((h['seconds'] as num) / 3600).clamp(0, 1).toDouble();
    }
    final taps = <(String, double)>[
      for (final t in (ribbon?['taps'] as List? ?? const []))
        if (when(t) != null && cut != null) (t as String, (when(t)!.difference(cut!).inSeconds / 86400).clamp(0, 1).toDouble()),
    ];
    final reduced = M.reduced(context);
    final h = big ? 56.0 : 40.0, base = big ? 36.0 : 30.0, mh = big ? 26.0 : 20.0;
    return Semantics(
      label: '${S.taps} ${taps.length}',
      child: SizedBox(
        height: h,
        child: LayoutBuilder(builder: (context, box) {
          final w = box.maxWidth, bw = w / 24;
          return Stack(clipBehavior: Clip.none, children: [
            Positioned(left: 0, right: 0, top: base, child: Container(height: 1, color: C.rule2)),
            for (var i = 0; i < 24; i++)
              Positioned(
                left: i * bw + .4,
                width: max(bw - .9, 0),
                bottom: h - base,
                child: TweenAnimationBuilder<double>(
                  tween: Tween(end: max(1.5, hours[i] * mh)),
                  duration: reduced ? M.fade : M.live,
                  curve: M.ease,
                  builder: (_, v, _) => Container(height: v, color: C.steel.withValues(alpha: .55)),
                ),
              ),
            if (big && cut != null)
              for (final i in const [2, 6, 10, 14, 18])
                Positioned(
                  left: i * bw,
                  bottom: 0,
                  child: Text(clock(cut!.add(Duration(hours: i)).toUtc().toIso8601String()).replaceAll(':00', ''),
                      style: T.mono(8.5)),
                ),
            for (final (at, x) in taps)
              Positioned(
                left: x * w - 3.5,
                top: base - mh - 3.5,
                child: fresh.contains(at) ? Strike(child: const TapDot()) : const TapDot(),
              ),
          ]);
        }),
      ),
    );
  }
}

/// A tap's dot drops onto the ribbon at its minute.
class Strike extends StatelessWidget {
  const Strike({super.key, required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) {
    final reduced = M.reduced(context);
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: 0, end: 1),
      duration: reduced ? M.fade : M.strike,
      curve: M.ease,
      builder: (_, v, c) => Opacity(
        opacity: v,
        child: reduced ? c : Transform.translate(offset: Offset(0, -9 * (1 - v)), child: c),
      ),
      child: child,
    );
  }
}

// --- Routes ---------------------------------------------------------------------------

/// Lay: a new page is laid over the last, the way trace goes down on a drawing.
Route<R> lay<R>(Widget page) => PageRouteBuilder<R>(
      transitionDuration: M.lay,
      reverseTransitionDuration: M.out,
      pageBuilder: (_, _, _) => page,
      transitionsBuilder: (context, a, _, child) {
        if (M.reduced(context)) return FadeTransition(opacity: a, child: child);
        return AnimatedBuilder(
          animation: a,
          child: child,
          builder: (_, c) {
            final v = M.ease.transform(a.value);
            final rev = a.status == AnimationStatus.reverse;
            final blur = rev ? 0.0 : 1.2 * (1 - v);
            Widget w = Transform(
              alignment: Alignment.center,
              transform: rev
                  ? (Matrix4.translationValues(0, -6 * (1 - v), 0)..scaleByDouble(.995 + .005 * v, .995 + .005 * v, 1, 1))
                  : (Matrix4.translationValues(0, 16 * (1 - v), 0)
                    ..rotateZ(-0.7 * pi / 180 * (1 - v))
                    ..scaleByDouble(.988 + .012 * v, .988 + .012 * v, 1, 1)),
              child: c,
            );
            if (blur > 0.01) w = ImageFiltered(imageFilter: ui.ImageFilter.blur(sigmaX: blur, sigmaY: blur), child: w);
            return Opacity(opacity: v, child: w);
          },
        );
      },
    );

/// The ground behind every screen.
class Ground extends StatelessWidget {
  const Ground({super.key, required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) =>
      DecoratedBox(decoration: const BoxDecoration(gradient: C.screen), child: child);
}
