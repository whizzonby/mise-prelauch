import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import 'support_models.dart';
import 'support_state.dart';

/// Support home: chat with the Mise team (people, not a bot) and the help centre.
class SupportScreen extends ConsumerWidget {
  const SupportScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final signedIn = ref.watch(isSignedInProvider);
    final avail = ref.watch(chatAvailabilityProvider).value;
    final convs = ref.watch(conversationsProvider);
    return Scaffold(
      appBar: AppBar(title: Text('Support', style: MiseType.h2)),
      body: RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(chatAvailabilityProvider);
          ref.invalidate(conversationsProvider);
        },
        child: ListView(
          padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.lg),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Talk to the Mise team', style: MiseType.h3),
                  const SizedBox(height: 4),
                  if (avail != null) ...[
                    if (avail.online == true) const MiseTag('Online now', tone: TagTone.success),
                    if (avail.online == false) const MiseTag('Away', tone: TagTone.neutral),
                    const SizedBox(height: 4),
                    Text(avail.responseExpectation.isNotEmpty ? avail.responseExpectation : "We'll reply as soon as we can.", style: MiseType.bodySmall),
                    if (avail.message.isNotEmpty) Text(avail.message, style: MiseType.bodySmall),
                  ],
                  const SizedBox(height: MiseSpace.md),
                  if (avail != null && !avail.enabled)
                    const NoticeBanner(tone: NoticeTone.warning, message: 'Messaging is closed right now. The help centre is open.')
                  else if (signedIn)
                    MiseButton(label: 'New message', icon: Icons.edit_outlined, onPressed: () => context.push('/support/new'))
                  else
                    MiseButton(
                      label: 'Sign in to message us',
                      onPressed: () => context.push(Uri(path: '/sign-in', queryParameters: {'from': '/support'}).toString()),
                    ),
                ],
              ),
            ),
            if (signedIn)
              convs.when(
                loading: () => const Padding(padding: EdgeInsets.all(MiseSpace.lg), child: MiseLoading()),
                error: (e, _) => ErrorState(title: 'Messages could not load', message: AppError.from(e).message, onRetry: () => ref.invalidate(conversationsProvider)),
                data: (list) => list.isEmpty
                    ? const SizedBox.shrink()
                    : Column(
                        children: [
                          const SectionHeader(title: 'Your conversations'),
                          for (final c in list)
                            MiseListTile(
                              title: c.subject.isNotEmpty ? c.subject : 'Conversation ${c.number}',
                              subtitle: [
                                c.lastMessage,
                                if (c.lastMessageAt != null) formatInstant(c.lastMessageAt!),
                                if (c.resolved) 'Resolved',
                              ].where((x) => x.isNotEmpty).join(' · '),
                              trailing: CountBadge(count: c.unread, child: const Icon(Icons.chevron_right, color: MiseColors.muted)),
                              onTap: () => context.push('/support/conversations/${c.id}'),
                            ),
                        ],
                      ),
              ),
            const SizedBox(height: MiseSpace.lg),
            const SectionHeader(title: 'Help centre'),
            MiseListTile(leading: Icons.search, title: 'Search help articles', onTap: () => context.push('/support/help')),
            ...(ref.watch(helpCategoriesProvider).value ?? const <HelpCategory>[])
                .where((c) => c.articles > 0)
                .map((c) => MiseListTile(title: c.name, subtitle: plural(c.articles, 'article'), onTap: () => context.push('/support/help?category=${c.slug}'))),
          ],
        ),
      ),
    );
  }
}

class NewConversationScreen extends ConsumerStatefulWidget {
  const NewConversationScreen({super.key});

  @override
  ConsumerState<NewConversationScreen> createState() => _NewConversationScreenState();
}

