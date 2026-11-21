import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';

/// The one approved theme: light. No dark theme until one is designed and
/// approved (brief §9).
ThemeData buildMiseTheme() {
  const scheme = ColorScheme(
    brightness: Brightness.light,
    primary: MiseColors.primary,
    onPrimary: MiseColors.onPrimary,
    secondary: MiseColors.secondary,
    onSecondary: MiseColors.onPrimary,
    tertiary: MiseColors.accent,
    onTertiary: MiseColors.onPrimary,
    error: MiseColors.error,
    onError: MiseColors.onPrimary,
    surface: MiseColors.background,
    onSurface: MiseColors.foreground,
    surfaceContainerLowest: MiseColors.surface,
    surfaceContainerLow: MiseColors.surface,
    surfaceContainer: MiseColors.surface,
    surfaceContainerHigh: MiseColors.surface,
    surfaceContainerHighest: MiseColors.surface,
    onSurfaceVariant: MiseColors.muted,
    outline: MiseColors.borderStrong,
    outlineVariant: MiseColors.border,
    shadow: Color(0x2922211D),
    scrim: Color(0x8022211D),
  );

  final text = TextTheme(
    displayLarge: MiseType.displayLarge,
    headlineLarge: MiseType.h1,
    headlineMedium: MiseType.h2,
    headlineSmall: MiseType.h3,
    titleLarge: MiseType.h3,
    titleMedium: MiseType.h4,
    titleSmall: MiseType.label,
    bodyLarge: MiseType.bodyLarge,
    bodyMedium: MiseType.body,
    bodySmall: MiseType.bodySmall,
    labelLarge: MiseType.button,
    labelMedium: MiseType.label,
    labelSmall: MiseType.caption,
  );

  const controlShape = RoundedRectangleBorder(borderRadius: BorderRadius.all(MiseRadii.md));

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: MiseColors.background,
    fontFamily: MiseType.sans,
    textTheme: text,
    splashFactory: InkSparkle.constantTurbulenceSeedSplashFactory,
    dividerTheme: const DividerThemeData(color: MiseColors.border, thickness: 1, space: 1),
    appBarTheme: AppBarTheme(
      backgroundColor: MiseColors.background,
      foregroundColor: MiseColors.foreground,
      elevation: 0,
      scrolledUnderElevation: 0,
      centerTitle: false,
      titleTextStyle: MiseType.h4,
      systemOverlayStyle: SystemUiOverlayStyle.dark.copyWith(statusBarColor: Colors.transparent),
      shape: const Border(bottom: BorderSide(color: Colors.transparent)),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: MiseColors.surface,
      elevation: 0,
      height: 68,
      indicatorColor: MiseColors.background,
      indicatorShape: controlShape,
      surfaceTintColor: Colors.transparent,
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => MiseType.caption.copyWith(
          color: s.contains(WidgetState.selected) ? MiseColors.primary : MiseColors.muted,
          fontWeight: s.contains(WidgetState.selected) ? FontWeight.w600 : FontWeight.w400,
        ),
      ),
      iconTheme: WidgetStateProperty.resolveWith(
        (s) => IconThemeData(color: s.contains(WidgetState.selected) ? MiseColors.primary : MiseColors.muted, size: 24),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: MiseColors.surface,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      hintStyle: MiseType.body.copyWith(color: MiseColors.muted),
      labelStyle: MiseType.label,
      floatingLabelBehavior: FloatingLabelBehavior.never,
      errorStyle: MiseType.caption.copyWith(color: MiseColors.error),
      border: const OutlineInputBorder(
        borderRadius: BorderRadius.all(MiseRadii.md),
        borderSide: BorderSide(color: MiseColors.borderStrong),
      ),
      enabledBorder: const OutlineInputBorder(
        borderRadius: BorderRadius.all(MiseRadii.md),
        borderSide: BorderSide(color: MiseColors.borderStrong),
      ),
      focusedBorder: const OutlineInputBorder(
        borderRadius: BorderRadius.all(MiseRadii.md),
        borderSide: BorderSide(color: MiseColors.primary, width: 2),
      ),
      errorBorder: const OutlineInputBorder(
        borderRadius: BorderRadius.all(MiseRadii.md),
        borderSide: BorderSide(color: MiseColors.error),
      ),
      focusedErrorBorder: const OutlineInputBorder(
        borderRadius: BorderRadius.all(MiseRadii.md),
        borderSide: BorderSide(color: MiseColors.error, width: 2),
      ),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: MiseColors.surface,
      surfaceTintColor: Colors.transparent,
      showDragHandle: true,
      dragHandleColor: MiseColors.border,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: MiseRadii.xl)),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: MiseColors.surface,
      surfaceTintColor: Colors.transparent,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(MiseRadii.lg)),
      titleTextStyle: MiseType.h3,
      contentTextStyle: MiseType.body,
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: MiseColors.foreground,
      contentTextStyle: MiseType.body.copyWith(color: MiseColors.surface),
      behavior: SnackBarBehavior.floating,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(MiseRadii.md)),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: MiseColors.background,
      selectedColor: MiseColors.primary,
      side: const BorderSide(color: MiseColors.borderStrong),
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(MiseRadii.md)),
      labelStyle: MiseType.label.copyWith(fontSize: 14),
      secondaryLabelStyle: MiseType.label.copyWith(fontSize: 14, color: MiseColors.onPrimary),
      checkmarkColor: MiseColors.onPrimary,
      showCheckmark: false,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
    ),
    progressIndicatorTheme: const ProgressIndicatorThemeData(color: MiseColors.primary, linearTrackColor: MiseColors.border),
    pageTransitionsTheme: const PageTransitionsTheme(
      builders: {
        TargetPlatform.android: PredictiveBackPageTransitionsBuilder(),
        TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
      },
    ),
  );
}
