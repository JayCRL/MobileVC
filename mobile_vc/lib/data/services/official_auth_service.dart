import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';

class OfficialAuthResult {
  final String accessToken;
  final String refreshToken;
  final String userId;
  final String name;
  final String email;
  final String provider;
  final int expiresIn;

  OfficialAuthResult({
    required this.accessToken,
    required this.refreshToken,
    required this.userId,
    required this.name,
    required this.email,
    required this.provider,
    required this.expiresIn,
  });

  factory OfficialAuthResult.fromJson(Map<String, dynamic> json) {
    final user = json['user'] as Map<String, dynamic>? ?? {};
    return OfficialAuthResult(
      accessToken: json['accessToken'] ?? '',
      refreshToken: json['refreshToken'] ?? '',
      userId: user['id'] ?? '',
      name: user['name'] ?? '',
      email: user['email'] ?? '',
      provider: user['provider'] ?? '',
      expiresIn: json['expiresIn'] ?? 0,
    );
  }

  Map<String, String> toPrefs() => {
        'official_access_token': accessToken,
        'official_refresh_token': refreshToken,
        'official_user_id': userId,
        'official_name': name,
        'official_email': email,
        'official_provider': provider,
      };
}

class OfficialAuthService {
  static const _keys = {
    'accessToken': 'official_access_token',
    'refreshToken': 'official_refresh_token',
    'userId': 'official_user_id',
    'name': 'official_name',
    'email': 'official_email',
    'provider': 'official_provider',
    'serverUrl': 'official_server_url',
  };

  Future<OfficialAuthResult?> loadTokens() async {
    final prefs = await SharedPreferences.getInstance();
    final accessToken = prefs.getString(_keys['accessToken']!);
    if (accessToken == null || accessToken.isEmpty) return null;

    return OfficialAuthResult(
      accessToken: accessToken,
      refreshToken: prefs.getString(_keys['refreshToken']!) ?? '',
      userId: prefs.getString(_keys['userId']!) ?? '',
      name: prefs.getString(_keys['name']!) ?? '',
      email: prefs.getString(_keys['email']!) ?? '',
      provider: prefs.getString(_keys['provider']!) ?? '',
      expiresIn: 0,
    );
  }

  /// Try to refresh the access token using the refresh token.
  /// Returns new [OfficialAuthResult] on success, or null if refresh fails.
  Future<OfficialAuthResult?> tryRefresh(String serverUrl, OfficialAuthResult current) async {
    if (current.refreshToken.isEmpty) return null;
    try {
      final resp = await http.post(
        Uri.parse('$serverUrl/api/auth/refresh'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'refreshToken': current.refreshToken}),
      );
      if (resp.statusCode == 200) {
        final data = jsonDecode(resp.body) as Map<String, dynamic>;
        final newToken = data['accessToken'] as String? ?? '';
        final newRefresh = data['refreshToken'] as String? ?? '';
        if (newToken.isNotEmpty) {
          final result = OfficialAuthResult(
            accessToken: newToken,
            refreshToken: newRefresh.isNotEmpty ? newRefresh : current.refreshToken,
            userId: current.userId,
            name: current.name,
            email: current.email,
            provider: current.provider,
            expiresIn: data['expiresIn'] ?? 900,
          );
          await saveTokens(result);
          return result;
        }
      }
    } catch (_) {}
    return null;
  }

  Future<void> saveTokens(OfficialAuthResult result) async {
    final prefs = await SharedPreferences.getInstance();
    for (final e in result.toPrefs().entries) {
      await prefs.setString(e.key, e.value);
    }
  }

  Future<void> clearTokens() async {
    final prefs = await SharedPreferences.getInstance();
    for (final k in _keys.values) {
      await prefs.remove(k);
    }
  }

  bool get supportsPlatform => true;

  Future<void> launchOAuthLogin(String serverUrl, String provider) async {
    final url = '$serverUrl/api/auth/oauth/$provider';
    await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
  }
}
