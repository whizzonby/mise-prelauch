import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mise_mobile/app/environment.dart';
import 'package:mise_mobile/app/app.dart';
import 'package:mise_mobile/app/providers.dart';
import 'package:mise_mobile/app/router.dart';
import 'package:mise_mobile/core/notifications/deep_links.dart';
import 'package:mise_mobile/core/api/api_client.dart';
import 'package:mise_mobile/core/authentication/session.dart';
import 'package:mise_mobile/core/storage/storage.dart';
import 'package:mise_mobile/design_system/design_system.dart';
import 'package:mise_mobile/shared/media.dart';

import 'fixtures.dart';

/// In-memory keystore.
class MemorySecureStore extends SecureStore {
  MemorySecureStore([Map<String, String>? initial]) : values = {...?initial};
  final Map<String, String> values;

  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String value) async => values[key] = value;
  @override
  Future<void> delete(String key) async => values.remove(key);
}

/// Answers API calls from [routes]: "GET /meals" → JSON `data`, or a
/// function for dynamic answers. Unknown routes are 404. [offline] makes
/// every call fail like a dropped connection.
class FakeApi implements HttpClientAdapter {
  FakeApi(this.routes);

  final Map<String, Object? Function(RequestOptions)> routes;
  bool offline = false;
  final List<String> calls = [];

  @override
  Future<ResponseBody> fetch(RequestOptions o, Stream<List<int>>? body, Future<void>? cancel) async {
    final key = '${o.method} ${o.path}';
    calls.add(key);
    if (offline) throw DioException(requestOptions: o, type: DioExceptionType.connectionError);
    for (final e in routes.entries) {
      if (_match(e.key, key)) {
        final v = e.value(o);
        if (v is (int, Object?)) return _json(v.$1, v.$2);
        return _json(200, {'data': v, 'meta': {'request_id': 'test'}});
      }
    }
    return _json(404, {
      'error': {'code': 'not_found', 'message': "We couldn't find that."},
    });
  }

  static bool _match(String pattern, String actual) {
    final p = pattern.split('/');
    final a = actual.split('/');
    if (p.length != a.length) return false;
    for (var i = 0; i < p.length; i++) {
      if (p[i] != '*' && p[i] != a[i]) return false;
    }
    return true;
  }

  static ResponseBody _json(int status, Object? body) =>
      ResponseBody.fromString(jsonEncode(body), status, headers: {Headers.contentTypeHeader: [Headers.jsonContentType]});

  @override
  void close({bool force = false}) {}
}

const testEnv = Environment(
  env: MiseEnv.local,
  apiBaseUrl: 'http://api.test/api/v1',
  siteUrl: 'https://mise.test',
  mediaBaseUrl: 'https://mise.test',
  appScheme: 'mise-local',
  deepLinkHost: null,
  pushEnabled: false,
  crashReportingEnabled: false,
  logHttp: false,
);

bool _fontsLoaded = false;

/// Real brand fonts and Material icons, so renders look like the app.
/// File reads need real async, so this runs inside `tester.runAsync`.
Future<void> loadFonts(WidgetTester tester) => tester.runAsync(_loadFonts);

Future<void> _loadFonts() async {
  if (_fontsLoaded) return;
  _fontsLoaded = true;
  Future<ByteData> file(String p) async => ByteData.sublistView(await File(p).readAsBytes());
  await (FontLoader('YoungSerif')..addFont(file('assets/fonts/YoungSerif-Regular.ttf'))).load();
  await (FontLoader('InstrumentSans')..addFont(file('assets/fonts/InstrumentSans-Variable.ttf'))).load();
  const icons = 'C:/flutter/bin/cache/artifacts/material_fonts/materialicons-regular.otf';
  if (File(icons).existsSync()) await (FontLoader('MaterialIcons')..addFont(file(icons))).load();
}

class TestApp {
  TestApp({required this.container, required this.api});

  final ProviderContainer container;
  final FakeApi api;
}

/// Builds the providers for a test: fake API, in-memory storage, signed in
/// or not, images from bundled assets.
Future<TestApp> makeApp({bool signedIn = true, bool onboardingSeen = true, Map<String, Object? Function(RequestOptions)>? extra, Directory? cacheDir}) async {
  final api = FakeApi({...fixtureRoutes(), ...?extra});
  final secure = MemorySecureStore(
    signedIn
        ? {
            'auth.access': 'access-token',
            'auth.access_exp': DateTime.now().toUtc().add(const Duration(minutes: 15)).toIso8601String(),
            'auth.refresh': 'refresh-token',
          }
        : null,
  );
  final dir = cacheDir ?? Directory.systemTemp.createTempSync('mise_cache');
  final container = ProviderContainer(
    retry: (_, _) => null,
    overrides: [
      environmentProvider.overrideWithValue(testEnv),
      secureStoreProvider.overrideWithValue(secure),
      cacheStoreProvider.overrideWithValue(CacheStore(root: () async => dir)),
      sessionManagerProvider.overrideWith((ref) {
        final d = Dio(BaseOptions(baseUrl: testEnv.apiBaseUrl, validateStatus: (_) => true))..httpClientAdapter = api;
        return SessionManager(secure: secure, refreshDio: d);
      }),
      apiClientProvider.overrideWith((ref) {
        final c = ApiClient.create(testEnv);
        c.dio.httpClientAdapter = api;
        c.dio.interceptors.insert(0, AuthInterceptor(ref.watch(sessionManagerProvider), c.dio));
        return c;
      }),
      mediaUrlProvider.overrideWithValue((src) => src == null ? null : 'asset:${fixtureImageFor(src)}'),
      // The link listener needs a platform plugin; routes are driven directly.
      deepLinkListenerProvider.overrideWithValue(null),
      onboardingSeenProvider.overrideWith(() => OnboardingSeen(onboardingSeen)),
    ],
  );
  await container.read(authControllerProvider.notifier).restore();
  return TestApp(container: container, api: api);
}

/// Pumps [child] as a phone-sized screen with the app theme.
Future<void> pumpScreen(WidgetTester tester, TestApp app, Widget child, {Size size = const Size(390, 844), double textScale = 1}) async {
  await loadFonts(tester);
  tester.view.physicalSize = size * 2;
  tester.view.devicePixelRatio = 2;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: app.container,
      child: MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: buildMiseTheme(),
        home: MediaQuery(
          data: MediaQueryData(size: size, devicePixelRatio: 2, textScaler: TextScaler.linear(textScale), padding: const EdgeInsets.only(top: 47, bottom: 34)),
          child: child,
        ),
      ),
    ),
  );
  await settle(tester);
}

/// Lets futures and images finish without waiting on endless animations.
Future<void> settle(WidgetTester tester) async {
  for (var i = 0; i < 12; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

/// Writes a render of the current screen to test/goldens/NAME.png.
Future<void> snap(WidgetTester tester, String name) async {
  await settle(tester);
  await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/$name.png'));
}

/// Keeps unawaited Futures in tests quiet.
void ignore(Future<void> f) => unawaited(f);

/// Pumps the whole app (router, tab bar) at [path].
Future<void> pumpRoute(WidgetTester tester, TestApp app, String path, {Size size = const Size(390, 844), double textScale = 1}) async {
  await loadFonts(tester);
  tester.view.physicalSize = size * 2;
  tester.view.devicePixelRatio = 2;
  tester.platformDispatcher.textScaleFactorTestValue = textScale;
  addTearDown(tester.view.reset);
  addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
  await tester.pumpWidget(UncontrolledProviderScope(container: app.container, child: const MiseApp()));
  app.container.read(routerProvider).go(path);
  await settle(tester);
}
