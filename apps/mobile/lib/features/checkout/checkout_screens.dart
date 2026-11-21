import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/api_client.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/models/commerce.dart';
import '../subscriptions/subscription_api.dart';
import 'payment_flow.dart';

/// Checkout for a new subscription: plan and server-priced total, delivery
/// address and its availability, payment method, then hosted payment.
class SubscribeScreen extends ConsumerStatefulWidget {
  const SubscribeScreen({super.key, required this.planSlug});

  final String planSlug;

  @override
  ConsumerState<SubscribeScreen> createState() => _SubscribeScreenState();
}

class _SubscribeScreenState extends ConsumerState<SubscribeScreen> {
  Quote? _quote;
  List<Address> _addresses = const [];
  List<PaymentOption> _options = const [];
  String? _addressId;
  String? _provider;
  AppError? _loadError;
  String? _submitError;
  bool _busy = false;

  /// One key per checkout attempt: a retry of the same tap cannot create a
  /// second subscription or charge.
  String _idempotencyKey = ApiClient.newKey();

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loadError = null);
    try {
      final api = ref.read(commerceApiProvider);
      final plan = await api.plan(widget.planSlug);
      final results = await Future.wait([
        api.quote(plan.id),
        api.addresses(),
        ref.read(paymentFlowProvider).options(plan.price?.currency ?? 'TTD'),
      ]);
      final addresses = results[1] as List<Address>;
      final options = results[2] as List<PaymentOption>;
      setState(() {
        _quote = results[0] as Quote;
        _addresses = addresses;
        _options = options;
        _addressId ??= (addresses.where((a) => a.isDefault).firstOrNull ?? addresses.firstOrNull)?.id;
        _provider ??= options.firstOrNull?.provider;
      });
    } on AppError catch (e) {
      setState(() => _loadError = e);
    }
  }

  Future<void> _pay() async {
    final q = _quote;
    if (q == null || _addressId == null || _busy) return;
    setState(() {
      _busy = true;
      _submitError = null;
    });
    final flow = ref.read(paymentFlowProvider);
    try {
      final (_, checkout, activated) = await flow.subscribe(
        planId: q.plan.id,
        addressId: _addressId!,
        provider: _provider,
        idempotencyKey: _idempotencyKey,
      );
      ref.invalidate(mySubscriptionProvider);
      if (!mounted) return;
      if (activated || checkout == null) {
        context.go('/box');
        showMessage(context, 'Your subscription is active.');
        return;
      }
      await flow.authorize(checkout);
      if (mounted) context.pushReplacement('/checkout/result/${checkout.id}');
    } on AppError catch (e) {
      setState(() => _submitError = switch (e.code) {
        'payment_in_progress' || 'payment_status_unknown' => 'An earlier payment attempt is still being confirmed. Please wait a minute, then check your subscription before trying again.',
        'payment_method_unavailable' => 'That payment method is not available right now. Please choose another.',
        'subscription_exists' => 'You already have a subscription.',
        _ => e.message,
      });
      if (e.code == 'payment_method_unavailable') unawaited(_load());
      // A new attempt after a definite refusal gets a new key.
      if (!e.isNetwork) _idempotencyKey = ApiClient.newKey();
    } on FormatException {
      setState(() => _submitError = 'The payment page could not be opened safely. Please try again later.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final q = _quote;
    return Scaffold(
      appBar: AppBar(title: const Text('Checkout')),
      body: q == null
          ? (_loadError != null ? ErrorState(message: _loadError!.message, onRetry: _load) : const MiseLoading(label: 'Preparing your checkout'))
          : ListView(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xxl),
              children: [
                const _StepTitle(number: 1, title: 'Your plan'),
                Text(q.plan.name, style: MiseType.h3),
                Text('${plural(q.plan.mealsPerWeek, 'meal')} a week · first box ${q.firstWeekLabel}', style: MiseType.bodySmall),
                if (q.firstWeekCutoffAt != null)
                  Text('Choose your first meals by ${formatInstant(q.firstWeekCutoffAt!)}', style: MiseType.bodySmall),
                const _StepTitle(number: 2, title: 'Delivery address'),
                if (_addresses.isEmpty)
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text('Add an address to continue.', style: MiseType.body),
                      const SizedBox(height: MiseSpace.sm),
                      MiseButton.secondary(
                        label: 'Add address',
                        onPressed: () async {
                          await context.push('/account/addresses');
                          ref.invalidate(addressesProvider);
                          unawaited(_load());
                        },
                      ),
                    ],
                  )
                else
                  RadioGroup<String>(
                    groupValue: _addressId,
                    onChanged: (v) => setState(() => _addressId = v),
                    child: Column(
                      children: [
                        for (final a in _addresses)
                          RadioListTile<String>(
                            contentPadding: EdgeInsets.zero,
                            value: a.id,
                            title: Text(a.label ?? a.recipientName, style: MiseType.body),
                            subtitle: Text('${a.oneLine}\n${a.availabilityLabel}', style: MiseType.caption),
                            isThreeLine: true,
                          ),
                      ],
                    ),
                  ),
                const _StepTitle(number: 3, title: 'Summary'),
                _AmountsTable(amounts: q.dueNow, label: 'Due now'),
                const SizedBox(height: MiseSpace.xs),
                Text(
                  'Then ${formatMoney(q.renewal.totalMinor, q.renewal.currency)} a week until you skip or cancel.',
                  style: MiseType.bodySmall,
                ),
                if (q.taxNote.isNotEmpty) Text(q.taxNote, style: MiseType.caption),
                const _StepTitle(number: 4, title: 'Payment method'),
                if (_options.isEmpty)
                  const NoticeBanner(tone: NoticeTone.warning, message: 'Online payment is not available right now. Please try again later.')
                else
                  RadioGroup<String>(
                    groupValue: _provider,
                    onChanged: (v) => setState(() => _provider = v),
                    child: Column(
                      children: [
                        for (final o in _options)
                          RadioListTile<String>(
                            contentPadding: EdgeInsets.zero,
                            value: o.provider,
                            title: Row(
                              children: [
                                Flexible(child: Text(o.label, style: MiseType.body)),
                                if (o.test) ...[const SizedBox(width: MiseSpace.xs), const MiseTag('Test', tone: TagTone.warning)],
                              ],
                            ),
                            subtitle: Text(
                              [
                                if (o.test) 'No real money moves.',
                                if (!o.recurring) "You'll pay each week yourself.",
                                "You'll finish on the provider's secure page.",
                              ].join(' '),
                              style: MiseType.caption,
                            ),
                          ),
                      ],
                    ),
                  ),
                if (q.terms.isNotEmpty) ...[const SizedBox(height: MiseSpace.md), Text(q.terms, style: MiseType.caption)],
                if (_submitError != null) ...[const SizedBox(height: MiseSpace.md), NoticeBanner(tone: NoticeTone.error, message: _submitError!)],
              ],
            ),
      bottomNavigationBar: q == null
          ? null
          : BottomActionBar(
              child: MiseButton(
                label: 'Pay ${formatMoney(q.dueNow.totalMinor, q.dueNow.currency)}',
                busy: _busy,
                semanticsLabel: 'Pay ${formatMoney(q.dueNow.totalMinor, q.dueNow.currency)} for ${q.plan.name}',
                onPressed: _addressId == null || _options.isEmpty || !q.payments.available ? null : _pay,
              ),
            ),
    );
  }
}

