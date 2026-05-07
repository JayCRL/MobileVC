import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'git_auth.dart';
import 'ish_bridge.dart';
import 'mini_runner.dart';

enum MiniClaudeStatus { idle, running, error }

class GitCredential {
  final String host;
  final String token;
  final String description;

  const GitCredential({
    required this.host,
    required this.token,
    this.description = '',
  });

  Map<String, dynamic> toJson() => {
        'host': host,
        'token': token,
        'description': description,
      };

  factory GitCredential.fromJson(Map<String, dynamic> json) => GitCredential(
        host: (json['host'] ?? '').toString(),
        token: (json['token'] ?? '').toString(),
        description: (json['description'] ?? '').toString(),
      );
}

class _GitCredential {
  final String host;
  final String token;
  final String description;

  const _GitCredential({
    required this.host,
    required this.token,
    this.description = '',
  });
}

class Workspace {
  final String id;
  final String name;
  final String path;

  const Workspace({required this.id, required this.name, required this.path});

  Map<String, dynamic> toJson() => {'id': id, 'name': name, 'path': path};

  factory Workspace.fromJson(Map<String, dynamic> json) => Workspace(
        id: (json['id'] ?? '').toString(),
        name: (json['name'] ?? '').toString(),
        path: (json['path'] ?? '').toString(),
      );
}

class MiniClaudeMessage {
  final String role;
  final String text;
  final DateTime timestamp;
  final List<MiniClaudeToolTrace>? toolTraces;

  const MiniClaudeMessage({
    required this.role,
    required this.text,
    required this.timestamp,
    this.toolTraces,
  });
}

class MiniClaudeToolTrace {
  final String toolName;
  final String input;
  final String output;
  final bool isError;

  const MiniClaudeToolTrace({
    required this.toolName,
    required this.input,
    required this.output,
    required this.isError,
  });
}

class MiniClaudeController extends ChangeNotifier {
  static const _apiKeyPrefsKey = 'mini_claude.api_key';
  static const _baseUrlPrefsKey = 'mini_claude.base_url';
  static const _modelPrefsKey = 'mini_claude.model';
  static const _workspacesPrefsKey = 'mini_claude.workspaces';
  static const _activeWorkspaceIdKey = 'mini_claude.active_workspace_id';
  static const _gitNamePrefsKey = 'mini_claude.git_name';
  static const _gitEmailPrefsKey = 'mini_claude.git_email';
  static const _gitCredentialsPrefsKey = 'mini_claude.git_credentials';
  static const _gitHubClientIdKey = 'mini_claude.github_client_id';

  // Register OAuth App at:
  //   GitHub: https://github.com/settings/developers
  //   Gitee:  https://gitee.com/oauth_applications
  // No callback URL needed for Device Flow.

  static const defaultGitHubClientId = 'Ov23ctAB9hQuk7Z7NOdk';

  String _gitHubClientId = '';
  String get gitHubClientId => _gitHubClientId.isNotEmpty ? _gitHubClientId : defaultGitHubClientId;

  MiniRunner? _runner;
  MiniClaudeStatus _status = MiniClaudeStatus.idle;
  final List<MiniClaudeMessage> _messages = [];
  String _apiKey = '';
  String _baseUrl = 'https://api.anthropic.com';
  String _model = 'claude-sonnet-4-6';
  String _errorMessage = '';
  final List<Workspace> _workspaces = [];
  Workspace? _activeWorkspace;
  String _gitName = '';
  String _gitEmail = '';
  final List<_GitCredential> _gitCredentials = [];

  MiniClaudeStatus get status => _status;
  List<MiniClaudeMessage> get messages => List.unmodifiable(_messages);
  String get apiKey => _apiKey;
  String get baseUrl => _baseUrl;
  String get model => _model;
  String get errorMessage => _errorMessage;
  bool get isConfigured => _apiKey.isNotEmpty;
  List<Workspace> get workspaces => List.unmodifiable(_workspaces);
  Workspace? get activeWorkspace => _activeWorkspace;
  String get workingDir => _activeWorkspace?.path ?? '';
  String get workspaceName => _activeWorkspace?.name ?? '';
  bool get hasWorkspace => _activeWorkspace != null;
  String get gitName => _gitName;
  String get gitEmail => _gitEmail;
  List<_GitCredential> get gitCredentials => List.unmodifiable(_gitCredentials);

