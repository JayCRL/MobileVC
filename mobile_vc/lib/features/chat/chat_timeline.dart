import 'package:flutter/material.dart';
import '../../data/models/events.dart';
import '../../data/models/session_models.dart';
import '../../widgets/event_card.dart';

class ChatTimeline extends StatefulWidget {
  const ChatTimeline({
    super.key,
    required this.items,
    this.activeReviewDiff,
    this.activeReviewGroup,
    this.pendingDiffCount = 0,
    this.pendingReviewGroupCount = 0,
    this.isManualReviewMode = true,
    this.isAutoAcceptMode = false,
    this.pendingPrompt,
    this.pendingInteraction,
    this.shouldShowReviewChoices = false,
    this.onOpenDiff,
    this.onOpenRuntimeInfo,
    this.onOpenFile,
    this.onReviewDecision,
    this.onAcceptAll,
    this.onPromptSubmit,
  });

  final List<TimelineItem> items;
  final HistoryContext? activeReviewDiff;
  final ReviewGroup? activeReviewGroup;
  final int pendingDiffCount;
  final int pendingReviewGroupCount;
  final bool isManualReviewMode;
  final bool isAutoAcceptMode;
  final PromptRequestEvent? pendingPrompt;
  final InteractionRequestEvent? pendingInteraction;
  final bool shouldShowReviewChoices;
  final VoidCallback? onOpenDiff;
  final VoidCallback? onOpenRuntimeInfo;
  final VoidCallback? onOpenFile;
  final ValueChanged<String>? onReviewDecision;
  final VoidCallback? onAcceptAll;
  final ValueChanged<String>? onPromptSubmit;

  @override
  State<ChatTimeline> createState() => _ChatTimelineState();
}

class _ChatTimelineState extends State<ChatTimeline> {
  final ScrollController _scrollController = ScrollController();
  int _lastCount = 0;

  @override
  void initState() {
    super.initState();
    _lastCount = widget.items.length;
  }

