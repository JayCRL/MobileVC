import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/format/time_formatters.dart';

void main() {
  group('formatElapsedClock', () {
    test('formats seconds as mm:ss under one hour', () {
      expect(formatElapsedClock(0), '00:00');
      expect(formatElapsedClock(9), '00:09');
      expect(formatElapsedClock(65), '01:05');
      expect(formatElapsedClock(3599), '59:59');
    });

    test('formats seconds as hh:mm:ss at one hour and above', () {
      expect(formatElapsedClock(3600), '01:00:00');
      expect(formatElapsedClock(3661), '01:01:01');
      expect(formatElapsedClock(10 * 3600 + 2 * 60 + 3), '10:02:03');
    });

    test('clamps negative values to zero', () {
      expect(formatElapsedClock(-1), '00:00');
    });
  });
}
