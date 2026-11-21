import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../app/providers.dart';

/// Turns an image `src` from the API into a URL the app may load.
/// Relative paths are served by the customer website; absolute URLs must be
/// https (plain http only outside production, for the local stack).
final mediaUrlProvider = Provider<String? Function(String?)>((ref) {
  final env = ref.watch(environmentProvider);
  return (String? src) {
    if (src == null || src.isEmpty) return null;
    if (src.startsWith('/') && !src.startsWith('//')) return '${env.mediaBaseUrl}$src';
    final uri = Uri.tryParse(src);
    if (uri == null || !uri.hasAuthority) return null;
    if (uri.scheme == 'https') return src;
    if (uri.scheme == 'http' && !env.isProduction) return src;
    return null;
  };
});
