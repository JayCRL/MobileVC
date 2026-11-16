class PermissionModeOption {
  const PermissionModeOption({
    required this.value,
    required this.label,
  });

  final String value;
  final String label;
}

const codexPermissionModeOptions = <PermissionModeOption>[
  PermissionModeOption(value: 'default', label: '默认权限'),
  PermissionModeOption(value: 'auto', label: '自动审查'),
  PermissionModeOption(value: 'bypassPermissions', label: '跳过审批'),
  PermissionModeOption(value: 'config', label: '自定义(config.toml)'),
];

const claudePermissionModeOptions = <PermissionModeOption>[
  PermissionModeOption(value: 'default', label: '默认权限'),
  PermissionModeOption(value: 'auto', label: '自动模式'),
  PermissionModeOption(value: 'bypassPermissions', label: '完全访问权限'),
];

List<PermissionModeOption> permissionModeOptionsForEngine(String engine) {
  return engine.trim().toLowerCase() == 'codex'
      ? codexPermissionModeOptions
      : claudePermissionModeOptions;
}

String normalizePermissionModeForEngine(String value, String engine) {
  final normalized = value.trim();
  final options = permissionModeOptionsForEngine(engine);
  for (final option in options) {
    if (option.value == normalized) {
      return option.value;
    }
  }
  return 'auto';
}

String permissionModeLabelForEngine(String value, String engine) {
  final normalized = normalizePermissionModeForEngine(value, engine);
  final options = permissionModeOptionsForEngine(engine);
  for (final option in options) {
    if (option.value == normalized) {
      return option.label;
    }
  }
  return '自动模式';
}
