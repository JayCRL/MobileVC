import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:speech_to_text/speech_to_text.dart' as speech;

import '../../core/config/app_config.dart';
import '../../data/models/events.dart';
import '../../data/models/session_models.dart';
import '../session/session_controller.dart';
import 'voice_api_client.dart';

class VoiceCallSheet extends StatefulWidget {
  const VoiceCallSheet({
    super.key,
    required this.controller,
  });

  final SessionController controller;

  @override
  State<VoiceCallSheet> createState() => _VoiceCallSheetState();
}

class _VoiceCallSheetState extends State<VoiceCallSheet> {
  late final TextEditingController _voiceApiUrlController;
  late final TextEditingController _voiceApiKeyController;
  late final TextEditingController _voiceModelController;
  late final TextEditingController _ttsUrlController;
  late final TextEditingController _ttsApiKeyController;
  late final TextEditingController _ttsModelController;
  late final TextEditingController _ttsVoiceController;
  final TextEditingController _userTextController = TextEditingController();
  final VoiceApiClient _apiClient = VoiceApiClient();
  final AudioPlayer _audioPlayer = AudioPlayer();
  final speech.SpeechToText _speech = speech.SpeechToText();
  final List<_VoiceTurn> _turns = <_VoiceTurn>[];
  Completer<void>? _playbackCompleter;
  StreamSubscription<void>? _playbackSubscription;
  Timer? _speechSilenceTimer;

  bool _configOpen = false;
  bool _showTranscript = false;
  bool _keyboardOpen = false;
  bool _savingConfig = false;
  bool _sending = false;
  bool _speaking = false;
  bool _speechReady = false;
  bool _listening = false;
  bool _autoSubmittingSpeech = false;
  bool _autoListening = false;
  bool _orchestrationActive = false;
  bool _awaitingBackendConfirmation = false;
  String _status = '';
  String _elapsedLabel = '00:00';
  String _lastBackendActionKey = '';
  String _lastBackendReplyId = '';
  String _pendingVoiceConfigProvider = '';
  late String _permissionMode;
  late final DateTime _callStartedAt;
  DateTime? _orchestrationStartedAt;
  Timer? _durationTimer;

