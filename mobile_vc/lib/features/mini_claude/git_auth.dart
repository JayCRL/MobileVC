import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

class GitHubDeviceFlow {
  final String clientId;
  final String baseUrl;
  final http.Client _client = http.Client();

  GitHubDeviceFlow({
    required this.clientId,
    this.baseUrl = 'https://github.com',
  });

  /// Step 1: Request a device code
  Future<DeviceCodeResponse> requestCode({
    String scope = 'repo,workflow',
  }) async {
    final uri = Uri.parse('$baseUrl/login/device/code');
    final resp = await _client.post(uri, headers: {
      'Accept': 'application/json',
    }, body: {
      'client_id': clientId,
      'scope': scope,
    });

    if (resp.statusCode != 200) {
      final body = jsonDecode(resp.body);
      throw Exception(
          'Device code request failed: ${body['error_description'] ?? resp.body}');
    }

    final data = jsonDecode(resp.body) as Map<String, dynamic>;
    return DeviceCodeResponse(
      deviceCode: data['device_code'] as String,
      userCode: data['user_code'] as String,
      verificationUri: data['verification_uri'] as String,
      expiresIn: data['expires_in'] as int,
      interval: data['interval'] as int? ?? 5,
    );
  }

  /// Step 2: Poll for access token
  Future<String> pollForToken(DeviceCodeResponse code,
      {void Function(String status)? onStatus}) async {
    final uri = Uri.parse('$baseUrl/login/oauth/access_token');

    // GitHub rate-limits to once per `interval` seconds
    final interval = Duration(seconds: code.interval.clamp(5, 60));
    final deadline = DateTime.now().add(Duration(seconds: code.expiresIn));

    while (DateTime.now().isBefore(deadline)) {
      final resp = await _client.post(uri, headers: {
        'Accept': 'application/json',
      }, body: {
        'client_id': clientId,
        'device_code': code.deviceCode,
        'grant_type': 'urn:ietf:params:oauth:grant-type:device_code',
      });

      final data = jsonDecode(resp.body) as Map<String, dynamic>;

      if (resp.statusCode == 200 && data['access_token'] != null) {
        return data['access_token'] as String;
      }

      final error = data['error'] as String? ?? '';
      switch (error) {
        case 'authorization_pending':
          onStatus?.call('等待授权...');
          break;
        case 'slow_down':
          onStatus?.call('请稍候...');
          break;
        case 'expired_token':
          throw Exception('授权码已过期，请重新操作');
        case 'access_denied':
          throw Exception('用户取消了授权');
        default:
          throw Exception('登录失败: ${data['error_description'] ?? error}');
      }

      await Future.delayed(interval);
    }
    throw Exception('授权超时，请重新操作');
  }

  void close() => _client.close();
}

class DeviceCodeResponse {
  final String deviceCode;
  final String userCode;
  final String verificationUri;
  final int expiresIn;
  final int interval;

  const DeviceCodeResponse({
    required this.deviceCode,
    required this.userCode,
    required this.verificationUri,
    required this.expiresIn,
    required this.interval,
  });
}

class GitLabDeviceFlow {
  final String clientId;
  final String baseUrl;
  final http.Client _client = http.Client();

  GitLabDeviceFlow({
    required this.clientId,
    this.baseUrl = 'https://gitlab.com',
  });

  Future<DeviceCodeResponse> requestCode({
    String scope = 'api,read_user,write_repository',
  }) async {
    final uri = Uri.parse('$baseUrl/oauth/authorize_device');
    final resp = await _client.post(uri, headers: {
      'Accept': 'application/json',
    }, body: {
      'client_id': clientId,
      'scope': scope,
    });

    if (resp.statusCode != 200) {
      final body = jsonDecode(resp.body);
      throw Exception(
          'Device code request failed: ${body['error_description'] ?? resp.body}');
    }

    final data = jsonDecode(resp.body) as Map<String, dynamic>;
    return DeviceCodeResponse(
      deviceCode: data['device_code'] as String,
      userCode: data['user_code'] as String,
      verificationUri: data['verification_uri'] as String,
      expiresIn: data['expires_in'] as int? ?? 900,
      interval: data['interval'] as int? ?? 5,
    );
  }

