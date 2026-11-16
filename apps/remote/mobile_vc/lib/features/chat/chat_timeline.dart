import 'package:flutter/material.dart';

import '../../data/models/events.dart';
import '../../data/models/runtime_meta.dart';
import '../../data/models/session_models.dart';
import '../../widgets/event_card.dart';

const List<PromptOption> _permissionPromptOptions = <PromptOption>[
  PromptOption(value: 'approve', label: '允许一次'),
  PromptOption(value: 'approve:session', label: '本会话允许'),
  PromptOption(value: 'approve:persistent', label: '长期允许'),
  PromptOption(value: 'deny', label: '拒绝'),
];

class ChatTimelineDiagnostics {
  int childIndexMapBuilds = 0;
  int reviewAnchorFullScans = 0;

  void reset() {
    childIndexMapBuilds = 0;
    reviewAnchorFullScans = 0;
  }
}

class ChatTimeline extends StatefulWidget {
  const ChatTimeline({
    super.key,
    required this.items,
    required this.sessionId,
    this.mediaPreviewStates = const {},
    this.activeReviewDiff,
    this.activeReviewGroup,
    this.pendingDiffCount = 0,
    this.pendingReviewGroupCount = 0,
    this.isManualReviewMode = true,
    this.isAutoAcceptMode = false,
    this.pendingPrompt,
    this.pendingInteraction,
    this.shouldShowReviewChoices = false,
    this.pendingPlanQuestion,
    this.pendingPlanProgressLabel = '',
    this.shouldShowPlanChoices = false,
    this.isAiRunning = false,
    this.aiStatusLabel = '',
    this.bottomPadding = 128,
    this.hasOlderItems = false,
    this.isLoadingOlderItems = false,
    this.onOpenDiff,
    this.onOpenRuntimeInfo,
    this.onOpenFile,
    this.onOpenAttachment,
    this.onRequestMediaPreview,
    this.onLoadOlderItems,
    this.onReviewDecision,
    this.onAcceptAll,
    this.onPromptSubmit,
    this.diagnostics,
  });

  final List<TimelineItem> items;
  final String sessionId;
  final Map<String, MediaPreviewState> mediaPreviewStates;
  final HistoryContext? activeReviewDiff;
  final ReviewGroup? activeReviewGroup;
  final int pendingDiffCount;
  final int pendingReviewGroupCount;
  final bool isManualReviewMode;
  final bool isAutoAcceptMode;
  final PromptRequestEvent? pendingPrompt;
  final InteractionRequestEvent? pendingInteraction;
  final bool shouldShowReviewChoices;
  final PlanQuestion? pendingPlanQuestion;
  final String pendingPlanProgressLabel;
  final bool shouldShowPlanChoices;
  final bool isAiRunning;
  final String aiStatusLabel;
  final double bottomPadding;
  final bool hasOlderItems;
  final bool isLoadingOlderItems;
  final VoidCallback? onOpenDiff;
  final VoidCallback? onOpenRuntimeInfo;
  final VoidCallback? onOpenFile;
  final ValueChanged<TimelineAttachment>? onOpenAttachment;
  final ValueChanged<TimelineAttachment>? onRequestMediaPreview;
  final VoidCallback? onLoadOlderItems;
  final ValueChanged<String>? onReviewDecision;
  final VoidCallback? onAcceptAll;
  final ValueChanged<String>? onPromptSubmit;
  final ChatTimelineDiagnostics? diagnostics;

  @override
  State<ChatTimeline> createState() => _ChatTimelineState();
}

