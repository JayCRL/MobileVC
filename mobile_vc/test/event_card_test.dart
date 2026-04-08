import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/widgets/event_card.dart';

void main() {
  testWidgets('markdown reply uses SelectionArea for cross-block selection',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'md-1',
              kind: 'markdown',
              timestamp: DateTime(2026, 4, 4, 12),
              body: '# Title\n\nfirst line\nsecond line\n\n- item 1\n- item 2',
            ),
          ),
        ),
      ),
    );

    expect(find.byType(SelectionArea), findsOneWidget);
    final markdown = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(markdown.selectable, isFalse);
  });
}
