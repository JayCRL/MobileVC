import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../../core/oauth_deeplink.dart';
import '../../data/services/official_auth_service.dart';
import '../../data/services/official_api_service.dart';
import '../mini_claude/mini_claude_page.dart';
import '../planning/mode_choice_page.dart';
import 'session_controller.dart';

class ModeSelectPage extends StatefulWidget {
  const ModeSelectPage({
    super.key,
    required this.controller,
    required this.darkModeEnabled,
    required this.onToggleTheme,
  });

  final SessionController controller;
  final bool darkModeEnabled;
  final VoidCallback onToggleTheme;

  @override
  State<ModeSelectPage> createState() => _ModeSelectPageState();
}

class _ModeSelectPageState extends State<ModeSelectPage> with WidgetsBindingObserver {
  final _authService = OfficialAuthService();
  final _serverUrlCtrl = TextEditingController(text: 'https://mobilevc.top');
  OfficialAuthResult? _authResult;
  bool _loading = false;
  StreamSubscription<String>? _deeplinkSub;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _loadAuth();
    _checkInitialUri();
    _deeplinkSub = OAuthDeepLink.onUri.listen(_handleDeepLink);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _checkInitialUri();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _deeplinkSub?.cancel();
    _serverUrlCtrl.dispose();
    super.dispose();
  }

  Future<void> _checkInitialUri() async {
    final uri = await OAuthDeepLink.getInitialUri();
    if (uri != null && uri.isNotEmpty) {
      _handleDeepLink(uri);
    }
  }

  void _handleDeepLink(String uri) {
    if (!uri.startsWith('mobilevc://oauth-callback')) return;
    final queryStart = uri.indexOf('?');
    if (queryStart < 0) return;
    final query = uri.substring(queryStart + 1);
    final params = Uri.splitQueryString(query);

    final accessToken = params['access_token'];
    final refreshToken = params['refresh_token'];
    if (accessToken == null || accessToken.isEmpty) return;

    final result = OfficialAuthResult(
      accessToken: accessToken,
      refreshToken: refreshToken ?? '',
      userId: '',
      name: '已登录用户',
      email: '',
      provider: 'github',
      expiresIn: 900,
    );
    _authService.saveTokens(result);
    setState(() => _authResult = result);

    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('登录成功！点击"官方服务器"查看节点')),
    );
  }

  Future<void> _loadAuth() async {
    final result = await _authService.loadTokens();
    if (mounted) setState(() => _authResult = result);
  }

  Future<void> _login(String provider) async {
    final serverUrl = _serverUrlCtrl.text.trim();
    if (serverUrl.isEmpty) return;
    await _authService.launchOAuthLogin(serverUrl, provider);
  }

  Future<void> _emailAuth({
    required String serverUrl,
    required String email,
    required String password,
    String? name,
    required bool isRegister,
    required VoidCallback onSuccess,
  }) async {
    final url = isRegister
        ? '$serverUrl/api/auth/register'
        : '$serverUrl/api/auth/login';

    final body = isRegister
        ? {'email': email, 'password': password, 'name': name ?? ''}
        : {'email': email, 'password': password};

    try {
      final resp = await http.post(
        Uri.parse(url),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode(body),
      );
      if (resp.statusCode == 200) {
        final data = jsonDecode(resp.body) as Map<String, dynamic>;
        final result = OfficialAuthResult.fromJson(data);
        await _authService.saveTokens(result);
        if (mounted) setState(() => _authResult = result);
        onSuccess();
      } else {
        final data = jsonDecode(resp.body) as Map<String, dynamic>;
        final error = data['error'] ?? '请求失败 (${resp.statusCode})';
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(error.toString())),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('网络错误: $e')),
        );
      }
    }
  }

  Future<void> _openNodeSelector() async {
    if (_authResult == null) return;
    final serverUrl = _serverUrlCtrl.text.trim();

    // Auto-refresh token if expired
    final refreshed = await _authService.tryRefresh(serverUrl, _authResult!);
    if (refreshed != null) {
      _authResult = refreshed;
    }

    setState(() => _loading = true);
    try {
      final api = OfficialApiService(
        baseUrl: serverUrl,
        accessToken: _authResult!.accessToken,
      );
      final nodes = await api.listNodes();

      if (!mounted) return;
      final selected = await showModalBottomSheet<String>(
        context: context,
        builder: (_) => _NodeSelectorSheet(
          nodes: nodes,
          serverUrl: serverUrl,
          authResult: _authResult!,
          onLogout: () {
            _authService.clearTokens();
            setState(() => _authResult = null);
          },
        ),
      );

      if (selected != null) {
        // Connect via official server P2P
        widget.controller.saveConfig(widget.controller.config.copyWith(
          connectionMode: 'official',
          officialServerUrl: serverUrl,
          officialAccessToken: _authResult!.accessToken,
          officialRefreshToken: _authResult!.refreshToken,
          officialUserId: _authResult!.userId,
          officialNodeId: selected,
        ));

        if (mounted) {
          Navigator.of(context).pushReplacement(
            MaterialPageRoute(
              builder: (_) => ModeChoicePage(
                controller: widget.controller,
                darkModeEnabled: widget.darkModeEnabled,
                onToggleTheme: widget.onToggleTheme,
              ),
            ),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('获取节点列表失败: $e')),
        );
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _showLanConfigDialog() async {
    final config = widget.controller.config;
    final hostCtrl = TextEditingController(text: config.host);
    final portCtrl = TextEditingController(text: config.port);
    final tokenCtrl = TextEditingController(text: config.token);

    final result = await showDialog<bool>(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('局域网配置'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: hostCtrl,
                decoration: const InputDecoration(
                  labelText: 'IP 地址',
                  hintText: '192.168.1.x',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: portCtrl,
                decoration: const InputDecoration(
                  labelText: '端口',
                  hintText: '19080',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 12),
              TextField(
                controller: tokenCtrl,
                decoration: const InputDecoration(
                  labelText: 'Token',
                  hintText: 'test',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('连接'),
          ),
        ],
      ),
    );

    if (result == true) {
      widget.controller.saveConfig(config.copyWith(
        host: hostCtrl.text.trim(),
        port: portCtrl.text.trim(),
        token: tokenCtrl.text.trim(),
      ));
      if (mounted) {
        Navigator.of(context).pushReplacement(
          MaterialPageRoute(
            builder: (_) => ModeChoicePage(
              controller: widget.controller,
              darkModeEnabled: widget.darkModeEnabled,
              onToggleTheme: widget.onToggleTheme,
            ),
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final loggedIn = _authResult != null;

    return Scaffold(
      appBar: AppBar(
        title: const Text('MobileVC'),
        actions: [
          IconButton(
            onPressed: widget.onToggleTheme,
            tooltip: widget.darkModeEnabled ? '切换浅色模式' : '切换深色模式',
            icon: Icon(
              widget.darkModeEnabled
                  ? Icons.light_mode_outlined
                  : Icons.dark_mode_outlined,
            ),
          ),
        ],
      ),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const SizedBox(height: 20),
              Icon(Icons.phone_iphone_outlined, size: 64,
                  color: theme.colorScheme.primary),
              const SizedBox(height: 12),
              Text('选择模式',
                  style: theme.textTheme.headlineSmall
                      ?.copyWith(fontWeight: FontWeight.w700)),
              const SizedBox(height: 32),

              // 本地模式
              _ModeCard(
                icon: Icons.language_outlined,
                title: '局域网模式',
                subtitle: '配置局域网 IP 直连电脑后端\n完整 Claude Code 功能',
                color: theme.colorScheme.primary,
                onTap: _showLanConfigDialog,
              ),
              const SizedBox(height: 16),

              // 官方服务器
              _ModeCard(
                icon: Icons.cloud_outlined,
                title: '官方服务器',
                subtitle: loggedIn
                    ? '已登录: ${_authResult!.name}\n查看节点列表并连接'
                    : '公网连接，GitHub 登录\n随时随地访问你的电脑',
                color: const Color(0xFF8B5CF6),
                trailing: loggedIn
                    ? (_loading
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2))
                        : const Icon(Icons.arrow_forward_ios, size: 16))
                    : null,
                onTap: loggedIn ? _openNodeSelector : () => _showLoginSheet(context),
              ),
              const SizedBox(height: 16),

              // 脱机模式
              _ModeCard(
                icon: Icons.smartphone_outlined,
                title: '脱机模式',
                subtitle: '手机独立运行 Mini Claude\n编辑代码 + Git + Linux 终端',
                color: theme.colorScheme.tertiary,
                onTap: () => Navigator.of(context).push(
                  MaterialPageRoute(builder: (_) => const MiniClaudePage()),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _showLoginSheet(BuildContext context) {
    bool isRegister = false;
    bool emailLoading = false;
    final emailCtrl = TextEditingController();
    final passwordCtrl = TextEditingController();
    final nameCtrl = TextEditingController();

    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
      ),
      isScrollControlled: true,
      builder: (_) => StatefulBuilder(
        builder: (ctx, setSheetState) => Padding(
          padding: EdgeInsets.only(
            left: 24,
            right: 24,
            top: 24,
            bottom: MediaQuery.of(ctx).viewInsets.bottom + 24,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('官方服务器地址',
                  style: Theme.of(ctx).textTheme.titleMedium),
              const SizedBox(height: 8),
              TextField(
                controller: _serverUrlCtrl,
                decoration: const InputDecoration(
                  hintText: 'https://mobilevc.top',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 20),
              Text('社交登录',
                  style: Theme.of(ctx).textTheme.titleMedium),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                onPressed: () {
                  Navigator.pop(ctx);
                  _login('github');
                },
                icon: const Icon(Icons.code),
                label: const Text('GitHub 登录'),
              ),
              const SizedBox(height: 20),
              Row(
                children: [
                  const Expanded(child: Divider()),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                    child: Text('或使用邮箱',
                        style: Theme.of(ctx).textTheme.bodySmall),
                  ),
                  const Expanded(child: Divider()),
                ],
              ),
              const SizedBox(height: 12),
              TextField(
                controller: emailCtrl,
                decoration: const InputDecoration(
                  labelText: '邮箱',
                  hintText: 'user@example.com',
                  border: OutlineInputBorder(),
                ),
                keyboardType: TextInputType.emailAddress,
              ),
              const SizedBox(height: 12),
              TextField(
                controller: passwordCtrl,
                decoration: const InputDecoration(
                  labelText: '密码',
                  border: OutlineInputBorder(),
                ),
                obscureText: true,
              ),
              if (isRegister) ...[
                const SizedBox(height: 12),
                TextField(
                  controller: nameCtrl,
                  decoration: const InputDecoration(
                    labelText: '昵称（选填）',
                    border: OutlineInputBorder(),
                  ),
                ),
              ],
              const SizedBox(height: 16),
              Row(
                children: [
                  Expanded(
                    child: _ModeToggleButton(
                      label: '登录',
                      selected: !isRegister,
                      onTap: () => setSheetState(() => isRegister = false),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: _ModeToggleButton(
                      label: '注册',
                      selected: isRegister,
                      onTap: () => setSheetState(() => isRegister = true),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              SizedBox(
                height: 44,
                child: emailLoading
                    ? const Center(child: CircularProgressIndicator())
                    : FilledButton(
                        onPressed: () {
                          final serverUrl =
                              _serverUrlCtrl.text.trim();
                          final email =
                              emailCtrl.text.trim();
                          final password = passwordCtrl.text;
                          if (serverUrl.isEmpty ||
                              email.isEmpty ||
                              password.isEmpty) {
                            ScaffoldMessenger.of(ctx).showSnackBar(
                              const SnackBar(
                                  content: Text('请填写完整信息')),
                            );
                            return;
                          }
                          setSheetState(
                              () => emailLoading = true);
                          _emailAuth(
                            serverUrl: serverUrl,
                            email: email,
                            password: password,
                            name: nameCtrl.text.trim(),
                            isRegister: isRegister,
                            onSuccess: () {
                              Navigator.pop(ctx);
                              _openNodeSelector();
                            },
                          );
                        },
                        child:
                            Text(isRegister ? '注册并登录' : '登录'),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _NodeSelectorSheet extends StatelessWidget {
  final List<NodeInfo> nodes;
  final String serverUrl;
  final OfficialAuthResult authResult;
  final VoidCallback onLogout;

  const _NodeSelectorSheet({
    required this.nodes,
    required this.serverUrl,
    required this.authResult,
    required this.onLogout,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final onlineNodes = nodes.where((n) => n.status == 'online').toList();

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                CircleAvatar(
                  radius: 20,
                  backgroundColor: theme.colorScheme.primaryContainer,
                  child: Text(authResult.name.isNotEmpty
                      ? authResult.name[0].toUpperCase()
                      : 'U'),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(authResult.name,
                          style: theme.textTheme.titleMedium),
                      Text(authResult.email.isNotEmpty
                          ? authResult.email
                          : '已登录',
                          style: theme.textTheme.bodySmall),
                    ],
                  ),
                ),
                TextButton(onPressed: onLogout, child: const Text('退出')),
              ],
            ),
            const SizedBox(height: 20),
            Text('在线节点', style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            if (onlineNodes.isEmpty)
              const Padding(
                padding: EdgeInsets.all(16),
                child: Text('暂无在线节点', textAlign: TextAlign.center),
              )
            else
              ...onlineNodes.map((n) => ListTile(
                    leading: Icon(Icons.computer,
                        color: n.status == 'online'
                            ? Colors.green
                            : Colors.grey),
                    title: Text(n.name.isNotEmpty ? n.name : n.id.substring(0, 12)),
                    subtitle: Text('v${n.version}  ${n.lastSeen}'),
                    trailing: const Icon(Icons.arrow_forward_ios, size: 16),
                    onTap: () => Navigator.pop(context, n.id),
                  )),
          ],
        ),
      ),
    );
  }
}

class _ModeCard extends StatelessWidget {
  const _ModeCard({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.color,
    required this.onTap,
    this.trailing,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final Color color;
  final VoidCallback onTap;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SizedBox(
      width: double.infinity,
      child: Card(
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(24),
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Row(
              children: [
                Container(
                  width: 56,
                  height: 56,
                  decoration: BoxDecoration(
                    color: color.withAlpha(30),
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Icon(icon, color: color, size: 28),
                ),
                const SizedBox(width: 20),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(title,
                          style: theme.textTheme.titleMedium
                              ?.copyWith(fontWeight: FontWeight.w600)),
                      const SizedBox(height: 4),
                      Text(subtitle,
                          style: theme.textTheme.bodySmall
                              ?.copyWith(color: theme.colorScheme.outline)),
                    ],
                  ),
                ),
                if (trailing != null) trailing!,
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ModeToggleButton extends StatelessWidget {
  const _ModeToggleButton({
    required this.label,
    required this.selected,
    required this.onTap,
  });

  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 40,
      child: selected
          ? FilledButton(onPressed: onTap, child: Text(label))
          : OutlinedButton(onPressed: onTap, child: Text(label)),
    );
  }
}
