import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Unread team replies across the customer's conversations (tab badge).
/// Filled in by the support feature (Stage 8).
final supportUnreadProvider = Provider<int>((ref) => 0);
