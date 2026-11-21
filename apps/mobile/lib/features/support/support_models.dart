import '../../core/api/api_client.dart';

class ChatAvailability {
  const ChatAvailability({required this.enabled, required this.online, required this.message, required this.responseExpectation});

  factory ChatAvailability.fromJson(Json j) => ChatAvailability(
    enabled: boolean(j, 'enabled'),
    online: j['online'] as bool?,
    message: str(j, 'message'),
    responseExpectation: str(j, 'response_expectation'),
  );

  final bool enabled;

  /// null: no hours configured — claim nothing.
  final bool? online;
  final String message;
  final String responseExpectation;
}

class Conversation {
  const Conversation({
    required this.id,
    required this.number,
    required this.status,
    required this.subject,
    required this.lastSeq,
    required this.lastMessage,
    required this.lastMessageAt,
    required this.unread,
    required this.teamReadSeq,
    required this.feedbackRating,
  });

  factory Conversation.fromJson(Json j) => Conversation(
    id: str(j, 'id'),
    number: integer(j, 'number'),
    status: str(j, 'status'),
    subject: str(j, 'subject'),
    lastSeq: integer(j, 'last_seq'),
    lastMessage: str(j, 'last_message'),
    lastMessageAt: time(j, 'last_message_at'),
    unread: integer(j, 'unread'),
    teamReadSeq: integer(j, 'team_read_seq'),
    feedbackRating: intOrNull(j, 'feedback_rating'),
  );

  final String id;
  final int number;

  /// open | pending | resolved
  final String status;
  final String subject;
  final int lastSeq;
  final String lastMessage;
  final DateTime? lastMessageAt;
  final int unread;
  final int teamReadSeq;
  final int? feedbackRating;

  bool get resolved => status == 'resolved';
}

enum SendState { sent, pending, failed }

class ChatMessage {
  const ChatMessage({
    required this.seq,
    required this.author,
    required this.authorName,
    required this.body,
    required this.clientMessageId,
    required this.createdAt,
    this.state = SendState.sent,
  });

  factory ChatMessage.fromJson(Json j) => ChatMessage(
    seq: integer(j, 'seq'),
    author: str(j, 'author'),
    authorName: str(j, 'author_name'),
    body: str(j, 'body'),
    clientMessageId: strOrNull(j, 'client_message_id'),
    createdAt: time(j, 'created_at') ?? DateTime.now().toUtc(),
  );

  /// 0 while not yet stored by the server.
  final int seq;

  /// customer | guest | agent | system
  final String author;
  final String authorName;
  final String body;
  final String? clientMessageId;
  final DateTime createdAt;
  final SendState state;

  bool get mine => author == 'customer' || author == 'guest';

  ChatMessage withState(SendState s) =>
      ChatMessage(seq: seq, author: author, authorName: authorName, body: body, clientMessageId: clientMessageId, createdAt: createdAt, state: s);

  Json toJson() => {
    'seq': seq,
    'author': author,
    'author_name': authorName,
    'body': body,
    'client_message_id': clientMessageId,
    'created_at': createdAt.toIso8601String(),
  };
}

/// The messages of one conversation, merged from history, the live stream
/// and the customer's own sends. Pure and testable.
///
/// - Stored messages are unique by `seq`.
/// - The customer's pending message is replaced by the stored one with the
///   same `client_message_id`, wherever it arrives from (POST answer or stream).
/// - A failed send keeps its `client_message_id`, so a retry can never post
///   it twice.
class MessageLog {
  const MessageLog({this.stored = const {}, this.pending = const []});

  final Map<int, ChatMessage> stored;
  final List<ChatMessage> pending;

  int get lastSeq => stored.isEmpty ? 0 : stored.keys.reduce((a, b) => a > b ? a : b);

  List<ChatMessage> get ordered {
    final s = stored.values.toList()..sort((a, b) => a.seq.compareTo(b.seq));
    return [...s, ...pending];
  }

  MessageLog merge(Iterable<ChatMessage> incoming) {
    final next = Map.of(stored);
    final arrivedIds = <String>{};
    for (final m in incoming) {
      if (m.seq <= 0) continue;
      next[m.seq] = m;
      if (m.clientMessageId != null) arrivedIds.add(m.clientMessageId!);
    }
    return MessageLog(stored: next, pending: [for (final p in pending) if (!arrivedIds.contains(p.clientMessageId)) p]);
  }

  MessageLog addPending(ChatMessage m) => MessageLog(stored: stored, pending: [...pending.where((p) => p.clientMessageId != m.clientMessageId), m]);

  MessageLog mark(String clientMessageId, SendState s) =>
      MessageLog(stored: stored, pending: [for (final p in pending) p.clientMessageId == clientMessageId ? p.withState(s) : p]);
}

class HelpCategory {
  const HelpCategory({required this.slug, required this.name, required this.articles});

  factory HelpCategory.fromJson(Json j) => HelpCategory(slug: str(j, 'slug'), name: str(j, 'name'), articles: integer(j, 'articles'));

  final String slug;
  final String name;
  final int articles;
}

class HelpArticle {
  const HelpArticle({required this.slug, required this.category, required this.title, required this.summary, this.body, this.updatedAt});

  factory HelpArticle.fromJson(Json j) => HelpArticle(
    slug: str(j, 'slug'),
    category: str(j, 'category'),
    title: str(j, 'title'),
    summary: str(j, 'summary'),
    body: strOrNull(j, 'body'),
    updatedAt: time(j, 'updated_at'),
  );

  final String slug;
  final String category;
  final String title;
  final String summary;

  /// Plain text; paragraphs separated by blank lines. Never rendered as HTML.
  final String? body;
  final DateTime? updatedAt;

  List<String> get paragraphs => (body ?? '').split(RegExp(r'\n\s*\n')).map((p) => p.trim()).where((p) => p.isNotEmpty).toList();
}