  Future<String> pollForToken(DeviceCodeResponse code,
      {void Function(String)? onStatus}) async {
    final uri = Uri.parse('$baseUrl/oauth/token');
    final interval = Duration(seconds: code.interval.clamp(5, 60));
    final deadline = DateTime.now().add(Duration(seconds: code.expiresIn));

    while (DateTime.now().isBefore(deadline)) {
      final resp = await _client.post(uri, headers: {
        'Accept': 'application/json',
      }, body: {
        'client_id': clientId,
        'device_code': code.deviceCode,
        'grant_type': 'urn:ietf:params:oauth:grant-type:device_code',
      });

      final data = jsonDecode(resp.body) as Map<String, dynamic>;

      if (data['access_token'] != null) {
        return data['access_token'] as String;
      }

      final error = data['error'] as String? ?? '';
      if (error == 'authorization_pending') {
        onStatus?.call('等待授权...');
      } else if (error == 'slow_down') {
        onStatus?.call('请稍候...');
      } else if (error == 'expired_token') {
        throw Exception('授权码已过期，请重新操作');
      } else if (error == 'access_denied') {
        throw Exception('用户取消了授权');
      }

      await Future.delayed(interval);
    }
    throw Exception('授权超时，请重新操作');
  }

  void close() => _client.close();
}

class GiteeDeviceFlow {
  final String clientId;
  final String baseUrl;
  final http.Client _client = http.Client();

  GiteeDeviceFlow({
    required this.clientId,
    this.baseUrl = 'https://gitee.com',
  });

  Future<DeviceCodeResponse> requestCode({
    String scope = 'user_info,projects,pull_requests,emails',
  }) async {
    final uri = Uri.parse('$baseUrl/oauth/authorize_device');
    final resp = await _client.post(uri, headers: {
      'Accept': 'application/json',
    }, body: {
      'client_id': clientId,
      'scope': scope,
    });

    if (resp.statusCode != 200) {
      throw Exception('Device code request failed: ${resp.statusCode}');
    }

    final data = jsonDecode(resp.body) as Map<String, dynamic>;
    return DeviceCodeResponse(
      deviceCode: data['device_code'] as String,
      userCode: data['user_code'] as String,
      verificationUri: data['verification_uri'] as String,
      expiresIn: data['expires_in'] as int? ?? 900,
      interval: data['interval'] as int? ?? 5,
    );
  }

  Future<String> pollForToken(DeviceCodeResponse code,
      {void Function(String)? onStatus}) async {
    final uri = Uri.parse('$baseUrl/oauth/token');
    final interval = Duration(seconds: code.interval.clamp(5, 60));
    final deadline = DateTime.now().add(Duration(seconds: code.expiresIn));

    while (DateTime.now().isBefore(deadline)) {
      final resp = await _client.post(uri, headers: {
        'Accept': 'application/json',
      }, body: {
        'client_id': clientId,
        'device_code': code.deviceCode,
        'grant_type': 'urn:ietf:params:oauth:grant-type:device_code',
      });

      if (resp.statusCode == 200) {
        final data = jsonDecode(resp.body) as Map<String, dynamic>;
        if (data['access_token'] != null) {
          return data['access_token'] as String;
        }
      }

      final data = jsonDecode(resp.body) as Map<String, dynamic>;
      final error = data['error'] as String? ?? '';
      if (error == 'authorization_pending') {
        onStatus?.call('等待授权...');
      } else if (error != '') {
        throw Exception('登录失败: $error');
      }

      await Future.delayed(interval);
    }
    throw Exception('授权超时，请重新操作');
  }

  void close() => _client.close();
}