/// Pays one week that is due or failed (`/checkout/pay/<billing cycle>`).
class PayWeekScreen extends ConsumerStatefulWidget {
  const PayWeekScreen({super.key, required this.billingCycleId});

  final String billingCycleId;

  @override
  ConsumerState<PayWeekScreen> createState() => _PayWeekScreenState();
}

class _PayWeekScreenState extends ConsumerState<PayWeekScreen> {
  List<PaymentOption>? _options;
  String? _provider;
  bool _busy = false;
  String? _error;
  String _key = ApiClient.newKey();

  @override
  void initState() {
    super.initState();
    _loadOptions();
  }

  Future<void> _loadOptions() async {
    try {
      final o = await ref.read(paymentFlowProvider).options('TTD');
      setState(() {
        _options = o;
        _provider = o.firstOrNull?.provider;
      });
    } on AppError catch (e) {
      setState(() => _error = e.message);
    }
  }

  Future<void> _pay() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    final flow = ref.read(paymentFlowProvider);
    try {
      final c = await flow.payWeek(billingCycleId: widget.billingCycleId, provider: _provider, idempotencyKey: _key);
      await flow.authorize(c);
      if (mounted) context.pushReplacement('/checkout/result/${c.id}');
    } on AppError catch (e) {
      setState(() => _error = e.code == 'payment_in_progress' || e.code == 'payment_status_unknown'
          ? 'An earlier attempt for this week is still being confirmed. Please wait before trying again.'
          : e.message);
      if (!e.isNetwork) _key = ApiClient.newKey();
    } on FormatException {
      setState(() => _error = 'The payment page could not be opened safely.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final sub = ref.watch(mySubscriptionProvider).value;
    final cycle = sub?.cycles.where((c) => c.billing?.id == widget.billingCycleId).firstOrNull ?? sub?.outstanding;
    final options = _options;
    return Scaffold(
      appBar: AppBar(title: const Text('Pay for a week')),
      body: ListView(
        padding: const EdgeInsets.all(MiseSpace.gutter),
        children: [
          if (cycle != null) ...[
            Text(cycle.weekLabel, style: MiseType.h2),
            if (cycle.billing != null) Text(formatMoney(cycle.billing!.totalMinor, cycle.billing!.currency), style: MiseType.figures(MiseType.h3)),
            const SizedBox(height: MiseSpace.lg),
          ],
          if (options == null && _error == null) const MiseLoading(),
          if (options != null && options.isEmpty) const NoticeBanner(tone: NoticeTone.warning, message: 'Online payment is not available right now.'),
          if (options != null && options.isNotEmpty)
            RadioGroup<String>(
              groupValue: _provider,
              onChanged: (v) => setState(() => _provider = v),
              child: Column(
                children: [
                  for (final o in options)
                    RadioListTile<String>(
                      contentPadding: EdgeInsets.zero,
                      value: o.provider,
                      title: Text(o.test ? '${o.label} (test)' : o.label),
                      subtitle: o.test ? const Text('No real money moves.') : null,
                    ),
                ],
              ),
            ),
          if (_error != null) ...[const SizedBox(height: MiseSpace.md), NoticeBanner(tone: NoticeTone.error, message: _error!)],
        ],
      ),
      bottomNavigationBar: BottomActionBar(
        child: MiseButton(
          label: cycle?.billing != null ? 'Pay ${formatMoney(cycle!.billing!.totalMinor, cycle.billing!.currency)}' : 'Pay',
          busy: _busy,
          onPressed: options == null || options.isEmpty ? null : _pay,
        ),
      ),
    );
  }
}

/// The server's verdict on a checkout. Polls; never decides on its own.
class CheckoutResultScreen extends ConsumerStatefulWidget {
  const CheckoutResultScreen({super.key, required this.checkoutId});

