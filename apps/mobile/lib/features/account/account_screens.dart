import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../core/errors/app_error.dart';
import '../../core/notifications/push.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/links.dart';
import '../../shared/media.dart';
import '../../shared/models/catalogue.dart';
import '../../shared/models/commerce.dart';
import '../catalogue/catalogue_api.dart';
import '../catalogue/menu_screen.dart';
import '../subscriptions/subscription_api.dart';

final profileProvider = FutureProvider<Json>((ref) async {
  if (!ref.watch(isSignedInProvider)) return const {};
  return ref.watch(apiClientProvider).get('/me/profile', asMap);
});

/// Account home. One account for app and website: everything here reads
/// and writes the same records as mise.tt.
class AccountScreen extends ConsumerWidget {
  const AccountScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authControllerProvider);
    if (auth is! SignedIn) {
      return Scaffold(
        appBar: AppBar(title: Text('Account', style: MiseType.h2)),
        body: ListView(
          children: [
            EmptyState(
              icon: Icons.person_outline,
              title: 'Sign in to Mise',
              message: 'Your subscription, orders, favourites and messages, on this phone and on mise.tt.',
              actionLabel: 'Sign in',
              onAction: () => context.push('/sign-in'),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
              child: MiseButton.quiet(label: 'Create an account', onPressed: () => context.push('/register')),
            ),
            const SizedBox(height: MiseSpace.lg),
            MiseListTile(leading: Icons.help_outline, title: 'Help centre', onTap: () => context.go('/support/help')),
            MiseListTile(leading: Icons.gavel_outlined, title: 'Terms and privacy', onTap: () => context.push('/account/legal')),
          ],
        ),
      );
    }
    final c = auth.customer;
    final sub = ref.watch(mySubscriptionProvider).value;
    return Scaffold(
      appBar: AppBar(title: Text('Account', style: MiseType.h2)),
      body: ListView(
        padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.lg),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(c == null ? 'Your account' : '${c.firstName} ${c.lastName}'.trim(), style: MiseType.h3),
                if (c != null) Text(c.email, style: MiseType.bodySmall),
              ],
            ),
          ),
          const SectionHeader(title: 'Your box'),
          MiseListTile(leading: Icons.autorenew, title: 'Subscription', subtitle: sub == null ? 'No subscription' : '${sub.planName} · ${sub.statusLabel}', onTap: () => context.push(sub == null ? '/plans' : '/subscription')),
          MiseListTile(leading: Icons.receipt_long_outlined, title: 'Orders and billing', onTap: () => context.push('/orders')),
          MiseListTile(leading: Icons.favorite_border, title: 'Favourites', onTap: () => context.push('/account/favorites')),
          const SizedBox(height: MiseSpace.md),
          const SectionHeader(title: 'Details'),
          MiseListTile(leading: Icons.badge_outlined, title: 'Profile', onTap: () => context.push('/account/profile')),
          MiseListTile(leading: Icons.home_outlined, title: 'Delivery addresses', onTap: () => context.push('/account/addresses')),
          MiseListTile(leading: Icons.restaurant_outlined, title: 'Dietary needs and allergies', onTap: () => context.push('/account/preferences')),
          MiseListTile(leading: Icons.notifications_none, title: 'Notifications', onTap: () => context.push('/account/notifications')),
          MiseListTile(leading: Icons.devices_outlined, title: 'Signed-in devices', onTap: () => context.push('/account/devices')),
          const SizedBox(height: MiseSpace.md),
          const SectionHeader(title: 'Help and privacy'),
          MiseListTile(leading: Icons.help_outline, title: 'Help centre', onTap: () => context.go('/support/help')),
          MiseListTile(leading: Icons.privacy_tip_outlined, title: 'Privacy and your data', onTap: () => context.push('/account/privacy')),
          MiseListTile(leading: Icons.gavel_outlined, title: 'Terms and licences', onTap: () => context.push('/account/legal')),
          const SizedBox(height: MiseSpace.md),
          MiseListTile(
            leading: Icons.logout,
            title: 'Sign out',
            danger: true,
            onTap: () async {
              final ok = await confirmAction(context, title: 'Sign out?', message: 'Saved orders and messages are removed from this phone.', confirmLabel: 'Sign out');
              if (!ok) return;
              await ref.read(pushServiceProvider).forgetDevice();
              await ref.read(authControllerProvider.notifier).signOut();
              if (context.mounted) context.go('/home');
            },
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------- profile

class ProfileScreen extends ConsumerStatefulWidget {
  const ProfileScreen({super.key});

  @override
  ConsumerState<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends ConsumerState<ProfileScreen> {
  final _first = TextEditingController();
  final _last = TextEditingController();
  final _phone = TextEditingController();
  String _channel = 'email';
  bool _loaded = false;
  bool _busy = false;
  Map<String, String> _fields = const {};

  @override
  void dispose() {
    for (final c in [_first, _last, _phone]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _fields = const {};
    });
    try {
      await ref.read(apiClientProvider).patch<void>('/me/profile', (_) {}, body: {
        'first_name': _first.text.trim(),
        'last_name': _last.text.trim(),
        'phone': _phone.text.trim(),
        'contact_channel': _channel,
      });
      ref.invalidate(profileProvider);
      await ref.read(authControllerProvider.notifier).refreshProfile();
      if (mounted) showMessage(context, 'Saved.');
    } on AppError catch (e) {
      setState(() => _fields = e.fields);
      if (mounted) showMessage(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = ref.watch(profileProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Profile')),
      body: p.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(profileProvider)),
        data: (j) {
          if (!_loaded) {
            _first.text = str(j, 'first_name');
            _last.text = str(j, 'last_name');
            _phone.text = str(j, 'phone');
            _channel = str(j, 'contact_channel', 'email');
            _loaded = true;
          }
          return ListView(
            padding: const EdgeInsets.all(MiseSpace.gutter),
            children: [
              FactRow(label: 'Email', value: str(j, 'email')),
              if (strOrNull(j, 'pending_email') != null) Text('Waiting for you to confirm ${strOrNull(j, 'pending_email')}.', style: MiseType.caption),
              const SizedBox(height: MiseSpace.md),
              MiseTextField(label: 'First name', controller: _first, autofillHints: const [AutofillHints.givenName], errorText: _fields['first_name']),
              const SizedBox(height: MiseSpace.md),
              MiseTextField(label: 'Last name', controller: _last, autofillHints: const [AutofillHints.familyName], errorText: _fields['last_name']),
              const SizedBox(height: MiseSpace.md),
              MiseTextField(label: 'Phone', controller: _phone, keyboardType: TextInputType.phone, autofillHints: const [AutofillHints.telephoneNumber], errorText: _fields['phone']),
              const SizedBox(height: MiseSpace.lg),
              Text('How should we contact you?', style: MiseType.label),
              RadioGroup<String>(
                groupValue: _channel,
                onChanged: (v) => setState(() => _channel = v ?? 'email'),
                child: const Column(
                  children: [
                    RadioListTile(contentPadding: EdgeInsets.zero, value: 'email', title: Text('Email')),
                    RadioListTile(contentPadding: EdgeInsets.zero, value: 'sms', title: Text('Text message')),
                    RadioListTile(contentPadding: EdgeInsets.zero, value: 'whatsapp', title: Text('WhatsApp')),
                  ],
                ),
              ),
              const SizedBox(height: MiseSpace.lg),
              MiseButton(label: 'Save', busy: _busy, onPressed: _save),
              const SizedBox(height: MiseSpace.sm),
              Text('To change your email or password, use mise.tt for now.', style: MiseType.caption),
            ],
          );
        },
      ),
    );
  }
}

// ---------------------------------------------------------------- addresses

class AddressesScreen extends ConsumerWidget {
  const AddressesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(addressesProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Delivery addresses')),
      body: list.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(addressesProvider)),
        data: (items) => ListView(
          padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
          children: [
            if (items.isEmpty) const EmptyState(title: 'No addresses yet', message: 'Add the address your box should go to.'),
            for (final a in items)
              MiseListTile(
                title: '${a.label ?? a.recipientName}${a.isDefault ? ' · Default' : ''}',
                subtitle: '${a.oneLine}\n${a.availabilityLabel}',
                onTap: () => _edit(context, ref, a),
              ),
            Padding(
              padding: const EdgeInsets.all(MiseSpace.gutter),
              child: MiseButton.secondary(label: 'Add address', icon: Icons.add, onPressed: () => _edit(context, ref, null)),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _edit(BuildContext context, WidgetRef ref, Address? a) async {
    final saved = await showMiseSheet<bool>(context, title: a == null ? 'New address' : 'Edit address', builder: (_) => _AddressForm(existing: a));
    if (saved == true) ref.invalidate(addressesProvider);
  }
}

class _AddressForm extends ConsumerStatefulWidget {
  const _AddressForm({this.existing});

  final Address? existing;

  @override
  ConsumerState<_AddressForm> createState() => _AddressFormState();
}

class _AddressFormState extends ConsumerState<_AddressForm> {
  late final _label = TextEditingController(text: widget.existing?.label ?? '');
  late final _name = TextEditingController(text: widget.existing?.recipientName ?? '');
  late final _phone = TextEditingController(text: widget.existing?.phone ?? '');
  late final _line1 = TextEditingController(text: widget.existing?.line1 ?? '');
  late final _line2 = TextEditingController(text: widget.existing?.line2 ?? '');
  late final _area = TextEditingController(text: widget.existing?.area ?? '');
  late final _city = TextEditingController(text: widget.existing?.city ?? '');
  late final _notes = TextEditingController(text: widget.existing?.instructions ?? '');
  late String? _region = widget.existing?.region;
  late bool _default = widget.existing?.isDefault ?? false;
  bool _busy = false;
  Map<String, String> _fields = const {};
  String? _error;

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _fields = const {};
      _error = null;
    });
    final body = {
      'label': _label.text.trim(),
      'recipient_name': _name.text.trim(),
      'phone': _phone.text.trim(),
      'line1': _line1.text.trim(),
      'line2': _line2.text.trim(),
      'area': _area.text.trim(),
      'city': _city.text.trim(),
      'region': ?_region,
      'country': 'TT',
      'instructions': _notes.text.trim(),
      'is_default': _default,
    };
    try {
      final api = ref.read(apiClientProvider);
      if (widget.existing == null) {
        await api.post<void>('/me/addresses', (_) {}, body: body);
      } else {
        await api.patch<void>('/me/addresses/${widget.existing!.id}', (_) {}, body: body);
      }
      if (mounted) Navigator.of(context).pop(true);
    } on AppError catch (e) {
      setState(() {
        _fields = e.fields;
        _error = e.message;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final regions = ref.watch(_regionsProvider).value ?? const <Term>[];
    Widget f(String label, TextEditingController c, String key, {TextInputType? kb, List<String>? hints, int lines = 1}) => Padding(
      padding: const EdgeInsets.only(bottom: MiseSpace.md),
      child: MiseTextField(label: label, controller: c, keyboardType: kb, autofillHints: hints, errorText: _fields[key], maxLines: lines, textCapitalization: TextCapitalization.words),
    );
    return AutofillGroup(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          f('Label (optional)', _label, 'label'),
          f('Recipient', _name, 'recipient_name', hints: const [AutofillHints.name]),
          f('Phone', _phone, 'phone', kb: TextInputType.phone, hints: const [AutofillHints.telephoneNumber]),
          f('Address line 1', _line1, 'line1', hints: const [AutofillHints.streetAddressLine1]),
          f('Address line 2 (optional)', _line2, 'line2', hints: const [AutofillHints.streetAddressLine2]),
          f('Area (optional)', _area, 'area'),
          f('City or town', _city, 'city', hints: const [AutofillHints.addressCity]),
          Text('Region', style: MiseType.label),
          const SizedBox(height: MiseSpace.xs),
          DropdownButtonFormField<String>(
            initialValue: regions.any((r) => r.key == _region) ? _region : null,
            items: [for (final r in regions) DropdownMenuItem(value: r.key, child: Text(r.label))],
            onChanged: (v) => setState(() => _region = v),
            decoration: InputDecoration(errorText: _fields['region']),
          ),
          const SizedBox(height: MiseSpace.md),
          f('Delivery notes (optional)', _notes, 'instructions', lines: 3),
          SwitchListTile(contentPadding: EdgeInsets.zero, value: _default, onChanged: (v) => setState(() => _default = v), title: const Text('Use as my default address')),
          if (_error != null) NoticeBanner(tone: NoticeTone.error, message: _error!),
          const SizedBox(height: MiseSpace.md),
          MiseButton(label: 'Save address', busy: _busy, onPressed: _save),
          if (widget.existing != null)
            MiseButton.quiet(
              label: 'Delete this address',
              expand: true,
              onPressed: () async {
                try {
                  await ref.read(apiClientProvider).delete('/me/addresses/${widget.existing!.id}');
                  if (context.mounted) Navigator.of(context).pop(true);
                } on AppError catch (e) {
                  setState(() => _error = e.message);
                }
              },
            ),
        ],
      ),
    );
  }
}

final _regionsProvider = FutureProvider<List<Term>>((ref) => ref.watch(apiClientProvider).get('/reference', (d) => asList(asMap(d)['regions'], Term.fromJson), options: publicOptions));

// ---------------------------------------------------------------- dietary needs

final _preferencesProvider = FutureProvider<Json>((ref) => ref.watch(apiClientProvider).get('/me/preferences', asMap));

class PreferencesScreen extends ConsumerStatefulWidget {
  const PreferencesScreen({super.key});

  @override
  ConsumerState<PreferencesScreen> createState() => _PreferencesScreenState();
}

class _PreferencesScreenState extends ConsumerState<PreferencesScreen> {
  Set<String>? _dietary;
  Set<String>? _allergies;
  bool _busy = false;

  Future<void> _save() async {
    setState(() => _busy = true);
    try {
      await ref.read(apiClientProvider).put<void>('/me/preferences', (_) {}, body: {
        'dietary': _dietary!.isEmpty ? ['no-preference'] : _dietary!.toList(),
        'allergies': [for (final a in _allergies!) {'allergen': a}],
      });
      ref.invalidate(_preferencesProvider);
      if (mounted) showMessage(context, 'Saved. Meal choices will warn you about these allergens.');
    } on AppError catch (e) {
      if (mounted) showMessage(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final prefs = ref.watch(_preferencesProvider);
    final reference = ref.watch(referenceAllProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Dietary needs and allergies')),
      body: prefs.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message),
        data: (p) {
          _dietary ??= strings(p, 'dietary').where((d) => d != 'no-preference').toSet();
          _allergies ??= asList(p['allergies'], (a) => str(a, 'allergen')).toSet();
          final r = reference.value;
          if (r == null) return const MiseLoading();
          return ListView(
            padding: const EdgeInsets.all(MiseSpace.gutter),
            children: [
              Text('Allergies', style: MiseType.h4),
              const SizedBox(height: 4),
              Text('We warn you when a meal contains these. Meals are made in a kitchen that handles all of them.', style: MiseType.bodySmall),
              const SizedBox(height: MiseSpace.sm),
              Wrap(spacing: 6, runSpacing: 6, children: [
                for (final t in r.$2)
                  FilterChip(label: Text(t.label), selected: _allergies!.contains(t.key), onSelected: (on) => setState(() => on ? _allergies!.add(t.key) : _allergies!.remove(t.key))),
              ]),
              const SizedBox(height: MiseSpace.xl),
              Text('Dietary preferences', style: MiseType.h4),
              const SizedBox(height: MiseSpace.sm),
              Wrap(spacing: 6, runSpacing: 6, children: [
                for (final t in r.$1)
                  FilterChip(label: Text(t.label), selected: _dietary!.contains(t.key), onSelected: (on) => setState(() => on ? _dietary!.add(t.key) : _dietary!.remove(t.key))),
              ]),
              const SizedBox(height: MiseSpace.xl),
              MiseButton(label: 'Save', busy: _busy, onPressed: _save),
            ],
          );
        },
      ),
    );
  }
}

/// (dietary, allergens) from /reference.
final referenceAllProvider = FutureProvider<(List<Term>, List<Term>)>((ref) => ref.watch(apiClientProvider).get(
  '/reference',
  (d) => (asList(asMap(d)['dietary'], Term.fromJson), asList(asMap(d)['allergens'], Term.fromJson)),
  options: publicOptions,
));

// ---------------------------------------------------------------- notifications

final _topicsProvider = FutureProvider<Json>((ref) => ref.watch(apiClientProvider).get('/me/notification-preferences', asMap));

class NotificationSettingsScreen extends ConsumerWidget {
  const NotificationSettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final prefs = ref.watch(_topicsProvider);
    final push = ref.watch(pushServiceProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Notifications')),
      body: prefs.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message),
        data: (p) {
          final topics = asList(p['topics'], (t) => t);
          return ListView(
            padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
            children: [
              const SectionHeader(title: 'On this phone'),
              if (!push.available)
                const Padding(
                  padding: EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                  child: NoticeBanner(message: 'Push notifications are not switched on in this version of the app. Updates still appear in your notification centre.'),
                )
              else
                SwitchListTile(
                  title: const Text('Push notifications'),
                  subtitle: const Text('Payments, your weekly meals, deliveries and replies. No marketing.'),
                  value: push.registered,
                  onChanged: (on) async {
                    on ? await push.enable() : await push.forgetDevice();
                    ref.invalidate(pushServiceProvider);
                  },
                ),
              const SizedBox(height: MiseSpace.md),
              const SectionHeader(title: 'Email'),
              const MiseListTile(title: 'Account and security', subtitle: 'Always sent', trailing: Icon(Icons.lock_outline, color: MiseColors.muted)),
              for (final t in topics)
                SwitchListTile(
                  title: Text(str(t, 'label')),
                  value: boolean(t, 'enabled'),
                  onChanged: (v) async {
                    try {
                      await ref.read(apiClientProvider).put<void>('/me/notification-preferences', (_) {}, body: {'topics': {str(t, 'topic'): v}});
                      ref.invalidate(_topicsProvider);
                    } on AppError catch (e) {
                      if (context.mounted) showMessage(context, e.message);
                    }
                  },
                ),
            ],
          );
        },
      ),
    );
  }
}

