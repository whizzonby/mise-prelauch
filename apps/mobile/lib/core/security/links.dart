/// Validation for paths that arrive from outside the app: deep links,
/// notification taps, payment returns and `?from=` after sign-in.
///
/// Only known route shapes pass; ids must look like UUIDs and slugs like
/// slugs. Anything else is dropped (the caller goes home). Passing this check
/// never grants access: signed-in routes still redirect to sign-in, and the
/// API still decides what the customer may see.
abstract final class SafeLinks {
  static final _uuid = r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
  static final _slug = r'[a-z0-9][a-z0-9-]{0,99}';
  static final _week = r'\d{4}-\d{2}-\d{2}';

  static final List<RegExp> _allowed = [
    RegExp(r'^/home$'),
    RegExp(r'^/menu$'),
    RegExp('^/menu/meal/$_slug\$'),
    RegExp('^/menu/week/$_week\$'),
    RegExp(r'^/box$'),
    RegExp('^/box/select/$_uuid\$'),
    RegExp(r'^/plans$'),
    RegExp('^/plans/$_slug\$'),
    RegExp(r'^/orders$'),
    RegExp('^/orders/$_uuid\$'),
    RegExp('^/orders/$_uuid/delivery\$'),
    RegExp('^/orders/$_uuid/cook/$_uuid\$'),
    RegExp(r'^/support$'),
    RegExp('^/support/conversations/$_uuid\$'),
    RegExp(r'^/support/help$'),
    RegExp('^/support/help/$_slug\$'),
    RegExp(r'^/notifications$'),
    RegExp(r'^/account$'),
    RegExp(r'^/account/[a-z-]{1,40}$'),
    RegExp(r'^/subscription$'),
    RegExp('^/checkout/result/$_uuid\$'),
  ];

  /// Public entry points whose token is checked by the API.
  static final List<RegExp> _tokenLinks = [RegExp(r'^/verify-email$'), RegExp(r'^/reset-password$'), RegExp(r'^/confirm-email-change$')];

  /// Returns [path] if it is an allowed in-app destination, otherwise null.
  /// Query strings are dropped except for token links.
  static String? internal(String? raw) {
    if (raw == null || raw.isEmpty || raw.length > 300) return null;
    final uri = Uri.tryParse(raw);
    if (uri == null || uri.hasScheme || uri.hasAuthority || uri.userInfo.isNotEmpty) return null;
    final path = uri.path;
    if (!path.startsWith('/') || path.contains('//') || path.contains('..')) return null;
    if (_allowed.any((r) => r.hasMatch(path))) return path;
    if (_tokenLinks.any((r) => r.hasMatch(path))) {
      final token = uri.queryParameters['token'];
      if (token != null && RegExp(r'^[A-Za-z0-9_-]{16,128}$').hasMatch(token)) return '$path?token=$token';
    }
    return null;
  }

  /// Maps an incoming https/app-scheme link to an internal path.
  /// [appScheme] is e.g. `mise`; [host] is the approved link domain, if any.
  static String? fromExternal(Uri uri, {required String appScheme, String? host}) {
    String path;
    if (uri.scheme == appScheme) {
      // mise://orders/<id> → /orders/<id>; mise:///orders/<id> also accepted.
      path = '/${uri.host}${uri.path}'.replaceAll(RegExp(r'/+'), '/');
      if (uri.host.isEmpty) path = uri.path;
    } else if (uri.scheme == 'https' && host != null && uri.host == host) {
      path = uri.path;
    } else {
      return null;
    }
    // Web URLs that differ from app routes.
    final m = RegExp(r'^/meals/([a-z0-9-]+)$').firstMatch(path);
    if (m != null) path = '/menu/meal/${m.group(1)}';
    final q = uri.queryParameters['token'];
    return internal(q == null ? path : '$path?token=$q');
  }
}
