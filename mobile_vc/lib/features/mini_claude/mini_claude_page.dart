import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:url_launcher/url_launcher.dart';

import 'git.dart';
import 'git_remote.dart';
import 'mini_claude_controller.dart';

class MiniClaudePage extends StatefulWidget {
  const MiniClaudePage({super.key, MiniClaudeController? controller})
      : _controller = controller;

  final MiniClaudeController? _controller;

  @override
  State<MiniClaudePage> createState() => _MiniClaudePageState();
}

class _MiniClaudePageState extends State<MiniClaudePage> {
  final TextEditingController _inputController = TextEditingController();
  final FocusNode _inputFocus = FocusNode();
  final ScrollController _scrollController = ScrollController();

  late final MiniClaudeController _controller;

  @override
  void initState() {
    super.initState();
    _controller = widget._controller ?? MiniClaudeController();
    _controller.addListener(_onChanged);
    _controller.initialize();
  }

  @override
  void dispose() {
    _controller.removeListener(_onChanged);
    if (widget._controller == null) _controller.dispose();
    _inputController.dispose();
    _inputFocus.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  void _onChanged() {
    if (!mounted) return;
    setState(() {});
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 200),
          curve: Curves.easeOut,
        );
      }
    });
  }

  Future<void> _pickAndAddWorkspace() async {
    // On iOS, File Provider Storage restricts direct file access.
    // Guide users to use the Files app to copy projects into the app's folder.
    final docs = await getApplicationDocumentsDirectory();
    final workspacesDir = Directory('${docs.path}/workspaces');
    if (!workspacesDir.existsSync()) workspacesDir.createSync(recursive: true);

    // Look for existing projects in the app sandbox
    final existing = workspacesDir
        .listSync()
        .whereType<Directory>()
        .toList();

    if (existing.isNotEmpty) {
      // Let user pick from already-imported projects or add new
      final result = await showDialog<String>(
        context: context,
        builder: (ctx) => SimpleDialog(
          title: const Text('导入工作区'),
          children: [
            ...existing.map((d) => SimpleDialogOption(
                  onPressed: () => Navigator.pop(ctx, d.path),
                  child: Text(d.path.split('/').last),
                )),
            const Divider(),
            SimpleDialogOption(
              onPressed: () => Navigator.pop(ctx, '__new__'),
              child: const Text('+ 添加新项目...'),
            ),
          ],
        ),
      );

      if (result == '__new__') {
        _showImportGuide();
        return;
      }
      if (result != null) {
        final name = await _showAliasDialog(result.split('/').last);
        if (name != null && name.trim().isNotEmpty) {
          await _controller.addWorkspace(name: name, path: result);
        }
        return;
      }
    } else {
      _showImportGuide();
    }
  }

  void _showImportGuide() {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('导入项目到工作区'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('请通过 iOS 文件 App 操作：'),
            const SizedBox(height: 12),
            _guideStepText('1', '打开 iOS「文件」App → 浏览 → 我的 iPhone'),
            _guideStepText('2', '找到「Mobile Vc」文件夹'),
            _guideStepText('3', '把项目文件夹拖入 Mobile Vc/workspaces/'),
            _guideStepText('4', '回到这里，重新点「选择文件夹」'),
            const SizedBox(height: 8),
            Text(
              '项目会自动出现在工作区列表中。',
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(ctx).colorScheme.outline,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('知道了'),
          ),
        ],
      ),
    );
  }

  Widget _guideStepText(String num, String text) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('$num. ', style: const TextStyle(fontWeight: FontWeight.w600)),
          Expanded(child: Text(text)),
        ],
      ),
    );
  }

  Future<String?> _showAliasDialog(String defaultName) async {
    final ctrl = TextEditingController(text: defaultName);
    return showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('工作区别名'),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          decoration: const InputDecoration(
            hintText: '给这个工作区取个名字',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, ctrl.text.trim()),
            child: const Text('确定'),
          ),
        ],
      ),
    );
  }

  void _send() {
    final text = _inputController.text.trim();
    if (text.isEmpty || _controller.status == MiniClaudeStatus.running) return;
    _inputController.clear();
    _controller.sendMessage(text);
  }

  void _showSettings() {
    final apiKeyCtrl = TextEditingController(text: _controller.apiKey);
    final baseUrlCtrl = TextEditingController(text: _controller.baseUrl);
    final modelCtrl = TextEditingController(text: _controller.model);
    final gitNameCtrl = TextEditingController(text: _controller.gitName);
    final gitEmailCtrl = TextEditingController(text: _controller.gitEmail);

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (ctx) => SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 24, 24, 24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text('API 设置',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
              const SizedBox(height: 20),
              TextField(
                controller: apiKeyCtrl,
                decoration: const InputDecoration(
                  labelText: 'API Key',
                  hintText: 'sk-ant-...',
                ),
                obscureText: true,
              ),
              const SizedBox(height: 12),
              TextField(
                controller: baseUrlCtrl,
                decoration: const InputDecoration(
                  labelText: 'API Base URL',
                  hintText: 'https://api.anthropic.com',
                ),
                keyboardType: TextInputType.url,
              ),
              const SizedBox(height: 12),
              TextField(
                controller: modelCtrl,
                decoration: const InputDecoration(
                  labelText: 'Model',
                  hintText: 'claude-sonnet-4-6',
                ),
              ),
              const Divider(height: 36),
              const Text('Git 提交信息',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
              const SizedBox(height: 4),
              Text('git commit 时使用的用户名和邮箱',
                  style: TextStyle(fontSize: 12, color: Theme.of(context).colorScheme.outline)),
              const SizedBox(height: 16),
              TextField(
                controller: gitNameCtrl,
                decoration: const InputDecoration(
                  labelText: 'Git 用户名',
                  hintText: 'Your Name',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: gitEmailCtrl,
                decoration: const InputDecoration(
                  labelText: 'Git 邮箱',
                  hintText: 'name@example.com',
                ),
                keyboardType: TextInputType.emailAddress,
              ),
              const Divider(height: 36),
              Row(
                children: [
                  Expanded(
                    child: Text('Git 凭证 (Token)',
                        style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
                  ),
                  IconButton(
                    icon: const Icon(Icons.add),
                    onPressed: () => _addCredentialDialog(ctx),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Text('用于 clone/fetch/push 时的鉴权，如 GitHub Personal Access Token',
                  style: TextStyle(fontSize: 12, color: Theme.of(context).colorScheme.outline)),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                onPressed: () => _startDeviceAuth(ctx, 'github'),
                icon: const Icon(Icons.login, size: 16),
                label: const Text('一键登录 GitHub'),
              ),
              const SizedBox(height: 8),
              _buildCredentialList(ctx),
              const SizedBox(height: 20),
              FilledButton(
                onPressed: () {
                  _controller.setApiKey(apiKeyCtrl.text);
                  _controller.setBaseUrl(baseUrlCtrl.text);
                  _controller.setModel(modelCtrl.text);
                  _controller.setGitUser(gitNameCtrl.text, gitEmailCtrl.text);
                  Navigator.pop(ctx);
                },
                child: const Text('保存'),
              ),
              const SizedBox(height: 12),
            ],
          ),
        ),
      ),
    );
  }

  String _deviceAuthVerificationUri = '';

  void _startDeviceAuth(BuildContext ctx, String platform) async {
    final controller = _controller;
    if (controller.gitHubClientId.isEmpty) {
      ScaffoldMessenger.of(ctx).showSnackBar(
        const SnackBar(content: Text('请先填写 OAuth Client ID')),
      );
      return;
    }

    _deviceAuthVerificationUri = '';

    showDialog(
      context: ctx,
      barrierDismissible: false,
      builder: (dCtx) {
        String status = '正在连接 ${platform}...';
        String userCode = '';
        return StatefulBuilder(
          builder: (sCtx, setDialogState) {
            if (status.startsWith('正在连接')) {
              controller.startDeviceFlow(
                platform: platform,
                onStatus: (s) {
                  setDialogState(() {
                    status = s;
                    // Parse out verification URI and user code
                    if (s.contains('http')) {
                      final lines = s.split('\n');
                      for (final line in lines) {
                        final trimmed = line.trim();
                        if (trimmed.startsWith('http')) {
                          _deviceAuthVerificationUri = trimmed;
                        }
                      }
                      // Auto-open browser
                      if (_deviceAuthVerificationUri.isNotEmpty) {
                        launchUrl(Uri.parse(_deviceAuthVerificationUri));
                      }
                    }
                    final codeMatch = RegExp(r'输入代码\s+([A-Z0-9-]+)').firstMatch(s);
                    if (codeMatch != null) {
                      userCode = codeMatch.group(1)!;
                    }
                  });
                },
              ).then((token) {
                Navigator.pop(dCtx);
                ScaffoldMessenger.of(ctx).showSnackBar(
                  SnackBar(content: Text('$platform 登录成功！token 已自动保存')),
                );
                setState(() {});
              }).catchError((e) {
                Navigator.pop(dCtx);
                ScaffoldMessenger.of(ctx).showSnackBar(
                  SnackBar(content: Text('登录失败: $e')),
                );
              });
            }
            return AlertDialog(
              title: Text('登录 $platform'),
              content: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.center,
                children: [
                  if (userCode.isNotEmpty) ...[
                    Icon(Icons.login, size: 48,
                        color: Theme.of(dCtx).colorScheme.primary),
                    const SizedBox(height: 16),
                    SelectableText(
                      '验证码: $userCode',
                      textAlign: TextAlign.center,
                      style: Theme.of(dCtx).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '浏览器应该已自动打开。\n如未打开，点击下方按钮。',
                      textAlign: TextAlign.center,
                      style: Theme.of(dCtx).textTheme.bodySmall,
                    ),
                    const SizedBox(height: 12),
                    if (_deviceAuthVerificationUri.isNotEmpty)
                      FilledButton.icon(
                        icon: const Icon(Icons.open_in_browser, size: 16),
                        label: const Text('打开授权页面'),
                        onPressed: () =>
                            launchUrl(Uri.parse(_deviceAuthVerificationUri)),
                      ),
                    const SizedBox(height: 12),
                    Text(status.contains('等待') ? '等待你在浏览器中确认...' : status,
                        style: Theme.of(dCtx).textTheme.bodySmall),
                  ] else
                    ...[
                      const CircularProgressIndicator(),
                      const SizedBox(height: 16),
                      Text(status),
                    ],
                ],
              ),
            );
          },
        );
      },
    );
  }

  void _addCredentialDialog(BuildContext ctx) {
    final hostCtrl = TextEditingController();
    final tokenCtrl = TextEditingController();
    showDialog(
      context: ctx,
      builder: (dCtx) => AlertDialog(
        title: const Text('添加 Git 凭证'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: hostCtrl,
              decoration: const InputDecoration(
                labelText: '主机地址',
                hintText: 'github.com',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: tokenCtrl,
              decoration: const InputDecoration(
                labelText: 'Token',
                hintText: 'ghp_...',
              ),
              obscureText: true,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dCtx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              final host = hostCtrl.text.trim();
              final token = tokenCtrl.text.trim();
              if (host.isNotEmpty && token.isNotEmpty) {
                _controller.addGitCredential(host: host, token: token);
              }
              Navigator.pop(dCtx);
            },
            child: const Text('添加'),
          ),
        ],
      ),
    );
  }

  Widget _buildCredentialList(BuildContext ctx) {
    final creds = _controller.gitCredentials;
    if (creds.isEmpty) {
      return Text('暂无凭证',
          style: TextStyle(color: Theme.of(ctx).colorScheme.outline));
    }
    return Column(
      children: List.generate(creds.length, (i) {
        final c = creds[i];
        return Container(
          margin: const EdgeInsets.only(bottom: 6),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          decoration: BoxDecoration(
            color: Theme.of(ctx).colorScheme.surface,
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: Theme.of(ctx).dividerColor),
          ),
          child: Row(
            children: [
              const Icon(Icons.vpn_key_outlined, size: 16),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '${c.host}  ***${c.token.length > 4 ? c.token.substring(c.token.length - 4) : ''}',
                  style: Theme.of(ctx).textTheme.bodySmall,
                ),
              ),
              IconButton(
                icon: const Icon(Icons.close, size: 16),
                onPressed: () {
                  _controller.removeGitCredential(i);
                  Navigator.pop(ctx);
                  _showSettings();
                },
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(),
              ),
            ],
          ),
        );
      }),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    // No workspace → show setup
    if (!_controller.hasWorkspace) {
      return Scaffold(
        appBar: AppBar(
          title: const Text('Mini Claude'),
          actions: [
            if (!_controller.isConfigured)
              IconButton(
                icon: const Icon(Icons.settings_outlined),
                onPressed: _showSettings,
              ),
          ],
        ),
        body: _buildSetupScreen(theme),
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: InkWell(
          onTap: () => _showWorkspaceSheet(context),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.folder_outlined, size: 18),
              const SizedBox(width: 6),
              Flexible(
                child: Text(
                  _controller.workspaceName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const SizedBox(width: 4),
              const Icon(Icons.arrow_drop_down, size: 18),
            ],
          ),
        ),
        actions: [
          if (_controller.status == MiniClaudeStatus.running)
            const Padding(
              padding: EdgeInsets.only(right: 8),
              child: SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            ),
          IconButton(
            icon: const Icon(Icons.settings_outlined),
            onPressed: _showSettings,
          ),
          IconButton(
            icon: const Icon(Icons.delete_outline),
            onPressed: () => _controller.clearMessages(),
          ),
        ],
      ),
      body: Column(
        children: [
          if (!_controller.isConfigured)
            Container(
              width: double.infinity,
              color: theme.colorScheme.errorContainer,
              padding:
                  const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
              child: Text(
                '请点击右上角设置，填入 API Key',
                style: TextStyle(color: theme.colorScheme.onErrorContainer),
              ),
            ),
          if (_controller.errorMessage.isNotEmpty)
            Container(
              width: double.infinity,
              color: theme.colorScheme.errorContainer,
              padding:
                  const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
              child: Text(
                _controller.errorMessage,
                style: TextStyle(color: theme.colorScheme.onErrorContainer),
              ),
            ),
          // Streaming status bar
          if (_controller.status == MiniClaudeStatus.running)
            Container(
              width: double.infinity,
              padding:
                  const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
              color: theme.colorScheme.primaryContainer.withAlpha(80),
              child: Row(
                children: [
                  SizedBox(
                    width: 12,
                    height: 12,
                    child: CircularProgressIndicator(
                      strokeWidth: 1.5,
                      color: theme.colorScheme.primary,
                    ),
                  ),
                  const SizedBox(width: 8),
                  Text(
                    '${_controller.elapsedDisplay}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      fontWeight: FontWeight.w600,
                      color: theme.colorScheme.primary,
                    ),
                  ),
                  const Spacer(),
                  Text(
                    '↑${_formatTokens(_controller.currentInputTokens)} '
                    '↓${_formatTokens(_controller.currentOutputTokens)}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.outline,
                    ),
                  ),
                ],
              ),
            ),
          Expanded(
            child: _controller.messages.isEmpty
                ? _buildEmptyState(theme)
                : _buildMessageList(theme),
          ),
          _buildInputBar(theme),
        ],
      ),
    );
  }

  Widget _buildSetupScreen(ThemeData theme) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.folder_open, size: 64, color: theme.colorScheme.outline),
            const SizedBox(height: 16),
            Text('还没有工作区',
                style: theme.textTheme.titleLarge
                    ?.copyWith(fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            Text('选择导入方式',
                style: theme.textTheme.bodyMedium
                    ?.copyWith(color: theme.colorScheme.outline)),
            const SizedBox(height: 24),

            // Option 1: Git clone
            Card(
              child: InkWell(
                onTap: () => _cloneFromGit(),
                borderRadius: BorderRadius.circular(24),
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Row(
                    children: [
                      Container(
                        width: 48, height: 48,
                        decoration: BoxDecoration(
                          color: theme.colorScheme.primary.withAlpha(30),
                          borderRadius: BorderRadius.circular(14),
                        ),
                        child: Icon(Icons.cloud_download_outlined,
                            color: theme.colorScheme.primary),
                      ),
                      const SizedBox(width: 16),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('从 Git 克隆',
                                style: theme.textTheme.titleSmall
                                    ?.copyWith(fontWeight: FontWeight.w600)),
                            Text('输入仓库 URL，自动 clone 到工作区',
                                style: theme.textTheme.bodySmall
                                    ?.copyWith(color: theme.colorScheme.outline)),
                          ],
                        ),
                      ),
                      Icon(Icons.arrow_forward_ios, size: 16,
                          color: theme.colorScheme.outline),
                    ],
                  ),
                ),
              ),
            ),
            const SizedBox(height: 12),

            // Option 2: Local files
            Card(
              child: InkWell(
                onTap: _pickAndAddWorkspace,
                borderRadius: BorderRadius.circular(24),
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Row(
                    children: [
                      Container(
                        width: 48, height: 48,
                        decoration: BoxDecoration(
                          color: theme.colorScheme.tertiary.withAlpha(30),
                          borderRadius: BorderRadius.circular(14),
                        ),
                        child: Icon(Icons.folder_outlined,
                            color: theme.colorScheme.tertiary),
                      ),
                      const SizedBox(width: 16),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('从本地文件夹',
                                style: theme.textTheme.titleSmall
                                    ?.copyWith(fontWeight: FontWeight.w600)),
                            Text('选择已通过文件 App 放入的文件夹',
                                style: theme.textTheme.bodySmall
                                    ?.copyWith(color: theme.colorScheme.outline)),
                          ],
                        ),
                      ),
                      Icon(Icons.arrow_forward_ios, size: 16,
                          color: theme.colorScheme.outline),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _cloneFromGit() {
    final urlCtrl = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('从 Git 克隆'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: urlCtrl,
              autofocus: true,
              decoration: const InputDecoration(
                hintText: 'https://github.com/user/repo.git',
                labelText: '仓库 URL',
              ),
              keyboardType: TextInputType.url,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () async {
              final url = urlCtrl.text.trim();
              if (url.isEmpty) return;
              Navigator.pop(ctx);

              // Parse repo name from URL
              final repoName = url.split('/').last.replaceAll('.git', '');
              final name = await _showAliasDialog(repoName);
              if (name == null || name.trim().isEmpty) return;

              // Create workspace dir and clone
              final docs = await getApplicationDocumentsDirectory();
              final destPath = '${docs.path}/workspaces/$repoName';

              setState(() {
                _cloneStatus = '正在 clone...';
              });
              _showCloneProgress(name, url, destPath);
            },
            child: const Text('克隆'),
          ),
        ],
      ),
    );
  }

  String _cloneStatus = '';

  void _showCloneProgress(String name, String url, String destPath) {
    _cloneStatus = '正在 clone $name...';
    showDialog(
      context: context,
      barrierDismissible: false,
      builder: (ctx) {
        _doClone(name, url, destPath).then((_) {
          Navigator.pop(ctx);
          _controller.addWorkspace(name: name, path: destPath);
        }).catchError((e) {
          Navigator.pop(ctx);
          ScaffoldMessenger.of(this.context).showSnackBar(
            SnackBar(content: Text('克隆失败: $e')),
          );
        });

        return StatefulBuilder(
          builder: (sCtx, setDialogState) {
            _cloneStatus;
            return AlertDialog(
              title: Text('克隆 $name'),
              content: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const CircularProgressIndicator(),
                  const SizedBox(height: 16),
                  Text(_cloneStatus),
                ],
              ),
            );
          },
        );
      },
    );
  }

  Future<void> _doClone(
      String name, String url, String destPath) async {
    // Ensure target directory exists and is empty
    final destDir = Directory(destPath);
    if (destDir.existsSync()) destDir.deleteSync(recursive: true);
    destDir.createSync(recursive: true);

    // Init empty git repo and clone using pure Dart implementation
    final repo = GitRepo(workTree: destPath);
    repo.init();
    final network = GitNetwork(repo: repo);
    await network.clone(url);
  }

  Widget _buildEmptyState(ThemeData theme) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.chat_bubble_outline,
              size: 48, color: theme.colorScheme.outline),
          const SizedBox(height: 12),
          Text('开始编辑',
              style: theme.textTheme.titleMedium?.copyWith(
                color: theme.colorScheme.outline,
              )),
          const SizedBox(height: 4),
          Text('直接发送指令，Claude 会帮你改代码',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.outline,
              )),
        ],
      ),
    );
  }

  Widget _buildMessageList(ThemeData theme) {
    return ListView.builder(
      controller: _scrollController,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      itemCount: _controller.messages.length,
      itemBuilder: (context, index) {
        final msg = _controller.messages[index];
        final isUser = msg.role == 'user';
        return _buildMessageBubble(theme, msg, isUser);
      },
    );
  }

  Widget _buildMessageBubble(
      ThemeData theme, MiniClaudeMessage msg, bool isUser) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment:
            isUser ? CrossAxisAlignment.end : CrossAxisAlignment.start,
        children: [
          Container(
            constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.85,
            ),
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: isUser
                  ? theme.colorScheme.primaryContainer
                  : theme.colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(18),
            ),
            child: SelectableText(msg.text, style: theme.textTheme.bodyMedium),
          ),
          if (msg.toolTraces != null && msg.toolTraces!.isNotEmpty)
            ...msg.toolTraces!.map((t) => _buildToolTrace(theme, t)),
        ],
      ),
    );
  }

  Widget _buildToolTrace(ThemeData theme, MiniClaudeToolTrace trace) {
    return Padding(
      padding: const EdgeInsets.only(top: 6, left: 4),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(
          color: trace.isError
              ? theme.colorScheme.errorContainer.withAlpha(120)
              : theme.colorScheme.surface,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(
            color: trace.isError
                ? theme.colorScheme.error
                : theme.colorScheme.outline.withAlpha(80),
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              trace.isError ? Icons.error_outline : Icons.build_outlined,
              size: 14,
              color: trace.isError
                  ? theme.colorScheme.error
                  : theme.colorScheme.outline,
            ),
            const SizedBox(width: 6),
            Flexible(
              child: Text(
                trace.output.isNotEmpty
                    ? '${trace.toolName}: ${trace.output}'
                    : '${trace.toolName}(${trace.input})...',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: trace.isError
                      ? theme.colorScheme.error
                      : theme.colorScheme.outline,
                ),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ),
      ),
    );
  }

  String _formatTokens(int tokens) {
    if (tokens >= 1000) return '${(tokens / 1000).toStringAsFixed(1)}k';
    return tokens.toString();
  }

  Widget _buildInputBar(ThemeData theme) {
    final busy = _controller.status == MiniClaudeStatus.running;

    return Container(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 10,
        bottom: MediaQuery.of(context).padding.bottom + 10,
      ),
      decoration: BoxDecoration(
        color: theme.scaffoldBackgroundColor,
        border: Border(top: BorderSide(color: theme.dividerColor)),
      ),
      child: Row(
        children: [
          Expanded(
            child: TextField(
              controller: _inputController,
              focusNode: _inputFocus,
              enabled: !busy,
              decoration: InputDecoration(
                hintText: busy ? 'Claude 思考中...' : '输入消息...',
                contentPadding:
                    const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
              ),
              maxLines: 4,
              minLines: 1,
              textInputAction: TextInputAction.send,
              onSubmitted: (_) => _send(),
            ),
          ),
          const SizedBox(width: 10),
          busy
              ? IconButton.filled(
                  style: IconButton.styleFrom(
                    backgroundColor: theme.colorScheme.error,
                    foregroundColor: theme.colorScheme.onError,
                  ),
                  icon: const Icon(Icons.stop),
                  onPressed: () => _controller.cancel(),
                )
              : IconButton.filled(
                  icon: const Icon(Icons.send_rounded),
                  onPressed: _send,
                ),
        ],
      ),
    );
  }

  void _showWorkspaceSheet(BuildContext context) {
    final theme = Theme.of(context);
    showModalBottomSheet(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.all(20),
              child: Text('工作区',
                  style: theme.textTheme.titleMedium
                      ?.copyWith(fontWeight: FontWeight.w600)),
            ),
            if (_controller.workspaces.isEmpty)
              const Padding(
                padding: EdgeInsets.all(20),
                child: Text('还没有工作区'),
              ),
            ..._controller.workspaces.map((ws) {
              final isActive = ws.id == _controller.activeWorkspace?.id;
              return ListTile(
                leading: Icon(
                  isActive ? Icons.folder : Icons.folder_outlined,
                  color: isActive ? theme.colorScheme.primary : null,
                ),
                title: Text(ws.name,
                    style: TextStyle(
                      fontWeight:
                          isActive ? FontWeight.w600 : FontWeight.normal,
                    )),
                subtitle: Text(ws.path,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall),
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (!isActive)
                      IconButton(
                        icon: const Icon(Icons.check, size: 20),
                        tooltip: '切换到此工作区',
                        onPressed: () {
                          _controller.switchWorkspace(ws.id);
                          Navigator.pop(ctx);
                        },
                      ),
                    IconButton(
                      icon: const Icon(Icons.delete_outline, size: 20),
                      tooltip: '移除工作区',
                      onPressed: () {
                        _controller.removeWorkspace(ws.id);
                        Navigator.pop(ctx);
                      },
                    ),
                  ],
                ),
              );
            }),
            const Divider(),
            ListTile(
              leading: const Icon(Icons.add),
              title: const Text('添加工作区'),
              onTap: () {
                Navigator.pop(ctx);
                _pickAndAddWorkspace();
              },
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }
}
