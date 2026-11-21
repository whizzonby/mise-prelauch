import 'package:dio/dio.dart';

enum ErrorKind {
  network,
  timeout,
  unauthorized,
  forbidden,
  notFound,
  conflict,
  validation,
  rateLimited,
  unavailable,
  server,
  unknown,
}

/// The one error type screens see. [message] is always safe to show: it is
/// either the API's human message (written for customers) or our own
/// sentence — never a stack trace, URL or database detail.
class AppError implements Exception {
  const AppError(this.kind, this.message, {this.code, this.status, this.fields = const {}, this.requestId});

  final ErrorKind kind;
  final String message;

  /// Stable API code (`meal_unavailable`, `payment_in_progress`, …).
  final String? code;
  final int? status;
  final Map<String, String> fields;
  final String? requestId;

  bool get isNetwork => kind == ErrorKind.network || kind == ErrorKind.timeout;

  static const _generic = 'We could not finish that. Please try again.';

  factory AppError.from(Object e) {
    if (e is AppError) return e;
    if (e is DioException) return AppError.fromDio(e);
    return const AppError(ErrorKind.unknown, _generic);
  }

  factory AppError.fromDio(DioException e) {
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.transformTimeout:
        return const AppError(ErrorKind.timeout, 'This is taking longer than it should. Check your connection and try again.');
      case DioExceptionType.connectionError:
        return const AppError(ErrorKind.network, "You're offline, or Mise can't be reached right now.");
      case DioExceptionType.cancel:
        return const AppError(ErrorKind.unknown, 'Cancelled.');
      case DioExceptionType.badCertificate:
        return const AppError(ErrorKind.network, 'A secure connection to Mise could not be made.');
      case DioExceptionType.badResponse:
      case DioExceptionType.unknown:
        break;
    }
    final res = e.response;
    if (res == null) return const AppError(ErrorKind.network, "You're offline, or Mise can't be reached right now.");
    return AppError.fromResponse(res.statusCode ?? 0, res.data, res.headers.value('x-request-id'));
  }

  /// Reads the API error envelope `{error: {code, message, fields}, meta: {request_id}}`.
  factory AppError.fromResponse(int status, Object? body, String? requestId) {
    String? code;
    String? message;
    final fields = <String, String>{};
    if (body is Map && body['error'] is Map) {
      final err = body['error'] as Map;
      code = err['code'] as String?;
      message = err['message'] as String?;
      final f = err['fields'];
      if (f is Map) {
        f.forEach((k, v) => fields['$k'] = '$v');
      } else if (f is List) {
        for (final item in f) {
          if (item is Map && item['field'] != null) fields['${item['field']}'] = '${item['message'] ?? item['code'] ?? ''}';
        }
      }
      final meta = body['meta'];
      if (meta is Map) requestId ??= meta['request_id'] as String?;
    }
    final kind = switch (status) {
      401 => ErrorKind.unauthorized,
      403 => ErrorKind.forbidden,
      404 => ErrorKind.notFound,
      409 => ErrorKind.conflict,
      410 => ErrorKind.notFound,
      422 => ErrorKind.validation,
      429 => ErrorKind.rateLimited,
      503 => ErrorKind.unavailable,
      >= 500 => ErrorKind.server,
      _ => ErrorKind.unknown,
    };
    final safe = switch (kind) {
      ErrorKind.server => 'Something went wrong on our side. Please try again in a moment.',
      ErrorKind.rateLimited => 'Too many tries in a short time. Please wait a minute and try again.',
      ErrorKind.unauthorized when code == 'unauthorized' || code == null => 'Please sign in again.',
      ErrorKind.notFound when message == null => "We couldn't find that.",
      _ => (message != null && message.isNotEmpty) ? message : _generic,
    };
    return AppError(kind, safe, code: code, status: status, fields: fields, requestId: requestId);
  }

  @override
  String toString() => 'AppError($kind, ${code ?? '-'}, $status)';
}