class _ChatTimelineState extends State<ChatTimeline> {
  final ScrollController _scrollController = ScrollController();
  int _lastItemCount = 0;
  int _lastResolvedItemCount = 0;
  String _lastBottomItemLayoutKey = '';
  String _lastTimelineHistoryKey = '';
  String _lastFirstItemId = '';
  String _lastLastItemId = '';
  bool _scrollToBottomScheduled = false;
  int _anchorItemsLength = -1;
  String _anchorDiffKey = '';
  String _anchorItemsKey = '';
  int _cachedReviewAnchorIndex = -1;
  String _childIndexCacheKey = '';
  Map<String, int> _childIndexByKey = const <String, int>{};

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_handleScroll);
    _rememberTimelineState(widget.items);
    if (widget.items.isNotEmpty) {
      _scrollToBottomAfterFrame();
    }
  }

  @override
  void dispose() {
    _scrollController
      ..removeListener(_handleScroll)
      ..dispose();
    super.dispose();
  }

  void _handleScroll() {
    if (!widget.hasOlderItems || widget.isLoadingOlderItems) {
      return;
    }
    if (!_scrollController.hasClients) {
      return;
    }
    if (_scrollController.position.pixels <= 80) {
      widget.onLoadOlderItems?.call();
    }
  }

  @override
  void didUpdateWidget(covariant ChatTimeline oldWidget) {
    super.didUpdateWidget(oldWidget);
    final previousItemCount = _lastItemCount;
    final previousResolvedItemCount = _lastResolvedItemCount;
    final previousBottomItemLayoutKey = _lastBottomItemLayoutKey;
    final previousHistoryKey = _lastTimelineHistoryKey;
    final previousFirstItemId = _lastFirstItemId;
    final previousLastItemId = _lastLastItemId;
    final currentHistoryKey = _timelineHistoryKey(widget.items);
    final currentLayout = _currentResolvedTimelineLayout();
    final switchedHistory = currentHistoryKey != previousHistoryKey;
    final switchedSession =
        widget.sessionId.trim() != oldWidget.sessionId.trim();
    final addedOlderItems = widget.items.length > previousItemCount &&
        widget.items.isNotEmpty &&
        previousItemCount > 0 &&
        widget.items.last.id == previousLastItemId &&
        widget.items.first.id != previousFirstItemId;
    if (!addedOlderItems &&
        (currentLayout.itemCount > previousResolvedItemCount ||
            currentLayout.bottomItemLayoutKey != previousBottomItemLayoutKey ||
            widget.items.length > previousItemCount ||
            switchedHistory ||
            switchedSession)) {
      _scrollToBottomAfterFrame();
    }
    _rememberTimelineState(widget.items);
  }

  void _rememberTimelineState(List<TimelineItem> items) {
    final currentLayout = _currentResolvedTimelineLayout();
    _lastItemCount = items.length;
    _lastResolvedItemCount = currentLayout.itemCount;
    _lastBottomItemLayoutKey = currentLayout.bottomItemLayoutKey;
    _lastTimelineHistoryKey = _timelineHistoryKey(items);
    _lastFirstItemId = items.isEmpty ? '' : items.first.id;
    _lastLastItemId = items.isEmpty ? '' : items.last.id;
  }

  _TimelineLayoutSnapshot _currentResolvedTimelineLayout() {
    final visiblePrompt = _visiblePrompt();
    final visiblePlanQuestion = _visiblePlanQuestion();
    final reviewAnchorIndex = _cachedReviewAnchorIndexFor(widget.items);
    final extraItems = _extraTimelineItems(
      reviewAnchorIndex: reviewAnchorIndex,
      visiblePrompt: visiblePrompt,
      visiblePromptMessage: _visiblePromptMessage(visiblePrompt),
      visiblePlanQuestion: visiblePlanQuestion,
    );
    final itemCount = _resolvedItemCount(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    if (itemCount == 0) {
      return const _TimelineLayoutSnapshot(
        itemCount: 0,
        bottomItemLayoutKey: '',
      );
    }
    final bottomItem = _timelineItemAt(
      itemCount - 1,
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    return _TimelineLayoutSnapshot(
      itemCount: itemCount,
      bottomItemLayoutKey: _timelineBottomLayoutFingerprint(
        bottomItem,
        visiblePrompt: visiblePrompt,
        visiblePlanQuestion: visiblePlanQuestion,
      ),
    );
  }

  void _scrollToBottomAfterFrame() {
    if (_scrollToBottomScheduled) {
      return;
    }
    _scrollToBottomScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) {
        _scrollToBottomScheduled = false;
        return;
      }
      _jumpToBottom();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _scrollToBottomScheduled = false;
        if (!mounted) {
          return;
        }
        _jumpToBottom();
      });
    });
  }

  void _jumpToBottom() {
    if (!mounted) {
      return;
    }
    if (!_scrollController.hasClients) {
      return;
    }
    _scrollController.jumpTo(_scrollController.position.maxScrollExtent);
  }

  void _handleAnimatedBodyProgress() {
    if (!_isNearBottom()) {
      return;
    }
    _scrollToBottomAfterFrame();
  }

  bool _isNearBottom() {
    if (!_scrollController.hasClients) {
      return true;
    }
    final position = _scrollController.position;
    final distance = position.maxScrollExtent - position.pixels;
    return distance <= 96;
  }

  @override
  Widget build(BuildContext context) {
    final visiblePrompt = _visiblePrompt();
    final visiblePlanQuestion = _visiblePlanQuestion();
    final visiblePromptMessage = _visiblePromptMessage(visiblePrompt);
    final reviewAnchorIndex = _cachedReviewAnchorIndexFor(widget.items);
    final extraItems = _extraTimelineItems(
      reviewAnchorIndex: reviewAnchorIndex,
      visiblePrompt: visiblePrompt,
      visiblePromptMessage: visiblePromptMessage,
      visiblePlanQuestion: visiblePlanQuestion,
    );
    final resolvedItemCount = _resolvedItemCount(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    if (resolvedItemCount == 0 && !widget.isAiRunning) {
      return const SizedBox.shrink();
    }
    final listView = ListView.separated(
      controller: _scrollController,
      reverse: false,
      padding: EdgeInsets.fromLTRB(16, 16, 16, widget.bottomPadding),
      itemBuilder: (context, index) {
        final item = _timelineItemAt(
          index,
          reviewAnchorIndex: reviewAnchorIndex,
          extraItems: extraItems,
        );
        final collapseThinkingByDefault = _shouldCollapseThinkingByDefault(
          index,
          reviewAnchorIndex: reviewAnchorIndex,
          extraItems: extraItems,
          resolvedItemCount: resolvedItemCount,
        );
        final itemKey = ValueKey<String>(_timelineChildKey(item));
        if (item.kind == 'file_diff') {
          return KeyedSubtree(
            key: itemKey,
            child: const SizedBox.shrink(),
          );
        }
        if (item.kind == 'review_summary') {
          return KeyedSubtree(
            key: itemKey,
            child: _ReviewSummaryCard(
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
            ),
          );
        }
        if ((item.kind == 'prompt_request' ||
                item.kind == 'interaction_request') &&
            visiblePrompt != null) {
          return KeyedSubtree(
            key: itemKey,
            child: visiblePrompt is InteractionRequestEvent
                ? _InteractionRequestCard(
                    interaction: visiblePrompt,
                    onSubmit: widget.onPromptSubmit,
                  )
                : _PromptRequestCard(
                    prompt: visiblePrompt as PromptRequestEvent,
                    onSubmit: widget.onPromptSubmit,
                  ),
          );
        }
        if (item.kind == 'plan_request' && visiblePlanQuestion != null) {
          return KeyedSubtree(
            key: itemKey,
            child: _PlanQuestionCard(
              question: visiblePlanQuestion,
              progressLabel: widget.pendingPlanProgressLabel,
              onSubmit: widget.onPromptSubmit,
            ),
          );
        }
        return KeyedSubtree(
          key: itemKey,
          child: EventCard(
            item: item,
            collapseThinkingByDefault: collapseThinkingByDefault,
            mediaPreviewStates: widget.mediaPreviewStates,
            onOpenAttachment: widget.onOpenAttachment,
            onRequestMediaPreview: widget.onRequestMediaPreview,
            onAnimatedBodyProgress: _handleAnimatedBodyProgress,
            onTap: () {
              if (item.kind == 'runtime_info_result') {
                widget.onOpenRuntimeInfo?.call();
              } else if (item.kind == 'fs_read_result') {
                widget.onOpenFile?.call();
              }
            },
          ),
        );
      },
      findChildIndexCallback: (key) {
        if (key is! ValueKey<String>) {
          return null;
        }
        final itemIndex = _timelineChildIndexForKey(
          key.value,
          reviewAnchorIndex: reviewAnchorIndex,
          extraItems: extraItems,
        );
        return itemIndex == null ? null : itemIndex * 2;
      },
      separatorBuilder: (_, __) => const SizedBox(height: 12),
      itemCount: resolvedItemCount,
    );
    return Stack(
      children: [
        Positioned.fill(child: listView),
        Positioned(
          left: 16,
          right: 16,
          bottom: widget.bottomPadding + 8,
          child: IgnorePointer(
            child: AnimatedOpacity(
              duration: const Duration(milliseconds: 220),
              curve: Curves.easeOut,
              opacity: widget.isAiRunning ? 1.0 : 0.0,
              child: _AiStatusIndicator(
                key: const ValueKey('ai-status-indicator'),
                label: widget.aiStatusLabel,
              ),
            ),
          ),
        ),
      ],
    );
  }

  List<TimelineItem> _extraTimelineItems({
    required int reviewAnchorIndex,
    required AppEvent? visiblePrompt,
    required String visiblePromptMessage,
    required PlanQuestion? visiblePlanQuestion,
  }) {
    final items = <TimelineItem>[];
    if (reviewAnchorIndex == -1 && widget.activeReviewDiff != null) {
      items.add(_reviewSummaryItem(null));
    }
    if (visiblePrompt != null) {
      items.add(_promptTimelineItem(visiblePrompt, visiblePromptMessage));
    }
    if (visiblePlanQuestion != null) {
      items.add(_planTimelineItem(visiblePlanQuestion, visiblePrompt));
    }
    return items;
  }

  AppEvent? _visiblePrompt() {
    final promptCandidate = widget.shouldShowReviewChoices
        ? null
        : (widget.pendingInteraction?.hasVisiblePrompt == true
            ? widget.pendingInteraction
            : (widget.pendingPrompt?.hasVisiblePrompt == true
                ? widget.pendingPrompt
                : null));
    if (promptCandidate is PromptRequestEvent &&
        _shouldHidePassiveReadyPrompt(promptCandidate)) {
      return null;
    }
    if (promptCandidate is InteractionRequestEvent &&
        _shouldHidePassiveReadyInteraction(promptCandidate)) {
      return null;
    }
    return promptCandidate;
  }

  PlanQuestion? _visiblePlanQuestion() {
    return widget.shouldShowPlanChoices ? widget.pendingPlanQuestion : null;
  }

  String _visiblePromptMessage(AppEvent? visiblePrompt) {
    if (visiblePrompt is InteractionRequestEvent) {
      return visiblePrompt.message;
    }
    if (visiblePrompt is PromptRequestEvent) {
      return visiblePrompt.message;
    }
    return '';
  }

  int _resolvedItemCount({
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
  }) {
    final inlineReviewCount =
        reviewAnchorIndex >= 0 && widget.activeReviewDiff != null ? 1 : 0;
    return widget.items.length + inlineReviewCount + extraItems.length;
  }

  TimelineItem _timelineItemAt(
    int index, {
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
  }) {
    if (reviewAnchorIndex >= 0 && widget.activeReviewDiff != null) {
      final reviewIndex = reviewAnchorIndex + 1;
      if (index == reviewIndex) {
        return _reviewSummaryItem(widget.items[reviewAnchorIndex]);
      }
      if (index > reviewIndex) {
        final sourceIndex = index - 1;
        if (sourceIndex < widget.items.length) {
          return widget.items[sourceIndex];
        }
        return extraItems[sourceIndex - widget.items.length];
      }
    }
    if (index < widget.items.length) {
      return widget.items[index];
    }
    return extraItems[index - widget.items.length];
  }

  bool _shouldCollapseThinkingByDefault(
    int index, {
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
    required int resolvedItemCount,
  }) {
    final item = _timelineItemAt(
      index,
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    if (item.kind != 'thinking') {
      return false;
    }
    for (var nextIndex = index + 1;
        nextIndex < resolvedItemCount;
        nextIndex++) {
      final next = _timelineItemAt(
        nextIndex,
        reviewAnchorIndex: reviewAnchorIndex,
        extraItems: extraItems,
      );
      if (_isVisibleAssistantResult(next)) {
        return true;
      }
    }
    return false;
  }

  bool _isVisibleAssistantResult(TimelineItem item) {
    return item.kind == 'markdown' && item.body.trim().isNotEmpty;
  }

  TimelineItem _reviewSummaryItem(TimelineItem? anchor) {
    final diff = widget.activeReviewDiff!;
    return TimelineItem(
      id: anchor == null
          ? 'review-summary-tail-${diff.id}-${widget.pendingDiffCount}'
          : 'review-summary-${diff.id}-${widget.pendingDiffCount}',
      kind: 'review_summary',
      timestamp: anchor?.timestamp ?? DateTime.now(),
      title: diff.title,
      body: diff.path,
      context: diff,
    );
  }

  TimelineItem _promptTimelineItem(AppEvent visiblePrompt, String message) {
    return TimelineItem(
      id: 'pending-prompt-${visiblePrompt.timestamp.microsecondsSinceEpoch}',
      kind: visiblePrompt is InteractionRequestEvent
          ? 'interaction_request'
          : 'prompt_request',
      timestamp: visiblePrompt.timestamp,
      title: visiblePrompt is InteractionRequestEvent
          ? (visiblePrompt.title.isNotEmpty ? visiblePrompt.title : '交互确认')
          : _promptRequestTitle(visiblePrompt as PromptRequestEvent),
      body: message,
      meta: visiblePrompt.runtimeMeta,
    );
  }

  TimelineItem _planTimelineItem(
    PlanQuestion question,
    AppEvent? visiblePrompt,
  ) {
    return TimelineItem(
      id: 'pending-plan-${question.id}-${widget.pendingPlanProgressLabel}',
      kind: 'plan_request',
      timestamp: DateTime.now(),
      title: question.displayLabel,
      body: question.message,
      meta: visiblePrompt?.runtimeMeta ?? const RuntimeMeta(),
    );
  }

  int _cachedReviewAnchorIndexFor(List<TimelineItem> items) {
    final activeDiffKey = _diffKey(widget.activeReviewDiff);
    if (activeDiffKey.isEmpty) {
      _anchorItemsLength = items.length;
      _anchorItemsKey = '';
      _anchorDiffKey = '';
      _cachedReviewAnchorIndex = -1;
      return -1;
    }
    final itemsKey = _reviewAnchorItemsKey(items);
    if (_anchorItemsLength == items.length &&
        _anchorItemsKey == itemsKey &&
        _anchorDiffKey == activeDiffKey &&
        _cachedReviewAnchorStillMatches(items)) {
      return _cachedReviewAnchorIndex;
    }
    _anchorItemsLength = items.length;
    _anchorItemsKey = itemsKey;
    _anchorDiffKey = activeDiffKey;
    _cachedReviewAnchorIndex = _reviewAnchorIndex(items);
    return _cachedReviewAnchorIndex;
  }

  String _reviewAnchorItemsKey(List<TimelineItem> items) {
    if (items.isEmpty) {
      return '0';
    }
    return [
      items.length.toString(),
      _timelineChildKey(items.first),
      _timelineChildKey(items.last),
      _timelineLayoutFingerprint(items.last),
    ].join('\n');
  }

  int _reviewAnchorIndex(List<TimelineItem> items) {
    widget.diagnostics?.reviewAnchorFullScans++;
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

  bool _cachedReviewAnchorStillMatches(List<TimelineItem> items) {
    if (_cachedReviewAnchorIndex == -1) {
      return true;
    }
    if (_cachedReviewAnchorIndex < 0 ||
        _cachedReviewAnchorIndex >= items.length) {
      return false;
    }
    final item = items[_cachedReviewAnchorIndex];
    if (item.kind != 'file_diff' || item.context == null) {
      return false;
    }
    final activeDiff = widget.activeReviewDiff;
    return activeDiff != null && _sameDiff(item.context!, activeDiff);
  }

  bool _sameDiff(HistoryContext left, HistoryContext right) {
    if (left.id.isNotEmpty && right.id.isNotEmpty) {
      return left.id == right.id;
    }
    return left.path == right.path;
  }

  String _diffKey(HistoryContext? diff) {
    if (diff == null) {
      return '';
    }
    if (diff.id.isNotEmpty) {
      return 'id:${diff.id}';
    }
    return 'path:${diff.path}';
  }

  String _timelineHistoryKey(List<TimelineItem> items) {
    if (items.isEmpty) {
      return '';
    }
    return '${items.first.id}\n${_timelineLayoutFingerprint(items.last)}\n${items.length}';
  }

  String _timelineLayoutFingerprint(TimelineItem item) {
    return [
      item.id,
      item.kind,
      item.title.hashCode,
      item.body.length,
      item.body.hashCode,
      item.status.hashCode,
      item.trigger.hashCode,
      item.attachments.length,
      item.codexSteps.length,
    ].join('\n');
  }

  String _timelineBottomLayoutFingerprint(
    TimelineItem item, {
    required AppEvent? visiblePrompt,
    required PlanQuestion? visiblePlanQuestion,
  }) {
    if (item.kind == 'prompt_request' && visiblePrompt is PromptRequestEvent) {
      return [
        _timelineLayoutFingerprint(item),
        visiblePrompt.blockingKind,
        visiblePrompt.isPermission,
        _promptOptionsLayoutKey(
          visiblePrompt.isPermission
              ? _permissionPromptOptions
              : visiblePrompt.options,
        ),
      ].join('\n');
    }
    if (item.kind == 'interaction_request' &&
        visiblePrompt is InteractionRequestEvent) {
      return [
        _timelineLayoutFingerprint(item),
        visiblePrompt.kind,
        visiblePrompt.title,
        visiblePrompt.blockingKind,
        visiblePrompt.isPermission,
        _interactionActionsLayoutKey(
          visiblePrompt.isPermission
              ? const <InteractionAction>[]
              : visiblePrompt.actions,
        ),
        _promptOptionsLayoutKey(
          visiblePrompt.isPermission
              ? _permissionPromptOptions
              : visiblePrompt.options,
        ),
      ].join('\n');
    }
    if (item.kind == 'plan_request' && visiblePlanQuestion != null) {
      return [
        _timelineLayoutFingerprint(item),
        visiblePlanQuestion.id,
        visiblePlanQuestion.title,
        visiblePlanQuestion.message,
        widget.pendingPlanProgressLabel,
        _promptOptionsLayoutKey(visiblePlanQuestion.options),
      ].join('\n');
    }
    if (item.kind == 'review_summary') {
      return [
        _timelineLayoutFingerprint(item),
        widget.pendingReviewGroupCount,
        widget.isManualReviewMode,
        widget.isAutoAcceptMode,
        widget.shouldShowReviewChoices,
        _reviewGroupLayoutKey(widget.activeReviewGroup),
      ].join('\n');
    }
    return _timelineLayoutFingerprint(item);
  }

  String _promptOptionsLayoutKey(List<PromptOption> options) {
    return [
      options.length,
      for (final option in options) '${option.value}|${option.displayText}',
    ].join('\n');
  }

  String _interactionActionsLayoutKey(List<InteractionAction> actions) {
    return [
      actions.length,
      for (final action in actions)
        [
          action.id,
          action.displayLabel,
          action.variant,
          action.value,
          action.decision,
          action.needsInput,
          action.destructive,
        ].join('|'),
    ].join('\n');
  }

  String _reviewGroupLayoutKey(ReviewGroup? group) {
    if (group == null) {
      return '';
    }
    return [
      group.id,
      group.title,
      group.pendingCount,
      group.files.length,
      for (final file in group.files)
        [
          file.id,
          file.path,
          file.title,
          file.pendingReview,
          file.reviewStatus,
        ].join('|'),
    ].join('\n');
  }

  String _timelineChildKey(TimelineItem item) {
    return [
      item.kind,
      item.id,
    ].join(':');
  }

  int? _timelineChildIndexForKey(
    String childKey, {
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
  }) {
    final indexes = _timelineChildIndexByKey(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    final cachedIndex = indexes[childKey];
    if (cachedIndex != null &&
        _timelineIndexMatchesChildKey(
          cachedIndex,
          childKey,
          reviewAnchorIndex: reviewAnchorIndex,
          extraItems: extraItems,
        )) {
      return cachedIndex;
    }
    final rebuilt = _rebuildTimelineChildIndexByKey(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    final rebuiltIndex = rebuilt[childKey];
    if (rebuiltIndex != null &&
        _timelineIndexMatchesChildKey(
          rebuiltIndex,
          childKey,
          reviewAnchorIndex: reviewAnchorIndex,
          extraItems: extraItems,
        )) {
      return rebuiltIndex;
    }
    return null;
  }

  Map<String, int> _timelineChildIndexByKey({
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
  }) {
    final cacheKey = _timelineChildIndexCacheKey(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    if (_childIndexCacheKey == cacheKey) {
      return _childIndexByKey;
    }
    return _rebuildTimelineChildIndexByKey(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
      cacheKey: cacheKey,
    );
  }

  Map<String, int> _rebuildTimelineChildIndexByKey({
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
    String? cacheKey,
  }) {
    final resolvedItemCount = _resolvedItemCount(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    final indexes = <String, int>{};
    for (var index = 0; index < resolvedItemCount; index++) {
      final item = _timelineItemAt(
        index,
        reviewAnchorIndex: reviewAnchorIndex,
        extraItems: extraItems,
      );
      indexes[_timelineChildKey(item)] = index;
    }
    widget.diagnostics?.childIndexMapBuilds++;
    _childIndexCacheKey = cacheKey ??
        _timelineChildIndexCacheKey(
          reviewAnchorIndex: reviewAnchorIndex,
          extraItems: extraItems,
        );
    _childIndexByKey = indexes;
    return indexes;
  }

  bool _timelineIndexMatchesChildKey(
    int index,
    String childKey, {
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
  }) {
    final resolvedItemCount = _resolvedItemCount(
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    if (index < 0 || index >= resolvedItemCount) {
      return false;
    }
    final item = _timelineItemAt(
      index,
      reviewAnchorIndex: reviewAnchorIndex,
      extraItems: extraItems,
    );
    return _timelineChildKey(item) == childKey;
  }

  String _timelineChildIndexCacheKey({
    required int reviewAnchorIndex,
    required List<TimelineItem> extraItems,
  }) {
    return [
      widget.items.length,
      widget.items.isEmpty ? '' : _timelineChildKey(widget.items.first),
      widget.items.isEmpty ? '' : _timelineChildKey(widget.items.last),
      reviewAnchorIndex,
      extraItems.length,
      for (final item in extraItems) _timelineChildKey(item),
    ].join('\n');
  }

  bool _shouldHidePassiveReadyPrompt(PromptRequestEvent prompt) {
    if (prompt.isPermission) {
      return false;
    }
    if (!prompt.isReady) {
      return false;
    }
    if (prompt.options.any((option) => option.displayText.isNotEmpty)) {
      return false;
    }
    final message = prompt.message.trim().toLowerCase();
    if (message.isEmpty) {
      return true;
    }
    return message.contains('会话已就绪') ||
        message.contains('可继续输入') ||
        message.contains('waiting for input') ||
        message.contains('continue input') ||
        message.contains('ready for input') ||
        message == 'ready' ||
        message == '等待输入';
  }

  bool _shouldHidePassiveReadyInteraction(InteractionRequestEvent interaction) {
    if (interaction.isPermission ||
        interaction.isReview ||
        interaction.isPlan) {
      return false;
    }
    if (interaction.actions.isNotEmpty) {
      return false;
    }
    if (interaction.options.any((option) => option.displayText.isNotEmpty)) {
      return false;
    }
    final title = interaction.title.trim().toLowerCase();
    final message = interaction.message.trim().toLowerCase();
    if (title.isEmpty && message.isEmpty) {
      return true;
    }
    final looksPassiveReady = title.contains('等待输入') ||
        title.contains('可继续输入') ||
        title.contains('waiting for input') ||
        title.contains('continue input') ||
        title.contains('ready for input') ||
        title == 'ready' ||
        message.contains('会话已就绪') ||
        message.contains('可继续输入') ||
        message.contains('waiting for input') ||
        message.contains('continue input') ||
        message.contains('ready for input') ||
        message == 'ready' ||
        message == '等待输入';
    return interaction.isReady || looksPassiveReady;
  }

  String _promptRequestTitle(PromptRequestEvent prompt) {
    return prompt.isPermission ? '授权确认' : '等待输入';
  }
}

class _TimelineLayoutSnapshot {
  const _TimelineLayoutSnapshot({
    required this.itemCount,
    required this.bottomItemLayoutKey,
  });

  final int itemCount;
  final String bottomItemLayoutKey;
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
    final isPermission = interaction.isPermission;
    final actions =
        isPermission ? const <InteractionAction>[] : interaction.actions;
    final options = isPermission
        ? _permissionPromptOptions
        : interaction.options
            .where((option) => option.displayText.isNotEmpty)
            .toList();
    return _ActionCardFrame(
      icon: Icons.touch_app_rounded,
      title:
          interaction.title.trim().isEmpty ? '交互确认' : interaction.title.trim(),
      description: interaction.message.trim(),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (actions.isNotEmpty) ...[
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

class _AiStatusIndicator extends StatefulWidget {
  const _AiStatusIndicator({super.key, required this.label});
  final String label;
  @override
  State<_AiStatusIndicator> createState() => _AiStatusIndicatorState();
}

class _AiStatusIndicatorState extends State<_AiStatusIndicator>
    with SingleTickerProviderStateMixin {
  late final AnimationController _pulse = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1400),
  );
  @override
  void initState() {
    super.initState();
    _pulse.repeat(reverse: true);
  }

  @override
  void dispose() {
    _pulse.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          AnimatedBuilder(
            animation: _pulse,
            builder: (context, _) {
              final opacity = 0.35 + 0.35 * _pulse.value;
              return Container(
                width: 8,
                height: 8,
                decoration: BoxDecoration(
                  color: scheme.primary.withValues(alpha: opacity),
                  shape: BoxShape.circle,
                ),
              );
            },
          ),
          const SizedBox(width: 10),
          Text(
            widget.label,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                  fontStyle: FontStyle.italic,
                ),
          ),
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
        return FilledButton(
            onPressed: onPressed, child: Text(action.displayLabel));
      case 'tonal':
        return FilledButton.tonal(
            onPressed: onPressed, child: Text(action.displayLabel));
      default:
        return OutlinedButton(
            onPressed: onPressed, child: Text(action.displayLabel));
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
    final isPermissionPrompt = prompt.isPermission;
    final options =
        isPermissionPrompt ? _permissionPromptOptions : _resolvedOptions();
    return _ActionCardFrame(
      icon: isPermissionPrompt
          ? Icons.verified_user_outlined
          : Icons.keyboard_outlined,
      title: isPermissionPrompt ? '授权确认' : '等待输入',
      description: prompt.message.trim(),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (options.isNotEmpty) ...[
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
    return prompt.options
        .where((option) => option.displayText.isNotEmpty)
        .toList(growable: false);
  }
}

class _PlanQuestionCard extends StatelessWidget {
  const _PlanQuestionCard({
    required this.question,
    required this.progressLabel,
    this.onSubmit,
  });

  final PlanQuestion question;
  final String progressLabel;
  final ValueChanged<String>? onSubmit;

  @override
  Widget build(BuildContext context) {
    final options = question.options
        .where((option) => option.displayText.isNotEmpty)
        .toList(growable: false);
    return _ActionCardFrame(
      icon: Icons.account_tree_outlined,
      title: question.displayLabel.isNotEmpty ? question.displayLabel : '计划选择',
      description: question.message.trim(),
      trailing: progressLabel.isEmpty
          ? null
          : Text(
              progressLabel,
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                    fontWeight: FontWeight.w700,
                  ),
            ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (options.isNotEmpty) ...[
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
                  .toList(growable: false),
            ),
          ],
        ],
      ),
    );
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
    return OutlinedButton(
      onPressed: onPressed,
      child: Text(_labelForOption(option)),
    );
  }

  String _labelForOption(PromptOption option) {
    return option.displayText;
  }
}

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
    final pendingLabelCount =
        groupFileCount > 0 ? groupFileCount : pendingDiffCount;
    final showReviewButtons =
        isSingle && isManualReviewMode && shouldShowReviewChoices;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            Theme.of(context).colorScheme.surface,
            Theme.of(context).colorScheme.surfaceContainerLow,
          ],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(
          color: Theme.of(context)
              .colorScheme
              .outlineVariant
              .withValues(alpha: 0.55),
        ),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.04),
            blurRadius: 18,
            offset: const Offset(0, 8),
          ),
        ],
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
                            color:
                                Theme.of(context).colorScheme.onSurfaceVariant,
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
                  label:
                      Text(pendingLabelCount > 1 ? '进入 differ 处理' : '查看 diff'),
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
    final pendingLabelCount =
        groupFileCount > 0 ? groupFileCount : pendingDiffCount;
    if (isAutoAcceptMode) {
      return '自动模式已开启，新的 diff 会自动确认。';
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

class _ActionCardFrame extends StatelessWidget {
  const _ActionCardFrame({
    required this.icon,
    required this.title,
    required this.description,
    required this.child,
    this.trailing,
  });

  final IconData icon;
  final String title;
  final String description;
  final Widget child;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            theme.colorScheme.surface,
            theme.colorScheme.surfaceContainerHigh,
          ],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.5),
        ),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.04),
            blurRadius: 18,
            offset: const Offset(0, 8),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primaryContainer,
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Icon(icon, color: theme.colorScheme.primary, size: 20),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                    if (description.trim().isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        description.trim(),
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                          height: 1.4,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              if (trailing != null) ...[
                const SizedBox(width: 12),
                trailing!,
              ],
            ],
          ),
          const SizedBox(height: 14),
          child,
        ],
      ),
    );
  }
}
