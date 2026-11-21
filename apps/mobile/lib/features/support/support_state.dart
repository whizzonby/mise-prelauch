import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../core/errors/app_error.dart';
import '../../core/realtime/sse.dart';
import '../../core/storage/storage.dart';
import 'support_models.dart';

/// Mise Chat for signed-in customers (`/me/chat/...`). Same inbox as the
/// website widget; no separate backend and no chatbot.
class SupportApi {
  SupportApi(this.api);

  final ApiClient api;

  Future<ChatAvailability> availability() => api.get('/chat/availability', (d) => ChatAvailability.fromJson(asMap(d)), options: publicOptions);

  Future<List<Conversation>> conversations() => api.get('/me/chat/conversations', (d) => itemsOf(d, Conversation.fromJson));

  Future<Conversation> conversation(String id) => api.get('/me/chat/conversations/$id', (d) => Conversation.fromJson(asMap(d)));

  /// All messages after [after], following the 200-per-page contract.
  Future<List<ChatMessage>> messagesAfter(String id, int after) async {
    final out = <ChatMessage>[];
    var cursor = after;
    while (true) {
      final page = await api.get('/me/chat/conversations/$id/messages', (d) => itemsOf(d, ChatMessage.fromJson), query: {'after': cursor});
      out.addAll(page);
      if (page.length < 200) return out;
      cursor = page.last.seq;
    }
  }

  Future<(Conversation, ChatMessage)> start({required String body, required String clientMessageId, String? subject}) => api.post(
    '/me/chat/conversations',
    (d) {
      final j = asMap(d);
      return (Conversation.fromJson(asMap(j['conversation'])), ChatMessage.fromJson(asMap(j['message'])));
    },
    body: {'body': body, 'client_message_id': clientMessageId, 'subject': ?subject, 'source': 'app'},
  );

  Future<ChatMessage> send(String id, String body, String clientMessageId) =>
      api.post('/me/chat/conversations/$id/messages', (d) => ChatMessage.fromJson(asMap(d)), body: {'body': body, 'client_message_id': clientMessageId});

  Future<void> markRead(String id, int seq) => api.post<void>('/me/chat/conversations/$id/read', (_) {}, body: {'seq': seq});
  Future<void> typing(String id) => api.post<void>('/me/chat/conversations/$id/typing', (_) {});
  Future<void> feedback(String id, int rating, String? comment) =>
      api.post<void>('/me/chat/conversations/$id/feedback', (_) {}, body: {'rating': rating, if (comment != null && comment.isNotEmpty) 'comment': comment});

  Future<List<HelpCategory>> helpCategories() => api.get('/help/categories', (d) => itemsOf(d, HelpCategory.fromJson), options: publicOptions);
  Future<List<HelpArticle>> helpArticles({String? q, String? category}) =>
      api.get('/help/articles', (d) => itemsOf(d, HelpArticle.fromJson), query: {'q': q, 'category': category}, options: publicOptions);
  Future<HelpArticle> helpArticle(String slug) => api.get('/help/articles/$slug', (d) => HelpArticle.fromJson(asMap(d)), options: publicOptions);
}

final supportApiProvider = Provider<SupportApi>((ref) => SupportApi(ref.watch(apiClientProvider)));

final chatAvailabilityProvider = FutureProvider<ChatAvailability>((ref) => ref.watch(supportApiProvider).availability());

final conversationsProvider = FutureProvider<List<Conversation>>((ref) async {
  if (!ref.watch(isSignedInProvider)) return const [];
  return ref.watch(supportApiProvider).conversations();
});

/// Unread team replies across the customer's conversations (tab badge).
final supportUnreadProvider = Provider<int>((ref) {
  final list = ref.watch(conversationsProvider).value ?? const [];
  return list.fold(0, (a, c) => a + c.unread);
});

final helpCategoriesProvider = FutureProvider<List<HelpCategory>>((ref) => ref.watch(supportApiProvider).helpCategories());

// ---------------------------------------------------------------- one conversation

class ChatState {
  const ChatState({this.conversation, this.log = const MessageLog(), this.loading = true, this.error, this.agentTyping = false, this.live = false});

  final Conversation? conversation;
  final MessageLog log;
  final bool loading;
  final AppError? error;
  final bool agentTyping;

  /// The live stream is connected (otherwise updates come on refresh).
  final bool live;

  ChatState copy({Conversation? conversation, MessageLog? log, bool? loading, AppError? error, bool clearError = false, bool? agentTyping, bool? live}) => ChatState(
    conversation: conversation ?? this.conversation,
    log: log ?? this.log,
    loading: loading ?? this.loading,
    error: clearError ? null : (error ?? this.error),
    agentTyping: agentTyping ?? this.agentTyping,
    live: live ?? this.live,
  );
}

class ChatController extends Notifier<ChatState> with WidgetsBindingObserver {
  ChatController(this.id);