  @override
  void didUpdateWidget(covariant ChatTimeline oldWidget) {
    super.didUpdateWidget(oldWidget);
    final currentCount = widget.items.length +
        ((widget.pendingInteraction?.hasVisiblePrompt == true ||
                widget.pendingPrompt?.hasVisiblePrompt == true)
            ? 1
            : 0);
    final previousCount = oldWidget.items.length +
        ((oldWidget.pendingInteraction?.hasVisiblePrompt == true ||
                oldWidget.pendingPrompt?.hasVisiblePrompt == true)
            ? 1
            : 0);
    if (currentCount > previousCount || widget.items.length > _lastCount) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!_scrollController.hasClients) {
          return;
        }
        _scrollController.jumpTo(_scrollController.position.maxScrollExtent);
      });
    }
    _lastCount = widget.items.length;
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final visiblePrompt = widget.shouldShowReviewChoices
        ? null
        : (widget.pendingInteraction?.hasVisiblePrompt == true
            ? widget.pendingInteraction
            : (widget.pendingPrompt?.hasVisiblePrompt == true
                ? widget.pendingPrompt
                : null));
    final visiblePromptMessage = visiblePrompt is InteractionRequestEvent
        ? visiblePrompt.message
        : visiblePrompt is PromptRequestEvent
            ? visiblePrompt.message
            : '';
    final reviewAnchorIndex = _reviewAnchorIndex(widget.items);
    final items = [
      for (var i = 0; i < widget.items.length; i++) ...[
        widget.items[i],
        if (reviewAnchorIndex == i && widget.activeReviewDiff != null)
          TimelineItem(
            id:
                'review-summary-${widget.activeReviewDiff!.id}-${widget.pendingDiffCount}',
            kind: 'review_summary',
            timestamp: widget.items[i].timestamp,
            title: widget.activeReviewDiff!.title,
            body: widget.activeReviewDiff!.path,
            context: widget.activeReviewDiff,
          ),
      ],
      if (reviewAnchorIndex == -1 && widget.activeReviewDiff != null)
        TimelineItem(
          id: 'review-summary-tail-${widget.activeReviewDiff!.id}-${widget.pendingDiffCount}',
          kind: 'review_summary',
          timestamp: DateTime.now(),
          title: widget.activeReviewDiff!.title,
          body: widget.activeReviewDiff!.path,
          context: widget.activeReviewDiff,
        ),
      if (visiblePrompt != null)
        TimelineItem(
          id: 'pending-prompt-${visiblePrompt.timestamp.microsecondsSinceEpoch}',
          kind: visiblePrompt is InteractionRequestEvent
              ? 'interaction_request'
              : 'prompt_request',
          timestamp: visiblePrompt.timestamp,
          title: visiblePrompt is InteractionRequestEvent
              ? (visiblePrompt.title.isNotEmpty
                  ? visiblePrompt.title
                  : '交互确认')
              : '授权确认',
          body: visiblePromptMessage,
          meta: visiblePrompt.runtimeMeta,
        ),
    ];
    if (items.isEmpty) {
      return const SizedBox.shrink();
    }
    return ListView.separated(
      controller: _scrollController,
      reverse: false,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 120),
      itemBuilder: (context, index) {
        final item = items[index];
        if (item.kind == 'file_diff') {
          return const SizedBox.shrink();
        }
        if (item.kind == 'review_summary') {
          return _ReviewSummaryCard(
            diff: item.context,
            reviewGroup: widget.activeReviewGroup,
            pendingDiffCount: widget.pendingDiffCount,
            pendingReviewGroupCount: widget.pendingReviewGroupCount,
            isManualReviewMode: widget.isManualReviewMode,
            isAutoAcceptMode: widget.isAutoAcceptMode,
            shouldShowReviewChoices: widget.shouldShowReviewChoices,
            onOpenDiff: widget.onOpenDiff,
            onReviewDecision: widget.onReviewDecision,
            onAcceptAll: widget.onAcceptAll,
          );
        }
        if ((item.kind == 'prompt_request' || item.kind == 'interaction_request') &&
            visiblePrompt != null) {
          return visiblePrompt is InteractionRequestEvent
              ? _InteractionRequestCard(
                  interaction: visiblePrompt,
                  onSubmit: widget.onPromptSubmit,
                )
              : _PromptRequestCard(
                  prompt: visiblePrompt as PromptRequestEvent,
                  onSubmit: widget.onPromptSubmit,
                );
        }
        return EventCard(
          item: item,
          onTap: () {
            if (item.kind == 'runtime_info_result') {
              widget.onOpenRuntimeInfo?.call();
            } else if (item.kind == 'fs_read_result') {
              widget.onOpenFile?.call();
            }
          },
        );
      },
      separatorBuilder: (_, __) => const SizedBox(height: 12),
      itemCount: items.length,
    );
  }

  int _reviewAnchorIndex(List<TimelineItem> items) {
    if (widget.activeReviewDiff == null) {
      return -1;
    }
    for (var i = items.length - 1; i >= 0; i--) {
      final item = items[i];
      if (item.kind != 'file_diff') {
        continue;
      }
      final context = item.context;
      if (context == null) {
        continue;
      }
      if (_sameDiff(context, widget.activeReviewDiff!)) {
        return i;
      }
    }
    return items.isEmpty ? -1 : items.length - 1;
  }

  bool _sameDiff(HistoryContext left, HistoryContext right) {
    if (left.id.isNotEmpty && right.id.isNotEmpty) {
      return left.id == right.id;
    }
    return left.path == right.path;
  }
}

class _InteractionRequestCard extends StatelessWidget {
  const _InteractionRequestCard({
    required this.interaction,
    this.onSubmit,
  });

  final InteractionRequestEvent interaction;
  final ValueChanged<String>? onSubmit;

  @override
  Widget build(BuildContext context) {
    final actions = interaction.actions;
    final options = interaction.options
        .where((option) => option.displayText.isNotEmpty)
        .toList();
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHigh,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: Theme.of(context)
              .colorScheme
              .outlineVariant
              .withValues(alpha: 0.5),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (interaction.title.trim().isNotEmpty) ...[
            Text(
              interaction.title.trim(),
              style: Theme.of(context)
                  .textTheme
                  .titleSmall
                  ?.copyWith(fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 8),
          ],
          if (interaction.message.trim().isNotEmpty)
            Text(interaction.message.trim()),
          if (actions.isNotEmpty) ...[
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: actions
                  .map(
                    (action) => _InteractionActionButton(
                      action: action,
                      onPressed: onSubmit == null
                          ? null
                          : () => onSubmit!(action.decision.isNotEmpty
                              ? action.decision
                              : (action.value.isNotEmpty
                                  ? action.value
                                  : action.id)),
                    ),
                  )
                  .toList(),
            ),
          ] else if (options.isNotEmpty) ...[
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: options
                  .map((option) => _PromptOptionButton(
                        option: option,
                        onPressed: onSubmit == null
                            ? null
                            : () => onSubmit!(option.value),
                      ))
                  .toList(),
            ),
          ],
        ],
      ),
    );
  }
}

