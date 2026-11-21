import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/links.dart';

const _minPassword = 12;

String? _emailProblem(String? v) {
  final s = (v ?? '').trim();
  if (s.isEmpty) return 'Enter your email address.';
  if (!RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch(s)) return 'Enter an email address like name@example.com.';
  return null;
}

String? _newPasswordProblem(String? v) {
  final s = v ?? '';
  if (s.runes.length < _minPassword) return 'Use at least $_minPassword characters.';
  if (s.trim().isEmpty) return 'Use letters, numbers or symbols, not only spaces.';
  return null;
}

/// Shared frame: wordmark, title, body; scrolls above the keyboard.
class _AuthFrame extends StatelessWidget {
  const _AuthFrame({required this.title, this.intro, required this.children, this.banner});

  final String title;
  final String? intro;
  final List<Widget> children;
  final Widget? banner;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Wordmark(size: 24)),
      body: SafeArea(
        child: SingleChildScrollView(
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.md, MiseSpace.gutter, MiseSpace.xxl),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (banner != null) ...[banner!, const SizedBox(height: MiseSpace.lg)],
              Semantics(header: true, child: Text(title, style: MiseType.h1)),
              if (intro != null) ...[const SizedBox(height: MiseSpace.xs), Text(intro!, style: MiseType.body.copyWith(color: MiseColors.muted))],
              const SizedBox(height: MiseSpace.xl),
              ...children,
            ],
          ),
        ),
      ),
    );
  }
}

class _FormError extends StatelessWidget {
  const _FormError(this.message);

  final String? message;

  @override
  Widget build(BuildContext context) {
    if (message == null) return const SizedBox.shrink();
    return Padding(padding: const EdgeInsets.only(bottom: MiseSpace.md), child: NoticeBanner(message: message!, tone: NoticeTone.error));
  }
}

// ---------------------------------------------------------------- sign in

class SignInScreen extends ConsumerStatefulWidget {
  const SignInScreen({super.key, this.from});

  /// Validated in-app path to return to.
  final String? from;

  @override
  ConsumerState<SignInScreen> createState() => _SignInScreenState();
}

class _SignInScreenState extends ConsumerState<SignInScreen> {
  final _form = GlobalKey<FormState>();
  final _email = TextEditingController();
  final _password = TextEditingController();
  bool _busy = false;
  String? _error;
  bool _unverified = false;

  @override
  void dispose() {
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy || !_form.currentState!.validate()) return;
    setState(() {
      _busy = true;
      _error = null;
      _unverified = false;
    });
    try {
      await ref.read(authControllerProvider.notifier).signIn(_email.text, _password.text);
      TextInput.finishAutofillContext();
      if (mounted) context.go(widget.from ?? '/home');
    } on AppError catch (e) {
      setState(() {
        _unverified = e.code == 'email_not_verified';
        // The API answers the same for a wrong password and an unknown email.
        _error = e.message;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authControllerProvider);
    final expired = auth is SignedOut && auth.expired;
    return _AuthFrame(
      title: 'Sign in',
      intro: 'Welcome back. Your box, your meals and your messages are waiting.',
      banner: expired ? const NoticeBanner(message: 'Your session ended. Please sign in again.', tone: NoticeTone.warning) : null,
      children: [
        _FormError(_error),
        if (_unverified)
          Padding(
            padding: const EdgeInsets.only(bottom: MiseSpace.md),
            child: MiseButton.secondary(label: 'I have confirmed my email', onPressed: _submit),
          ),
        AutofillGroup(
          child: Form(
            key: _form,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                MiseTextField(
                  label: 'Email',
                  controller: _email,
                  keyboardType: TextInputType.emailAddress,
                  textInputAction: TextInputAction.next,
                  autofillHints: const [AutofillHints.email, AutofillHints.username],
                  validator: _emailProblem,
                ),
                const SizedBox(height: MiseSpace.md),
                MiseTextField(
                  label: 'Password',
                  controller: _password,
                  obscure: true,
                  textInputAction: TextInputAction.done,
                  autofillHints: const [AutofillHints.password],
                  onSubmitted: (_) => _submit(),
                  validator: (v) => (v ?? '').isEmpty ? 'Enter your password.' : null,
                ),
              ],
            ),
          ),
        ),
        Align(
          alignment: Alignment.centerLeft,
          child: MiseButton.quiet(label: 'Forgotten your password?', onPressed: () => context.push('/forgot-password')),
        ),
        const SizedBox(height: MiseSpace.md),
        MiseButton(label: 'Sign in', busy: _busy, onPressed: _submit),
        const SizedBox(height: MiseSpace.xl),
        const Divider(),
        const SizedBox(height: MiseSpace.lg),
        Text('New to Mise?', style: MiseType.h4),
        const SizedBox(height: MiseSpace.sm),
        MiseButton.secondary(
          label: 'Create an account',
          onPressed: () => context.pushReplacement(Uri(path: '/register', queryParameters: {'from': ?widget.from}).toString()),
        ),
        const SizedBox(height: MiseSpace.xs),
        MiseButton.quiet(label: 'Keep browsing as a guest', expand: true, onPressed: () => context.go('/menu')),
      ],
    );
  }
}

