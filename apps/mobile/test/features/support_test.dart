import 'package:flutter_test/flutter_test.dart';
import 'package:mise_mobile/core/realtime/sse.dart';
import 'package:mise_mobile/features/support/support_models.dart';

ChatMessage msg(int seq, {String author = 'agent', String? cid, SendState state = SendState.sent}) => ChatMessage(
  seq: seq,
  author: author,
  authorName: author == 'agent' ? 'Ria' : '',
  body: 'm$seq',
  clientMessageId: cid,
  createdAt: DateTime.utc(2026, 11, 21, 12, seq),
  state: state,
);

void main() {
  group('SSE parser', () {
    test('reads events, ids, multi-line data, and drops pings', () async {
      final lines = Stream.fromIterable([
        ': ping',
        'event: message',
        'id: 7',
        'data: {"a":1,',
        'data: "b":2}',
        '',
        ': ping',
        'event: typing',
        'data: {"who":"agent"}',
        '',
      ]);
      final events = await lines.transform(const SseParser()).toList();
      expect(events.length, 2);
      expect(events[0].event, 'message');
      expect(events[0].id, '7');
      expect(events[0].data, '{"a":1,\n"b":2}');
      expect(events[1].event, 'typing');
    });

    test('reconnect backoff is 1, 2, 5, then 15 seconds', () {
      expect([for (var i = 0; i < 5; i++) reconnectDelay(i).inSeconds], [1, 2, 5, 15, 15]);
    });
  });

  group('chat message deduplication', () {
    test('the same seq from history and the stream appears once', () {
      var log = const MessageLog().merge([msg(1), msg(2)]);
      log = log.merge([msg(2), msg(3)]);
      expect(log.ordered.map((m) => m.seq), [1, 2, 3]);
      expect(log.lastSeq, 3);
    });

    test('a pending message is replaced by its stored copy, whichever arrives first', () {
      final pending = msg(0, author: 'customer', cid: 'client-123456', state: SendState.pending);
      var log = const MessageLog().merge([msg(1)]).addPending(pending);
      expect(log.ordered.last.state, SendState.pending);
      // The stream delivers it before the POST answers…
      log = log.merge([msg(2, author: 'customer', cid: 'client-123456')]);
      // …then the POST answer arrives too.
      log = log.merge([msg(2, author: 'customer', cid: 'client-123456')]);
      expect(log.ordered.length, 2);
      expect(log.pending, isEmpty);
    });

    test('a failed send keeps its client id for a retry, and a retry does not duplicate', () {
      final pending = msg(0, author: 'customer', cid: 'client-abcdefgh', state: SendState.pending);
      var log = const MessageLog().addPending(pending).mark('client-abcdefgh', SendState.failed);
      expect(log.pending.single.state, SendState.failed);
      expect(log.pending.single.clientMessageId, 'client-abcdefgh');
      log = log.addPending(pending); // retry with the same id
      expect(log.pending.length, 1);
    });
  });

  test('help article bodies split into plain paragraphs', () {
    const a = HelpArticle(slug: 's', category: 'c', title: 't', summary: '', body: 'First.\n\n  Second line\nstill second.\n\n\n<b>Third</b>');
    expect(a.paragraphs, ['First.', 'Second line\nstill second.', '<b>Third</b>']);
  });
}
