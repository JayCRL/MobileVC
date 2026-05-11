import 'dart:async';
import 'package:flutter/material.dart';

import '../../data/models/events.dart';
import '../session/session_controller.dart';

class PlanningSessionPage extends StatefulWidget {
  const PlanningSessionPage({
    super.key,
    required this.controller,
    required this.apiKey,
    this.baseUrl = '',
  });

  final SessionController controller;
  final String apiKey;
  final String baseUrl;

  @override
  State<PlanningSessionPage> createState() => _PlanningSessionPageState();
}

class _PlanningSessionPageState extends State<PlanningSessionPage> {
  final _taskCtrl = TextEditingController();
  final _adjustCtrl = TextEditingController();
  bool _running = false;
  bool _awaitingConfirmation = false;
  StreamSubscription<AppEvent>? _eventSub;

  String _phase = 'idle';
  String _currentTask = '';
  String _currentAgent = '';
  String _message = '';
  List<PlanTask> _tasks = [];

  String get _phaseLabel {
    switch (_phase) {
      case 'planning':
        return '规划中';
      case 'awaiting_confirmation':
        return '等待确认';
      case 'executing':
        return '执行中';
      case 'reviewing':
        return '审查中';
      case 'checkpoint':
        return '检查点';
      case 'completed':
        return '已完成';
      default:
        return '就绪';
    }
  }

  Color get _phaseColor {
    switch (_phase) {
      case 'planning':
        return const Color(0xFF3B82F6);
      case 'awaiting_confirmation':
        return const Color(0xFFF59E0B);
      case 'checkpoint':
        return const Color(0xFF8B5CF6);
      case 'executing':
        return const Color(0xFFF59E0B);
      case 'reviewing':
        return const Color(0xFF8B5CF6);
      case 'completed':
        return const Color(0xFF22C55E);
      default:
        return Colors.grey;
    }
  }

  IconData get _phaseIcon {
    switch (_phase) {
      case 'planning':
        return Icons.psychology;
      case 'awaiting_confirmation':
        return Icons.pause_circle;
      case 'checkpoint':
        return Icons.flag_circle;
      case 'executing':
        return Icons.play_circle;
      case 'reviewing':
        return Icons.rate_review;
      case 'completed':
        return Icons.check_circle;
      default:
        return Icons.circle_outlined;
    }
  }

  @override
  void initState() {
    super.initState();
    _eventSub = widget.controller.events.listen(_onEvent);
  }

  @override
  void dispose() {
    _taskCtrl.dispose();
    _adjustCtrl.dispose();
    _eventSub?.cancel();
    super.dispose();
  }

  void _onEvent(AppEvent event) {
    if (event is PlanningStateEvent) {
      setState(() {
        _phase = event.phase;
        _currentTask = event.currentTask;
        _currentAgent = event.currentAgent;
        _message = event.message;
        _tasks = event.tasks;
        _awaitingConfirmation = event.phase == 'awaiting_confirmation' || event.phase == 'checkpoint';

        if (event.phase == 'completed' && _running) {
          _running = false;
          _awaitingConfirmation = false;
        }
      });
    }
  }

  void _startPlanning() {
    final task = _taskCtrl.text.trim();
    if (task.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请输入要完成的任务')),
      );
      return;
    }

    setState(() {
      _running = true;
      _awaitingConfirmation = false;
      _phase = 'planning';
      _message = '正在启动 Planning Session...';
      _tasks = [];
    });

