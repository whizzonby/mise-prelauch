import 'package:flutter/material.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';
import 'display.dart';

/// A meal: photograph first, then the name and one line of facts. No box
/// around it — the photograph and type carry it.
class MealCard extends StatelessWidget {
  const MealCard({
    super.key,
    required this.name,
    required this.imageUrl,
    required this.onTap,
    this.imageAlt = '',
    this.meta,
    this.tags = const [],
    this.favorite,
    this.onFavorite,
    this.heroTag,
    this.compact = false,
    this.trailing,
  });

  final String name;
  final String? imageUrl;
  final String imageAlt;
  final String? meta;
  final List<String> tags;
  final VoidCallback onTap;

  /// null hides the favourite control (guests, or not supported here).
  final bool? favorite;
  final VoidCallback? onFavorite;
  final Object? heroTag;
  final bool compact;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    Widget photo = MiseNetworkImage(url: imageUrl, alt: imageAlt, aspectRatio: compact ? 1 : 4 / 3);
    if (heroTag != null) photo = Hero(tag: heroTag!, child: photo);

    return Semantics(
      button: true,
      label: [name, ?meta, ...tags].join(', '),
      excludeSemantics: true,
      onTap: onTap,
      child: InkWell(
        onTap: onTap,
        borderRadius: const BorderRadius.all(MiseRadii.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Stack(
              children: [
                photo,
                if (favorite != null)
                  Positioned(
                    top: MiseSpace.xs,
                    right: MiseSpace.xs,
                    child: _FavoriteButton(active: favorite!, onTap: onFavorite, mealName: name),
                  ),
              ],
            ),
            const SizedBox(height: MiseSpace.sm),
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Text(
                    name,
                    style: compact ? MiseType.h4.copyWith(fontFamily: MiseType.display, fontWeight: FontWeight.w400) : MiseType.h3,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                ?trailing,
              ],
            ),
            if (meta != null) ...[const SizedBox(height: 2), Text(meta!, style: MiseType.bodySmall)],
            if (tags.isNotEmpty) ...[
              const SizedBox(height: MiseSpace.xs),
              Wrap(spacing: 6, runSpacing: 6, children: [for (final t in tags) MiseTag(t)]),
            ],
          ],
        ),
      ),
    );
  }
}

class _FavoriteButton extends StatelessWidget {
  const _FavoriteButton({required this.active, required this.onTap, required this.mealName});

  final bool active;
  final VoidCallback? onTap;
  final String mealName;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      // Replaces the card's own semantics for this control.
      container: true,
      child: Material(
        color: MiseColors.surface,
        shape: const CircleBorder(),
        child: IconButton(
          tooltip: active ? 'Remove $mealName from favourites' : 'Save $mealName to favourites',
          isSelected: active,
          onPressed: onTap,
          icon: Icon(active ? Icons.favorite : Icons.favorite_border, color: active ? MiseColors.accent : MiseColors.foreground),
        ),
      ),
    );
  }
}

/// A plan: name, what is in it, the server's price, and a choice action.
/// Selected plans get a green rule, not a filled box.
class PlanCard extends StatelessWidget {
  const PlanCard({
    super.key,
    required this.name,
    required this.priceLabel,
    required this.facts,
    required this.onTap,
    this.selected = false,
    this.badge,
  });

  final String name;
  final String priceLabel;
  final List<String> facts;
  final VoidCallback onTap;
  final bool selected;
  final String? badge;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      selected: selected,
      label: '$name, $priceLabel. ${facts.join('. ')}',
      excludeSemantics: true,
      child: InkWell(
        onTap: onTap,
        borderRadius: const BorderRadius.all(MiseRadii.md),
        child: AnimatedContainer(
          duration: MiseMotion.fast,
          curve: MiseMotion.interaction,
          padding: const EdgeInsets.all(MiseSpace.md),
          decoration: BoxDecoration(
            color: MiseColors.surface,
            borderRadius: const BorderRadius.all(MiseRadii.md),
            border: Border.all(color: selected ? MiseColors.primary : MiseColors.border, width: selected ? 2 : 1),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(child: Text(name, style: MiseType.h3)),
                  if (badge != null) MiseTag(badge!, tone: TagTone.accent),
                ],
              ),
              const SizedBox(height: MiseSpace.xs),
              Text(priceLabel, style: MiseType.figures(MiseType.h4)),
              const SizedBox(height: MiseSpace.sm),
              for (final f in facts)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Padding(padding: EdgeInsets.only(top: 3), child: Icon(Icons.check, size: 16, color: MiseColors.secondary)),
                      const SizedBox(width: MiseSpace.xs),
                      Expanded(child: Text(f, style: MiseType.bodySmall.copyWith(color: MiseColors.foreground))),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