class _NewConversationScreenState extends ConsumerState<NewConversationScreen> {
  final _subject = TextEditingController();
  final _body = TextEditingController();
  final String _clientId = ApiClient.newKey();
  bool _busy = false;
  String? _error;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final order = GoRouterState.of(context).uri.queryParameters['order'];
    if (order != null && _subject.text.isEmpty && RegExp(r'^[A-Za-z0-9-]{1,40}$').hasMatch(order)) _subject.text = 'Order $order';
  }

  @override
  void dispose() {
    _subject.dispose();
    _body.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final body = _body.text.trim();
    if (body.isEmpty || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      // Retrying with the same client id returns the same conversation.
      final (c, _) = await ref.read(supportApiProvider).start(body: body, clientMessageId: _clientId, subject: _subject.text.trim().isEmpty ? null : _subject.text.trim());
      ref.invalidate(conversationsProvider);
      if (mounted) context.pushReplacement('/support/conversations/${c.id}');
    } on AppError catch (e) {
      setState(() => _error = e.isNetwork ? 'Not sent: you seem to be offline. Your message is still here.' : e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('New message')),
      body: ListView(
        padding: const EdgeInsets.all(MiseSpace.gutter),
        children: [
          Text('A person from the Mise team will answer here. Please do not share card numbers or passwords.', style: MiseType.bodySmall),
          const SizedBox(height: MiseSpace.lg),
          MiseTextField(label: 'Subject (optional)', controller: _subject, maxLength: 160, textCapitalization: TextCapitalization.sentences),
          const SizedBox(height: MiseSpace.md),
          MiseTextField(label: 'Message', controller: _body, maxLines: 6, maxLength: 4000, textCapitalization: TextCapitalization.sentences),
          if (_error != null) ...[const SizedBox(height: MiseSpace.md), NoticeBanner(tone: NoticeTone.error, message: _error!)],
        ],
      ),
      bottomNavigationBar: BottomActionBar(child: MiseButton(label: 'Send', busy: _busy, onPressed: _send)),
    );
  }
}

class ConversationScreen extends ConsumerStatefulWidget {
  const ConversationScreen({super.key, required this.id});

  final String id;

  @override
  ConsumerState<ConversationScreen> createState() => _ConversationScreenState();
}

class _ConversationScreenState extends ConsumerState<ConversationScreen> {
  final _input = TextEditingController();

  @override
  void dispose() {
    _input.dispose();
    super.dispose();
  }

