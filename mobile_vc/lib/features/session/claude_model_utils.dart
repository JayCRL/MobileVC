const Set<String> _claudeAliasModels = <String>{
  'default',
  'sonnet',
  'sonnet[1m]',
  'opus',
  'opus[1m]',
  'haiku',
  'opusplan',
  'claude-sonnet-4-5',
  'claude-sonnet-4-6',
  'claude-opus-4-6',
  'claude-haiku-4-5',
};

final RegExp _claudePinnedModelPattern = RegExp(
  r'^claude-[a-z0-9][a-z0-9.-]*(?:\[1m\])?$',
  caseSensitive: false,
);

String normalizeClaudeModelSelection(
  String value, {
  String fallback = 'sonnet',
}) {
  final trimmed = value.trim();
  if (trimmed.isEmpty) {
    return fallback;
  }
  final canonicalAlias = canonicalClaudeModelAlias(trimmed);
  if (canonicalAlias != null) {
    return canonicalAlias;
  }
  if (_claudePinnedModelPattern.hasMatch(trimmed)) {
    return trimmed.toLowerCase();
  }
  return trimmed;
}

String claudeModelDisplayLabel(String value) {
  final trimmed = value.trim();
  if (trimmed.isEmpty) {
    return 'Sonnet';
  }
  switch (canonicalClaudeModelAlias(trimmed)) {
    case 'default':
      return 'Default';
    case 'sonnet':
      return 'Sonnet';
    case 'sonnet[1m]':
      return 'Sonnet 1M';
    case 'opus':
      return 'Opus';
    case 'opus[1m]':
      return 'Opus 1M';
    case 'haiku':
      return 'Haiku';
    case 'opusplan':
      return 'Opus Plan';
    case 'claude-sonnet-4-5':
      return 'Sonnet 4.5';
    case 'claude-sonnet-4-6':
      return 'Sonnet 4.6';
    case 'claude-opus-4-6':
      return 'Opus 4.6';
    case 'claude-haiku-4-5':
      return 'Haiku 4.5';
  }
  final lower = trimmed.toLowerCase();
  if (lower.endsWith('[1m]')) {
    return '${trimmed.substring(0, trimmed.length - 4)} 1M';
  }
  return trimmed;
}

String? canonicalClaudeModelAlias(String value) {
  final normalized = value.trim().toLowerCase();
  if (normalized.isEmpty) {
    return null;
  }
  if (_claudeAliasModels.contains(normalized)) {
    return normalized;
  }
  switch (normalized) {
    case 'opus plan':
    case 'opus-plan':
      return 'opusplan';
    case 'sonnet 1m':
    case 'sonnet-1m':
    case 'sonnet [1m]':
      return 'sonnet[1m]';
    case 'opus 1m':
    case 'opus-1m':
    case 'opus [1m]':
      return 'opus[1m]';
    default:
      return null;
  }
}

String? parseClaudeModelFromText(String text) {
  final normalized = text.trim().toLowerCase();
  if (normalized.isEmpty) {
    return null;
  }
  final pinnedMatch = RegExp(
    r'claude-[a-z0-9][a-z0-9.-]*(?:\[1m\])?',
    caseSensitive: false,
  ).firstMatch(normalized);
  if (pinnedMatch != null) {
    return pinnedMatch.group(0)?.toLowerCase();
  }
  const orderedAliases = <String>[
    'claude-sonnet-4-6',
    'claude-opus-4-6',
    'claude-haiku-4-5',
    'claude-sonnet-4-5',
    'sonnet[1m]',
    'opus[1m]',
    'opusplan',
    'default',
    'sonnet',
    'opus',
    'haiku',
  ];
  for (final alias in orderedAliases) {
    if (normalized.contains(alias)) {
      return alias;
    }
  }
  if (normalized.contains('opus plan') || normalized.contains('opus-plan')) {
    return 'opusplan';
  }
  if (normalized.contains('sonnet 1m') || normalized.contains('sonnet-1m')) {
    return 'sonnet[1m]';
  }
  if (normalized.contains('opus 1m') || normalized.contains('opus-1m')) {
    return 'opus[1m]';
  }
  return null;
}

bool isEquivalentClaudeModelSelection(String left, String right) {
  return normalizeClaudeModelSelection(left) ==
      normalizeClaudeModelSelection(right);
}
