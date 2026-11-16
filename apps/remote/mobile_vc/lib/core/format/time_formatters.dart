String formatElapsedClock(int totalSeconds) {
  final normalized = totalSeconds < 0 ? 0 : totalSeconds;
  final hours = normalized ~/ 3600;
  final minutes = (normalized % 3600) ~/ 60;
  final seconds = normalized % 60;

  final secondLabel = seconds.toString().padLeft(2, '0');
  final minuteLabel = minutes.toString().padLeft(2, '0');
  if (hours <= 0) {
    return '$minuteLabel:$secondLabel';
  }
  final hourLabel = hours.toString().padLeft(2, '0');
  return '$hourLabel:$minuteLabel:$secondLabel';
}
