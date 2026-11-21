import 'package:flutter/material.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';
import 'buttons.dart';

/// A centred spinner with an announced label.
class MiseLoading extends StatelessWidget {
  const MiseLoading({super.key, this.label = 'Loading'});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Semantics(
        label: label,
        liveRegion: true,
        child: const SizedBox.square(dimension: 28, child: CircularProgressIndicator(strokeWidth: 2.5)),
      ),
    );
  }
}

/// A grey block that pulses gently while content loads. Respects reduced motion.
class Skeleton extends StatefulWidget {
  const Skeleton({super.key, this.width, this.height = 16, this.radius = MiseRadii.sm});

  final double? width;
  final double height;
  final Radius radius;

  @override
  State<Skeleton> createState() => _SkeletonState();
}

class _SkeletonState extends State<Skeleton> with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1100));

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (MediaQuery.disableAnimationsOf(context)) {
      _c.stop();
    } else if (!_c.isAnimating) {
      _c.repeat(reverse: true);
    }
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: FadeTransition(
        opacity: Tween(begin: 0.55, end: 1.0).animate(_c),
        child: Container(
          width: widget.width,
          height: widget.height,
          decoration: BoxDecoration(color: MiseColors.imagePlaceholder, borderRadius: BorderRadius.all(widget.radius)),
        ),
      ),
    );
  }
}

/// Placeholder for a list of meal cards.
class MealListSkeleton extends StatelessWidget {
  const MealListSkeleton({super.key, this.count = 3});

  final int count;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'Loading meals',
      liveRegion: true,
      child: Column(
        children: [
          for (var i = 0; i < count; i++)
            const Padding(
              padding: EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xl),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  AspectRatio(aspectRatio: 4 / 3, child: Skeleton(radius: MiseRadii.lg)),
                  SizedBox(height: MiseSpace.sm),
                  Skeleton(width: 220, height: 22),
                  SizedBox(height: MiseSpace.xs),
                  Skeleton(width: 140),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Nothing here yet — with a way forward when there is one.
class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.title, this.message, this.actionLabel, this.onAction, this.icon});

  final String title;
  final String? message;
  final String? actionLabel;
  final VoidCallback? onAction;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter, vertical: MiseSpace.xxl),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (icon != null) ...[Icon(icon, size: 32, color: MiseColors.secondary), const SizedBox(height: MiseSpace.md)],
          Semantics(header: true, child: Text(title, style: MiseType.h3)),
          if (message != null) ...[const SizedBox(height: MiseSpace.xs), Text(message!, style: MiseType.bodySmall)],
          if (actionLabel != null) ...[
            const SizedBox(height: MiseSpace.lg),
            MiseButton.secondary(label: actionLabel!, onPressed: onAction, expand: false),
          ],
        ],
      ),
    );
  }
}

/// Something went wrong — a plain sentence and a retry. Never a stack trace.
class ErrorState extends StatelessWidget {
  const ErrorState({super.key, required this.message, this.onRetry, this.title = 'Something went wrong'});

  final String title;
  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter, vertical: MiseSpace.xxl),
      child: Semantics(
        liveRegion: true,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Icon(Icons.error_outline, size: 32, color: MiseColors.error),
            const SizedBox(height: MiseSpace.md),
            Text(title, style: MiseType.h3),
            const SizedBox(height: MiseSpace.xs),
            Text(message, style: MiseType.body.copyWith(color: MiseColors.muted)),
            if (onRetry != null) ...[
              const SizedBox(height: MiseSpace.lg),
              MiseButton.secondary(label: 'Try again', onPressed: onRetry, expand: false, icon: Icons.refresh),
            ],
          ],
        ),
      ),
    );
  }
}

/// A thin strip that says the content is stale or offline.
class NoticeBanner extends StatelessWidget {
  const NoticeBanner({super.key, required this.message, this.tone = NoticeTone.info, this.icon});

  final String message;
  final NoticeTone tone;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final color = switch (tone) {
      NoticeTone.info => MiseColors.primary,
      NoticeTone.warning => MiseColors.warning,
      NoticeTone.error => MiseColors.error,
      NoticeTone.success => MiseColors.success,
    };
    return Semantics(
      liveRegion: true,
      container: true,
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: MiseSpace.md, vertical: MiseSpace.sm),
        decoration: BoxDecoration(
          color: MiseColors.surface,
          border: Border(left: BorderSide(color: color, width: 3)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon ?? _icon(tone), size: 20, color: color),
            const SizedBox(width: MiseSpace.sm),
            Expanded(child: Text(message, style: MiseType.bodySmall.copyWith(color: MiseColors.foreground))),
          ],
        ),
      ),
    );
  }

  static IconData _icon(NoticeTone t) => switch (t) {
    NoticeTone.info => Icons.info_outline,
    NoticeTone.warning => Icons.schedule,
    NoticeTone.error => Icons.error_outline,
    NoticeTone.success => Icons.check_circle_outline,
  };
}

enum NoticeTone { info, warning, error, success }
