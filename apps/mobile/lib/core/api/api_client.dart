import 'dart:developer' as developer;

import 'package:dio/dio.dart';
import 'package:uuid/uuid.dart';

import '../../app/environment.dart';
import '../errors/app_error.dart';

typedef Json = Map<String, dynamic>;

/// Thin typed wrapper over Dio for the Mise API (`docs/api/openapi.yaml`).
/// Unwraps the `{data, meta}` envelope and turns every failure into an
/// [AppError]. Business rules stay on the server: this class never computes
/// prices, eligibility or status.
class ApiClient {
  ApiClient(this.dio);

  final Dio dio;

  factory ApiClient.create(Environment env, {List<Interceptor> interceptors = const []}) {
    final dio = Dio(
      BaseOptions(
        baseUrl: env.apiBaseUrl,
        connectTimeout: const Duration(seconds: 10),
        sendTimeout: const Duration(seconds: 20),
        receiveTimeout: const Duration(seconds: 20),
        contentType: Headers.jsonContentType,
        responseType: ResponseType.json,
        headers: {'Accept': 'application/json'},
        // Statuses are handled below; Dio would otherwise throw for 4xx.
        validateStatus: (s) => s != null && s < 600,
      ),
    );
    dio.interceptors.addAll(interceptors);
    if (env.logHttp) dio.interceptors.add(_SafeLog());
    return ApiClient(dio);
  }

  static const _uuid = Uuid();

  /// A fresh idempotency key or client message id (UUID v4).
  static String newKey() => _uuid.v4();

  Future<T> get<T>(String path, T Function(Object? data) parse, {Map<String, dynamic>? query, Options? options}) =>
      _send(() => dio.get<Object?>(path, queryParameters: _clean(query), options: options), parse);

  Future<T> post<T>(String path, T Function(Object? data) parse, {Object? body, String? idempotencyKey, Options? options}) =>
      _send(() => dio.post<Object?>(path, data: body ?? const <String, dynamic>{}, options: _idem(options, idempotencyKey)), parse);

  Future<T> put<T>(String path, T Function(Object? data) parse, {Object? body, Options? options}) =>
      _send(() => dio.put<Object?>(path, data: body, options: options), parse);

  Future<T> patch<T>(String path, T Function(Object? data) parse, {Object? body, Options? options}) =>
      _send(() => dio.patch<Object?>(path, data: body, options: options), parse);

  Future<void> delete(String path, {Options? options}) => _send(() => dio.delete<Object?>(path, options: options), (_) {});

  Options? _idem(Options? o, String? key) {
    if (key == null) return o;
    return (o ?? Options()).copyWith(headers: {...?o?.headers, 'Idempotency-Key': key});
  }

  Map<String, dynamic>? _clean(Map<String, dynamic>? q) {
    if (q == null) return null;
    return {
      for (final e in q.entries)
        if (e.value != null && !(e.value is String && (e.value as String).isEmpty)) e.key: e.value,
    };
  }

  Future<T> _send<T>(Future<Response<Object?>> Function() call, T Function(Object? data) parse) async {
    final Response<Object?> res;
    try {
      res = await call();
    } on DioException catch (e) {
      throw AppError.fromDio(e);
    }
    final status = res.statusCode ?? 0;
    if (status >= 400) throw AppError.fromResponse(status, res.data, res.headers.value('x-request-id'));
    final body = res.data;
    final data = body is Map ? body['data'] : null;
    try {
      return parse(data);
    } on AppError {
      rethrow;
    } catch (e) {
      // A shape we do not understand: report as a server problem, not a crash.
      developer.log('response parse failed for ${res.requestOptions.path}: ${e.runtimeType}', name: 'api');
      throw const AppError(ErrorKind.server, 'Something went wrong on our side. Please try again in a moment.');
    }
  }
}

/// Logs method, path and status only. Never headers, bodies or query strings
/// (they can hold tokens, emails or message text).
class _SafeLog extends Interceptor {
  @override
  void onResponse(Response<dynamic> response, ResponseInterceptorHandler handler) {
    developer.log('${response.requestOptions.method} ${response.requestOptions.path} → ${response.statusCode}', name: 'api');
    handler.next(response);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) {
    developer.log('${err.requestOptions.method} ${err.requestOptions.path} → ${err.type.name}', name: 'api');
    handler.next(err);
  }
}

// ---- JSON helpers: tolerant of extra fields (API v1 only adds fields). ----

Json asMap(Object? v) => v is Map ? v.cast<String, dynamic>() : <String, dynamic>{};

List<T> asList<T>(Object? v, T Function(Json) f) => v is List ? [for (final e in v) f(asMap(e))] : <T>[];

/// Lists come either bare or as `{items: [...]}`.
List<T> itemsOf<T>(Object? data, T Function(Json) f) => data is List ? asList(data, f) : asList(asMap(data)['items'], f);

String str(Json j, String k, [String fallback = '']) => (j[k] as String?) ?? fallback;
String? strOrNull(Json j, String k) => j[k] as String?;
int integer(Json j, String k, [int fallback = 0]) => (j[k] as num?)?.toInt() ?? fallback;
int? intOrNull(Json j, String k) => (j[k] as num?)?.toInt();
double? doubleOrNull(Json j, String k) => (j[k] as num?)?.toDouble();
bool boolean(Json j, String k, [bool fallback = false]) => (j[k] as bool?) ?? fallback;
DateTime? time(Json j, String k) => j[k] is String ? DateTime.tryParse(j[k] as String) : null;
List<String> strings(Json j, String k) => j[k] is List ? [for (final e in j[k] as List) '$e'] : const [];