  final String checkoutId;

  @override
  ConsumerState<CheckoutResultScreen> createState() => _CheckoutResultScreenState();
}

class _CheckoutResultScreenState extends ConsumerState<CheckoutResultScreen> {
  CheckoutResult? _result;
  bool _timedOut = false;
  StreamSubscription<CheckoutResult>? _sub;

  @override
  void initState() {
    super.initState();
    _start();
  }

  void _start() {
    setState(() => _timedOut = false);
    _sub?.cancel();
    _sub = pollOutcome(() => ref.read(paymentFlowProvider).result(widget.checkoutId)).listen(
      (r) => setState(() => _result = r),
      onDone: () {
        if (!mounted) return;
        ref.invalidate(mySubscriptionProvider);
        ref.invalidate(weeksProvider);
        if (_result == null || _result!.outcome == PaymentOutcome.pending) setState(() => _timedOut = true);
      },
    );
  }

  @override
  void dispose() {
    _sub?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final r = _result;
    final outcome = r?.outcome ?? PaymentOutcome.pending;
    final copy = outcomeCopy(outcome, timedOut: _timedOut);
    final (icon, color) = switch (outcome) {
      PaymentOutcome.succeeded => (Icons.check_circle_outline, MiseColors.success),
      PaymentOutcome.failed || PaymentOutcome.expired => (Icons.error_outline, MiseColors.error),
      PaymentOutcome.pending => (Icons.hourglass_top, MiseColors.warning),
    };
    return PopScope(
      canPop: outcome != PaymentOutcome.pending || _timedOut,
      child: Scaffold(
        appBar: AppBar(automaticallyImplyLeading: false, title: const Text('Payment')),
        body: Padding(
          padding: const EdgeInsets.all(MiseSpace.gutter),
          child: Semantics(
            liveRegion: true,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const SizedBox(height: MiseSpace.xl),
                if (outcome == PaymentOutcome.pending && !_timedOut)
                  const SizedBox.square(dimension: 40, child: CircularProgressIndicator())
                else
                  Icon(icon, size: 48, color: color),
                const SizedBox(height: MiseSpace.lg),
                Text(copy.title, style: MiseType.h1),
                const SizedBox(height: MiseSpace.sm),
                Text(copy.body, style: MiseType.bodyLarge),
                if (r != null) ...[
                  const SizedBox(height: MiseSpace.lg),
                  FactRow(label: 'Amount', value: formatMoney(r.checkout.amountMinor, r.checkout.currency)),
                  if (r.weekLabel.isNotEmpty) FactRow(label: 'For', value: r.weekLabel),
                  if (r.checkout.test) const FactRow(label: 'Mode', value: 'Test payment — no real money moved'),
                ],
              ],
            ),
          ),
        ),
        bottomNavigationBar: BottomActionBar(
          child: switch (outcome) {
            PaymentOutcome.succeeded => MiseButton(label: 'Choose my meals', onPressed: () => context.go('/box')),
            PaymentOutcome.failed || PaymentOutcome.expired => Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                MiseButton(label: 'Try again', onPressed: () => context.go('/subscription')),
                MiseButton.quiet(label: 'Get help', expand: true, onPressed: () => context.go('/support')),
              ],
            ),
            PaymentOutcome.pending when _timedOut => Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                MiseButton(label: 'Check again', onPressed: _start),
                MiseButton.quiet(label: 'Go to my subscription', expand: true, onPressed: () => context.go('/subscription')),
              ],
            ),
            PaymentOutcome.pending => const SizedBox(height: kMinTouchTarget),
          },
        ),
      ),
    );
  }
}

