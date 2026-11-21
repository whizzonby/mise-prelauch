import 'dart:async';
import 'dart:io' show Platform;

import 'package:dio/dio.dart';
import 'package:uuid/uuid.dart';

import '../api/api_client.dart';
import '../errors/app_error.dart';
import '../storage/storage.dart';

/// The signed-in customer, as `/me` and the login grant describe them.
class Customer {
  const Customer({required this.id, required this.email, required this.firstName, required this.lastName, required this.emailVerified});

  factory Customer.fromJson(Json j) => Customer(
    id: str(j, 'id'),
    email: str(j, 'email'),
    firstName: str(j, 'first_name'),
    lastName: str(j, 'last_name'),
    emailVerified: boolean(j, 'email_verified'),
  );

  final String id;
  final String email;
  final String firstName;
  final String lastName;
  final bool emailVerified;
}

class TokenPair {
  const TokenPair({required this.access, required this.accessExpiresAt, required this.refresh, required this.sessionId});

  factory TokenPair.fromJson(Json j) => TokenPair(
    access: str(j, 'access_token'),
    accessExpiresAt: time(j, 'access_expires_at') ?? DateTime.now().toUtc(),
    refresh: str(j, 'refresh_token'),
    sessionId: str(j, 'session_id'),
  );

  final String access;
  final DateTime accessExpiresAt;
  final String refresh;
  final String sessionId;
}

/// Holds the token pair in the keystore and refreshes it.
///
/// - The access token is attached to API calls by [AuthInterceptor].
/// - Refresh is single-flight: concurrent 401s wait for one exchange, because
///   a refresh token works once and a second use would revoke the device.
/// - On a failed refresh the tokens are erased and [onSignedOut] fires.
class SessionManager {
  SessionManager({required this.secure, required this.refreshDio, this.onSignedOut});

  final SecureStore secure;

  /// A Dio without the auth interceptor, for the token endpoints.
  final Dio refreshDio;
  void Function()? onSignedOut;

  static const _kAccess = 'auth.access';
  static const _kAccessExp = 'auth.access_exp';
  static const _kRefresh = 'auth.refresh';
  static const _kSession = 'auth.session';
  static const _kInstall = 'app.installation_id';

  String? _access;
  DateTime? _accessExp;
  String? _refresh;
  Future<bool>? _refreshing;

  bool get hasSession => _refresh != null;
  String? get accessToken => _access;

  Future<void> load() async {
    _access = await secure.read(_kAccess);
    _refresh = await secure.read(_kRefresh);
    final exp = await secure.read(_kAccessExp);
    _accessExp = exp == null ? null : DateTime.tryParse(exp);
  }

  /// A random id for this installation (not a hardware id). Created once.
  Future<String> installationId() async {
    final existing = await secure.read(_kInstall);
    if (existing != null) return existing;
    final id = const Uuid().v4();
    await secure.write(_kInstall, id);
    return id;
  }

  static String deviceName() {
    try {
      if (Platform.isIOS) return 'Mise app on iPhone';
      if (Platform.isAndroid) return 'Mise app on Android';
    } catch (_) {}
    return 'Mise app';
  }

  Future<void> store(TokenPair t) async {
    _access = t.access;
    _accessExp = t.accessExpiresAt;
    _refresh = t.refresh;
    await secure.write(_kAccess, t.access);
    await secure.write(_kAccessExp, t.accessExpiresAt.toIso8601String());
    await secure.write(_kRefresh, t.refresh);
    await secure.write(_kSession, t.sessionId);
  }

  /// True when the access token is missing or ends within 30 seconds.
  bool get accessStale => _access == null || _accessExp == null || _accessExp!.isBefore(DateTime.now().toUtc().add(const Duration(seconds: 30)));

  /// Exchanges the refresh token once, however many callers ask at the same time.
  Future<bool> refresh() {
    final running = _refreshing;
    if (running != null) return running;
    final f = _doRefresh();
    _refreshing = f;
    return f.whenComplete(() => _refreshing = null);
  }