  SessionController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    final config = controller.config;
    _voiceApiUrlController = TextEditingController(text: config.voiceApiUrl);
    _voiceApiKeyController = TextEditingController(text: config.voiceApiKey);
    _voiceModelController = TextEditingController(text: config.voiceModelName);
    _ttsUrlController = TextEditingController(text: config.voiceTtsUrl);
    _ttsApiKeyController = TextEditingController(text: config.voiceTtsApiKey);
    _ttsModelController = TextEditingController(text: config.voiceTtsModelName);
    _ttsVoiceController = TextEditingController(text: config.voiceTtsVoice);
    _permissionMode = config.permissionMode;
    _configOpen = !config.hasVoiceCallConfig;
    _callStartedAt = DateTime.now();
    _durationTimer = Timer.periodic(
      const Duration(seconds: 1),
      (_) => _tickCallDuration(),
    );
    controller.addListener(_handleControllerAutomationUpdate);
    _tickCallDuration();
    WidgetsBinding.instance.addPostFrameCallback((_) => _scheduleAutoListen());
  }

  @override
  void dispose() {
    controller.removeListener(_handleControllerAutomationUpdate);
    _durationTimer?.cancel();
    _speechSilenceTimer?.cancel();
    unawaited(_playbackSubscription?.cancel());
    unawaited(_speech.stop());
    unawaited(_audioPlayer.dispose());
    _apiClient.close();
    _voiceApiUrlController.dispose();
    _voiceApiKeyController.dispose();
    _voiceModelController.dispose();
    _ttsUrlController.dispose();
    _ttsApiKeyController.dispose();
    _ttsModelController.dispose();
    _ttsVoiceController.dispose();
    _userTextController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final media = MediaQuery.of(context);
    final bottomInset = media.viewInsets.bottom;
    final minimumHeight = bottomInset > 0 ? 360.0 : 520.0;
    final sheetHeight =
        math.max(minimumHeight, media.size.height * 0.92 - bottomInset);
    return AnimatedPadding(
      duration: const Duration(milliseconds: 180),
      curve: Curves.easeOut,
      padding: EdgeInsets.only(bottom: bottomInset),
      child: SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 14),
          child: SizedBox(
            height: sheetHeight,
            child: ListenableBuilder(
              listenable: controller,
              builder: (context, _) {
                return Column(
                  children: [
                    _buildCallHeader(theme),
                    const SizedBox(height: 10),
                    Expanded(
                      child: AnimatedSwitcher(
                        duration: const Duration(milliseconds: 180),
                        child: _configOpen
                            ? SingleChildScrollView(
                                key: const ValueKey('voice-config'),
                                child: Column(
                                  children: [
                                    _buildConnectionStrip(theme),
                                    const SizedBox(height: 12),
                                    _buildConfigPanel(theme),
                                  ],
                                ),
                              )
                            : _buildCallMain(theme),
                      ),
                    ),
                    if (_keyboardOpen && !_configOpen) ...[
                      const SizedBox(height: 10),
                      _buildKeyboardComposer(theme),
                    ],
                    const SizedBox(height: 10),
                    _buildCallControls(theme),
                  ],
                );
              },
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildCallHeader(ThemeData theme) {
    final scheme = theme.colorScheme;
    return Row(
      children: [
        IconButton(
          tooltip: '结束通话',
          onPressed: () => Navigator.of(context).pop(),
          icon: const Icon(Icons.close_rounded),
        ),
        Expanded(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                'AI 通话',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              Text(
                _elapsedLabel,
                style: theme.textTheme.labelMedium?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
        IconButton(
          tooltip: _showTranscript ? '收起记录' : '通话记录',
          onPressed: () => setState(() => _showTranscript = !_showTranscript),
          icon: Icon(
            _showTranscript
                ? Icons.chat_bubble_rounded
                : Icons.chat_bubble_outline_rounded,
          ),
        ),
        IconButton(
          tooltip: _configOpen ? '返回通话' : '语音配置',
          onPressed: _sending || _savingConfig
              ? null
              : () => setState(() => _configOpen = !_configOpen),
          icon: Icon(
            _configOpen ? Icons.call_outlined : Icons.tune_outlined,
            color: _configOpen ? scheme.primary : null,
          ),
        ),
      ],
    );
  }

  Widget _buildCallMain(ThemeData theme) {
    return Column(
      key: const ValueKey('voice-call'),
      children: [
        if (!_keyboardOpen) ...[
          _buildConnectionStrip(theme),
          const SizedBox(height: 12),
        ],
        Expanded(child: _buildCallStage(theme)),
        if (!_keyboardOpen) ...[
          AnimatedSwitcher(
            duration: const Duration(milliseconds: 180),
            child: _showTranscript
                ? SizedBox(
                    key: const ValueKey('transcript-open'),
                    height: 220,
                    child: _buildConversation(theme),
                  )
                : _buildLatestTurnPreview(theme),
          ),
          const SizedBox(height: 12),
          _buildPermissionSelector(theme),
          if (_status.trim().isNotEmpty) ...[
            const SizedBox(height: 10),
            _buildStatusStrip(theme),
          ],
        ],
      ],
    );
  }

  Widget _buildCallStage(ThemeData theme) {
    final scheme = theme.colorScheme;
    final stage = _callStage;
    final accent = _stageColor(scheme, stage);
    final icon = _stageIcon(stage);
    return LayoutBuilder(
      builder: (context, constraints) {
        final rawSize = math.min(
          math.min(constraints.maxWidth * 0.72, constraints.maxHeight * 0.78),
          244.0,
        );
        final visualSize = rawSize.clamp(92.0, 244.0).toDouble();
        final showLabel = constraints.maxHeight >= 136;
        final showSubtitle = constraints.maxHeight >= 176;
        return Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              SizedBox(
                width: visualSize,
                height: visualSize,
                child: Stack(
                  alignment: Alignment.center,
                  children: [
                    AnimatedContainer(
                      duration: const Duration(milliseconds: 220),
                      width: stage == _VoiceCallStage.listening
                          ? visualSize
                          : visualSize * 0.9,
                      height: stage == _VoiceCallStage.listening
                          ? visualSize
                          : visualSize * 0.9,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: accent.withValues(alpha: 0.08),
                        border: Border.all(
                          color: accent.withValues(alpha: 0.22),
                          width: 2,
                        ),
                      ),
                    ),
                    AnimatedContainer(
                      duration: const Duration(milliseconds: 220),
                      width: stage == _VoiceCallStage.speaking
                          ? visualSize * 0.74
                          : visualSize * 0.68,
                      height: stage == _VoiceCallStage.speaking
                          ? visualSize * 0.74
                          : visualSize * 0.68,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: accent.withValues(alpha: 0.16),
                        border: Border.all(
                          color: accent.withValues(alpha: 0.38),
                        ),
                      ),
                    ),
                    Container(
                      width: visualSize * 0.46,
                      height: visualSize * 0.46,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: accent,
                        boxShadow: [
                          BoxShadow(
                            color: accent.withValues(alpha: 0.22),
                            blurRadius: 26,
                            offset: const Offset(0, 10),
                          ),
                        ],
                      ),
                      child: Icon(
                        icon,
                        color: scheme.onPrimary,
                        size: visualSize * 0.22,
                      ),
                    ),
                  ],
                ),
              ),
              if (showLabel) ...[
                const SizedBox(height: 18),
                Text(
                  _stageLabel(stage),
                  textAlign: TextAlign.center,
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ],
              if (showSubtitle) ...[
                const SizedBox(height: 6),
                Text(
                  _stageSubtitle(stage),
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        );
      },
    );
  }

  Widget _buildLatestTurnPreview(ThemeData theme) {
    if (_turns.isEmpty) {
      return const SizedBox.shrink(key: ValueKey('no-latest-turn'));
    }
    final turn = _turns.last;
    final isUser = turn.role == 'user';
    return Container(
      key: ValueKey('latest-${_turns.length}'),
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.08),
        ),
      ),
      child: Row(
        children: [
          Icon(
            isUser ? Icons.person_outline_rounded : Icons.auto_awesome_rounded,
            size: 18,
            color:
                isUser ? theme.colorScheme.primary : theme.colorScheme.tertiary,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              turn.content,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodyMedium,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildStatusStrip(ThemeData theme) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Text(
        _status,
        style: theme.textTheme.bodySmall,
      ),
    );
  }

  Widget _buildCallControls(ThemeData theme) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        _buildControlButton(
          theme: theme,
          tooltip: _keyboardOpen ? '收起键盘' : '键盘输入',
          icon: _keyboardOpen
              ? Icons.keyboard_hide_outlined
              : Icons.keyboard_outlined,
          label: '输入',
          onPressed: _savingConfig
              ? null
              : () => setState(() {
                    _keyboardOpen = !_keyboardOpen;
                    _configOpen = false;
                    _showTranscript = false;
                  }),
        ),
        _buildPrimaryVoiceButton(theme),
        _buildControlButton(
          theme: theme,
          tooltip: _orchestrationActive ? '停止接管' : '交给 AI',
          icon: _orchestrationActive
              ? Icons.stop_circle_outlined
              : Icons.assistant_direction_outlined,
          label: _orchestrationActive ? '停止' : '交给 AI',
          onPressed: _sending || _savingConfig
              ? null
              : _orchestrationActive
                  ? _stopOrchestration
                  : _handoffToNativeAgent,
        ),
        _buildControlButton(
          theme: theme,
          tooltip: '结束通话',
          icon: Icons.call_end_rounded,
          label: '结束',
          danger: true,
          onPressed: () => Navigator.of(context).pop(),
        ),
      ],
    );
  }

  Widget _buildPrimaryVoiceButton(ThemeData theme) {
    final scheme = theme.colorScheme;
    final disabled = _savingConfig || (_sending && !_speaking && !_listening);
    final Color fill = _listening
        ? scheme.error
        : _speaking
            ? scheme.tertiary
            : scheme.primary;
    final icon = _listening
        ? Icons.stop_rounded
        : _speaking
            ? Icons.volume_off_outlined
            : Icons.mic_rounded;
    final label = _listening
        ? (_autoListening ? '在听' : '发送')
        : _speaking
            ? '打断'
            : '说话';
    return SizedBox(
      width: 86,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Tooltip(
            message: label,
            child: InkResponse(
              onTap: disabled ? null : _handlePrimaryVoiceControl,
              radius: 42,
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 180),
                width: 68,
                height: 68,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: disabled ? scheme.surfaceContainerHighest : fill,
                  boxShadow: disabled
                      ? null
                      : [
                          BoxShadow(
                            color: fill.withValues(alpha: 0.26),
                            blurRadius: 22,
                            offset: const Offset(0, 10),
                          ),
                        ],
                ),
                child: Icon(
                  icon,
                  color: disabled ? scheme.onSurfaceVariant : scheme.onPrimary,
                  size: 30,
                ),
              ),
            ),
          ),
          const SizedBox(height: 6),
          Text(
            label,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: theme.textTheme.labelMedium?.copyWith(
              color: disabled ? scheme.onSurfaceVariant : scheme.onSurface,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildControlButton({
    required ThemeData theme,
    required String tooltip,
    required IconData icon,
    required String label,
    required VoidCallback? onPressed,
    bool danger = false,
  }) {
    final scheme = theme.colorScheme;
    final enabled = onPressed != null;
    final foreground = danger ? scheme.error : scheme.onSurface;
    final background = danger
        ? scheme.errorContainer.withValues(alpha: 0.62)
        : scheme.surfaceContainerHighest.withValues(alpha: 0.8);
    return SizedBox(
      width: 74,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Tooltip(
            message: tooltip,
            child: InkResponse(
              onTap: onPressed,
              radius: 32,
              child: Container(
                width: 52,
                height: 52,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: enabled
                      ? background
                      : scheme.surfaceContainerHighest.withValues(alpha: 0.45),
                ),
                child: Icon(
                  icon,
                  color: enabled ? foreground : scheme.onSurfaceVariant,
                  size: 23,
                ),
              ),
            ),
          ),
          const SizedBox(height: 6),
          Text(
            label,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: theme.textTheme.labelMedium?.copyWith(
              color: enabled ? foreground : scheme.onSurfaceVariant,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildKeyboardComposer(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.12),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: TextField(
              controller: _userTextController,
              minLines: 1,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: '输入补充信息',
                border: InputBorder.none,
              ),
            ),
          ),
          const SizedBox(width: 8),
          IconButton.filledTonal(
            tooltip: '发送',
            onPressed: _sending ? null : _sendToVoiceAssistant,
            icon: _sending
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.send_rounded),
          ),
        ],
      ),
    );
  }

  _VoiceCallStage get _callStage {
    if (!_configFromFields().hasVoiceCallConfig) {
      return _VoiceCallStage.needsConfig;
    }
    if (_listening) {
      return _VoiceCallStage.listening;
    }
    if (_speaking) {
      return _VoiceCallStage.speaking;
    }
    if (_sending) {
      return _VoiceCallStage.thinking;
    }
    return _VoiceCallStage.idle;
  }

  Color _stageColor(ColorScheme scheme, _VoiceCallStage stage) {
    switch (stage) {
      case _VoiceCallStage.needsConfig:
        return scheme.secondary;
      case _VoiceCallStage.listening:
        return scheme.primary;
      case _VoiceCallStage.thinking:
        return scheme.tertiary;
      case _VoiceCallStage.speaking:
        return const Color(0xFF0F766E);
      case _VoiceCallStage.idle:
        return scheme.primary;
    }
  }

  IconData _stageIcon(_VoiceCallStage stage) {
    switch (stage) {
      case _VoiceCallStage.needsConfig:
        return Icons.tune_outlined;
      case _VoiceCallStage.listening:
        return Icons.mic_rounded;
      case _VoiceCallStage.thinking:
        return Icons.auto_awesome_rounded;
      case _VoiceCallStage.speaking:
        return Icons.graphic_eq_rounded;
      case _VoiceCallStage.idle:
        return Icons.call_outlined;
    }
  }

  String _stageLabel(_VoiceCallStage stage) {
    switch (stage) {
      case _VoiceCallStage.needsConfig:
        return '配置语音模型';
      case _VoiceCallStage.listening:
        return '正在听';
      case _VoiceCallStage.thinking:
        return 'AI 正在整理';
      case _VoiceCallStage.speaking:
        return 'AI 正在说';
      case _VoiceCallStage.idle:
        return _turns.isEmpty ? '准备通话' : '继续确认';
    }
  }

  String _stageSubtitle(_VoiceCallStage stage) {
    switch (stage) {
      case _VoiceCallStage.needsConfig:
        return '完成模型配置后开始';
      case _VoiceCallStage.listening:
        return _autoListening ? '说完会自动发送' : '说完后点发送';
      case _VoiceCallStage.thinking:
        return '正在生成回应';
      case _VoiceCallStage.speaking:
        return '可随时打断';
      case _VoiceCallStage.idle:
        return _turns.isEmpty ? '可以直接说话' : '可以继续说话';
    }
  }

  void _tickCallDuration() {
    final elapsed = DateTime.now().difference(_callStartedAt);
    final minutes = elapsed.inMinutes.remainder(60).toString().padLeft(2, '0');
    final seconds = elapsed.inSeconds.remainder(60).toString().padLeft(2, '0');
    final label = '$minutes:$seconds';
    if (!mounted) {
      _elapsedLabel = label;
      return;
    }
    if (_elapsedLabel != label) {
      setState(() => _elapsedLabel = label);
    }
  }

  Future<void> _handlePrimaryVoiceControl() async {
    if (!_configFromFields().hasVoiceCallConfig) {
      setState(() {
        _configOpen = true;
        _status = '请先配置语音模型 URL 和模型名称';
      });
      return;
    }
    if (_speaking) {
      await _audioPlayer.stop();
      _completePlayback();
      if (mounted) {
        setState(() => _speaking = false);
      }
      await _startListening();
      return;
    }
    await _toggleListening();
  }

  Widget _buildConnectionStrip(ThemeData theme) {
    final engine = controller.config.engine.trim().isEmpty
        ? 'Claude'
        : controller.config.engine.trim();
    final label = controller.connected ? '已连接' : '未连接';
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        children: [
          Icon(
            controller.connected ? Icons.link_rounded : Icons.link_off_rounded,
            size: 18,
            color: controller.connected
                ? theme.colorScheme.primary
                : theme.colorScheme.outline,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '$label · $engine · ${controller.effectiveCwd}',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildConfigPanel(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.12),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.record_voice_over_outlined,
                  size: 18, color: theme.colorScheme.primary),
              const SizedBox(width: 8),
              Text(
                '语音模型',
                style: theme.textTheme.titleSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          _buildVoiceApiConfigSync(theme),
          const SizedBox(height: 10),
          TextField(
            controller: _voiceApiUrlController,
            decoration: const InputDecoration(
              labelText: 'Voice API URL',
              hintText: 'https://api.example.com/v1/chat/completions',
            ),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _voiceApiKeyController,
            obscureText: true,
            decoration: const InputDecoration(labelText: 'Voice API Key'),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _voiceModelController,
            decoration: const InputDecoration(labelText: 'Voice Model Name'),
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              Icon(Icons.graphic_eq_outlined,
                  size: 18, color: theme.colorScheme.primary),
              const SizedBox(width: 8),
              Text(
                '文字转语音',
                style: theme.textTheme.titleSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _ttsUrlController,
            decoration: const InputDecoration(
              labelText: 'TTS URL',
              hintText: 'https://api.example.com/v1/audio/speech',
            ),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _ttsApiKeyController,
            obscureText: true,
            decoration: const InputDecoration(labelText: 'TTS API Key'),
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _ttsModelController,
                  decoration:
                      const InputDecoration(labelText: 'TTS Model Name'),
                ),
              ),
              const SizedBox(width: 10),
              SizedBox(
                width: 120,
                child: TextField(
                  controller: _ttsVoiceController,
                  decoration: const InputDecoration(
                    labelText: 'Voice',
                    hintText: 'alloy / mimo_default',
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _savingConfig ? null : _saveConfig,
                  icon: _savingConfig
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.save_outlined),
                  label: const Text('保存'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: FilledButton.tonalIcon(
                  onPressed: _savingConfig ? null : _saveConfigAndReturn,
                  icon: const Icon(Icons.call_outlined),
                  label: const Text('返回通话'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildVoiceApiConfigSync(ThemeData theme) {
    final loading = controller.voiceApiConfigLoading;
    final canSync = controller.connected && !_savingConfig && !loading;
    final message = controller.voiceApiConfigMessage.trim();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            OutlinedButton.icon(
              onPressed: canSync ? () => _syncVoiceApiConfig('codex') : null,
              icon: _pendingVoiceConfigProvider == 'codex' && loading
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.terminal_outlined),
              label: const Text('同步 Codex'),
            ),
            OutlinedButton.icon(
              onPressed: canSync ? () => _syncVoiceApiConfig('claude') : null,
              icon: _pendingVoiceConfigProvider == 'claude' && loading
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.auto_awesome_outlined),
              label: const Text('同步 Claude'),
            ),
            IconButton.outlined(
              tooltip: '刷新本机配置',
              onPressed: canSync
                  ? () => controller.requestVoiceApiConfigCandidates(
                        force: true,
                      )
                  : null,
              icon: loading
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.sync_rounded),
            ),
          ],
        ),
        if (!controller.connected) ...[
          const SizedBox(height: 6),
          Text(
            '连接后可同步电脑上的 Codex / Claude 配置',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ] else if (message.isNotEmpty) ...[
          const SizedBox(height: 6),
          Text(
            message,
            style: theme.textTheme.bodySmall?.copyWith(
              color: controller.voiceApiConfigUnavailable
                  ? theme.colorScheme.error
                  : theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ],
    );
  }

  Widget _buildConversation(ThemeData theme) {
    if (_turns.isEmpty) {
      return Container(
        width: double.infinity,
        constraints: const BoxConstraints(minHeight: 140),
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color:
              theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.35),
          borderRadius: BorderRadius.circular(16),
        ),
        child: Icon(
          Icons.phone_in_talk_outlined,
          size: 42,
          color: theme.colorScheme.outline,
        ),
      );
    }
    return Container(
      width: double.infinity,
      constraints: const BoxConstraints(maxHeight: 320),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color:
            theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.35),
        borderRadius: BorderRadius.circular(16),
      ),
      child: ListView.separated(
        shrinkWrap: true,
        itemCount: _turns.length,
        separatorBuilder: (_, __) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          final turn = _turns[index];
          final isUser = turn.role == 'user';
          return Align(
            alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 340),
              child: DecoratedBox(
                decoration: BoxDecoration(
                  color: isUser
                      ? theme.colorScheme.primaryContainer
                      : theme.colorScheme.surface,
                  borderRadius: BorderRadius.circular(14),
                  border: Border.all(
                    color: theme.colorScheme.outline.withValues(alpha: 0.08),
                  ),
                ),
                child: Padding(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                  child: Text(
                    turn.content,
                    style: theme.textTheme.bodyMedium,
                  ),
                ),
              ),
            ),
          );
        },
      ),
    );
  }

  Widget _buildPermissionSelector(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '权限模式',
          style: theme.textTheme.labelLarge,
        ),
        const SizedBox(height: 8),
        SizedBox(
          width: double.infinity,
          child: SegmentedButton<String>(
            segments: const [
              ButtonSegment(
                value: 'default',
                icon: Icon(Icons.rule_outlined),
                label: Text('常规'),
              ),
              ButtonSegment(
                value: 'auto',
                icon: Icon(Icons.auto_awesome_outlined),
                label: Text('自动'),
              ),
              ButtonSegment(
                value: 'bypassPermissions',
                icon: Icon(Icons.shield_outlined),
                label: Text('放行'),
              ),
            ],
            selected: {_permissionMode},
            onSelectionChanged: (selection) {
              setState(() => _permissionMode = selection.first);
            },
          ),
        ),
      ],
    );
  }

  Future<bool> _saveConfig() async {
    setState(() {
      _savingConfig = true;
      _status = '';
    });
    try {
      await controller.saveConfig(_configFromFields());
      if (!mounted) {
        return true;
      }
      setState(() => _status = '语音配置已保存');
      return true;
    } catch (error) {
      if (!mounted) {
        return false;
      }
      setState(() => _status = '保存失败：$error');
      return false;
    } finally {
      if (mounted) {
        setState(() => _savingConfig = false);
      }
    }
  }

  Future<void> _saveConfigAndReturn() async {
    final saved = await _saveConfig();
    if (!mounted) {
      return;
    }
    if (saved && _configFromFields().hasVoiceCallConfig) {
      setState(() => _configOpen = false);
    }
  }

  Future<void> _syncVoiceApiConfig(String provider) async {
    final normalizedProvider = provider.trim().toLowerCase();
    if (!controller.connected) {
      setState(() => _status = '连接后才能同步电脑上的配置');
      return;
    }
    setState(() {
      _pendingVoiceConfigProvider = normalizedProvider;
      _status = '正在读取${_voiceApiProviderLabel(normalizedProvider)}配置';
    });
    controller.requestVoiceApiConfigCandidates(force: true);
  }

  void _maybeApplyPendingVoiceApiConfigSync() {
    final provider = _pendingVoiceConfigProvider.trim().toLowerCase();
    if (provider.isEmpty || controller.voiceApiConfigLoading || !mounted) {
      return;
    }
    _pendingVoiceConfigProvider = '';
    final candidate = _voiceApiCandidate(provider);
    if (candidate?.hasUsableConfig == true) {
      unawaited(_applyVoiceApiConfigCandidate(candidate!));
      return;
    }
    final detail = candidate?.detail.trim();
    final message = detail?.isNotEmpty == true
        ? detail!
        : controller.voiceApiConfigMessage.trim();
    setState(() {
      _status = message.isEmpty
          ? '没有找到可同步的${_voiceApiProviderLabel(provider)}配置'
          : message;
    });
  }

  Future<void> _applyVoiceApiConfigCandidate(
    VoiceApiConfigCandidate candidate,
  ) async {
    if (!mounted) {
      return;
    }
    setState(() {
      _savingConfig = true;
      _voiceApiUrlController.text = candidate.apiUrl;
      _voiceApiKeyController.text = candidate.apiKey;
      _voiceModelController.text = candidate.modelName;
      _status = '';
    });
    try {
      await controller.saveConfig(_configFromFields());
      if (!mounted) {
        return;
      }
      setState(() {
        _status = '已同步${_voiceApiProviderLabel(candidate.provider)}配置';
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() => _status = '同步失败：$error');
    } finally {
      if (mounted) {
        setState(() => _savingConfig = false);
      }
    }
  }

  VoiceApiConfigCandidate? _voiceApiCandidate(String provider) {
    final normalizedProvider = provider.trim().toLowerCase();
    for (final candidate in controller.voiceApiConfigCandidates) {
      if (candidate.provider == normalizedProvider) {
        return candidate;
      }
    }
    return null;
  }

  String _voiceApiProviderLabel(String provider) {
    switch (provider.trim().toLowerCase()) {
      case 'codex':
        return ' Codex';
      case 'claude':
        return ' Claude';
      default:
        return ' Voice API';
    }
  }

  Future<void> _toggleListening() async {
    if (_listening) {
      await _stopListening(submit: true);
      return;
    }
    await _startListening();
  }

  Future<void> _startListening({bool automatic = false}) async {
    if (_listening || _sending || _savingConfig || _speaking) {
      return;
    }
    if (_configOpen || _keyboardOpen) {
      return;
    }
    if (!_configFromFields().hasVoiceCallConfig) {
      if (!automatic && mounted) {
        setState(() {
          _configOpen = true;
          _status = '请先配置语音模型 URL 和模型名称';
        });
      }
      return;
    }
    try {
      if (!_speechReady) {
        _speechReady = await _speech.initialize(
          onStatus: (status) {
            if (!mounted) {
              return;
            }
            if (status == 'done' || status == 'notListening') {
              _speechSilenceTimer?.cancel();
              _speechSilenceTimer = null;
              final shouldSubmit = _listening;
              setState(() {
                _listening = false;
                _autoListening = false;
              });
              if (shouldSubmit) {
                unawaited(_submitRecognizedSpeech());
              }
            }
          },
          onError: (error) {
            if (!mounted) {
              return;
            }
            _speechSilenceTimer?.cancel();
            _speechSilenceTimer = null;
            setState(() {
              _listening = false;
              _autoListening = false;
              _status = '录音失败：${error.errorMsg}';
            });
            _scheduleAutoListen();
          },
        );
      }
      if (!_speechReady) {
        setState(() => _status = '麦克风或语音识别不可用');
        return;
      }
      setState(() {
        _listening = true;
        _autoListening = automatic;
        _keyboardOpen = false;
        _configOpen = false;
        _status = automatic ? '正在听你说话' : '';
      });
      await _speech.listen(
        listenOptions: speech.SpeechListenOptions(
          listenMode: speech.ListenMode.dictation,
          partialResults: true,
          listenFor: const Duration(seconds: 45),
          pauseFor: const Duration(milliseconds: 2800),
          localeId: 'zh_CN',
        ),
        onResult: (result) {
          if (!mounted) {
            return;
          }
          _userTextController.text = result.recognizedWords;
          _userTextController.selection = TextSelection.collapsed(
            offset: _userTextController.text.length,
          );
          _scheduleSpeechSilenceSubmit();
        },
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _listening = false;
        _autoListening = false;
        _status = '录音失败：$error';
      });
      _speechSilenceTimer?.cancel();
      _speechSilenceTimer = null;
      _scheduleAutoListen();
    }
  }

  Future<void> _stopListening({required bool submit}) async {
    _speechSilenceTimer?.cancel();
    _speechSilenceTimer = null;
    await _speech.stop();
    if (mounted) {
      setState(() {
        _listening = false;
        _autoListening = false;
      });
    }
    if (submit) {
      await _submitRecognizedSpeech();
    }
  }

  void _scheduleSpeechSilenceSubmit() {
    if (!_listening || _sending || _userTextController.text.trim().isEmpty) {
      return;
    }
    _speechSilenceTimer?.cancel();
    _speechSilenceTimer = Timer(const Duration(milliseconds: 2800), () {
      if (!mounted ||
          !_listening ||
          _sending ||
          _userTextController.text.trim().isEmpty) {
        return;
      }
      unawaited(_stopListening(submit: true));
    });
  }

  void _scheduleAutoListen() {
    if (!mounted ||
        _listening ||
        _sending ||
        _savingConfig ||
        _speaking ||
        _configOpen ||
        _keyboardOpen ||
        !_configFromFields().hasVoiceCallConfig) {
      return;
    }
    Future<void>.delayed(const Duration(milliseconds: 420), () async {
      if (!mounted ||
          _listening ||
          _sending ||
          _savingConfig ||
          _speaking ||
          _configOpen ||
          _keyboardOpen ||
          !_configFromFields().hasVoiceCallConfig) {
        return;
      }
      await _startListening(automatic: true);
    });
  }

  Future<void> _submitRecognizedSpeech() async {
    if (_autoSubmittingSpeech || _sending) {
      _scheduleAutoListen();
      return;
    }
    if (_userTextController.text.trim().isEmpty) {
      _scheduleAutoListen();
      return;
    }
    _autoSubmittingSpeech = true;
    try {
      await _sendToVoiceAssistant();
    } finally {
      _autoSubmittingSpeech = false;
    }
  }

  Future<void> _sendToVoiceAssistant() async {
    final userText = _userTextController.text.trim();
    if (userText.isEmpty) {
      setState(() => _status = '请输入或录入内容');
      _scheduleAutoListen();
      return;
    }
    if (_looksLikeStopOrchestrationCommand(userText)) {
      _stopOrchestration();
      _userTextController.clear();
      _scheduleAutoListen();
      return;
    }
    if (_orchestrationActive) {
      await _sendToNativeAgentFromCall(userText);
      return;
    }
    if (_looksLikeAutoHandoffTrigger(userText)) {
      await _startNativeAgentHandoff(
        pendingText: userText,
        requireExistingContext: false,
      );
      return;
    }
    final config = _configFromFields();
    if (!config.hasVoiceCallConfig) {
      setState(() {
        _configOpen = true;
        _status = '请先配置语音模型 URL 和模型名称';
      });
      return;
    }
    setState(() {
      _sending = true;
      _status = '';
      _keyboardOpen = false;
      _turns.add(_VoiceTurn(role: 'user', content: userText));
      _userTextController.clear();
    });
    try {
      await controller.saveConfig(config);
      final result = await _apiClient.complete(
        apiUrl: config.voiceApiUrl,
        apiKey: config.voiceApiKey,
        modelName: config.voiceModelName,
        messages: _buildVoiceMessages(),
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _turns.add(_VoiceTurn(role: 'assistant', content: result.content));
      });
      if (config.hasVoiceTtsConfig) {
        await _speakAssistantReply(config, result.content);
      }
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() => _status = '语音助手失败：$error');
    } finally {
      if (mounted) {
        setState(() => _sending = false);
        _scheduleAutoListen();
      }
    }
  }

  Future<void> _speakAssistantReply(AppConfig config, String content) async {
    setState(() => _speaking = true);
    try {
      final audio = await _apiClient.synthesize(
        ttsUrl: config.voiceTtsUrl,
        apiKey: config.voiceTtsApiKey,
        modelName: config.voiceTtsModelName,
        voice: config.voiceTtsVoice,
        text: content,
      );
      await _playbackSubscription?.cancel();
      _completePlayback();
      final completer = Completer<void>();
      _playbackCompleter = completer;
      _playbackSubscription = _audioPlayer.onPlayerComplete.listen((_) {
        if (!completer.isCompleted) {
          completer.complete();
        }
      });
      await _audioPlayer.stop();
      final sourceFile = await _writeTtsPlaybackFile(audio);
      await _audioPlayer.play(DeviceFileSource(sourceFile.path));
      await completer.future.timeout(
        const Duration(minutes: 3),
        onTimeout: () {},
      );
    } catch (error) {
      if (mounted) {
        setState(() => _status = 'TTS 播放失败：$error');
      }
    } finally {
      await _playbackSubscription?.cancel();
      _playbackSubscription = null;
      _playbackCompleter = null;
      if (mounted) {
        setState(() => _speaking = false);
        _scheduleAutoListen();
      }
    }
  }

  Future<File> _writeTtsPlaybackFile(VoiceSynthesisResult audio) async {
    final tempDir = await getTemporaryDirectory();
    final extension = _audioFileExtension(audio.contentType);
    final file = File(
      '${tempDir.path}/mobilevc_tts_${DateTime.now().microsecondsSinceEpoch}.$extension',
    );
    await file.writeAsBytes(audio.bytes, flush: true);
    return file;
  }

  String _audioFileExtension(String contentType) {
    final normalized = contentType.toLowerCase();
    if (normalized.contains('mpeg') || normalized.contains('mp3')) {
      return 'mp3';
    }
    if (normalized.contains('aac')) {
      return 'aac';
    }
    if (normalized.contains('m4a') || normalized.contains('mp4')) {
      return 'm4a';
    }
    if (normalized.contains('wav') || normalized.contains('wave')) {
      return 'wav';
    }
    return 'wav';
  }

  void _completePlayback() {
    final completer = _playbackCompleter;
    if (completer != null && !completer.isCompleted) {
      completer.complete();
    }
  }

  Future<void> _handoffToNativeAgent() async {
    await _startNativeAgentHandoff(
      pendingText: _userTextController.text.trim(),
      requireExistingContext: true,
    );
  }

  Future<void> _startNativeAgentHandoff({
    required String pendingText,
    required bool requireExistingContext,
  }) async {
    final extraText = pendingText.trim();
    if (requireExistingContext && _turns.isEmpty && extraText.isEmpty) {
      setState(() => _status = '请先通话或输入任务内容');
      _scheduleAutoListen();
      return;
    }
    await controller.saveConfig(_configFromFields());
    final prompt = _buildHandoffPrompt(extraUserText: extraText);
    final now = DateTime.now();
    final latestReply = _latestBackendReply();
    setState(() {
      _orchestrationActive = true;
      _awaitingBackendConfirmation = false;
      _orchestrationStartedAt = now;
      _lastBackendReplyId = latestReply?.id ?? '';
      _lastBackendActionKey = _currentBackendActionKey();
      _status = '已开始自动接管，正在把通话提示词发给 AI';
      _keyboardOpen = false;
      _configOpen = false;
      if (extraText.isNotEmpty) {
        _turns.add(_VoiceTurn(role: 'user', content: extraText));
        _userTextController.clear();
      }
    });
    final submitted = controller.submitVoiceHandoff(
      prompt,
      permissionMode: _permissionMode,
    );
    if (!submitted) {
      setState(() {
        _orchestrationActive = false;
        _awaitingBackendConfirmation = false;
        _status = '交接失败，请检查连接或当前会话状态';
      });
      _scheduleAutoListen();
      return;
    }
    _handleControllerAutomationUpdate();
    _scheduleAutoListen();
  }

  Future<void> _sendToNativeAgentFromCall(String userText) async {
    if (_looksLikeStopOrchestrationCommand(userText) &&
        !_awaitingBackendConfirmation) {
      _stopOrchestration();
      return;
    }
    if (!controller.connected) {
      setState(() => _status = '未连接 MobileVC 后端，暂时不能自动接管');
      return;
    }
    setState(() {
      _sending = true;
      _status = '';
      _keyboardOpen = false;
      _turns.add(_VoiceTurn(role: 'user', content: userText));
      _userTextController.clear();
    });
    try {
      final config = _configFromFields();
      await controller.saveConfig(config);
      if (_permissionMode.trim().isNotEmpty &&
          controller.displayPermissionMode != _permissionMode.trim()) {
        controller.updatePermissionMode(_permissionMode);
      }
      if (_awaitingBackendConfirmation) {
        final answer = _normalizeBackendConfirmation(userText);
        if (controller.shouldShowReviewChoices) {
          controller.sendReviewDecision(answer);
        } else {
          controller.submitPromptOption(answer);
        }
        _awaitingBackendConfirmation = false;
        if (mounted) {
          setState(() => _status = '已把你的确认提交给 AI，等待下一步结果');
        }
      } else {
        final prompt = _buildBackendFollowUpPrompt(userText);
        controller.sendInputText(prompt);
        if (mounted) {
          setState(() => _status = '已把你的补充提示词发给 AI');
        }
      }
    } catch (error) {
      if (mounted) {
        setState(() => _status = '自动接管失败：$error');
      }
    } finally {
      if (mounted) {
        setState(() => _sending = false);
        _scheduleAutoListen();
      }
    }
  }

  void _stopOrchestration() {
    if (!mounted) {
      return;
    }
    setState(() {
      _orchestrationActive = false;
      _awaitingBackendConfirmation = false;
      _lastBackendActionKey = '';
      _status = '自动接管已停止，可以继续普通语音沟通';
    });
    _scheduleAutoListen();
  }

  void _handleControllerAutomationUpdate() {
    _maybeApplyPendingVoiceApiConfigSync();
    if (!_orchestrationActive || !mounted) {
      return;
    }
    final actionKey = _currentBackendActionKey();
    if (actionKey.isNotEmpty && actionKey != _lastBackendActionKey) {
      _lastBackendActionKey = actionKey;
      _awaitingBackendConfirmation = true;
      final message = _buildBackendConfirmationMessage();
      unawaited(_announceAutomationMessage(
        message,
        status: 'AI 正在等待你的确认',
      ));
      return;
    }
    if (actionKey.isEmpty && _lastBackendActionKey.isNotEmpty) {
      _lastBackendActionKey = '';
    }
    final reply = _latestBackendReply();
    if (reply == null ||
        reply.id == _lastBackendReplyId ||
        !_replyBelongsToCurrentOrchestration(reply)) {
      return;
    }
    _lastBackendReplyId = reply.id;
    if (controller.isSessionBusy ||
        controller.awaitInput ||
        controller.hasPendingPermissionPrompt ||
        controller.hasPendingPlanPrompt ||
        controller.hasPendingPlanQuestions ||
        controller.shouldShowReviewChoices) {
      return;
    }
    unawaited(_announceAutomationMessage(
      _buildBackendReplyMessage(reply),
      status: 'AI 已回复，可以继续口头补充',
    ));
  }

  Future<void> _announceAutomationMessage(
    String message, {
    required String status,
  }) async {
    final content = message.trim();
    if (content.isEmpty || !mounted) {
      return;
    }
    setState(() {
      _turns.add(_VoiceTurn(role: 'assistant', content: content));
      _status = status;
    });
    final config = _configFromFields();
    if (config.hasVoiceTtsConfig) {
      await _speakAssistantReply(config, content);
    } else {
      _scheduleAutoListen();
    }
  }

  String _currentBackendActionKey() {
    final prompt = controller.pendingPrompt;
    final interaction = controller.pendingInteraction;
    if (controller.hasPendingPermissionPrompt) {
      final requestId =
          prompt?.runtimeMeta.permissionRequestId.trim().isNotEmpty == true
              ? prompt!.runtimeMeta.permissionRequestId.trim()
              : interaction?.runtimeMeta.permissionRequestId.trim() ?? '';
      return 'permission:${requestId.isEmpty ? _promptTextHash() : requestId}';
    }
    if (controller.shouldShowReviewChoices) {
      final diff = controller.reviewActionTargetDiff;
      return 'review:${diff?.id ?? diff?.path ?? 'current'}';
    }
    if (controller.hasPendingPlanQuestions || controller.hasPendingPlanPrompt) {
      final question = controller.currentPendingPlanQuestion;
      return 'plan:${controller.pendingPlanQuestionIndex}:${question?.id ?? _promptTextHash()}';
    }
    return '';
  }

  String _promptTextHash() {
    final prompt = controller.pendingPrompt;
    final interaction = controller.pendingInteraction;
    final message = [
      prompt?.message ?? '',
      interaction?.message ?? '',
      interaction?.title ?? '',
    ].join('\n').trim();
    return message.hashCode.toString();
  }

  String _buildBackendConfirmationMessage() {
    if (controller.hasPendingPermissionPrompt) {
      final message = _backendPromptMessage();
      return [
        'AI 需要权限确认。',
        if (message.isNotEmpty) message,
        '你可以说允许、拒绝，或者说仅本次允许。',
      ].join('\n');
    }
    if (controller.shouldShowReviewChoices) {
      final diff = controller.reviewActionTargetDiff;
      return [
        'AI 已产生需要审核的改动。',
        if (diff?.path.trim().isNotEmpty == true) '文件：${diff!.path}',
        '你可以说接受、继续修改，或者回退。',
      ].join('\n');
    }
    if (controller.hasPendingPlanQuestions || controller.hasPendingPlanPrompt) {
      final question = controller.currentPendingPlanQuestion;
      final message = question?.displayLabel.trim().isNotEmpty == true
          ? question!.displayLabel.trim()
          : _backendPromptMessage();
      final options = question?.options ?? _backendPromptOptions();
      final optionText = _formatPromptOptions(options);
      return [
        'AI 需要你确认计划选项。',
        if (message.isNotEmpty) message,
        if (optionText.isNotEmpty) optionText,
      ].join('\n');
    }
    final message = _backendPromptMessage();
    return [
      'AI 需要你补充信息。',
      if (message.isNotEmpty) message,
    ].join('\n');
  }

  String _backendPromptMessage() {
    final prompt = controller.pendingPrompt;
    final interaction = controller.pendingInteraction;
    return [
      interaction?.title ?? '',
      interaction?.message ?? '',
      prompt?.message ?? '',
    ].where((value) => value.trim().isNotEmpty).join('\n').trim();
  }

  List<PromptOption> _backendPromptOptions() {
    final promptOptions = controller.pendingPrompt?.options ?? const [];
    if (promptOptions.isNotEmpty) {
      return promptOptions;
    }
    return controller.pendingInteraction?.options ?? const [];
  }

  String _formatPromptOptions(List<PromptOption> options) {
    if (options.isEmpty) {
      return '';
    }
    final labels = <String>[];
    for (var index = 0; index < options.length; index++) {
      final option = options[index];
      final label = option.displayText.trim().isNotEmpty
          ? option.displayText.trim()
          : option.value.trim();
      if (label.isNotEmpty) {
        labels.add('${index + 1}. $label');
      }
    }
    return labels.isEmpty ? '' : '可选项：${labels.join('；')}';
  }

  TimelineItem? _latestBackendReply() {
    for (final item in controller.timeline.reversed) {
      final kind = item.kind.trim().toLowerCase();
      if (kind == 'assistant_reply' || kind == 'markdown') {
        final body = item.body.trim();
        if (body.isNotEmpty) {
          return item;
        }
      }
    }
    return null;
  }

  bool _replyBelongsToCurrentOrchestration(TimelineItem reply) {
    final startedAt = _orchestrationStartedAt;
    if (startedAt == null) {
      return true;
    }
    return !reply.timestamp.isBefore(startedAt);
  }

  String _buildBackendReplyMessage(TimelineItem reply) {
    final preview = _compactForVoice(reply.body);
    return [
      'AI 有新回复。',
      if (preview.isNotEmpty) preview,
      '要继续的话直接告诉我下一步要补充什么；如果已经好了，可以说结束接管。',
    ].join('\n');
  }

  String _compactForVoice(String value) {
    final normalized = value
        .replaceAll(RegExp(r'```[\s\S]*?```'), '代码片段已省略。')
        .replaceAll(RegExp(r'\s+'), ' ')
        .trim();
    if (normalized.length <= 220) {
      return normalized;
    }
    return '${normalized.substring(0, 220)}…';
  }

  String _normalizeBackendConfirmation(String userText) {
    if (controller.shouldShowReviewChoices) {
      return _normalizeReviewDecision(userText);
    }
    if (controller.hasPendingPermissionPrompt) {
      return _normalizePermissionDecision(userText);
    }
    final matchedOption = _matchPromptOption(userText);
    if (matchedOption.isNotEmpty) {
      return matchedOption;
    }
    return userText.trim();
  }

  String _normalizePermissionDecision(String userText) {
    final normalized = userText.trim().toLowerCase();
    if (_containsAny(
        normalized, const ['拒绝', '不同意', '不要', '不行', 'deny', 'no'])) {
      return 'deny';
    }
    final persistent = _containsAny(
      normalized,
      const ['永久', '一直', '以后', 'persistent', 'always'],
    );
    if (_containsAny(
      normalized,
      const ['允许', '同意', '可以', '继续', '批准', 'approve', 'allow', 'ok', 'yes'],
    )) {
      return persistent ? 'approve:persistent' : 'approve:session';
    }
    return userText.trim();
  }

  String _normalizeReviewDecision(String userText) {
    final normalized = userText.trim().toLowerCase();
    if (_containsAny(normalized, const ['回退', '撤销', '不要', '拒绝', 'revert'])) {
      return 'revert';
    }
    if (_containsAny(normalized, const ['修改', '继续改', '调整', 'revise'])) {
      return 'revise';
    }
    if (_containsAny(normalized,
        const ['接受', '同意', '可以', '通过', 'accept', 'approve', 'ok'])) {
      return 'accept';
    }
    return userText.trim();
  }

  String _matchPromptOption(String userText) {
    final normalized = userText.trim().toLowerCase();
    final options = controller.currentPendingPlanQuestion?.options ??
        _backendPromptOptions();
    if (options.isEmpty) {
      return '';
    }
    final numeric = RegExp(r'\d+').firstMatch(normalized);
    if (numeric != null) {
      final index = int.tryParse(numeric.group(0) ?? '') ?? 0;
      if (index > 0 && index <= options.length) {
        return _optionSubmissionValue(options[index - 1]);
      }
    }
    const ordinalMap = {
      '第一': 0,
      '第一个': 0,
      '第二': 1,
      '第二个': 1,
      '第三': 2,
      '第三个': 2,
      '第四': 3,
      '第四个': 3,
    };
    for (final entry in ordinalMap.entries) {
      if (normalized.contains(entry.key) && entry.value < options.length) {
        return _optionSubmissionValue(options[entry.value]);
      }
    }
    for (final option in options) {
      final value = option.value.trim();
      final label = option.displayText.trim();
      if ((value.isNotEmpty && normalized.contains(value.toLowerCase())) ||
          (label.isNotEmpty && normalized.contains(label.toLowerCase()))) {
        return _optionSubmissionValue(option);
      }
    }
    return '';
  }

  String _optionSubmissionValue(PromptOption option) {
    if (option.value.trim().isNotEmpty) {
      return option.value.trim();
    }
    return option.displayText.trim();
  }

  bool _containsAny(String value, List<String> needles) {
    for (final needle in needles) {
      if (value.contains(needle)) {
        return true;
      }
    }
    return false;
  }

  bool _looksLikeAutoHandoffTrigger(String userText) {
    final normalized = userText.trim().toLowerCase();
    return _containsAny(normalized, const [
      '开始执行',
      '开始接管',
      '自动接管',
      '交给 ai',
      '交给ai',
      '交给 claude',
      '交给 codex',
      '帮我执行',
      '帮我修改',
      '开始修改',
      '执行吧',
      '修改吧',
      'run it',
      'start',
    ]);
  }

  bool _looksLikeStopOrchestrationCommand(String userText) {
    final normalized = userText.trim().toLowerCase();
    return _containsAny(normalized, const [
      '停止',
      '停一下',
      '结束接管',
      '结束自动',
      '退出接管',
      '不用继续',
      'stop',
      'cancel takeover',
    ]);
  }

  String _buildBackendFollowUpPrompt(String userText) {
    final engine = controller.config.engine.trim().isEmpty
        ? 'AI'
        : controller.config.engine.trim();
    return [
      '来源：MobileVC 语音通话自动接管。',
      '用户在听取 $engine 当前回复后追加了新的口头提示词。',
      '请结合当前会话上下文继续推进，不要重复已经完成的工作。',
      '',
      '用户补充：${userText.trim()}',
    ].join('\n');
  }

  AppConfig _configFromFields() {
    return controller.config.copyWith(
      voiceApiUrl: _voiceApiUrlController.text.trim(),
      voiceApiKey: _voiceApiKeyController.text.trim(),
      voiceModelName: _voiceModelController.text.trim(),
      voiceTtsUrl: _ttsUrlController.text.trim(),
      voiceTtsApiKey: _ttsApiKeyController.text.trim(),
      voiceTtsModelName: _ttsModelController.text.trim(),
      voiceTtsVoice: _ttsVoiceController.text.trim().isEmpty
          ? 'alloy'
          : _ttsVoiceController.text.trim(),
      permissionMode: _permissionMode,
    );
  }

  List<VoiceChatMessage> _buildVoiceMessages() {
    final engine = controller.config.engine.trim().isEmpty
        ? 'claude'
        : controller.config.engine.trim();
    return [
      VoiceChatMessage(
        role: 'system',
        content: [
          '你是 MobileVC 的任务预沟通语音助手。',
          '你只负责在任务开始前和用户澄清目标、约束、风险、权限模式和执行边界。',
          '基座执行能力来自用户电脑上的原生 Claude Code / Codex CLI，不要声称你自己正在执行代码。',
          '当前目标执行引擎：$engine。',
          '回复要简短、适合语音播放；信息不足时继续追问。',
          '当信息足够时，输出一段可交给 $engine 的明确执行指令。',
        ].join('\n'),
      ),
      ..._turns.map(
        (turn) => VoiceChatMessage(role: turn.role, content: turn.content),
      ),
    ];
  }

  String _buildHandoffPrompt({String extraUserText = ''}) {
    final engine = controller.config.engine.trim().isEmpty
        ? 'AI'
        : controller.config.engine.trim();
    final buffer = StringBuffer()
      ..writeln('来源：MobileVC 语音通话预沟通。')
      ..writeln('请基于下面已确认的信息开始执行任务。')
      ..writeln('基座执行能力仍使用这台电脑上的原生 $engine CLI。')
      ..writeln('权限模式：$_permissionMode。')
      ..writeln()
      ..writeln('通话记录：');
    for (final turn in _turns) {
      final speaker = turn.role == 'user' ? '用户' : '语音助手';
      buffer.writeln('$speaker：${turn.content}');
    }
    if (extraUserText.trim().isNotEmpty) {
      buffer.writeln('用户补充：${extraUserText.trim()}');
    }
    buffer
      ..writeln()
      ..writeln('执行要求：')
      ..writeln('1. 先按通话记录确认任务目标、关键约束和权限边界。')
      ..writeln('2. 如果信息仍不足，先追问；如果已经足够，直接开始执行。')
      ..writeln('3. 需要权限或计划确认时，按 MobileVC 当前权限模式和界面提示处理。');
    return buffer.toString().trim();
  }
}

class _VoiceTurn {
  const _VoiceTurn({
    required this.role,
    required this.content,
  });

  final String role;
  final String content;
}

enum _VoiceCallStage {
  needsConfig,
  idle,
  listening,
  thinking,
  speaking,
}