class _InteractionActionButton extends StatelessWidget {
  const _InteractionActionButton({
    required this.action,
    this.onPressed,
  });

  final InteractionAction action;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    switch (action.variant) {
      case 'primary':
        return FilledButton(onPressed: onPressed, child: Text(action.displayLabel));
      case 'tonal':
        return FilledButton.tonal(
            onPressed: onPressed, child: Text(action.displayLabel));
      default:
        return OutlinedButton(onPressed: onPressed, child: Text(action.displayLabel));
    }
  }
}

class _PromptRequestCard extends StatelessWidget {
  const _PromptRequestCard({
    required this.prompt,
    this.onSubmit,
  });

  final PromptRequestEvent prompt;
  final ValueChanged<String>? onSubmit;

  @override
  Widget build(BuildContext context) {
    final options = _resolvedOptions();
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHigh,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: Theme.of(context)
              .colorScheme
              .outlineVariant
              .withValues(alpha: 0.5),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (prompt.message.trim().isNotEmpty) ...[
            Text(
              prompt.message.trim(),
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ],
          if (options.isNotEmpty) ...[
            if (prompt.message.trim().isNotEmpty) const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: options
                  .map((option) => _PromptOptionButton(
                        option: option,
                        onPressed: onSubmit == null
                            ? null
                            : () => onSubmit!(option.value),
                      ))
                  .toList(),
            ),
          ],
        ],
      ),
    );
  }

  List<PromptOption> _resolvedOptions() {
    final options = prompt.options
        .where((option) => option.displayText.isNotEmpty)
        .toList(growable: false);
    if (options.isNotEmpty) {
      return options;
    }
    if (prompt.looksLikePermissionPrompt) {
      return const [
        PromptOption(value: 'y', label: '允许'),
        PromptOption(value: 'n', label: '拒绝'),
      ];
    }
    return const [];
  }
}

class _PromptOptionButton extends StatelessWidget {
  const _PromptOptionButton({
    required this.option,
    this.onPressed,
  });

  final PromptOption option;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final style = _styleForValue(option.value);
    switch (style) {
      case _PromptButtonStyle.primary:
        return FilledButton(
          onPressed: onPressed,
          child: Text(_labelForOption(option)),
        );
      case _PromptButtonStyle.tonal:
        return FilledButton.tonal(
          onPressed: onPressed,
          child: Text(_labelForOption(option)),
        );
      case _PromptButtonStyle.outlined:
        return OutlinedButton(
          onPressed: onPressed,
          child: Text(_labelForOption(option)),
        );
    }
  }

  _PromptButtonStyle _styleForValue(String raw) {
    final value = raw.trim().toLowerCase();
    if (_approveValues.contains(value)) {
      return _PromptButtonStyle.primary;
    }
    if (_denyValues.contains(value)) {
      return _PromptButtonStyle.tonal;
    }
    return _PromptButtonStyle.outlined;
  }

  String _labelForOption(PromptOption option) {
    final value = option.value.trim().toLowerCase();
    if (_approveValues.contains(value)) {
      return '允许';
    }
    if (_denyValues.contains(value)) {
      return '拒绝';
    }
    return option.displayText;
  }
}

enum _PromptButtonStyle { primary, tonal, outlined }

const Set<String> _approveValues = {
  'y',
  'yes',
  'ok',
  'approve',
  'approved',
  'allow',
  'accept',
  'continue',
};

const Set<String> _denyValues = {
  'n',
  'no',
  'deny',
  'denied',
  'reject',
  'cancel',
  'stop',
};

class _ReviewSummaryCard extends StatelessWidget {
  const _ReviewSummaryCard({
    required this.diff,
    required this.reviewGroup,
    required this.pendingDiffCount,
    required this.pendingReviewGroupCount,
    required this.isManualReviewMode,
    required this.isAutoAcceptMode,
    required this.shouldShowReviewChoices,
    this.onOpenDiff,
    this.onReviewDecision,
    this.onAcceptAll,
  });