  final String id;
  SseConnection? _sse;
  Timer? _typingClear;
  DateTime _lastTypingSent = DateTime.fromMillisecondsSinceEpoch(0);

  SupportApi get _api => ref.read(supportApiProvider);
  CacheStore get _cache => ref.read(cacheStoreProvider);

  @override
  ChatState build() {
    WidgetsBinding.instance.addObserver(this);
    ref.onDispose(() {
      WidgetsBinding.instance.removeObserver(this);
      _sse?.close();
      _typingClear?.cancel();
    });
    unawaited(_load());
    return const ChatState();
  }

  Future<void> _load() async {
    // Offline: show the saved copy first, then catch up.
    final saved = await _cache.get(cacheAccount, 'chat_$id');
    if (saved != null && state.log.stored.isEmpty) {
      final msgs = asList(saved.value, ChatMessage.fromJson);
      state = state.copy(log: state.log.merge(msgs));
    }
    try {
      final c = await _api.conversation(id);
      final msgs = await _api.messagesAfter(id, state.log.lastSeq);
      state = state.copy(conversation: c, log: state.log.merge(msgs), loading: false, clearError: true);
      _save();
      _openStream();
      await _markRead();
    } on AppError catch (e) {
      state = state.copy(loading: false, error: e);
    }
  }

  void _save() {
    final list = [for (final m in state.log.stored.values) m.toJson()];
    unawaited(_cache.put(cacheAccount, 'chat_$id', list));
  }

  void _openStream() {
    _sse?.close();
    _sse = SseConnection(
      dio: ref.read(apiClientProvider).dio,
      path: '/me/chat/conversations/$id/stream',
      lastSeq: () => state.log.lastSeq,
      onEvent: _onEvent,
      onFatal: () => state = state.copy(live: false),
    )..open();
    state = state.copy(live: true);
  }

  void _onEvent(SseEvent e) {
    try {
      switch (e.event) {
        case 'message':
          final m = ChatMessage.fromJson(asMap(jsonDecode(e.data)));
          state = state.copy(log: state.log.merge([m]), agentTyping: m.mine ? state.agentTyping : false);
          _save();
          if (!m.mine) unawaited(_markRead());
        case 'conversation':
          state = state.copy(conversation: Conversation.fromJson(asMap(jsonDecode(e.data))));
        case 'typing':
          state = state.copy(agentTyping: true);
          _typingClear?.cancel();
          _typingClear = Timer(const Duration(seconds: 4), () => state = state.copy(agentTyping: false));
      }
    } catch (_) {
      // A malformed event is ignored; the next catch-up corrects the view.
    }
  }

  /// The stream is closed in the background (a suspended app cannot hold it)
  /// and the gap is fetched on return.
  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.paused) {
      _sse?.close();
      state = state.copy(live: false);
    } else if (s == AppLifecycleState.resumed) {
      unawaited(refresh());
    }
  }

  Future<void> refresh() async {
    try {
      final msgs = await _api.messagesAfter(id, state.log.lastSeq);
      final c = await _api.conversation(id);
      state = state.copy(conversation: c, log: state.log.merge(msgs), clearError: true);
      _save();
      _openStream();
      await _markRead();
      ref.invalidate(conversationsProvider);
    } on AppError catch (e) {
      state = state.copy(error: e);
    }
  }

  Future<void> _markRead() async {
    final seq = state.log.lastSeq;
    if (seq > 0) {
      try {
        await _api.markRead(id, seq);
      } catch (_) {}
    }
  }

  /// Shows the message as pending, then as sent only when the server stores
  /// it. The client id is fixed before the first attempt and reused for
  /// every retry, so a message is never posted twice.
  Future<void> send(String body, {String? clientMessageId}) async {
    final cid = clientMessageId ?? ApiClient.newKey();
    final pending = ChatMessage(seq: 0, author: 'customer', authorName: '', body: body, clientMessageId: cid, createdAt: DateTime.now().toUtc(), state: SendState.pending);
    state = state.copy(log: state.log.addPending(pending));
    try {
      final stored = await _api.send(id, body, cid);
      state = state.copy(log: state.log.merge([stored]));
      _save();
    } on AppError {
      state = state.copy(log: state.log.mark(cid, SendState.failed));
    }
  }

  Future<void> retry(ChatMessage m) => send(m.body, clientMessageId: m.clientMessageId);

  void typing() {
    if (DateTime.now().difference(_lastTypingSent) < const Duration(seconds: 3)) return;
    _lastTypingSent = DateTime.now();
    unawaited(_api.typing(id).catchError((_) {}));
  }

  Future<void> feedback(int rating, String? comment) async {
    await _api.feedback(id, rating, comment);
    await refresh();
  }
}

final chatControllerProvider = NotifierProvider.autoDispose.family<ChatController, ChatState, String>(ChatController.new);