// ---------------------------------------------------------------- devices

final _sessionsProvider = FutureProvider<List<Json>>((ref) => ref.watch(apiClientProvider).get('/me/sessions', (d) => asList(asMap(d)['items'], (j) => j)));

class DevicesScreen extends ConsumerWidget {
  const DevicesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(_sessionsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Signed-in devices')),
      body: list.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message),
        data: (items) => ListView(
          padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
          children: [
            for (final s in items)
              MiseListTile(
                leading: str(s, 'client') == 'mobile' ? Icons.phone_iphone : Icons.laptop_outlined,
                title: str(s, 'device_name').isNotEmpty ? str(s, 'device_name') : (str(s, 'client') == 'mobile' ? 'Mise app' : 'Web browser'),
                subtitle: [if (boolean(s, 'current')) 'This device', if (time(s, 'last_seen_at') != null) 'Last used ${formatInstant(time(s, 'last_seen_at')!)}'].join(' · '),
                trailing: boolean(s, 'current')
                    ? null
                    : TextButton(
                        onPressed: () async {
                          await ref.read(apiClientProvider).delete('/me/sessions/${str(s, 'id')}');
                          ref.invalidate(_sessionsProvider);
                        },
                        child: const Text('Sign out'),
                      ),
              ),
            Padding(
              padding: const EdgeInsets.all(MiseSpace.gutter),
              child: MiseButton.secondary(
                label: 'Sign out all other devices',
                onPressed: () async {
                  final ok = await confirmAction(context, title: 'Sign out other devices?', message: 'Every other phone and browser will need to sign in again.', confirmLabel: 'Sign them out');
                  if (!ok) return;
                  await ref.read(apiClientProvider).delete('/me/sessions');
                  ref.invalidate(_sessionsProvider);
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------- favourites

class FavoritesScreen extends ConsumerWidget {
  const FavoritesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final meals = ref.watch(favoriteMealsProvider);
    final media = ref.watch(mediaUrlProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Favourites')),
      body: meals.when(
        loading: () => const MealListSkeleton(),
        error: (e, _) => ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(favoriteMealsProvider)),
        data: (list) => list.isEmpty
            ? EmptyState(icon: Icons.favorite_border, title: 'No favourites yet', message: 'Tap the heart on a meal to keep it here.', actionLabel: 'Browse the menu', onAction: () => context.go('/menu'))
            : ListView.separated(
                padding: const EdgeInsets.all(MiseSpace.gutter),
                itemCount: list.length + 1,
                separatorBuilder: (_, _) => const SizedBox(height: MiseSpace.xl),
                itemBuilder: (context, i) {
                  if (i == 0) return Text('Saved meals are not always on the menu. They show here whenever they are published.', style: MiseType.bodySmall);
                  final m = list[i - 1];
                  return MealCard(
                    name: m.name,
                    imageUrl: media(m.image?.src),
                    imageAlt: m.image?.alt ?? '',
                    meta: m.facts,
                    favorite: true,
                    onFavorite: () => toggleFavorite(context, ref, m),
                    onTap: () => context.push('/menu/meal/${m.slug}'),
                  );
                },
              ),
      ),
    );
  }
}

// ---------------------------------------------------------------- privacy and deletion

final _deletionProvider = FutureProvider<Json>((ref) => ref.watch(apiClientProvider).get('/me/account/deletion-request', asMap));

class PrivacyScreen extends ConsumerStatefulWidget {
  const PrivacyScreen({super.key});

  @override
  ConsumerState<PrivacyScreen> createState() => _PrivacyScreenState();
}

class _PrivacyScreenState extends ConsumerState<PrivacyScreen> {
  final _reason = TextEditingController();
  bool _ack = false;
  bool _busy = false;

  @override
  void dispose() {
    _reason.dispose();
    super.dispose();
  }

  Future<void> _request() async {
    final ok = await confirmAction(
      context,
      title: 'Ask us to delete your account?',
      message: 'We will contact you before anything is deleted. You can withdraw until we start.',
      confirmLabel: 'Send request',
      destructive: true,
    );
    if (!ok) return;
    setState(() => _busy = true);
    try {
      await ref.read(apiClientProvider).post<void>('/me/account/deletion-request', (_) {}, body: {'reason': _reason.text.trim(), 'acknowledged': true});
      ref.invalidate(_deletionProvider);
    } on AppError catch (e) {
      if (mounted) showMessage(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final d = ref.watch(_deletionProvider);
    final sub = ref.watch(mySubscriptionProvider).value;
    return Scaffold(
      appBar: AppBar(title: const Text('Privacy and your data')),
      body: d.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message),
        data: (j) {
          final req = j['request'] is Map ? asMap(j['request']) : null;
          final open = req != null && (str(req, 'status') == 'requested' || str(req, 'status') == 'in_progress');
          return ListView(
            padding: const EdgeInsets.all(MiseSpace.gutter),
            children: [
              MiseListTile(title: 'Privacy notice', onTap: () => openSitePage(ref, '/privacy')),
              const SizedBox(height: MiseSpace.lg),
              const Divider(color: MiseColors.foreground),
              const SizedBox(height: MiseSpace.sm),
              Text('Delete your account', style: MiseType.h3),
              const SizedBox(height: MiseSpace.xs),
              Text(str(j, 'notice'), style: MiseType.body),
              if (sub != null && sub.isActiveLike) ...[
                const SizedBox(height: MiseSpace.md),
                const NoticeBanner(tone: NoticeTone.warning, message: 'You still have an active subscription. Cancel it first, or tell us in your request what should happen to paid weeks.'),
              ],
              const SizedBox(height: MiseSpace.lg),
              if (open) ...[
                NoticeBanner(tone: NoticeTone.info, message: 'Request sent ${formatInstant(time(req, 'requested_at')!)}. Status: ${str(req, 'status').replaceAll('_', ' ')}.'),
                const SizedBox(height: MiseSpace.md),
                if (str(req, 'status') == 'requested')
                  MiseButton.secondary(
                    label: 'Withdraw my request',
                    onPressed: () async {
                      await ref.read(apiClientProvider).delete('/me/account/deletion-request');
                      ref.invalidate(_deletionProvider);
                    },
                  ),
              ] else ...[
                MiseTextField(label: 'Anything we should know? (optional)', controller: _reason, maxLines: 3, maxLength: 1000),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  controlAffinity: ListTileControlAffinity.leading,
                  value: _ack,
                  onChanged: (v) => setState(() => _ack = v ?? false),
                  title: const Text('I understand that some records must be kept and that nothing is deleted until the team contacts me.'),
                ),
                MiseButton(label: 'Request account deletion', kind: MiseButtonKind.danger, busy: _busy, onPressed: _ack ? _request : null),
              ],
            ],
          );
        },
      ),
    );
  }
}

class LegalScreen extends ConsumerWidget {
  const LegalScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(title: const Text('Terms and licences')),
      body: ListView(
        children: [
          MiseListTile(title: 'Terms', onTap: () => openSitePage(ref, '/terms')),
          MiseListTile(title: 'Privacy notice', onTap: () => openSitePage(ref, '/privacy')),
          MiseListTile(title: 'Photo credits', onTap: () => openSitePage(ref, '/credits')),
          MiseListTile(
            title: 'Open-source licences',
            onTap: () async {
              final info = await PackageInfo.fromPlatform();
              if (context.mounted) showLicensePage(context: context, applicationName: 'Mise', applicationVersion: '${info.version} (${info.buildNumber})');
            },
          ),
          FutureBuilder<PackageInfo>(
            future: PackageInfo.fromPlatform(),
            builder: (context, s) => Padding(
              padding: const EdgeInsets.all(MiseSpace.gutter),
              child: Text(s.hasData ? 'Version ${s.data!.version} (${s.data!.buildNumber})' : '', style: MiseType.caption),
            ),
          ),
        ],
      ),
    );
  }
}
