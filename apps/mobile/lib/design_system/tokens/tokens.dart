import 'package:flutter/animation.dart';
import 'package:flutter/painting.dart';

/// Brand colours, the same values as the web design system
/// (`packages/ui/src/styles.css`, `docs/architecture/design-system.md`).
/// Named for what is on a Trinidadian kitchen counter.
abstract final class MiseColors {
  /// cassava: the page.
  static const background = Color(0xFFF3ECDD);

  /// coconut: forms, panels, anything that sits on the page.
  static const surface = Color(0xFFFCFAF4);

  /// coal-pot charcoal: text.
  static const foreground = Color(0xFF22211D);

  /// Secondary text, 5.7:1 on [background].
  static const muted = Color(0xFF5F5B50);

  /// dasheen leaf: headlines, primary buttons, dark sections.
  static const primary = Color(0xFF1E3A2B);
  static const onPrimary = Color(0xFFFCFAF4);

  /// chadon beni: supporting green.
  static const secondary = Color(0xFF4A6A28);

  /// roucou (annatto): used sparingly — numerals, the wordmark's full stop, markers.
  static const accent = Color(0xFFB4411C);

  static const border = Color(0xFFD9CFBA);

  /// Form control outlines, 3:1 on [background].
  static const borderStrong = Color(0xFF8A8372);

  static const success = Color(0xFF2F6B3F);
  static const warning = Color(0xFF8A5A00);
  static const error = Color(0xFF9B1C2E);

  /// Placeholder behind photographs while they load.
  static const imagePlaceholder = Color(0xFFE6DCC6);
}

/// 4-point spacing scale.
abstract final class MiseSpace {
  static const xxs = 4.0;
  static const xs = 8.0;
  static const sm = 12.0;
  static const md = 16.0;
  static const lg = 24.0;
  static const xl = 32.0;
  static const xxl = 48.0;
  static const xxxl = 64.0;

  /// Horizontal page gutter.
  static const gutter = 20.0;
}

/// Softened, never pill-shaped. Same four values as the web.
abstract final class MiseRadii {
  static const sm = Radius.circular(4); // tags, checkboxes
  static const md = Radius.circular(8); // buttons, form controls
  static const lg = Radius.circular(12); // photographs
  static const xl = Radius.circular(20); // a frame around a photograph
}

/// Motion tokens, mirrored from `packages/ui/src/motion.ts`.
abstract final class MiseMotion {
  static const fast = Duration(milliseconds: 150);
  static const standard = Duration(milliseconds: 300);
  static const editorial = Duration(milliseconds: 900);

  /// Entrances.
  static const settle = Cubic(0.22, 1, 0.36, 1);

  /// Press, toggle.
  static const interaction = Cubic(0.4, 0, 0.2, 1);
}

/// Minimum touch target (WCAG 2.5.5 / platform guidance).
const double kMinTouchTarget = 48;
