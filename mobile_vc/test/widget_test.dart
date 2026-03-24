import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:mobile_vc/app/app.dart';

void main() {
  testWidgets('renders MobileVC shell', (tester) async {
    await tester.pumpWidget(const MobileVcApp());
    await tester.pump();

    expect(find.text('MobileVC'), findsAtLeastNWidgets(1));
    expect(find.byType(EditableText), findsOneWidget);
  });
}