  Future<void> initialize() async {
    final prefs = await SharedPreferences.getInstance();
    _apiKey = prefs.getString(_apiKeyPrefsKey) ?? '';
    _baseUrl = prefs.getString(_baseUrlPrefsKey) ?? 'https://api.anthropic.com';
    _model = prefs.getString(_modelPrefsKey) ?? 'claude-sonnet-4-6';
    _gitName = prefs.getString(_gitNamePrefsKey) ?? '';
    _gitEmail = prefs.getString(_gitEmailPrefsKey) ?? '';
    _gitHubClientId = prefs.getString(_gitHubClientIdKey) ?? '';

    final rawCreds = prefs.getStringList(_gitCredentialsPrefsKey) ?? const <String>[];
    _gitCredentials.clear();
    for (final raw in rawCreds) {
      try {
        final json = jsonDecode(raw) as Map<String, dynamic>;
        _gitCredentials.add(_GitCredential(
          host: (json['host'] ?? '').toString(),
          token: (json['token'] ?? '').toString(),
          description: (json['description'] ?? '').toString(),
        ));
      } catch (_) {}
    }

    final rawWorkspaces =
        (prefs.getStringList(_workspacesPrefsKey) ?? const <String>[]);
    _workspaces.clear();
    for (final raw in rawWorkspaces) {
      try {
        final json = jsonDecode(raw) as Map<String, dynamic>;
        _workspaces.add(Workspace.fromJson(json));
      } catch (_) {}
    }

    final activeId = prefs.getString(_activeWorkspaceIdKey) ?? '';
    if (activeId.isNotEmpty) {
      _activeWorkspace = _workspaces.where((w) => w.id == activeId).firstOrNull;
    }
  }

  Future<void> _saveWorkspaces() async {
    final prefs = await SharedPreferences.getInstance();
    final rawList = _workspaces.map((w) => jsonEncode(w.toJson())).toList();
    await prefs.setStringList(_workspacesPrefsKey, rawList);
  }