// ---------------------------------------------------------------- register

class RegisterScreen extends ConsumerStatefulWidget {
  const RegisterScreen({super.key, this.from});

  final String? from;

  @override
  ConsumerState<RegisterScreen> createState() => _RegisterScreenState();
}

class _RegisterScreenState extends ConsumerState<RegisterScreen> {
  final _form = GlobalKey<FormState>();
  final _first = TextEditingController();
  final _last = TextEditingController();
  final _email = TextEditingController();
  final _password = TextEditingController();
  bool _terms = false;
  bool _termsError = false;
  bool _busy = false;
  String? _error;
  Map<String, String> _fields = const {};

  @override
  void dispose() {
    for (final c in [_first, _last, _email, _password]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _submit() async {
    final ok = _form.currentState!.validate();
    setState(() => _termsError = !_terms);
    if (_busy || !ok || !_terms) return;
    setState(() {
      _busy = true;
      _error = null;
      _fields = const {};
    });
    try {
      await ref.read(authApiProvider).register(
        firstName: _first.text,
        lastName: _last.text,
        email: _email.text,
        password: _password.text,
        acceptTerms: _terms,
      );
      TextInput.finishAutofillContext();
      if (mounted) context.go(Uri(path: '/check-email', queryParameters: {'email': _email.text.trim()}).toString());
    } on AppError catch (e) {
      setState(() {
        _error = e.message;
        _fields = e.fields;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return _AuthFrame(
      title: 'Create your account',
      intro: 'One account for the app and mise.tt. We will send you an email to confirm it.',
      children: [
        _FormError(_error),
        AutofillGroup(
          child: Form(
            key: _form,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                MiseTextField(
                  label: 'First name',
                  controller: _first,
                  textCapitalization: TextCapitalization.words,
                  textInputAction: TextInputAction.next,
                  autofillHints: const [AutofillHints.givenName],
                  errorText: _fields['first_name'],
                  validator: (v) => (v ?? '').trim().isEmpty ? 'Enter your first name.' : null,
                ),
                const SizedBox(height: MiseSpace.md),
                MiseTextField(
                  label: 'Last name',
                  controller: _last,
                  textCapitalization: TextCapitalization.words,
                  textInputAction: TextInputAction.next,
                  autofillHints: const [AutofillHints.familyName],
                  errorText: _fields['last_name'],
                  validator: (v) => (v ?? '').trim().isEmpty ? 'Enter your last name.' : null,
                ),
                const SizedBox(height: MiseSpace.md),
                MiseTextField(
                  label: 'Email',
                  controller: _email,
                  keyboardType: TextInputType.emailAddress,
                  textInputAction: TextInputAction.next,
                  autofillHints: const [AutofillHints.email],
                  errorText: _fields['email'],
                  validator: _emailProblem,
                ),
                const SizedBox(height: MiseSpace.md),
                MiseTextField(
                  label: 'Password',
                  hint: 'At least $_minPassword characters',
                  controller: _password,
                  obscure: true,
                  textInputAction: TextInputAction.done,
                  autofillHints: const [AutofillHints.newPassword],
                  errorText: _fields['password'],
                  validator: _newPasswordProblem,
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: MiseSpace.md),
        _TermsCheckbox(value: _terms, error: _termsError, onChanged: (v) => setState(() => _terms = v)),
        const SizedBox(height: MiseSpace.lg),
        MiseButton(label: 'Create account', busy: _busy, onPressed: _submit),
        const SizedBox(height: MiseSpace.md),
        MiseButton.quiet(
          label: 'I already have an account',
          expand: true,
          onPressed: () => context.pushReplacement(Uri(path: '/sign-in', queryParameters: {'from': ?widget.from}).toString()),
        ),
      ],
    );
  }
}

class _TermsCheckbox extends ConsumerWidget {
  const _TermsCheckbox({required this.value, required this.error, required this.onChanged});

  final bool value;
  final bool error;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final link = MiseType.body.copyWith(color: MiseColors.primary, decoration: TextDecoration.underline);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        MergeSemantics(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(value: value, onChanged: (v) => onChanged(v ?? false), isError: error),
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text.rich(
                    TextSpan(
                      style: MiseType.body,
                      children: [
                        const TextSpan(text: 'I agree to the '),
                        TextSpan(text: 'terms', style: link, recognizer: TapGestureRecognizer()..onTap = () => openSitePage(ref, '/terms')),
                        const TextSpan(text: ' and have read the '),
                        TextSpan(text: 'privacy notice', style: link, recognizer: TapGestureRecognizer()..onTap = () => openSitePage(ref, '/privacy')),
                        const TextSpan(text: '.'),
                      ],
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
        if (error)
          Padding(
            padding: const EdgeInsets.only(left: 12),
            child: Semantics(liveRegion: true, child: Text('Please accept the terms to continue.', style: MiseType.caption.copyWith(color: MiseColors.error))),
          ),
      ],
    );
  }
}

// ---------------------------------------------------------------- check email

class CheckEmailScreen extends StatelessWidget {
  const CheckEmailScreen({super.key, required this.email});

  final String email;

  @override
  Widget build(BuildContext context) {
    return _AuthFrame(
      title: 'Check your email',
      intro: email.isEmpty
          ? 'We have sent you a link to confirm your address.'
          : 'If $email can be used for a Mise account, we have sent a link to confirm it. The link works for 48 hours.',
      children: [
        Text('Open the link on this phone and the app will take it from there, or sign in once you have confirmed.', style: MiseType.body),
        const SizedBox(height: MiseSpace.xl),
        MiseButton(label: 'Sign in', onPressed: () => context.go('/sign-in')),
        const SizedBox(height: MiseSpace.xs),
        MiseButton.quiet(label: 'Browse the menu', expand: true, onPressed: () => context.go('/menu')),
      ],
    );
  }
}

// ---------------------------------------------------------------- forgot / reset

class ForgotPasswordScreen extends ConsumerStatefulWidget {
  const ForgotPasswordScreen({super.key});

  @override
  ConsumerState<ForgotPasswordScreen> createState() => _ForgotPasswordScreenState();
}

class _ForgotPasswordScreenState extends ConsumerState<ForgotPasswordScreen> {
  final _form = GlobalKey<FormState>();
  final _email = TextEditingController();
  bool _busy = false;
  bool _sent = false;
  String? _error;

  @override
  void dispose() {
    _email.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy || !_form.currentState!.validate()) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(authApiProvider).forgotPassword(_email.text);
      setState(() => _sent = true);
    } on AppError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_sent) {
      return _AuthFrame(
        title: 'Check your email',
        // Same answer whether or not the address has an account.
        intro: 'If there is a Mise account for ${_email.text.trim()}, we have sent a link to choose a new password. It works for one hour.',
        children: [MiseButton(label: 'Back to sign in', onPressed: () => context.go('/sign-in'))],
      );
    }
    return _AuthFrame(
      title: 'Forgotten your password?',
      intro: "Enter your email and we'll send you a link to choose a new one.",
      children: [
        _FormError(_error),
        Form(
          key: _form,
          child: MiseTextField(
            label: 'Email',
            controller: _email,
            keyboardType: TextInputType.emailAddress,
            autofillHints: const [AutofillHints.email],
            textInputAction: TextInputAction.send,
            onSubmitted: (_) => _submit(),
            validator: _emailProblem,
          ),
        ),
        const SizedBox(height: MiseSpace.lg),
        MiseButton(label: 'Send link', busy: _busy, onPressed: _submit),
      ],
    );
  }
}

class ResetPasswordScreen extends ConsumerStatefulWidget {
  const ResetPasswordScreen({super.key, required this.token});

  final String token;

  @override
  ConsumerState<ResetPasswordScreen> createState() => _ResetPasswordScreenState();
}

class _ResetPasswordScreenState extends ConsumerState<ResetPasswordScreen> {
  final _form = GlobalKey<FormState>();
  final _password = TextEditingController();
  bool _busy = false;
  bool _done = false;
  String? _error;

  @override
  void dispose() {
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy || !_form.currentState!.validate()) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(authApiProvider).resetPassword(widget.token, _password.text);
      setState(() => _done = true);
    } on AppError catch (e) {
      setState(() => _error = e.code == 'token_invalid' ? 'This link has expired or was already used. Ask for a new one.' : e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.token.isEmpty) {
      return _AuthFrame(
        title: 'This link is incomplete',
        children: [MiseButton(label: 'Ask for a new link', onPressed: () => context.go('/forgot-password'))],
      );
    }
    if (_done) {
      return _AuthFrame(
        title: 'Password changed',
        intro: 'You have been signed out everywhere else. Sign in with your new password.',
        children: [MiseButton(label: 'Sign in', onPressed: () => context.go('/sign-in'))],
      );
    }
    return _AuthFrame(
      title: 'Choose a new password',
      children: [
        _FormError(_error),
        AutofillGroup(
          child: Form(
            key: _form,
            child: MiseTextField(
              label: 'New password',
              hint: 'At least $_minPassword characters',
              controller: _password,
              obscure: true,
              autofillHints: const [AutofillHints.newPassword],
              onSubmitted: (_) => _submit(),
              validator: _newPasswordProblem,
            ),
          ),
        ),
        const SizedBox(height: MiseSpace.lg),
        MiseButton(label: 'Save password', busy: _busy, onPressed: _submit),
        if (_error != null) ...[
          const SizedBox(height: MiseSpace.xs),
          MiseButton.quiet(label: 'Ask for a new link', expand: true, onPressed: () => context.go('/forgot-password')),
        ],
      ],
    );
  }
}

class VerifyEmailScreen extends ConsumerStatefulWidget {
  const VerifyEmailScreen({super.key, required this.token});

  final String token;

  @override
  ConsumerState<VerifyEmailScreen> createState() => _VerifyEmailScreenState();
}

class _VerifyEmailScreenState extends ConsumerState<VerifyEmailScreen> {
  late final Future<void> _result = widget.token.isEmpty
      ? Future.error(const AppError(ErrorKind.validation, 'This link is incomplete.'))
      : ref.read(authApiProvider).verifyEmail(widget.token);

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<void>(
      future: _result,
      builder: (context, snap) {
        if (snap.connectionState != ConnectionState.done) {
          return const Scaffold(body: MiseLoading(label: 'Confirming your email'));
        }
        if (snap.hasError) {
          final e = AppError.from(snap.error!);
          return _AuthFrame(
            title: 'We could not confirm your email',
            intro: e.code == 'token_invalid' || e.kind == ErrorKind.validation
                ? 'This link has expired or was already used. Signing in sends a new one.'
                : e.message,
            children: [MiseButton(label: 'Sign in', onPressed: () => context.go('/sign-in'))],
          );
        }
        return _AuthFrame(
          title: 'Email confirmed',
          intro: 'Thank you. You can sign in now.',
          children: [MiseButton(label: 'Sign in', onPressed: () => context.go('/sign-in'))],
        );
      },
    );
  }
}
