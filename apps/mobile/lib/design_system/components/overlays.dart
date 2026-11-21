import 'package:flutter/material.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';
import 'buttons.dart';

/// A bottom sheet with a title, scrollable body and safe-area padding.
Future<T?> showMiseSheet<T>(BuildContext context, {required String title, required WidgetBuilder builder}) {
  return showModalBottomSheet<T>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (ctx) => Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(ctx).bottom),
      child: ConstrainedBox(
        constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(ctx).height * 0.85),
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xl),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Semantics(header: true, child: Text(title, style: MiseType.h3)),
              const SizedBox(height: MiseSpace.md),
              builder(ctx),
            ],
          ),
        ),
      ),
    ),
  );
}

/// Asks before a consequential action and says what will happen.
/// Returns true only on an explicit confirm.
Future<bool> confirmAction(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  String cancelLabel = 'Keep it as it is',
  bool destructive = false,
}) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(title),
      content: Text(message),
      actionsPadding: const EdgeInsets.fromLTRB(MiseSpace.lg, 0, MiseSpace.lg, MiseSpace.lg),
      actions: [
        Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            MiseButton(
              label: confirmLabel,
              kind: destructive ? MiseButtonKind.danger : MiseButtonKind.primary,
              onPressed: () => Navigator.of(ctx).pop(true),
            ),
            const SizedBox(height: MiseSpace.xs),
            MiseButton.quiet(label: cancelLabel, onPressed: () => Navigator.of(ctx).pop(false), expand: true),
          ],
        ),
      ],
    ),
  );
  return ok ?? false;
}

void showMessage(BuildContext context, String message) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(message)));
}

/// A fixed action area at the bottom of a screen (meal detail, checkout),
/// above the home indicator and the keyboard.
class BottomActionBar extends StatelessWidget {
  const BottomActionBar({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: const BoxDecoration(
        color: MiseColors.surface,
        border: Border(top: BorderSide(color: MiseColors.border)),
      ),
      child: SafeArea(
        top: false,
        minimum: const EdgeInsets.only(bottom: MiseSpace.sm),
        child: Padding(padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.sm, MiseSpace.gutter, 0), child: child),
      ),
    );
  }
}
