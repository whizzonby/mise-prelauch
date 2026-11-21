import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';

/// − n + with announced value. The server still decides what is allowed.
class QuantitySelector extends StatelessWidget {
  const QuantitySelector({
    super.key,
    required this.value,
    required this.onChanged,
    this.min = 0,
    this.max = 99,
    required this.itemLabel,
    this.enabled = true,
  });

  final int value;
  final ValueChanged<int> onChanged;
  final int min;
  final int max;

  /// What is being counted, for screen readers ("Jerk chicken").
  final String itemLabel;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final canDec = enabled && value > min;
    final canInc = enabled && value < max;
    return Semantics(
      label: itemLabel,
      value: '$value',
      increasedValue: canInc ? '${value + 1}' : null,
      decreasedValue: canDec ? '${value - 1}' : null,
      onIncrease: canInc ? () => onChanged(value + 1) : null,
      onDecrease: canDec ? () => onChanged(value - 1) : null,
      child: ExcludeSemantics(
        child: Container(
          decoration: BoxDecoration(
            border: Border.all(color: MiseColors.borderStrong),
            borderRadius: const BorderRadius.all(MiseRadii.md),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              _StepButton(icon: Icons.remove, onTap: canDec ? () => _change(value - 1) : null),
              SizedBox(
                width: 32,
                child: Text('$value', textAlign: TextAlign.center, style: MiseType.figures(MiseType.h4)),
              ),
              _StepButton(icon: Icons.add, onTap: canInc ? () => _change(value + 1) : null),
            ],
          ),
        ),
      ),
    );
  }

  void _change(int v) {
    HapticFeedback.selectionClick();
    onChanged(v);
  }
}

class _StepButton extends StatelessWidget {
  const _StepButton({required this.icon, required this.onTap});

  final IconData icon;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: const BorderRadius.all(MiseRadii.md),
      child: SizedBox.square(
        dimension: kMinTouchTarget,
        child: Icon(icon, size: 20, color: onTap == null ? MiseColors.border : MiseColors.primary),
      ),
    );
  }
}

/// A horizontally scrolling row of single- or multi-select filters.
class FilterChips<T> extends StatelessWidget {
  const FilterChips({super.key, required this.options, required this.selected, required this.onToggle, this.label});

  final List<(T, String)> options;
  final Set<T> selected;
  final ValueChanged<T> onToggle;
  final String? label;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      container: true,
      label: label,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
        child: Row(
          children: [
            for (final (value, text) in options)
              Padding(
                padding: const EdgeInsets.only(right: MiseSpace.xs),
                child: FilterChip(
                  label: Text(text),
                  selected: selected.contains(value),
                  onSelected: (_) => onToggle(value),
                  materialTapTargetSize: MaterialTapTargetSize.padded,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// A row in a settings-style list: label, optional detail, chevron.
class MiseListTile extends StatelessWidget {
  const MiseListTile({super.key, required this.title, this.subtitle, this.leading, this.onTap, this.trailing, this.danger = false});

  final String title;
  final String? subtitle;
  final IconData? leading;
  final VoidCallback? onTap;
  final Widget? trailing;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final color = danger ? MiseColors.error : MiseColors.foreground;
    return InkWell(
      onTap: onTap,
      child: ConstrainedBox(
        constraints: const BoxConstraints(minHeight: 56),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter, vertical: MiseSpace.sm),
          child: Row(
            children: [
              if (leading != null) ...[Icon(leading, color: danger ? MiseColors.error : MiseColors.primary), const SizedBox(width: MiseSpace.md)],
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: MiseType.body.copyWith(color: color)),
                    if (subtitle != null) Text(subtitle!, style: MiseType.caption),
                  ],
                ),
              ),
              trailing ?? (onTap != null ? const Icon(Icons.chevron_right, color: MiseColors.muted) : const SizedBox()),
            ],
          ),
        ),
      ),
    );
  }
}
