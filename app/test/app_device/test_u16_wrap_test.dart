// n.app_device D2 kill test: packet numbers wrap at 65535 without dropping
// audio (MR-1: start the counter just below 65535 ⇒ the same audio, in order).
import 'package:flutter_test/flutter_test.dart';
import 'package:plebian/device/omi_protocol.dart';

import '../support/mem_transport.dart';

void main() {
  final frames = [
    for (var i = 0; i < 8; i++) [for (var k = 0; k < 160 + i * 7; k++) (i * 31 + k) & 0xFF],
  ];

  List<List<int>> assemble(List<List<int>> notes) {
    final out = <List<int>>[];
    final a = FrameAssembler((f) => out.add(f.bytes));
    for (final n in notes) {
      a.add(n, 0);
    }
    // The next frame's first fragment completes the last one.
    final next = ((notes.last[0] | (notes.last[1] << 8)) + 1) & 0xFFFF;
    a.add([next & 0xFF, next >> 8, 0], 0);
    return out;
  }

  test('frames crossing the u16 wrap arrive whole and in order', () {
    final from0 = assemble(fragment(frames, mtu: 64));
    final wrapped = assemble(fragment(frames, mtu: 64, first: 65533));
    expect(from0, frames);
    expect(wrapped, from0);
  });

  test('a real gap still drops the frame in flight', () {
    final notes = fragment(frames, mtu: 64, first: 65533)..removeAt(5);
    expect(assemble(notes).length, lessThan(frames.length));
  });
}
