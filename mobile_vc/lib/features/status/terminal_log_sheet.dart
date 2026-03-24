import 'package:flutter/material.dart';

class TerminalLogSheet extends StatelessWidget {
  const TerminalLogSheet({
    super.key,
    required this.stdout,
    required this.stderr,
  });

  final String stdout;
  final String stderr;

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 6, 16, 24),
          child: Column(
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text('运行日志', style: Theme.of(context).textTheme.titleLarge),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              const TabBar(
                tabs: [
                  Tab(text: 'stdout'),
                  Tab(text: 'stderr'),
                ],
              ),
              const SizedBox(height: 12),
              Expanded(
                child: TabBarView(
                  children: [
                    _LogPane(text: stdout),
                    _LogPane(text: stderr),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _LogPane extends StatelessWidget {
  const _LogPane({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    if (text.isEmpty) {
      return const Center(child: Text('暂无日志'));
    }
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(18),
      ),
      child: SingleChildScrollView(
        child: SelectableText(
          text,
          style: const TextStyle(
            color: Color(0xFFE2E8F0),
            fontFamily: 'monospace',
            height: 1.45,
          ),
        ),
      ),
    );
  }
}
