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

  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_onControllerChange);
    _connect();
  }

  @override
  void dispose() {
    widget.controller.removeListener(_onControllerChange);
    super.dispose();
  }

  void _onControllerChange() {
    if (!mounted) return;
    final ctrl = widget.controller;
    if (ctrl.connected && _connecting) {
      setState(() => _connecting = false);
    }
    if (ctrl.connectionStage == SessionConnectionStage.failed) {
      setState(() {
        _connecting = false;
        _error = ctrl.connectionMessage;
      });
    }
  }

  Future<void> _connect() async {
    setState(() {
      _connecting = true;
      _error = '';
    });
    try {
      await widget.controller.connect();
    } catch (e) {
      if (mounted) {
        setState(() {
          _connecting = false;
          _error = e.toString();
        });
      }
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
            padding: const EdgeInsets.all(32),
            child: _connecting ? _buildConnecting(theme) : _buildChoice(theme),
          ),
        ),
      ),
    );
  }

  Widget _buildConnecting(ThemeData theme) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const SizedBox(
          width: 48,
          height: 48,
          child: CircularProgressIndicator(strokeWidth: 3),
        ),
        const SizedBox(height: 24),
        Text('正在连接...',
            style: theme.textTheme.titleMedium
                ?.copyWith(fontWeight: FontWeight.w600)),
        if (_error.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text(_error,
              style: theme.textTheme.bodyMedium
                  ?.copyWith(color: theme.colorScheme.error),
              textAlign: TextAlign.center),
          const SizedBox(height: 16),
          OutlinedButton(onPressed: _connect, child: const Text('重试')),
        ],
      ],
    );
  }

  Widget _buildChoice(ThemeData theme) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(Icons.check_circle, size: 56, color: theme.colorScheme.primary),
        const SizedBox(height: 16),
        Text('已连接',
            style: theme.textTheme.headlineSmall
                ?.copyWith(fontWeight: FontWeight.w700)),
        const SizedBox(height: 8),
        Text('选择操作模式',
            style: theme.textTheme.bodyLarge
                ?.copyWith(color: theme.colorScheme.outline)),
        const SizedBox(height: 40),
        // Manager mode
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
                    Text('Manager 模式',
                        style: TextStyle(
                            fontWeight: FontWeight.w600, fontSize: 16)),
                    SizedBox(height: 4),
                    Text('手动操作，逐个发送指令',
                        style: TextStyle(
                            color: Colors.grey, fontSize: 13)),
                  ],
                ),
              ),
            ),
          ),
        ),
        const SizedBox(height: 20),
        // Boss mode
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
                    Icon(Icons.account_tree_outlined,
                        size: 40, color: Color(0xFF10B981)),
                    SizedBox(height: 12),
                    Text('Boss 模式',
                        style: TextStyle(
                            fontWeight: FontWeight.w600,
                            fontSize: 16,
                            color: Color(0xFF10B981))),
                    SizedBox(height: 4),
                    Text('AI 管理层协调多 Agent\n分步确认，自动执行',
                        textAlign: TextAlign.center,
                        style: TextStyle(
                            color: Colors.grey, fontSize: 13)),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}
