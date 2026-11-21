import 'package:flutter/material.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';

enum MilestoneState { done, current, upcoming, problem }

class Milestone {
  const Milestone({required this.title, required this.state, this.detail});

  final String title;
  final MilestoneState state;
  final String? detail;
}

/// A vertical list of real backend milestones (order or delivery). The line
/// fills up to the current step; nothing is animated as if it were live.
class MilestoneTimeline extends StatelessWidget {
  const MilestoneTimeline({super.key, required this.milestones});

  final List<Milestone> milestones;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        for (var i = 0; i < milestones.length; i++)
          _MilestoneRow(milestone: milestones[i], first: i == 0, last: i == milestones.length - 1, position: i + 1, total: milestones.length),
      ],
    );
  }
}

class _MilestoneRow extends StatelessWidget {
  const _MilestoneRow({required this.milestone, required this.first, required this.last, required this.position, required this.total});

  final Milestone milestone;
  final bool first;
  final bool last;
  final int position;
  final int total;

  @override
  Widget build(BuildContext context) {
    final m = milestone;
    final (color, icon) = switch (m.state) {
      MilestoneState.done => (MiseColors.primary, Icons.check),
      MilestoneState.current => (MiseColors.accent, null),
      MilestoneState.upcoming => (MiseColors.border, null),
      MilestoneState.problem => (MiseColors.error, Icons.priority_high),
    };
    final stateLabel = switch (m.state) {
      MilestoneState.done => 'done',
      MilestoneState.current => 'current step',
      MilestoneState.upcoming => 'not yet',
      MilestoneState.problem => 'needs attention',
    };
    final lineColor = m.state == MilestoneState.done ? MiseColors.primary : MiseColors.border;
    return Semantics(
      label: 'Step $position of $total: ${m.title}, $stateLabel${m.detail != null ? '. ${m.detail}' : ''}',
      excludeSemantics: true,
      child: IntrinsicHeight(
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            SizedBox(
              width: 28,
              child: Column(
                children: [
                  Container(width: 2, height: 6, color: first ? Colors.transparent : lineColor),
                  Container(
                    width: 22,
                    height: 22,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: m.state == MilestoneState.upcoming ? MiseColors.background : color,
                      border: Border.all(color: color, width: 2),
                    ),
                    child: icon == null ? null : Icon(icon, size: 14, color: MiseColors.onPrimary),
                  ),
                  Expanded(child: Container(width: 2, color: last ? Colors.transparent : lineColor)),
                ],
              ),
            ),
            const SizedBox(width: MiseSpace.md),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(top: 6, bottom: MiseSpace.lg),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      m.title,
                      style: MiseType.body.copyWith(
                        fontWeight: m.state == MilestoneState.current ? FontWeight.w600 : FontWeight.w400,
                        color: m.state == MilestoneState.upcoming ? MiseColors.muted : MiseColors.foreground,
                      ),
                    ),
                    if (m.detail != null) Text(m.detail!, style: MiseType.caption),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// "2 of 3 chosen" with a thin bar.
class SelectionProgress extends StatelessWidget {
  const SelectionProgress({super.key, required this.chosen, required this.required});

  final int chosen;
  final int required;

  @override
  Widget build(BuildContext context) {
    final done = chosen >= required;
    return Semantics(
      label: '$chosen of $required meals chosen',
      excludeSemantics: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            done ? 'All $required meals chosen' : '$chosen of $required chosen',
            style: MiseType.label.copyWith(color: done ? MiseColors.success : MiseColors.foreground),
          ),
          const SizedBox(height: MiseSpace.xs),
          ClipRRect(
            borderRadius: const BorderRadius.all(MiseRadii.sm),
            child: LinearProgressIndicator(value: required == 0 ? 0 : (chosen / required).clamp(0, 1), minHeight: 4),
          ),
        ],
      ),
    );
  }
}
