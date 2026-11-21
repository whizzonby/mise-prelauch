import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../app/providers.dart';

/// Opens a page of the public website (terms, privacy) in an in-app browser
/// tab (Custom Tabs / SFSafariViewController), not a WebView.
Future<void> openSitePage(WidgetRef ref, String path) async {
  final base = ref.read(environmentProvider).siteUrl;
  await launchUrl(Uri.parse('$base$path'), mode: LaunchMode.inAppBrowserView);
}
