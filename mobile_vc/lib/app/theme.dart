import 'package:flutter/material.dart';

class AppTheme {
  static ThemeData light() {
    const seed = Color(0xFF2563EB);
    final scheme =
        ColorScheme.fromSeed(seedColor: seed, brightness: Brightness.light);
    return _build(
      scheme: scheme,
      scaffoldBackground: const Color(0xFFF4F7FB),
      surface: Colors.white,
      snackBarBackground: const Color(0xFF0F172A),
      snackBarForeground: Colors.white,
      outlineAlpha: 0.72,
    );
  }

  static ThemeData dark() {
    const seed = Color(0xFF60A5FA);
    final scheme =
        ColorScheme.fromSeed(seedColor: seed, brightness: Brightness.dark);
    return _build(
      scheme: scheme,
      scaffoldBackground: const Color(0xFF0B1120),
      surface: const Color(0xFF111827),
      snackBarBackground: const Color(0xFFE5E7EB),
      snackBarForeground: const Color(0xFF0F172A),
      outlineAlpha: 0.36,
    );
  }

  static ThemeData _build({
    required ColorScheme scheme,
    required Color scaffoldBackground,
    required Color surface,
    required Color snackBarBackground,
    required Color snackBarForeground,
    required double outlineAlpha,
  }) {
    final outline = scheme.outlineVariant.withValues(alpha: outlineAlpha);
    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      brightness: scheme.brightness,
      scaffoldBackgroundColor: scaffoldBackground,
      dividerColor: outline,
      appBarTheme: AppBarTheme(
        centerTitle: false,
        backgroundColor: scaffoldBackground.withValues(alpha: 0.92),
        foregroundColor: scheme.onSurface,
        elevation: 0,
        scrolledUnderElevation: 0,
        surfaceTintColor: Colors.transparent,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: surface,
        contentPadding:
            const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(20),
          borderSide: BorderSide(color: outline),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(20),
          borderSide: BorderSide(color: outline),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(20),
          borderSide: BorderSide(color: scheme.primary, width: 1.4),
        ),
      ),
      cardTheme: CardThemeData(
        elevation: 0,
        color: surface,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(24),
          side: BorderSide(color: outline),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(0, 46),
          shape:
              RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: const Size(0, 44),
          shape:
              RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
          side: BorderSide(color: outline),
        ),
      ),
      chipTheme: ChipThemeData(
        backgroundColor: scheme.surfaceContainerHighest,
        selectedColor: scheme.primaryContainer,
        side: BorderSide(color: outline),
        labelStyle: TextStyle(
          color: scheme.onSurface,
          fontWeight: FontWeight.w600,
        ),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(999)),
      ),
      dividerTheme: DividerThemeData(
        color: outline,
        thickness: 1,
        space: 1,
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: snackBarBackground,
        contentTextStyle: TextStyle(color: snackBarForeground),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
      ),
    );
  }
}
