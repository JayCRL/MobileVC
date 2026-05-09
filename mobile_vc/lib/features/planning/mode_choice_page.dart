import 'dart:io';
import 'package:flutter/material.dart';

import '../session/session_controller.dart';
import '../session/session_home_page.dart';
import 'planning_setup_page.dart';

class ModeChoicePage extends StatefulWidget {
  const ModeChoicePage({
    super.key,
    required this.controller,
    this.darkModeEnabled = false,
    this.onToggleTheme,
  });

  final SessionController controller;
  final bool darkModeEnabled;
  final VoidCallback? onToggleTheme;

  @override
  State<ModeChoicePage> createState() => _ModeChoicePageState();
}

class _ModeChoicePageState extends State<ModeChoicePage> {
  bool _connecting = true;
  String _error = '';
  final _diagResults = <String>[];

  @override
  void initState() {
    super.initState();
    _connect();
  }

  void _addDiag(String msg) {
    setState(() => _diagResults.add('[$msg]'));
  }

  Future<void> _testWs(String label, String url, {Duration timeout = const Duration(seconds: 8)}) async {
    try {
      _addDiag('$label: 连接 $url ...');
      final ws = await WebSocket.connect(url).timeout(timeout);
      _addDiag('$label: OK (readyState=${ws.readyState})');
      await ws.close();
    } catch (e) {
      _addDiag('$label: 失败 — $e');
    }
  }

  Future<void> _connect() async {
    setState(() {
      _connecting = true;
      _error = '';
    });

    final c = widget.controller.config;
    final realToken = c.officialAccessToken;

    // --- WS diagnostics ---
    _addDiag('=== WebSocket 诊断 ===');
    await _testWs('echo-wss', 'wss://ws.postman-echo.com/raw');
    await _testWs('mobilevc-wss', 'wss://mobilevc.top/ws/signaling?token=diag-test');
    // Test with REAL OAuth token
    if (realToken.isNotEmpty) {
      final tokenPreview = realToken.length > 20 ? '${realToken.substring(0, 15)}...' : realToken;
      _addDiag('real-token: $tokenPreview');
      await _testWs('real-token-wss', 'wss://mobilevc.top/ws/signaling?token=$realToken');
    } else {
      _addDiag('real-token: EMPTY — Token not set!');
    }
    // Test full signaling flow: connect + send connect_request
    if (realToken.isNotEmpty && c.officialNodeId.isNotEmpty) {
      _addDiag('--- full flow test ---');
      try {
        final sigWs = await WebSocket.connect('wss://mobilevc.top/ws/signaling?token=$realToken');
        sigWs.add('{"type":"connect_request","nodeId":"${c.officialNodeId}","peerId":"diag-full"}');
        _addDiag('full-flow: connect_request sent, waiting...');
        sigWs.listen((data) {
          _addDiag('full-flow: response=$data');
        });
        await Future.delayed(const Duration(seconds: 3));
        await sigWs.close();
      } catch (e) {
        _addDiag('full-flow: FAIL — $e');
      }
    }
    _addDiag('=== 诊断完毕 ===');

    _addDiag('mode=${c.connectionMode} node=${c.officialNodeId} url=${c.officialServerUrl}');

    try {
      await widget.controller.connect();
    } catch (e) {
      if (mounted) {
        setState(() {
          _connecting = false;
          _error = 'FAIL: $e';
        });
      }
      return;
    }

    if (mounted) {
      setState(() => _connecting = false);
    }
  }

  void _enterManagerMode() {
    if (!mounted) return;
    Navigator.of(context).pushReplacement(
      MaterialPageRoute(
        builder: (_) => SessionHomePage(
          controller: widget.controller,
          darkModeEnabled: widget.darkModeEnabled,
          onToggleTheme: widget.onToggleTheme,
        ),
      ),
    );
  }

  void _enterBossMode() {
    if (!mounted) return;
    Navigator.of(context).pushReplacement(
      MaterialPageRoute(
        builder: (_) => PlanningSetupPage(
          controller: widget.controller,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: _connecting ? _buildDiag(theme) : _buildChoice(theme),
          ),
        ),
      ),
    );
  }

  Widget _buildDiag(ThemeData theme) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const SizedBox(width: 48, height: 48, child: CircularProgressIndicator(strokeWidth: 3)),
        const SizedBox(height: 16),
        Text('连接诊断中...', style: theme.textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w600)),
        const SizedBox(height: 16),
        SizedBox(
          width: double.infinity,
          height: 280,
          child: Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: theme.colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(8),
            ),
            child: ListView.builder(
              itemCount: _diagResults.length,
              itemBuilder: (_, i) => Text(
                _diagResults[i],
                style: theme.textTheme.bodySmall?.copyWith(
                  fontFamily: 'monospace',
                  color: _diagResults[i].contains('失败')
                      ? Colors.red
                      : _diagResults[i].contains('OK')
                          ? Colors.green
                          : theme.colorScheme.outline,
                ),
              ),
            ),
          ),
        ),
        if (_error.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text(_error, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error)),
          const SizedBox(height: 12),
          OutlinedButton(onPressed: _connect, child: const Text('重试')),
        ],
      ],
    );
  }

  Widget _buildChoice(ThemeData theme) {
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.check_circle, size: 48, color: theme.colorScheme.primary),
          const SizedBox(height: 12),
          Text('已连接', style: theme.textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w700)),
          const SizedBox(height: 4),
          Text('选择操作模式', style: theme.textTheme.bodyLarge?.copyWith(color: theme.colorScheme.outline)),

          // Always show diagnostics below
          if (_diagResults.isNotEmpty) ...[
            const SizedBox(height: 16),
            SizedBox(
              width: double.infinity,
              height: 200,
              child: Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: ListView.builder(
                  itemCount: _diagResults.length,
                  itemBuilder: (_, i) => Text(
                    _diagResults[i],
                    style: theme.textTheme.bodySmall?.copyWith(
                      fontFamily: 'monospace',
                      fontSize: 11,
                      color: _diagResults[i].contains('失败')
                          ? Colors.red
                          : _diagResults[i].contains('OK')
                              ? Colors.green
                              : theme.colorScheme.outline,
                    ),
                  ),
                ),
              ),
            ),
          ],

          if (_error.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(_error, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error)),
          ],

          const SizedBox(height: 24),
        SizedBox(
          width: 280,
          child: Card(
            child: InkWell(
              onTap: _enterManagerMode,
              borderRadius: BorderRadius.circular(16),
              child: const Padding(
                padding: EdgeInsets.all(24),
                child: Column(
                  children: [
                    Icon(Icons.person_outlined, size: 40),
                    SizedBox(height: 12),
                    Text('Manager 模式', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 16)),
                    SizedBox(height: 4),
                    Text('手动操作，逐个发送指令', style: TextStyle(color: Colors.grey, fontSize: 13)),
                  ],
                ),
              ),
            ),
          ),
        ),
        const SizedBox(height: 20),
        SizedBox(
          width: 280,
          child: Card(
            child: InkWell(
              onTap: _enterBossMode,
              borderRadius: BorderRadius.circular(16),
              child: const Padding(
                padding: EdgeInsets.all(24),
                child: Column(
                  children: [
                    Icon(Icons.account_tree_outlined, size: 40, color: Color(0xFF10B981)),
                    SizedBox(height: 12),
                    Text('Boss 模式', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 16, color: Color(0xFF10B981))),
                    SizedBox(height: 4),
                    Text('AI 管理层协调多 Agent\n分步确认，自动执行', textAlign: TextAlign.center, style: TextStyle(color: Colors.grey, fontSize: 13)),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
      ),
    );
  }
}
