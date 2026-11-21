import 'package:flutter/material.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';

enum BubbleStatus { sent, pending, failed, seen }

/// One chat message. The customer's own messages sit on the right in green;
/// the team's on the left on the surface colour. No shadows, no tails.
class ChatBubble extends StatelessWidget {
  const ChatBubble({
    super.key,
    required this.body,
    required this.mine,
    required this.timeLabel,
    this.authorName,
    this.status = BubbleStatus.sent,
    this.onRetry,
  });

  final String body;
  final bool mine;
  final String timeLabel;
  final String? authorName;
  final BubbleStatus status;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final statusText = switch (status) {
      BubbleStatus.pending => 'Sending…',
      BubbleStatus.failed => 'Not sent',
      BubbleStatus.seen => 'Seen',
      BubbleStatus.sent => null,
    };
    final who = mine ? 'You' : (authorName ?? 'Mise team');
    return Semantics(
      label: '$who, $timeLabel: $body${statusText != null ? '. $statusText' : ''}',
      excludeSemantics: true,
      child: Align(
        alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: MediaQuery.sizeOf(context).width * 0.8),
          child: Column(
            crossAxisAlignment: mine ? CrossAxisAlignment.end : CrossAxisAlignment.start,
            children: [
              if (!mine && authorName != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4, left: 4),
                  child: Text(authorName!, style: MiseType.caption.copyWith(fontWeight: FontWeight.w600)),
                ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                decoration: BoxDecoration(
                  color: mine ? MiseColors.primary : MiseColors.surface,
                  border: mine ? null : Border.all(color: MiseColors.border),
                  borderRadius: BorderRadius.only(
                    topLeft: MiseRadii.lg,
                    topRight: MiseRadii.lg,
                    bottomLeft: mine ? MiseRadii.lg : MiseRadii.sm,
                    bottomRight: mine ? MiseRadii.sm : MiseRadii.lg,
                  ),
                ),
                child: Opacity(
                  opacity: status == BubbleStatus.pending ? 0.7 : 1,
                  child: SelectableText(
                    body,
                    style: MiseType.body.copyWith(color: mine ? MiseColors.onPrimary : MiseColors.foreground),
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.only(top: 4, left: 4, right: 4),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(timeLabel, style: MiseType.caption),
                    if (statusText != null) ...[
                      const Text(' · ', style: TextStyle(color: MiseColors.muted)),
                      Text(
                        statusText,
                        style: MiseType.caption.copyWith(color: status == BubbleStatus.failed ? MiseColors.error : MiseColors.muted),
                      ),
                    ],
                    if (status == BubbleStatus.failed && onRetry != null)
                      TextButton(onPressed: onRetry, child: Text('Retry', style: MiseType.caption.copyWith(color: MiseColors.primary, fontWeight: FontWeight.w600))),
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

/// One step of approved cooking instructions: a large roucou numeral and the
/// text, readable at arm's length.
class RecipeStep extends StatelessWidget {
  const RecipeStep({super.key, required this.number, required this.total, required this.body, this.large = false});

  final int number;
  final int total;
  final String body;
  final bool large;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'Step $number of $total. $body',
      excludeSemantics: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            number.toString().padLeft(2, '0'),
            style: MiseType.figures(MiseType.displayLarge.copyWith(color: MiseColors.accent, fontSize: large ? 64 : 40)),
          ),
          const SizedBox(height: MiseSpace.sm),
          Text(body, style: large ? MiseType.bodyLarge.copyWith(fontSize: 22, height: 1.45) : MiseType.bodyLarge),
        ],
      ),
    );
  }
}
