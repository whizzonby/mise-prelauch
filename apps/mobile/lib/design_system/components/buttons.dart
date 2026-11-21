import 'package:flutter/material.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';

enum MiseButtonKind { primary, secondary, quiet, danger }

/// The one button. `busy` keeps the button's size, disables it and announces
/// progress, so a double tap cannot submit twice.
class MiseButton extends StatelessWidget {
  const MiseButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.kind = MiseButtonKind.primary,
    this.busy = false,
    this.icon,
    this.expand = true,
    this.semanticsLabel,
  });

  const MiseButton.secondary({
    super.key,
    required this.label,
    required this.onPressed,
    this.busy = false,
    this.icon,
    this.expand = true,
    this.semanticsLabel,
  }) : kind = MiseButtonKind.secondary;

  const MiseButton.quiet({
    super.key,
    required this.label,
    required this.onPressed,
    this.busy = false,
    this.icon,
    this.expand = false,
    this.semanticsLabel,
  }) : kind = MiseButtonKind.quiet;

  final String label;
  final VoidCallback? onPressed;
  final MiseButtonKind kind;
  final bool busy;
  final IconData? icon;
  final bool expand;
  final String? semanticsLabel;

  @override
  Widget build(BuildContext context) {
    final enabled = onPressed != null && !busy;
    final (bg, fg, side) = switch (kind) {
      MiseButtonKind.primary => (MiseColors.primary, MiseColors.onPrimary, BorderSide.none),
      MiseButtonKind.secondary => (Colors.transparent, MiseColors.primary, const BorderSide(color: MiseColors.primary)),
      MiseButtonKind.quiet => (Colors.transparent, MiseColors.primary, BorderSide.none),
      MiseButtonKind.danger => (Colors.transparent, MiseColors.error, const BorderSide(color: MiseColors.error)),
    };

    final child = Row(
      mainAxisSize: expand ? MainAxisSize.max : MainAxisSize.min,
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        if (busy)
          SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2, color: fg))
        else if (icon != null)
          Icon(icon, size: 20, color: fg),
        if (busy || icon != null) const SizedBox(width: MiseSpace.xs),
        Flexible(
          child: Text(
            label,
            textAlign: TextAlign.center,
            style: MiseType.button.copyWith(
              color: fg,
              decoration: kind == MiseButtonKind.quiet ? TextDecoration.underline : null,
              decorationColor: fg,
            ),
          ),
        ),
      ],
    );

    return Semantics(
      button: true,
      enabled: enabled,
      label: semanticsLabel,
      liveRegion: busy,
      child: AnimatedOpacity(
        duration: MiseMotion.fast,
        opacity: enabled || busy ? 1 : 0.45,
        child: TextButton(
          onPressed: enabled ? onPressed : null,
          style: TextButton.styleFrom(
            backgroundColor: bg,
            disabledBackgroundColor: bg,
            foregroundColor: fg,
            disabledForegroundColor: fg,
            minimumSize: Size(expand ? double.infinity : kMinTouchTarget, kMinTouchTarget + 4),
            padding: EdgeInsets.symmetric(horizontal: kind == MiseButtonKind.quiet ? MiseSpace.xs : MiseSpace.lg, vertical: 14),
            shape: RoundedRectangleBorder(borderRadius: const BorderRadius.all(MiseRadii.md), side: side),
            tapTargetSize: MaterialTapTargetSize.padded,
          ),
          child: child,
        ),
      ),
    );
  }
}