  final HistoryContext? diff;
  final ReviewGroup? reviewGroup;
  final int pendingDiffCount;
  final int pendingReviewGroupCount;
  final bool isManualReviewMode;
  final bool isAutoAcceptMode;
  final bool shouldShowReviewChoices;
  final VoidCallback? onOpenDiff;
  final ValueChanged<String>? onReviewDecision;
  final VoidCallback? onAcceptAll;

  @override
  Widget build(BuildContext context) {
    final reviewDiff = diff;
    if (reviewDiff == null) {
      return const SizedBox.shrink();
    }
    final isSingle = pendingDiffCount <= 1;
    final group = reviewGroup;
    final groupFileCount = group?.files.length ?? 0;
    final pendingLabelCount = groupFileCount > 0 ? groupFileCount : pendingDiffCount;
    final showReviewButtons =
        isSingle && isManualReviewMode && shouldShowReviewChoices;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(
          color: Theme.of(context)
              .colorScheme
              .outlineVariant
              .withValues(alpha: 0.55),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: Theme.of(context).colorScheme.primaryContainer,
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Icon(
                  Icons.rate_review_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      pendingLabelCount > 1 ? '待审核改动已聚合' : '待审核改动',
                      style: Theme.of(context).textTheme.titleSmall?.copyWith(
                            fontWeight: FontWeight.w800,
                          ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      pendingLabelCount > 1
                          ? '当前有 $pendingLabelCount 个文件待审核，可进入 differ 逐个处理。'
                          : '当前文件已准备好审核，可直接在这里完成操作。',
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: Theme.of(context)
                                .colorScheme
                                .onSurfaceVariant,
                            height: 1.45,
                          ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            reviewDiff.title.isNotEmpty ? reviewDiff.title : '当前改动',
            style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
          ),
          if (reviewDiff.path.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              reviewDiff.path,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
          ],
          if (group != null) ...[
            const SizedBox(height: 8),
            Text(
              group.title.isNotEmpty ? group.title : '当前修改组',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
            ),
            const SizedBox(height: 4),
            Text(
              pendingReviewGroupCount > 1
                  ? '当前共有 $pendingReviewGroupCount 组修改待处理，本组剩余 ${group.pendingCount} 个文件。'
                  : '本组共 ${group.files.length} 个文件，剩余 ${group.pendingCount} 个待处理。',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
          ],
          const SizedBox(height: 12),
          if (showReviewButtons)
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton(
                  onPressed: () => onReviewDecision?.call('accept'),
                  child: const Text('同意'),
                ),
                FilledButton.tonal(
                  onPressed: () => onReviewDecision?.call('revert'),
                  child: const Text('撤销'),
                ),
                OutlinedButton(
                  onPressed: () => onReviewDecision?.call('revise'),
                  child: const Text('继续调整'),
                ),
              ],
            )
          else
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.tonalIcon(
                  onPressed: onOpenDiff,
                  icon: const Icon(Icons.difference_outlined, size: 16),
                  label: Text(pendingLabelCount > 1 ? '进入 differ 处理' : '查看 diff'),
                ),
                if (pendingLabelCount > 1)
                  FilledButton(
                    onPressed: onAcceptAll,
                    child: const Text('一键接受并继续'),
                  ),
              ],
            ),
          const SizedBox(height: 10),
          Text(
            _statusText(),
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
    );
  }

  String _statusText() {
    final group = reviewGroup;
    final groupFileCount = group?.files.length ?? 0;
    final pendingLabelCount = groupFileCount > 0 ? groupFileCount : pendingDiffCount;
    if (isAutoAcceptMode) {
      return '自动接受修改模式已开启，新的 diff 会自动确认。';
    }
    if (!isManualReviewMode) {
      return '当前模式不需要手动确认 diff。';
    }
    if (pendingDiffCount > 1 || pendingLabelCount > 1) {
      return shouldShowReviewChoices
          ? '聊天流已聚合本轮审核，底层仍会逐条发送 review_decision。'
          : '等待审核上下文进入输入态后继续处理。';
    }
    return shouldShowReviewChoices ? '当前 diff 正在等待审核。' : '等待当前审核上下文激活';
  }
}