    final extra = <String, dynamic>{
      'task': task,
      'apiKey': widget.apiKey,
    };
    if (widget.baseUrl.isNotEmpty) {
      extra['baseUrl'] = widget.baseUrl;
    }
    widget.controller.sendRawAction('planning_start', extra);
  }

  void _confirm() {
    final notes = _adjustCtrl.text.trim();
    setState(() {
      _awaitingConfirmation = false;
      _phase = 'executing';
      _message = '已确认，开始执行...';
    });
    widget.controller.sendRawAction('planning_confirm', {
      'decision': 'confirm',
      if (notes.isNotEmpty) 'notes': notes,
    });
    _adjustCtrl.clear();
  }

  void _continueCheckpoint() {
    final notes = _adjustCtrl.text.trim();
    setState(() {
      _awaitingConfirmation = false;
      _phase = 'executing';
      _message = '继续执行...';
    });
    widget.controller.sendRawAction('planning_confirm', {
      'decision': 'continue',
      if (notes.isNotEmpty) 'notes': notes,
    });
    _adjustCtrl.clear();
  }

  void _adjust() {
    final notes = _adjustCtrl.text.trim();
    if (notes.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请输入调整意见')),
      );
      return;
    }
    widget.controller.sendRawAction('planning_confirm', {
      'decision': 'adjust',
      'notes': notes,
    });
    _adjustCtrl.clear();
    setState(() {
      _phase = 'planning';
      _message = '已发送调整意见，等待重新规划...';
    });
  }

  void _cancelPlanning() {
    widget.controller.sendRawAction('planning_confirm', {
      'decision': 'cancel',
    });
    setState(() {
      _running = false;
      _awaitingConfirmation = false;
      _phase = 'completed';
      _message = '已取消';
    });
  }

  Color _taskStatusColor(String status) {
    switch (status) {
      case 'running':
        return const Color(0xFFF59E0B);
      case 'done':
        return const Color(0xFF22C55E);
      case 'failed':
        return const Color(0xFFEF4444);
      default:
        return Colors.grey;
    }
  }

  IconData _taskStatusIcon(String status) {
    switch (status) {
      case 'running':
        return Icons.hourglass_top;
      case 'done':
        return Icons.check_circle;
      case 'failed':
        return Icons.error;
      default:
        return Icons.circle_outlined;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Boss Mode'),
        actions: [
          if (_running)
            IconButton(
              icon: const Icon(Icons.stop),
              tooltip: '停止',
              onPressed: _cancelPlanning,
            ),
        ],
      ),
      body: Column(
        children: [
          // ---- Phase indicator ----
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(20),
            color: _phaseColor.withAlpha(25),
            child: Column(
              children: [
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Icon(_phaseIcon, color: _phaseColor, size: 32),
                    const SizedBox(width: 12),
                    Text(_phaseLabel,
                        style: theme.textTheme.headlineSmall?.copyWith(
                          color: _phaseColor,
                          fontWeight: FontWeight.w700,
                        )),
                  ],
                ),
                if (_currentTask.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  Text(_currentTask,
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: theme.colorScheme.outline)),
                ],
                if (_currentAgent.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  Container(
                    padding:
                        const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
                    decoration: BoxDecoration(
                      color: _phaseColor.withAlpha(40),
                      borderRadius: BorderRadius.circular(20),
                    ),
                    child: Text(
                      'Agent: $_currentAgent',
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: _phaseColor),
                    ),
                  ),
                ],
                if (_message.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    _message,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurface,
                    ),
                    textAlign: TextAlign.center,
                    maxLines: 4,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
                if (_running && !_awaitingConfirmation) ...[
                  const SizedBox(height: 12),
                  const SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                ],
              ],
            ),
          ),

          // ---- Confirmation / Checkpoint card ----
          if (_awaitingConfirmation)
            Container(
              margin: const EdgeInsets.all(16),
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: theme.colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(16),
                border: Border.all(
                    color: _phase == 'checkpoint'
                        ? const Color(0xFF8B5CF6)
                        : const Color(0xFFF59E0B)),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Row(
                    children: [
                      Icon(
                          _phase == 'checkpoint'
                              ? Icons.flag
                              : Icons.info_outline,
                          color: _phase == 'checkpoint'
                              ? const Color(0xFF8B5CF6)
                              : const Color(0xFFF59E0B)),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          _phase == 'checkpoint'
                              ? '阶段完成，等待确认'
                              : 'AI 已完成规划，等待你的确认',
                          style: theme.textTheme.titleSmall
                              ?.copyWith(fontWeight: FontWeight.w600),
                        ),
                      ),
                    ],
                  ),
                  if (_message.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: theme.colorScheme.surface,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(_message,
                          style: theme.textTheme.bodyMedium),
                    ),
                  ],
                  const SizedBox(height: 12),
                  TextField(
                    controller: _adjustCtrl,
                    decoration: InputDecoration(
                      hintText: _phase == 'checkpoint'
                          ? '反馈（可选）'
                          : '调整意见（可选，直接确认可留空）',
                      border: const OutlineInputBorder(),
                      contentPadding: const EdgeInsets.symmetric(
                          horizontal: 12, vertical: 10),
                    ),
                    maxLines: 2,
                    minLines: 1,
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Expanded(
                        child: OutlinedButton(
                          onPressed: _cancelPlanning,
                          child: const Text('取消'),
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: OutlinedButton(
                          onPressed: _adjust,
                          child: const Text('调整'),
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        flex: 2,
                        child: FilledButton(
                          onPressed: () {
                            if (_phase == 'checkpoint') {
                              _continueCheckpoint();
                            } else {
                              _confirm();
                            }
                          },
                          child: Text(_phase == 'checkpoint' ? '继续' : '确认执行'),
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),

          // ---- Task list ----
          if (_tasks.isNotEmpty)
            Expanded(
              child: ListView.builder(
                padding: const EdgeInsets.all(16),
                itemCount: _tasks.length,
                itemBuilder: (context, index) {
                  final task = _tasks[index];
                  return Card(
                    child: ListTile(
                      leading: Icon(
                        _taskStatusIcon(task.status),
                        color: _taskStatusColor(task.status),
                        size: 24,
                      ),
                      title: Text(task.title,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            fontWeight: task.status == 'running'
                                ? FontWeight.w600
                                : FontWeight.w400,
                          )),
                      subtitle: task.agent.isNotEmpty
                          ? Text(task.agent,
                              style: theme.textTheme.bodySmall?.copyWith(
                                  color: theme.colorScheme.outline))
                          : null,
                      trailing: _taskStatusBadge(task.status),
                    ),
                  );
                },
              ),
            )
          else if (!_running)
            Expanded(
              child: Center(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Icon(Icons.account_tree_outlined,
                        size: 64, color: theme.colorScheme.outline),
                    const SizedBox(height: 16),
                    Text('输入任务描述，AI 先规划再等你确认',
                        style: theme.textTheme.bodyLarge?.copyWith(
                            color: theme.colorScheme.outline)),
                    const SizedBox(height: 4),
                    Text('Boss Mode：你只跟管理层交互',
                        style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.outline)),
                  ],
                ),
              ),
            )
          else
            const Expanded(child: SizedBox.shrink()),

          // ---- Input area ----
          if (!_running)
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: theme.colorScheme.surface,
                border:
                    Border(top: BorderSide(color: theme.dividerColor)),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _taskCtrl,
                      decoration: const InputDecoration(
                        hintText: '描述你想完成的任务...',
                        border: OutlineInputBorder(),
                        contentPadding: EdgeInsets.symmetric(
                            horizontal: 16, vertical: 12),
                      ),
                      maxLines: 2,
                      minLines: 1,
                    ),
                  ),
                  const SizedBox(width: 12),
                  IconButton.filled(
                    onPressed: _startPlanning,
                    icon: const Icon(Icons.send),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  Widget _taskStatusBadge(String status) {
    final Color color;
    final String label;
    switch (status) {
      case 'running':
        color = const Color(0xFFF59E0B);
        label = '运行中';
      case 'done':
        color = const Color(0xFF22C55E);
        label = '完成';
      case 'failed':
        color = const Color(0xFFEF4444);
        label = '失败';
      default:
        color = Colors.grey;
        label = '等待中';
    }
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withAlpha(30),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(label,
          style: TextStyle(
              color: color, fontSize: 11, fontWeight: FontWeight.w500)),
    );
  }
}
