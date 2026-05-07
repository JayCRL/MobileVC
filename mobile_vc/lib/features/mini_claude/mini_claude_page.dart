import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

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
    final path = await FilePicker.platform.getDirectoryPath(
      dialogTitle: '选择项目文件夹',
    );
    if (path == null || path.trim().isEmpty) return;

    final folderName = path.split('/').last;
    final name = await _showAliasDialog(folderName);
    if (name == null || name.trim().isEmpty) return;

    await _controller.addWorkspace(name: name, path: path);
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

  void _startDeviceAuth(BuildContext ctx, String platform) async {
    final controller = _controller;
    if (controller.gitHubClientId.isEmpty) {
      ScaffoldMessenger.of(ctx).showSnackBar(
        const SnackBar(content: Text('请先填写 OAuth Client ID')),
      );
      return;
    }

    // Show progress dialog
    showDialog(
      context: ctx,
      barrierDismissible: false,
      builder: (dCtx) {
        String status = '正在连接 ${platform}...';
        return StatefulBuilder(
          builder: (sCtx, setDialogState) {
            // Start the flow
            if (status.startsWith('正在连接')) {
              controller.startDeviceFlow(
                platform: platform,
                onStatus: (s) => setDialogState(() => status = s),
              ).then((token) {
                Navigator.pop(dCtx);
                ScaffoldMessenger.of(ctx).showSnackBar(
                  SnackBar(content: Text('$platform 登录成功！token 已自动保存')),
                );
                setState(() {}); // refresh credential list
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
                  if (status.startsWith('请在'))
                    ...[
                      Icon(Icons.login, size: 48,
                          color: Theme.of(dCtx).colorScheme.primary),
                      const SizedBox(height: 16),
                      SelectableText(
                        status.replaceFirst('请在浏览器打开\n', '请在浏览器打开\n\n'),
                        textAlign: TextAlign.center,
                        style: Theme.of(dCtx).textTheme.bodyMedium,
                      ),
                      const SizedBox(height: 12),
                      FilledButton.icon(
                        icon: const Icon(Icons.open_in_browser, size: 16),
                        label: Text(
                            '打开 ${platform == 'github' ? 'github.com/login/device' : 'gitee.com/oauth/authorize_device'}'),
                        onPressed: () {
                          final url = status.contains('http')
                              ? status.split('\n')[0].replaceFirst('请在浏览器打开', '').trim()
                              : 'https://${platform}.com/login/device';
                          launchUrl(Uri.parse(url));
                        },
                      ),
                    ]
                  else
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
            Text(
              '选择一个本地文件夹作为工作区，\nMini Claude 将在其中读写文件。\n\n通过 iOS 文件 App 或 Mac Finder\n把项目文件夹放到手机后即可导入。',
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyMedium
                  ?.copyWith(color: theme.colorScheme.outline),
            ),
            const SizedBox(height: 24),
            FilledButton.icon(
              onPressed: _pickAndAddWorkspace,
              icon: const Icon(Icons.create_new_folder_outlined),
              label: const Text('选择本地文件夹'),
            ),
            const SizedBox(height: 8),
            Text('也支持直接选择 iOS 文件 App 共享的目录',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.outline)),
          ],
        ),
      ),
    );
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
              ? IconButton(
                  icon: const Icon(Icons.stop),
                  onPressed: () {},
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
