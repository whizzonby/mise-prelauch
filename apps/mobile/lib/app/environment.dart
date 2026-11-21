/// Build-time configuration, set with `--dart-define` (see `apps/mobile/README.md`).
/// Holds no secrets: server payment keys, push service accounts and AI keys
/// stay on the server.
enum MiseEnv { local, development, staging, production }

class Environment {
  const Environment({
    required this.env,
    required this.apiBaseUrl,
    required this.siteUrl,
    required this.appScheme,
    required this.deepLinkHost,
    required this.pushEnabled,
    required this.crashReportingEnabled,
    required this.logHttp,
  });

  final MiseEnv env;

  /// e.g. https://api.mise.tt/api/v1 — no trailing slash.
  final String apiBaseUrl;

  /// The public website, for legal pages and links that live there.
  final String siteUrl;

  /// Custom URL scheme for payment returns and links (`mise`, `mise-dev`).
  final String appScheme;

  /// Host for Android App Links / iOS Universal Links, when one is approved.
  final String? deepLinkHost;

  /// Push needs a Firebase configuration supplied with the build.
  final bool pushEnabled;
  final bool crashReportingEnabled;

  /// Method, path and status only — never bodies or headers.
  final bool logHttp;

  bool get isProduction => env == MiseEnv.production;

  /// Test payment methods are never offered in a production build, whatever
  /// the server says (brief §7).
  bool get allowsTestPayments => !isProduction;

  static Environment fromDefines() {
    const name = String.fromEnvironment('MISE_ENV', defaultValue: 'local');
    final env = MiseEnv.values.firstWhere((e) => e.name == name, orElse: () => MiseEnv.local);
    const base = String.fromEnvironment('MISE_API_BASE_URL');
    const host = String.fromEnvironment('MISE_DEEP_LINK_HOST');
    const site = String.fromEnvironment('MISE_SITE_URL');
    const scheme = String.fromEnvironment('MISE_APP_SCHEME');
    const push = bool.fromEnvironment('MISE_PUSH');
    const crash = bool.fromEnvironment('MISE_CRASH_REPORTING');
    final e = Environment(
      env: env,
      // Android emulator reaches the host machine's API (port 8090) at 10.0.2.2.
      apiBaseUrl: base.isNotEmpty ? base : 'http://10.0.2.2:8090/api/v1',
      siteUrl: site.isNotEmpty ? site : 'http://10.0.2.2:3100',
      appScheme: scheme.isNotEmpty ? scheme : switch (env) { MiseEnv.production => 'mise', MiseEnv.staging => 'mise-staging', _ => 'mise-local' },
      deepLinkHost: host.isEmpty ? null : host,
      pushEnabled: push,
      crashReportingEnabled: crash,
      logHttp: env == MiseEnv.local || env == MiseEnv.development,
    );
    e.validate();
    return e;
  }

  /// Refuses an unsafe configuration at start-up rather than at checkout.
  void validate() {
    final uri = Uri.tryParse(apiBaseUrl);
    if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
      throw StateError('MISE_API_BASE_URL is not a URL');
    }
    if ((env == MiseEnv.staging || env == MiseEnv.production) && uri.scheme != 'https') {
      throw StateError('$env needs an https API URL');
    }
    if (env == MiseEnv.production && (uri.host == '10.0.2.2' || uri.host == 'localhost' || uri.host.endsWith('.test'))) {
      throw StateError('a production build cannot point at a development API');
    }
  }
}