  Future<void> _saveActiveWorkspace() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _activeWorkspaceIdKey,
      _activeWorkspace?.id ?? '',
    );
  }

  void _warmUpIsh() {
    IshBridge.ensureInitialized();
  }

  Future<void> addWorkspace({required String name, required String path}) async {
    _warmUpIsh();
    final id = DateTime.now().millisecondsSinceEpoch.toString();
    final ws = Workspace(id: id, name: name.trim(), path: path.trim());
    _workspaces.add(ws);
    _activeWorkspace = ws;
    await _saveWorkspaces();
    await _saveActiveWorkspace();
    _messages.clear();
    _runner?.reset();
    notifyListeners();
  }

  Future<void> removeWorkspace(String id) async {
    _workspaces.removeWhere((w) => w.id == id);
    if (_activeWorkspace?.id == id) {
      _activeWorkspace = _workspaces.isNotEmpty ? _workspaces.first : null;
      _messages.clear();
      _runner?.reset();
      await _saveActiveWorkspace();
    }
    await _saveWorkspaces();
    notifyListeners();
  }

  Future<void> switchWorkspace(String id) async {
    final ws = _workspaces.where((w) => w.id == id).firstOrNull;
    if (ws == null) return;
    _activeWorkspace = ws;
    _messages.clear();
    _runner?.reset();
    await _saveActiveWorkspace();
    _warmUpIsh();
    notifyListeners();
  }

  Future<void> setApiKey(String key) async {
    _apiKey = key.trim();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_apiKeyPrefsKey, _apiKey);
    notifyListeners();
  }

  Future<void> setBaseUrl(String url) async {
    _baseUrl = url.trim();
    if (_baseUrl.endsWith('/')) {
      _baseUrl = _baseUrl.substring(0, _baseUrl.length - 1);
    }
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_baseUrlPrefsKey, _baseUrl);
    notifyListeners();
  }

  Future<void> setGitUser(String name, String email) async {
    _gitName = name.trim();
    _gitEmail = email.trim();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_gitNamePrefsKey, _gitName);
    await prefs.setString(_gitEmailPrefsKey, _gitEmail);
    notifyListeners();
  }

  Future<void> addGitCredential(
      {required String host, required String token, String description = ''}) async {
    _gitCredentials.add(_GitCredential(
      host: host.trim(),
      token: token.trim(),
      description: description.trim(),
    ));
    await _saveGitCredentials();
    notifyListeners();
  }

  Future<void> removeGitCredential(int index) async {
    if (index < _gitCredentials.length) {
      _gitCredentials.removeAt(index);
      await _saveGitCredentials();
      notifyListeners();
    }
  }

  Future<String> startDeviceFlow({
    required String platform, // 'github', 'gitlab', 'gitee'
    String? clientIdOverride,
    void Function(String status)? onStatus,
  }) async {
    final clientId = clientIdOverride ?? gitHubClientId;
    if (clientId.isEmpty) {
      throw Exception('请先在设置中配置 $platform OAuth App client_id\n'
          '在 ${_oauthRegisterUrl(platform)} 注册');
    }

    DeviceCodeResponse code;
    switch (platform) {
      case 'github':
        code = await GitHubDeviceFlow(clientId: clientId).requestCode();
        break;
      case 'gitlab':
        code = await GitLabDeviceFlow(clientId: clientId).requestCode();
        break;
      case 'gitee':
        code = await GiteeDeviceFlow(clientId: clientId).requestCode();
        break;
      default:
        throw Exception('Unsupported platform: $platform');
    }

    onStatus?.call('请在浏览器打开\n${code.verificationUri}\n输入代码 ${code.userCode}');

    String token;
    switch (platform) {
      case 'github':
        token = await GitHubDeviceFlow(clientId: clientId).pollForToken(code, onStatus: onStatus);
        break;
      case 'gitlab':
        token = await GitLabDeviceFlow(clientId: clientId).pollForToken(code, onStatus: onStatus);
        break;
      case 'gitee':
        token = await GiteeDeviceFlow(clientId: clientId).pollForToken(code, onStatus: onStatus);
        break;
      default:
        throw Exception('Unsupported platform: $platform');
    }

    final host = _platformHost(platform);
    await addGitCredential(host: host, token: token,
        description: '$platform Device Flow');
    return token;
  }

  String _oauthRegisterUrl(String platform) => switch (platform) {
    'github' => 'https://github.com/settings/developers',
    'gitlab' => 'https://gitlab.com/-/user_settings/applications',
    'gitee' => 'https://gitee.com/oauth_applications',
    _ => '',
  };

  String _platformHost(String platform) => switch (platform) {
    'github' => 'github.com',
    'gitlab' => 'gitlab.com',
    'gitee' => 'gitee.com',
    _ => platform,
  };

  String? getGitToken(String host) {
    for (final c in _gitCredentials) {
      if (host.contains(c.host) || c.host.contains(host)) return c.token;
    }
    return null;
  }

  Future<void> setGitHubClientId(String id) async {
    _gitHubClientId = id.trim();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_gitHubClientIdKey, _gitHubClientId);
    notifyListeners();
  }

  Future<void> _saveGitCredentials() async {
    final prefs = await SharedPreferences.getInstance();
    final rawList = _gitCredentials.map((c) => jsonEncode({
          'host': c.host,
          'token': c.token,
          'description': c.description,
        })).toList();
    await prefs.setStringList(_gitCredentialsPrefsKey, rawList);
  }

  Future<void> setModel(String model) async {
    _model = model.trim();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_modelPrefsKey, _model);
    notifyListeners();
  }

  Future<void> sendMessage(String text) async {
    if (_apiKey.isEmpty) {
      _errorMessage = '请先设置 API Key';
      _status = MiniClaudeStatus.error;
      notifyListeners();
      return;
    }
    if (_activeWorkspace == null) {
      _errorMessage = '请先添加工作区';
      _status = MiniClaudeStatus.error;
      notifyListeners();
      return;
    }

    _runner?.dispose();
    _runner = MiniRunner(
      apiKey: _apiKey,
      baseUrl: _baseUrl,
      workingDir: _activeWorkspace!.path,
      model: _model,
      gitName: _gitName.isNotEmpty ? _gitName : 'mini-claude',
      gitEmail: _gitEmail.isNotEmpty ? _gitEmail : 'mini@claude',
      gitCredentials: {
          for (final c in _gitCredentials) c.host: c.token,
        },
    );

    _messages.add(MiniClaudeMessage(
      role: 'user',
      text: text,
      timestamp: DateTime.now(),
    ));

    _status = MiniClaudeStatus.running;
    _errorMessage = '';
    notifyListeners();

    final assistantText = StringBuffer();
    List<MiniClaudeToolTrace>? pendingTraces;

    await _runner!.run(text, (event) {
      switch (event) {
        case MiniRunnerTextEvent(:final text):
          assistantText.write(text);
        case MiniRunnerToolCallEvent(:final toolName, :final input):
          pendingTraces ??= [];
          pendingTraces!.add(MiniClaudeToolTrace(
            toolName: toolName,
            input: _formatInput(input),
            output: '',
            isError: false,
          ));
          notifyListeners();
        case MiniRunnerToolResultEvent(
              :final toolName,
              :final content,
              :final isError
            ):
          if (pendingTraces?.isNotEmpty == true) {
            for (var i = pendingTraces!.length - 1; i >= 0; i--) {
              if (pendingTraces![i].toolName == toolName &&
                  pendingTraces![i].output.isEmpty) {
                pendingTraces![i] = MiniClaudeToolTrace(
                  toolName: toolName,
                  input: pendingTraces![i].input,
                  output: content.length > 500
                      ? '${content.substring(0, 500)}...'
                      : content,
                  isError: isError,
                );
                break;
              }
            }
          }
          notifyListeners();
        case MiniRunnerDoneEvent():
          break;
        case MiniRunnerErrorEvent(:final message):
          _errorMessage = message;
          assistantText.write('\n\n[错误: $message]');
      }
    });

    _messages.add(MiniClaudeMessage(
      role: 'assistant',
      text: assistantText.toString(),
      timestamp: DateTime.now(),
      toolTraces: pendingTraces,
    ));

    _status = MiniClaudeStatus.idle;
    notifyListeners();
  }

  void clearMessages() {
    _messages.clear();
    _runner?.reset();
    _status = MiniClaudeStatus.idle;
    _errorMessage = '';
    notifyListeners();
  }

  String _formatInput(Map<String, dynamic> input) {
    final entries =
        input.entries.map((e) => '${e.key}=${e.value}').join(', ');
    return entries.length > 200 ? '${entries.substring(0, 200)}...' : entries;
  }

  @override
  void dispose() {
    _runner?.dispose();
    super.dispose();
  }
}
