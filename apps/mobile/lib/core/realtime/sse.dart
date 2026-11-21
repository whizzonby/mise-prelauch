import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';

/// One Server-Sent Event.
class SseEvent {
  const SseEvent({required this.event, required this.data, this.id});

  final String event;
  final String data;
  final String? id;
}

/// Parses an SSE byte stream (text/event-stream). Comments (`: ping`) are
/// dropped; multi-line `data:` is joined with newlines.
class SseParser extends StreamTransformerBase<String, SseEvent> {
  const SseParser();

  @override
  Stream<SseEvent> bind(Stream<String> lines) async* {
    String event = 'message';
    String? id;
    final data = <String>[];
    await for (final raw in lines) {
      final line = raw.endsWith('\r') ? raw.substring(0, raw.length - 1) : raw;
      if (line.isEmpty) {
        if (data.isNotEmpty) yield SseEvent(event: event, data: data.join('\n'), id: id);
        event = 'message';
        id = null;
        data.clear();
        continue;
      }
      if (line.startsWith(':')) continue;
      final i = line.indexOf(':');
      final field = i < 0 ? line : line.substring(0, i);
      var value = i < 0 ? '' : line.substring(i + 1);
      if (value.startsWith(' ')) value = value.substring(1);
      switch (field) {
        case 'event':
          event = value;
        case 'data':
          data.add(value);
        case 'id':
          id = value;
      }
    }
  }
}

/// Backoff for reconnecting: 1 s, 2 s, 5 s, then every 15 s
/// (docs/api/FLUTTER_CONTRACTS.md §3).
Duration reconnectDelay(int attempt) => switch (attempt) {
  0 => const Duration(seconds: 1),
  1 => const Duration(seconds: 2),
  2 => const Duration(seconds: 5),
  _ => const Duration(seconds: 15),
};

/// Opens a stream and keeps it open with backoff until [close] is called or
/// the server says the session is over (401/404 or an `end` event).
class SseConnection {
  SseConnection({required this.dio, required this.path, required this.lastSeq, required this.onEvent, this.onFatal});

  final Dio dio;
  final String path;

  /// The last seq the client holds; sent as Last-Event-ID so nothing is missed.
  final int Function() lastSeq;
  final void Function(SseEvent) onEvent;

  /// Called once when reconnecting would be pointless (signed out, not found).
  final void Function()? onFatal;

  CancelToken? _cancel;
  bool _closed = false;
  int _attempt = 0;

  void open() {
    _closed = false;
    unawaited(_run());
  }

  void close() {
    _closed = true;
    _cancel?.cancel();
  }

  Future<void> _run() async {
    while (!_closed) {
      _cancel = CancelToken();
      try {
        final res = await dio.get<ResponseBody>(
          path,
          cancelToken: _cancel,
          options: Options(
            responseType: ResponseType.stream,
            headers: {'Accept': 'text/event-stream', 'Last-Event-ID': '${lastSeq()}'},
            receiveTimeout: const Duration(seconds: 45), // pings every 15 s
          ),
        );
        final status = res.statusCode ?? 0;
        if (status == 401 || status == 404) {
          _closed = true;
          onFatal?.call();
          return;
        }
        if (status != 200 || res.data == null) throw Exception('stream status $status');
        _attempt = 0;
        final lines = res.data!.stream.cast<List<int>>().transform(utf8.decoder).transform(const LineSplitter());
        await for (final e in lines.transform(const SseParser())) {
          if (_closed) break;
          if (e.event == 'end') {
            _closed = true;
            onFatal?.call();
            break;
          }
          onEvent(e);
        }
      } catch (_) {
        if (_closed) return;
      }
      if (_closed) return;
      await Future<void>.delayed(reconnectDelay(_attempt++));
    }
  }
}
