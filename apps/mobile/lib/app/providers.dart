import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/api_client.dart';
import '../core/authentication/session.dart';
import '../core/errors/app_error.dart';
import '../core/storage/storage.dart';
import 'environment.dart';

/// Overridden in `bootstrap.dart` (and in tests).
final environmentProvider = Provider<Environment>((ref) => throw UnimplementedError('environment'));
final secureStoreProvider = Provider<SecureStore>((ref) => SecureStore());
final cacheStoreProvider = Provider<CacheStore>((ref) => CacheStore());
final flagsProvider = Provider<Flags>((ref) => throw UnimplementedError('flags'));

final sessionManagerProvider = Provider<SessionManager>((ref) {
  final env = ref.watch(environmentProvider);
  return SessionManager(
    secure: ref.watch(secureStoreProvider),
    refreshDio: ApiClient.create(env).dio,
  );
});

final apiClientProvider = Provider<ApiClient>((ref) {
  final env = ref.watch(environmentProvider);
  final session = ref.watch(sessionManagerProvider);
  final client = ApiClient.create(env);
  client.dio.interceptors.insert(0, AuthInterceptor(session, client.dio));
  return client;
});

final authApiProvider = Provider<AuthApi>((ref) => AuthApi(ref.watch(apiClientProvider), ref.watch(sessionManagerProvider)));

// ---------------------------------------------------------------- auth state

sealed class AuthState {
  const AuthState();
}

/// Before the stored session has been read.
class AuthUnknown extends AuthState {
  const AuthUnknown();
}

class SignedOut extends AuthState {
  const SignedOut({this.expired = false});

  /// The session ended on the server (expiry, revocation, sign-out elsewhere).
  final bool expired;
}

class SignedIn extends AuthState {
  const SignedIn(this.customer);

  /// null until `/me` has answered (e.g. starting offline).
  final Customer? customer;
}

class AuthController extends Notifier<AuthState> {
  @override
  AuthState build() {
    final session = ref.watch(sessionManagerProvider);
    session.onSignedOut = _sessionLost;
    return const AuthUnknown();
  }

  SessionManager get _session => ref.read(sessionManagerProvider);
  AuthApi get _api => ref.read(authApiProvider);

  /// Reads the keystore. Does not wait for the network.
  Future<void> restore() async {
    await _session.load();
    if (!_session.hasSession) {
      state = const SignedOut();
      return;
    }
    state = const SignedIn(null);
    unawaited(refreshProfile());
  }

  Future<void> refreshProfile() async {
    try {
      final me = await _api.me();
      if (state is SignedIn) state = SignedIn(me);
    } on AppError catch (e) {
      if (e.kind == ErrorKind.unauthorized) await _sessionLost();
      // Offline: stay signed in with what we have.
    }
  }

  Future<void> signIn(String email, String password) async {
    final c = await _api.login(email, password);
    state = SignedIn(c);
  }

  Future<void> signOut() async {
    await _api.signOut();
    await ref.read(cacheStoreProvider).clearAccount();
    state = const SignedOut();
  }

  Future<void> _sessionLost() async {
    await _session.clear();
    await ref.read(cacheStoreProvider).clearAccount();
    state = const SignedOut(expired: true);
  }
}

final authControllerProvider = NotifierProvider<AuthController, AuthState>(AuthController.new);

final isSignedInProvider = Provider<bool>((ref) => ref.watch(authControllerProvider) is SignedIn);

/// Requests marked this way skip the bearer token (public endpoints).
final publicOptions = Options(extra: {'auth': false});