  Future<bool> _doRefresh() async {
    final rt = _refresh;
    if (rt == null) return false;
    try {
      final res = await refreshDio.post<Object?>('/auth/mobile/refresh', data: {'refresh_token': rt});
      final status = res.statusCode ?? 0;
      if (status == 200) {
        await store(TokenPair.fromJson(asMap(asMap(res.data)['data'])));
        return true;
      }
      if (status == 401) {
        await clear();
        onSignedOut?.call();
      }
      return false;
    } on DioException catch (e) {
      // Offline: keep the tokens; the next call will try again.
      if (e.response?.statusCode == 401) {
        await clear();
        onSignedOut?.call();
      }
      return false;
    }
  }

  /// Ends this device's session on the server (best effort) and forgets the tokens.
  Future<void> signOut() async {
    final rt = _refresh;
    await clear();
    if (rt != null) {
      try {
        await refreshDio.post<Object?>('/auth/mobile/logout', data: {'refresh_token': rt});
      } catch (_) {
        // Offline sign-out still signs out locally; the server session ends at its expiry.
      }
    }
  }

  Future<void> clear() async {
    _access = null;
    _accessExp = null;
    _refresh = null;
    for (final k in [_kAccess, _kAccessExp, _kRefresh, _kSession]) {
      await secure.delete(k);
    }
  }
}

/// Adds `Authorization: Bearer` and recovers once from an expired access
/// token. Requests marked `extra['auth'] = false` are sent without it.
class AuthInterceptor extends QueuedInterceptor {
  AuthInterceptor(this.session, this.dio);

  final SessionManager session;
  final Dio dio;

  @override
  Future<void> onRequest(RequestOptions options, RequestInterceptorHandler handler) async {
    if (options.extra['auth'] != false && session.hasSession) {
      if (session.accessStale) await session.refresh();
      final t = session.accessToken;
      if (t != null) options.headers['Authorization'] = 'Bearer $t';
    }
    handler.next(options);
  }

  @override
  Future<void> onResponse(Response<dynamic> response, ResponseInterceptorHandler handler) async {
    final opts = response.requestOptions;
    if (response.statusCode == 401 && opts.headers.containsKey('Authorization') && opts.extra['retried'] != true) {
      if (await session.refresh()) {
        try {
          final retry = await dio.fetch<dynamic>(
            opts.copyWith(headers: {...opts.headers, 'Authorization': 'Bearer ${session.accessToken}'}, extra: {...opts.extra, 'retried': true}),
          );
          return handler.next(retry);
        } on DioException catch (e) {
          return handler.reject(e);
        }
      }
    }
    handler.next(response);
  }
}

/// Account endpoints that do not need a session.
class AuthApi {
  AuthApi(this.api, this.session);

  final ApiClient api;
  final SessionManager session;

  static final _noAuth = Options(extra: {'auth': false});

  Future<Customer> login(String email, String password) async {
    final installation = await session.installationId();
    final data = await api.post<Json>(
      '/auth/mobile/login',
      asMap,
      body: {'email': email.trim(), 'password': password, 'device_name': SessionManager.deviceName(), 'installation_id': installation},
      options: _noAuth,
    );
    await session.store(TokenPair.fromJson(data));
    return Customer.fromJson(asMap(data['user']));
  }

  /// Answers the same whether or not the address is already registered.
  Future<void> register({
    required String firstName,
    required String lastName,
    required String email,
    required String password,
    required bool acceptTerms,
  }) => api.post<void>(
    '/auth/register',
    (_) {},
    body: {
      'first_name': firstName.trim(),
      'last_name': lastName.trim(),
      'email': email.trim(),
      'password': password,
      'accept_terms': acceptTerms,
    },
    options: _noAuth,
  );

  Future<void> verifyEmail(String token) => api.post<void>('/auth/verify-email', (_) {}, body: {'token': token}, options: _noAuth);

  Future<void> forgotPassword(String email) =>
      api.post<void>('/auth/password/forgot', (_) {}, body: {'email': email.trim()}, options: _noAuth);

  Future<void> resetPassword(String token, String password) =>
      api.post<void>('/auth/password/reset', (_) {}, body: {'token': token, 'password': password}, options: _noAuth);

  Future<Customer> me() => api.get('/me', (d) => Customer.fromJson(asMap(d)));

  Future<void> signOut() => session.signOut();

  static bool isUnverified(AppError e) => e.code == 'email_not_verified';
}
