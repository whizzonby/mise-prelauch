import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';

import '../../shared/formatting.dart';
import '../tokens/tokens.dart';
import '../typography/typography.dart';

/// The Mise wordmark: the name in the display face, closed with a roucou full stop.
class Wordmark extends StatelessWidget {
  const Wordmark({super.key, this.size = 28, this.onDark = false});

  final double size;
  final bool onDark;

  @override
  Widget build(BuildContext context) {
    final base = MiseType.h1.copyWith(
      fontSize: size,
      height: 1,
      color: onDark ? MiseColors.onPrimary : MiseColors.primary,
    );
    return Semantics(
      label: 'Mise',
      excludeSemantics: true,
      child: Text.rich(
        TextSpan(
          text: 'Mise',
          style: base,
          children: [TextSpan(text: '.', style: base.copyWith(color: onDark ? MiseColors.onPrimary : MiseColors.accent))],
        ),
      ),
    );
  }
}

/// An amount from the API, with its currency. Never computed in the app.
class PriceText extends StatelessWidget {
  const PriceText({super.key, required this.minor, required this.currency, this.style, this.suffix});

  final int minor;
  final String currency;
  final TextStyle? style;

  /// e.g. "a week".
  final String? suffix;

  @override
  Widget build(BuildContext context) {
    final s = MiseType.figures(style ?? MiseType.h4);
    return Text.rich(
      TextSpan(
        text: formatMoney(minor, currency),
        style: s,
        children: [if (suffix != null) TextSpan(text: ' $suffix', style: MiseType.bodySmall)],
      ),
    );
  }
}

enum TagTone { neutral, success, warning, error, accent }

/// A short piece of state: a dietary indicator, a status.
class MiseTag extends StatelessWidget {
  const MiseTag(this.label, {super.key, this.tone = TagTone.neutral});

  final String label;
  final TagTone tone;

  @override
  Widget build(BuildContext context) {
    final color = switch (tone) {
      TagTone.neutral => MiseColors.muted,
      TagTone.success => MiseColors.success,
      TagTone.warning => MiseColors.warning,
      TagTone.error => MiseColors.error,
      TagTone.accent => MiseColors.accent,
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        border: Border.all(color: color.withValues(alpha: 0.55)),
        borderRadius: const BorderRadius.all(MiseRadii.sm),
      ),
      child: Text(label, style: MiseType.caption.copyWith(color: color, fontWeight: FontWeight.w600)),
    );
  }
}

/// Unread count on an icon. Hidden at zero.
class CountBadge extends StatelessWidget {
  const CountBadge({super.key, required this.count, required this.child});

  final int count;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Badge(
      isLabelVisible: count > 0,
      backgroundColor: MiseColors.accent,
      textColor: MiseColors.onPrimary,
      label: Text(count > 99 ? '99+' : '$count'),
      child: child,
    );
  }
}

/// A section title with a hairline rule above it, the editorial device the
/// web uses instead of boxes.
class SectionHeader extends StatelessWidget {
  const SectionHeader({super.key, required this.title, this.action, this.onAction, this.padding});

  final String title;
  final String? action;
  final VoidCallback? onAction;
  final EdgeInsetsGeometry? padding;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: padding ?? const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Divider(color: MiseColors.foreground),
          const SizedBox(height: MiseSpace.sm),
          Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: [
              Expanded(child: Semantics(header: true, child: Text(title, style: MiseType.h3))),
              if (action != null)
                TextButton(
                  onPressed: onAction,
                  style: TextButton.styleFrom(minimumSize: const Size(kMinTouchTarget, kMinTouchTarget)),
                  child: Text(action!, style: MiseType.label.copyWith(color: MiseColors.primary)),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

/// A photograph from the API, cached on disk and decoded at display size so
/// the memory cache stays bounded.
class MiseNetworkImage extends StatelessWidget {
  const MiseNetworkImage({
    super.key,
    required this.url,
    this.alt = '',
    this.aspectRatio,
    this.radius = MiseRadii.lg,
    this.fit = BoxFit.cover,
  });

  final String? url;
  final String alt;
  final double? aspectRatio;
  final Radius radius;
  final BoxFit fit;

  @override
  Widget build(BuildContext context) {
    final placeholder = Container(color: MiseColors.imagePlaceholder);
    Widget image = LayoutBuilder(
      builder: (context, c) {
        final dpr = MediaQuery.devicePixelRatioOf(context);
        final w = c.maxWidth.isFinite ? (c.maxWidth * dpr).round() : null;
        if (url == null || url!.isEmpty) {
          return Container(
            color: MiseColors.imagePlaceholder,
            alignment: Alignment.center,
            child: const Icon(Icons.restaurant_outlined, color: MiseColors.borderStrong, size: 32),
          );
        }
        // Bundled images (`asset:` URLs) — used for test renders and fallbacks.
        if (url!.startsWith('asset:')) {
          return Image.asset(url!.substring(6), fit: fit, cacheWidth: w);
        }
        return CachedNetworkImage(
          imageUrl: url!,
          fit: fit,
          memCacheWidth: w,
          fadeInDuration: MiseMotion.standard,
          placeholder: (_, _) => placeholder,
          errorWidget: (_, _, _) => Container(
            color: MiseColors.imagePlaceholder,
            alignment: Alignment.center,
            child: const Icon(Icons.image_not_supported_outlined, color: MiseColors.borderStrong),
          ),
        );
      },
    );
    if (aspectRatio != null) image = AspectRatio(aspectRatio: aspectRatio!, child: image);
    return Semantics(
      image: alt.isNotEmpty,
      label: alt.isEmpty ? null : alt,
      excludeSemantics: true,
      child: ClipRRect(borderRadius: BorderRadius.all(radius), child: image),
    );
  }
}

/// Label above, value below. For order and account facts.
class FactRow extends StatelessWidget {
  const FactRow({super.key, required this.label, required this.value, this.trailing});

  final String label;
  final String value;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return MergeSemantics(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: MiseSpace.sm),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(label, style: MiseType.caption),
                  const SizedBox(height: 2),
                  Text(value, style: MiseType.body),
                ],
              ),
            ),
            ?trailing,
          ],
        ),
      ),
    );
  }
}