  void _send() {
    final body = _input.text.trim();
    if (body.isEmpty) return;
    _input.clear();
    unawaited(ref.read(chatControllerProvider(widget.id).notifier).send(body));
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(chatControllerProvider(widget.id));
    final ctrl = ref.read(chatControllerProvider(widget.id).notifier);
    final c = s.conversation;
    final msgs = s.log.ordered.reversed.toList();
    final seenUpTo = c?.teamReadSeq ?? 0;
    final mineSeqs = [for (final m in s.log.ordered) if (m.mine && m.seq > 0) m.seq];
    final lastMineSeq = mineSeqs.isEmpty ? 0 : mineSeqs.last;

    if (s.error != null && s.error!.kind == ErrorKind.notFound) {
      return Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Conversation not found'));
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(c == null ? 'Conversation' : (c.subject.isNotEmpty ? c.subject : 'Conversation ${c.number}')),
        bottom: s.live || s.loading
            ? null
            : const PreferredSize(
                preferredSize: Size.fromHeight(32),
                child: NoticeBanner(tone: NoticeTone.warning, icon: Icons.sync_problem, message: 'Reconnecting… new replies appear when you are back online.'),
              ),
      ),
      body: Column(
        children: [
          if (s.error != null && !s.error!.isNetwork)
            Padding(padding: const EdgeInsets.all(MiseSpace.sm), child: NoticeBanner(tone: NoticeTone.error, message: s.error!.message)),
          Expanded(
            child: s.loading && msgs.isEmpty
                ? const MiseLoading(label: 'Loading messages')
                : RefreshIndicator(
                    onRefresh: ctrl.refresh,
                    child: ListView.separated(
                      reverse: true,
                      padding: const EdgeInsets.all(MiseSpace.gutter),
                      itemCount: msgs.length + (s.agentTyping ? 1 : 0),
                      separatorBuilder: (_, _) => const SizedBox(height: MiseSpace.sm),
                      itemBuilder: (context, i) {
                        if (s.agentTyping && i == 0) {
                          return Semantics(liveRegion: true, child: Text('The Mise team is typing…', style: MiseType.caption));
                        }
                        final m = msgs[i - (s.agentTyping ? 1 : 0)];
                        final status = switch (m.state) {
                          SendState.pending => BubbleStatus.pending,
                          SendState.failed => BubbleStatus.failed,
                          SendState.sent => (m.mine && m.seq == lastMineSeq && m.seq <= seenUpTo) ? BubbleStatus.seen : BubbleStatus.sent,
                        };
                        if (m.author == 'system') {
                          return Center(child: Text(m.body, style: MiseType.caption, textAlign: TextAlign.center));
                        }
                        return ChatBubble(
                          body: m.body,
                          mine: m.mine,
                          authorName: m.mine ? null : (m.authorName.isEmpty ? 'Mise team' : m.authorName),
                          timeLabel: formatTime(m.createdAt),
                          status: status,
                          onRetry: m.state == SendState.failed ? () => ctrl.retry(m) : null,
                        );
                      },
                    ),
                  ),
          ),
          if (c != null && c.resolved && c.feedbackRating == null) _FeedbackPrompt(onRate: (r) => ctrl.feedback(r, null)),
          DecoratedBox(
            decoration: const BoxDecoration(color: MiseColors.surface, border: Border(top: BorderSide(color: MiseColors.border))),
            child: SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(MiseSpace.sm, MiseSpace.xs, MiseSpace.xs, MiseSpace.xs),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _input,
                        minLines: 1,
                        maxLines: 5,
                        maxLength: 4000,
                        textCapitalization: TextCapitalization.sentences,
                        onChanged: (_) => ctrl.typing(),
                        decoration: InputDecoration(
                          hintText: c?.resolved == true ? 'Write to reopen the conversation' : 'Write a message',
                          counterText: '',
                        ),
                      ),
                    ),
                    IconButton(tooltip: 'Send', icon: const Icon(Icons.send, color: MiseColors.primary), onPressed: _send),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _FeedbackPrompt extends StatelessWidget {
  const _FeedbackPrompt({required this.onRate});

  final ValueChanged<int> onRate;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter, vertical: MiseSpace.xs),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('How did we do?', style: MiseType.label),
          Row(
            children: [
              for (var r = 1; r <= 5; r++)
                IconButton(tooltip: '$r out of 5', icon: const Icon(Icons.star_border, color: MiseColors.accent), onPressed: () => onRate(r)),
            ],
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------- help centre

class HelpScreen extends ConsumerStatefulWidget {
  const HelpScreen({super.key, this.category});

  final String? category;

  @override
  ConsumerState<HelpScreen> createState() => _HelpScreenState();
}

class _HelpScreenState extends ConsumerState<HelpScreen> {
  final _q = TextEditingController();
  Timer? _debounce;
  late Future<List<HelpArticle>> _articles = _fetch();

  Future<List<HelpArticle>> _fetch() => ref.read(supportApiProvider).helpArticles(q: _q.text.trim(), category: widget.category);

  @override
  void dispose() {
    _debounce?.cancel();
    _q.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Help centre')),
      body: ListView(
        padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
            child: MiseSearchField(
              controller: _q,
              hint: 'Search help',
              onChanged: (_) {
                _debounce?.cancel();
                _debounce = Timer(const Duration(milliseconds: 350), () => setState(() => _articles = _fetch()));
              },
            ),
          ),
          FutureBuilder<List<HelpArticle>>(
            future: _articles,
            builder: (context, snap) {
              if (snap.connectionState != ConnectionState.done) return const MiseLoading();
              if (snap.hasError) return ErrorState(message: AppError.from(snap.error!).message, onRetry: () => setState(() => _articles = _fetch()));
              final list = snap.data!;
              if (list.isEmpty) {
                return EmptyState(
                  title: _q.text.isEmpty ? 'No articles yet' : 'Nothing found',
                  message: 'Message the Mise team and we will help.',
                  actionLabel: 'Message us',
                  onAction: () => context.go('/support'),
                );
              }
              return Column(
                children: [for (final a in list) MiseListTile(title: a.title, subtitle: a.summary, onTap: () => context.push('/support/help/${a.slug}'))],
              );
            },
          ),
        ],
      ),
    );
  }
}

class HelpArticleScreen extends ConsumerWidget {
  const HelpArticleScreen({super.key, required this.slug});

  final String slug;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(),
      body: FutureBuilder<HelpArticle>(
        future: ref.read(supportApiProvider).helpArticle(slug),
        builder: (context, snap) {
          if (snap.connectionState != ConnectionState.done) return const MiseLoading();
          if (snap.hasError) {
            final e = AppError.from(snap.error!);
            return e.kind == ErrorKind.notFound ? const EmptyState(title: 'Article not found') : ErrorState(message: e.message);
          }
          final a = snap.data!;
          return ListView(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xxl),
            children: [
              Semantics(header: true, child: Text(a.title, style: MiseType.h1)),
              if (a.updatedAt != null) Text('Updated ${formatInstant(a.updatedAt!)}', style: MiseType.caption),
              const SizedBox(height: MiseSpace.lg),
              // Plain text only — never rendered as HTML.
              for (final p in a.paragraphs) Padding(padding: const EdgeInsets.only(bottom: MiseSpace.md), child: Text(p, style: MiseType.bodyLarge)),
              const SizedBox(height: MiseSpace.lg),
              const Divider(),
              const SizedBox(height: MiseSpace.md),
              Text('Still need help?', style: MiseType.h4),
              const SizedBox(height: MiseSpace.sm),
              MiseButton.secondary(label: 'Message the Mise team', onPressed: () => context.go('/support')),
            ],
          );
        },
      ),
    );
  }
}