class _StepTitle extends StatelessWidget {
  const _StepTitle({required this.number, required this.title});

  final int number;
  final String title;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: MiseSpace.xl, bottom: MiseSpace.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Divider(color: MiseColors.foreground),
          const SizedBox(height: MiseSpace.sm),
          Semantics(
            header: true,
            child: Row(
              children: [
                Text('$number'.padLeft(2, '0'), style: MiseType.figures(MiseType.h4.copyWith(color: MiseColors.accent))),
                const SizedBox(width: MiseSpace.sm),
                Text(title, style: MiseType.h4),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _AmountsTable extends StatelessWidget {
  const _AmountsTable({required this.amounts, required this.label});

  final Amounts amounts;
  final String label;

  @override
  Widget build(BuildContext context) {
    final c = amounts.currency;
    final rows = [
      ('Meals', formatMoney(amounts.subtotalMinor, c)),
      if (amounts.discountMinor != 0) ('Discount', formatMoney(-amounts.discountMinor, c)),
      ('Delivery', amounts.deliveryFeeMinor == 0 ? formatMoney(0, c) : formatMoney(amounts.deliveryFeeMinor, c)),
      if (amounts.taxMinor != 0) ('Tax', formatMoney(amounts.taxMinor, c)),
    ];
    return Column(
      children: [
        for (final (k, v) in rows)
          MergeSemantics(
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 4),
              child: Row(children: [Expanded(child: Text(k, style: MiseType.body)), Text(v, style: MiseType.figures(MiseType.body))]),
            ),
          ),
        const Divider(),
        MergeSemantics(
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 6),
            child: Row(
              children: [
                Expanded(child: Text(label, style: MiseType.h4)),
                Text(formatMoney(amounts.totalMinor, c), style: MiseType.figures(MiseType.h4)),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
