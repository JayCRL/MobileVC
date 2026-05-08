import 'package:flutter/material.dart';

import '../mini_claude/mini_claude_page.dart';
import 'session_home_page.dart';
import 'session_controller.dart';

class ModeSelectPage extends StatelessWidget {
  const ModeSelectPage({
    super.key,
    required this.controller,
    required this.darkModeEnabled,
    required this.onToggleTheme,
  });

  final SessionController controller;
  final bool darkModeEnabled;
  final VoidCallback onToggleTheme;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('MobileVC'),
        actions: [
          IconButton(
            onPressed: onToggleTheme,
            tooltip: darkModeEnabled ? '切换浅色模式' : '切换深色模式',
            icon: Icon(
              darkModeEnabled
                  ? Icons.light_mode_outlined
                  : Icons.dark_mode_outlined,
            ),
          ),
        ],
      ),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox(height: 40),
                Icon(
                  Icons.phone_iphone_outlined,
                  size: 64,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(height: 12),
                Text(
                  '选择模式',
                  style: theme.textTheme.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 40),

                // 联机模式
                _ModeCard(
                  icon: Icons.computer_outlined,
                  title: '联机模式',
                  subtitle: '连接电脑后端，完整 Claude Code 功能',
                  color: theme.colorScheme.primary,
                  onTap: () => Navigator.of(context).pushReplacement(
                    MaterialPageRoute(
                      builder: (_) => SessionHomePage(
                        controller: controller,
                        darkModeEnabled: darkModeEnabled,
                        onToggleTheme: onToggleTheme,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 16),

                // 脱机模式
                _ModeCard(
                  icon: Icons.smartphone_outlined,
                  title: '脱机模式',
                  subtitle: '手机独立运行 Mini Claude\n编辑代码 + Git + Linux 终端',
                  color: theme.colorScheme.tertiary,
                  onTap: () => Navigator.of(context).push(
                    MaterialPageRoute(builder: (_) => const MiniClaudePage()),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ModeCard extends StatelessWidget {
  const _ModeCard({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.color,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final Color color;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SizedBox(
      width: double.infinity,
      child: Card(
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(24),
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Row(
              children: [
                Container(
                  width: 56,
                  height: 56,
                  decoration: BoxDecoration(
                    color: color.withAlpha(30),
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Icon(icon, color: color, size: 28),
                ),
                const SizedBox(width: 20),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title,
                        style: theme.textTheme.titleMedium?.copyWith(
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        subtitle,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.outline,
                        ),
                      ),
                    ],
                  ),
                ),
                Icon(Icons.arrow_forward_ios, size: 16,
                    color: theme.colorScheme.outline),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
