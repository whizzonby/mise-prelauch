import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mise_mobile/app/environment.dart';
import 'package:mise_mobile/core/authentication/session.dart';
import 'package:mise_mobile/core/errors/app_error.dart';
import 'package:mise_mobile/core/notifications/deep_links.dart';
import 'package:mise_mobile/core/security/links.dart';
import 'package:mise_mobile/core/storage/storage.dart';
import 'package:mise_mobile/shared/formatting.dart';

const _id = '3f1c2a9e-1b2c-4d3e-8f4a-1234567890ab';

/// In-memory stand-in for the keystore.
class MemorySecureStore extends SecureStore {
  MemorySecureStore() : super();
  final Map<String, String> values = {};

  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String value) async => values[key] = value;
  @override
  Future<void> delete(String key) async => values.remove(key);
}

void main() {
  group('SafeLinks.internal', () {
    test('accepts known routes with well-formed ids', () {
      expect(SafeLinks.internal('/orders/$_id'), '/orders/$_id');
      expect(SafeLinks.internal('/orders/$_id/delivery'), '/orders/$_id/delivery');
      expect(SafeLinks.internal('/menu/meal/pelau-with-chicken'), '/menu/meal/pelau-with-chicken');
      expect(SafeLinks.internal('/support/conversations/$_id'), '/support/conversations/$_id');
    });

    test('rejects unknown routes, bad ids, external URLs and traversal', () {
      for (final bad in [
        '/admin/intelligence',
        '/orders/123',
        '/orders/$_id/../../admin',
        'https://evil.example/orders/$_id',
        '//evil.example/home',
        '/menu/meal/<script>',
        '',
        null,
        'javascript:alert(1)',
      ]) {
        expect(SafeLinks.internal(bad), isNull, reason: '$bad');
      }
    });

    test('drops query strings except tokens on token links', () {
      expect(SafeLinks.internal('/home?next=https://evil'), '/home');
      expect(SafeLinks.internal('/reset-password?token=abcdefghijklmnop1234'), '/reset-password?token=abcdefghijklmnop1234');
      expect(SafeLinks.internal('/reset-password?token=short'), isNull);
    });

    test('maps external links by scheme and approved host only', () {
      expect(SafeLinks.fromExternal(Uri.parse('mise://orders/$_id'), appScheme: 'mise'), '/orders/$_id');
      expect(SafeLinks.fromExternal(Uri.parse('https://mise.tt/meals/doubles'), appScheme: 'mise', host: 'mise.tt'), '/menu/meal/doubles');
      expect(SafeLinks.fromExternal(Uri.parse('https://other.tt/meals/doubles'), appScheme: 'mise', host: 'mise.tt'), isNull);
      expect(SafeLinks.fromExternal(Uri.parse('mise-local://orders/$_id'), appScheme: 'mise'), isNull);
    });
  });

  group('notification routing', () {
    test('maps known types to validated routes', () {
      expect(routeForNotification({'type': 'chat.reply', 'conversation_id': _id}), '/support/conversations/$_id');
      expect(routeForNotification({'type': 'delivery.update', 'order_id': _id}), '/orders/$_id/delivery');
      expect(routeForNotification({'type': 'payment.failed'}), '/subscription');
    });

    test('drops malformed ids and unknown links', () {
      expect(routeForNotification({'type': 'chat.reply', 'conversation_id': '../admin'}), isNull);
      expect(routeForNotification({'type': 'unknown', 'link': 'https://evil.example'}), isNull);
      expect(routeForNotification({}), isNull);
    });
  });

  group('AppError', () {
    test('reads the API error envelope and field list', () {
      final e = AppError.fromResponse(422, {
        'error': {
          'code': 'validation_failed',
          'message': 'Some fields need attention.',
          'fields': [
            {'field': 'email', 'code': 'invalid', 'message': 'Enter a valid email.'},
          ],
        },
        'meta': {'request_id': 'req-1'},
      }, null);
      expect(e.kind, ErrorKind.validation);
      expect(e.code, 'validation_failed');
      expect(e.fields['email'], 'Enter a valid email.');
      expect(e.requestId, 'req-1');
    });

    test('never shows server internals', () {
      final e = AppError.fromResponse(500, {
        'error': {'code': 'internal_error', 'message': 'pq: relation "orders" does not exist'},
      }, null);
      expect(e.kind, ErrorKind.server);
      expect(e.message, isNot(contains('relation')));
    });

    test('maps connection failures to an offline message', () {
      final e = AppError.fromDio(DioException(requestOptions: RequestOptions(path: '/x'), type: DioExceptionType.connectionError));
      expect(e.isNetwork, isTrue);
    });
  });

  group('Environment', () {
    Environment env(MiseEnv e, String url) => Environment(
      env: e,
      apiBaseUrl: url,
      siteUrl: 'https://mise.tt',
      appScheme: 'mise',
      deepLinkHost: null,
      pushEnabled: false,
      crashReportingEnabled: false,
      logHttp: false,
    );

    test('production refuses plain http and development hosts', () {
      expect(() => env(MiseEnv.production, 'http://api.mise.tt/api/v1').validate(), throwsStateError);
      expect(() => env(MiseEnv.production, 'https://10.0.2.2/api/v1').validate(), throwsStateError);
      expect(() => env(MiseEnv.staging, 'http://staging.mise.tt/api/v1').validate(), throwsStateError);
      env(MiseEnv.production, 'https://api.mise.tt/api/v1').validate();
      env(MiseEnv.local, 'http://10.0.2.2:8090/api/v1').validate();
    });

    test('test payments are never offered in production', () {
      expect(env(MiseEnv.production, 'https://api.mise.tt/api/v1').allowsTestPayments, isFalse);
      expect(env(MiseEnv.staging, 'https://s.mise.tt/api/v1').allowsTestPayments, isTrue);
    });
  });

  test('money is formatted like the web, never computed', () {
    expect(formatMoney(33000, 'TTD'), 'TTD 330.00');
    expect(formatMoney(123456789, 'TTD'), 'TTD 1,234,567.89');
    expect(formatMoney(-505, 'USD'), '-USD 5.05');
  });

  group('SessionManager', () {
    test('refresh is single-flight and stores the rotated pair', () async {
      final store = MemorySecureStore();
      final dio = Dio(BaseOptions(baseUrl: 'http://test', validateStatus: (_) => true));
      var calls = 0;
      final gate = Completer<void>();
      dio.httpClientAdapter = _FakeAdapter((options) async {
        calls++;
        await gate.future;
        return _json(200, {
          'data': {
            'access_token': 'access-2',
            'access_expires_at': DateTime.now().toUtc().add(const Duration(minutes: 15)).toIso8601String(),
            'refresh_token': 'refresh-2',
            'session_id': _id,
          },
        });
      });
      final s = SessionManager(secure: store, refreshDio: dio);
      await s.store(TokenPair(access: 'access-1', accessExpiresAt: DateTime.now().toUtc(), refresh: 'refresh-1', sessionId: _id));

      final a = s.refresh();
      final b = s.refresh();
      gate.complete();
      expect(await a, isTrue);
      expect(await b, isTrue);
      expect(calls, 1, reason: 'a refresh token works once; a second exchange would revoke the device');
      expect(s.accessToken, 'access-2');
      expect(store.values['auth.refresh'], 'refresh-2');
    });

    test('a rejected refresh signs out and erases the tokens', () async {
      final store = MemorySecureStore();
      final dio = Dio(BaseOptions(baseUrl: 'http://test', validateStatus: (_) => true));
      dio.httpClientAdapter = _FakeAdapter((_) async => _json(401, {'error': {'code': 'refresh_invalid', 'message': 'Please sign in again.'}}));
      var signedOut = false;
      final s = SessionManager(secure: store, refreshDio: dio, onSignedOut: () => signedOut = true);
      await s.store(TokenPair(access: 'a', accessExpiresAt: DateTime.now().toUtc(), refresh: 'r', sessionId: _id));
      expect(await s.refresh(), isFalse);
      expect(signedOut, isTrue);
      expect(s.hasSession, isFalse);
      expect(store.values.containsKey('auth.refresh'), isFalse);
    });

    test('offline refresh keeps the session', () async {
      final store = MemorySecureStore();
      final dio = Dio(BaseOptions(baseUrl: 'http://test'));
      dio.httpClientAdapter = _FakeAdapter((o) async => throw DioException(requestOptions: o, type: DioExceptionType.connectionError));
      final s = SessionManager(secure: store, refreshDio: dio);
      await s.store(TokenPair(access: 'a', accessExpiresAt: DateTime.now().toUtc(), refresh: 'r', sessionId: _id));
      expect(await s.refresh(), isFalse);
      expect(s.hasSession, isTrue);
    });
  });
}

ResponseBody _json(int status, Object body) =>
    ResponseBody.fromString(jsonEncode(body), status, headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    });

class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.handler);

  final Future<ResponseBody> Function(RequestOptions) handler;

  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<List<int>>? requestStream, Future<void>? cancelFuture) => handler(options);

  @override
  void close({bool force = false}) {}
}
