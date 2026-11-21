import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Credentials only: Keychain on iOS (this device only, after first unlock),
/// Keystore-backed encryption on Android. Never ordinary preferences.
class SecureStore {
  SecureStore([FlutterSecureStorage? storage])
    : _s =
          storage ??
          const FlutterSecureStorage(
            aOptions: AndroidOptions(resetOnError: true),
            iOptions: IOSOptions(accessibility: KeychainAccessibility.first_unlock_this_device),
          );

  final FlutterSecureStorage _s;

  Future<String?> read(String key) => _s.read(key: key);
  Future<void> write(String key, String value) => _s.write(key: key, value: value);
  Future<void> delete(String key) => _s.delete(key: key);
}

/// Non-personal flags only (onboarding seen, last tab). No ids, no tokens.
class Flags {
  Flags(this._p);

  final SharedPreferencesAsync _p;

  static const onboardingSeen = 'onboarding_seen_v1';

  Future<bool> getBool(String k) async => (await _p.getBool(k)) ?? false;
  Future<void> setBool(String k, bool v) => _p.setBool(k, v);
}

/// A cached response with the time it was fetched, so screens can label it.
class Cached<T> {
  const Cached(this.value, this.fetchedAt);

  final T value;
  final DateTime fetchedAt;
}

/// Offline copies of what the customer has already seen (brief §42): JSON
/// files in app-private storage. Two areas:
/// - `public`: catalogue and menus, kept across sign-out;
/// - `account`: orders, recipes, conversations — wiped at sign-out.
/// The app directory is private to the app and excluded from backups
/// (Android `allowBackup=false`; iOS files are marked excluded).
class CacheStore {
  CacheStore({Future<Directory> Function()? root}) : _root = root ?? getApplicationSupportDirectory;

  final Future<Directory> Function() _root;

  Future<File> _file(String area, String key) async {
    final dir = Directory('${(await _root()).path}/cache/$area');
    if (!await dir.exists()) await dir.create(recursive: true);
    final safe = key.replaceAll(RegExp(r'[^A-Za-z0-9_.-]'), '_');
    return File('${dir.path}/$safe.json');
  }

  Future<void> put(String area, String key, Object? json) async {
    try {
      final f = await _file(area, key);
      await f.writeAsString(jsonEncode({'at': DateTime.now().toUtc().toIso8601String(), 'v': json}), flush: true);
    } catch (_) {
      // A cache write failing must never break the screen.
    }
  }

  Future<Cached<Object?>?> get(String area, String key) async {
    try {
      final f = await _file(area, key);
      if (!await f.exists()) return null;
      final m = jsonDecode(await f.readAsString()) as Map<String, dynamic>;
      return Cached(m['v'], DateTime.parse(m['at'] as String));
    } catch (_) {
      return null;
    }
  }

  /// Removes everything personal. Called at sign-out and session loss.
  Future<void> clearAccount() async {
    try {
      final dir = Directory('${(await _root()).path}/cache/account');
      if (await dir.exists()) await dir.delete(recursive: true);
    } catch (_) {}
  }
}

const cachePublic = 'public';
const cacheAccount = 'account';
