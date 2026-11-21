import 'package:intl/intl.dart';

/// Formats minor units the same way as the web (`formatMoney` in
/// `packages/api-client`): 33000 TTD is "TTD 330.00". Display only — every
/// amount comes from the API; the app never computes a price.
String formatMoney(int minor, String currency) {
  final sign = minor < 0 ? '-' : '';
  final abs = minor.abs();
  final whole = NumberFormat.decimalPattern('en_US').format(abs ~/ 100);
  return '$sign$currency $whole.${(abs % 100).toString().padLeft(2, '0')}';
}

/// Trinidad and Tobago time (UTC-4, no daylight saving) — the zone every Mise
/// deadline and delivery date is set in.
DateTime toTT(DateTime utc) => utc.toUtc().subtract(const Duration(hours: 4));

final _day = DateFormat('EEEE d MMMM', 'en_US');
final _shortDay = DateFormat('EEE d MMM', 'en_US');
final _time = DateFormat('h:mm a', 'en_US');

/// "Thursday 26 November" for a `YYYY-MM-DD` date (no time zone applies).
String formatDate(String isoDate) {
  final d = DateTime.tryParse(isoDate);
  return d == null ? isoDate : _day.format(d);
}

String formatShortDate(String isoDate) {
  final d = DateTime.tryParse(isoDate);
  return d == null ? isoDate : _shortDay.format(d);
}

/// "Thursday 26 November, 11:59 PM" in Trinidad time, for an RFC 3339 instant.
String formatInstant(DateTime instant) {
  final t = toTT(instant);
  return '${_day.format(t)}, ${_time.format(t)}';
}

String formatTime(DateTime instant) => _time.format(toTT(instant));

/// "2 days left", "5 hours left", "closed" — relative to [now].
String timeLeft(DateTime deadline, DateTime now) {
  final d = deadline.difference(now);
  if (d.isNegative) return 'Closed';
  if (d.inDays >= 1) return '${d.inDays} ${d.inDays == 1 ? 'day' : 'days'} left';
  if (d.inHours >= 1) return '${d.inHours} ${d.inHours == 1 ? 'hour' : 'hours'} left';
  return '${d.inMinutes.clamp(1, 59)} min left';
}

String plural(int n, String one, [String? many]) => '$n ${n == 1 ? one : (many ?? '${one}s')}';
