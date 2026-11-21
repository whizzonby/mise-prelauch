import 'package:flutter/painting.dart';

import '../tokens/tokens.dart';

/// Young Serif for display and headings (one weight; hierarchy comes from size
/// and space). Instrument Sans for everything else. Sentence case throughout.
abstract final class MiseType {
  static const display = 'YoungSerif';
  static const sans = 'InstrumentSans';

  static TextStyle _serif(double size, double height) => TextStyle(
    fontFamily: display,
    fontSize: size,
    height: height,
    letterSpacing: -0.02 * size,
    color: MiseColors.foreground,
  );

  static TextStyle _sans(double size, double height, {double weight = 400, Color color = MiseColors.foreground}) =>
      TextStyle(
        fontFamily: sans,
        fontSize: size,
        height: height,
        color: color,
        fontWeight: _weight(weight),
        // The bundled Instrument Sans is a variable font: set the axis too.
        fontVariations: [FontVariation('wght', weight)],
      );

  static FontWeight _weight(double w) => switch (w) {
    >= 700 => FontWeight.w700,
    >= 600 => FontWeight.w600,
    >= 500 => FontWeight.w500,
    _ => FontWeight.w400,
  };

  static final displayLarge = _serif(44, 1.02);
  static final h1 = _serif(34, 1.08);
  static final h2 = _serif(28, 1.12);
  static final h3 = _serif(22, 1.2);
  static final h4 = _sans(18, 1.3, weight: 600);
  static final bodyLarge = _sans(18, 1.5);
  static final body = _sans(16, 1.5);
  static final bodySmall = _sans(14.5, 1.45, color: MiseColors.muted);
  static final label = _sans(15, 1.3, weight: 600);
  static final button = _sans(16, 1.2, weight: 600);
  static final caption = _sans(13, 1.4, color: MiseColors.muted);

  /// Tabular numerals for prices, times and counts.
  static TextStyle figures(TextStyle base) => base.copyWith(fontFeatures: const [FontFeature.tabularFigures()]);
}
