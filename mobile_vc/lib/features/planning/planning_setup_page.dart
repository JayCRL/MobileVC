import 'dart:async';
import 'package:flutter/material.dart';

import '../../data/models/events.dart';
import '../session/session_controller.dart';
import 'planning_session_page.dart';

class PlanningSetupPage extends StatefulWidget {
  const PlanningSetupPage({super.key, required this.controller});

  final SessionController controller;

  @override
  State<PlanningSetupPage> createState() => _PlanningSetupPageState();
}

class _PlanningSetupPageState extends State<PlanningSetupPage> {
  final _keyCtrl = TextEditingController();
  final _baseUrlCtrl = TextEditingController();
  bool _checking = false;
  bool _claudeInstalled = false;
  String _claudeVersion = '';
  String _claudeError = '';
  String _installHint = '';
  StreamSubscription<AppEvent>? _eventSub;

  @override
  void initState() {
    super.initState();
    _eventSub = widget.controller.events.listen(_onEvent);
  }

  @override
  void dispose() {
    _keyCtrl.dispose();
    _baseUrlCtrl.dispose();
    _eventSub?.cancel();
    super.dispose();
  }

  void _onEvent(AppEvent event) {
    if (event is PlanningCheckEvent) {
      setState(() {
        _checking = false;
        _claudeInstalled = event.installed;
        _claudeVersion = event.version;
        _claudeError = event.error;
        _installHint = event.installHint;
      });
    }
  }

  void _checkClaude() {
    setState(() => _checking = true);
    widget.controller.sendRawAction('planning_check');
  }

  void _saveKeyAndStart() {
    final apiKey = _keyCtrl.text.trim();
    if (apiKey.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请输入 Anthropic API Key')),
      );
      return;
    }
    widget.controller.sendRawAction('planning_set_key', {
      'apiKey': apiKey,
    });

    Navigator.of(context).pushReplacement(
      MaterialPageRoute(
        builder: (_) => PlanningSessionPage(
          controller: widget.controller,
          apiKey: apiKey,
          baseUrl: _baseUrlCtrl.text.trim(),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Boss 模式设置')),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // API Key
            Text('Anthropic API Key',
                style: theme.textTheme.titleMedium
                    ?.copyWith(fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            TextField(
              controller: _keyCtrl,
              obscureText: true,
              decoration: const InputDecoration(
                hintText: 'sk-ant-api...',
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 4),
            Text('密钥仅保存在本地电脑，不会上传云端',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.outline)),
            const SizedBox(height: 20),

            // Base URL (optional)
            Text('API 地址（可选）',
                style: theme.textTheme.titleMedium
                    ?.copyWith(fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            TextField(
              controller: _baseUrlCtrl,
              decoration: const InputDecoration(
                hintText: '默认 https://api.anthropic.com',
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 4),
            Text('使用代理或自定义端点时填写，留空使用默认',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.outline)),
            const SizedBox(height: 24),

            // Claude status
            Text('Claude Code 状态',
                style: theme.textTheme.titleMedium
                    ?.copyWith(fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    Icon(
                      _checking
                          ? Icons.hourglass_empty
                          : _claudeInstalled
                              ? Icons.check_circle
                              : Icons.warning_amber,
                      color: _checking
                          ? Colors.orange
                          : _claudeInstalled
                              ? Colors.green
                              : Colors.red,
                      size: 28,
                    ),
                    const SizedBox(width: 16),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            _checking
                                ? '检查中...'
                                : _claudeInstalled
                                    ? 'Claude Code 已安装'
                                    : '未检测到 Claude Code',
                            style: theme.textTheme.bodyMedium
                                ?.copyWith(fontWeight: FontWeight.w500),
                          ),
                          if (_claudeVersion.isNotEmpty)
                            Text(_claudeVersion,
                                style: theme.textTheme.bodySmall),
                          if (_claudeError.isNotEmpty)
                            Text(_claudeError,
                                style: theme.textTheme.bodySmall
                                    ?.copyWith(color: Colors.red)),
                          if (_installHint.isNotEmpty)
                            Text(_installHint,
                                style: theme.textTheme.bodySmall?.copyWith(
                                    color: theme.colorScheme.outline)),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 8),
            OutlinedButton.icon(
              onPressed: _checking ? null : _checkClaude,
              icon: const Icon(Icons.refresh),
              label: const Text('检查安装状态'),
            ),
            if (!_claudeInstalled && !_checking && _claudeError.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                '安装方法：在电脑终端运行 npm install -g @anthropic-ai/claude-code',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.outline),
              ),
            ],
            const SizedBox(height: 32),

            FilledButton.icon(
              onPressed: (_claudeInstalled && !_checking)
                  ? _saveKeyAndStart
                  : null,
              icon: const Icon(Icons.play_arrow),
              label: const Text('开始 Planning Session'),
              style: FilledButton.styleFrom(
                minimumSize: const Size(double.infinity, 48),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
