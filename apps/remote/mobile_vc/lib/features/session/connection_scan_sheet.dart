import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

class ConnectionScanSheet extends StatefulWidget {
  const ConnectionScanSheet({super.key});

  @override
  State<ConnectionScanSheet> createState() => _ConnectionScanSheetState();
}

class _ConnectionScanSheetState extends State<ConnectionScanSheet> {
  final MobileScannerController _scannerController = MobileScannerController(
    detectionSpeed: DetectionSpeed.noDuplicates,
    facing: CameraFacing.back,
  );
  bool _handled = false;

  @override
  void dispose() {
    _scannerController.dispose();
    super.dispose();
  }

  void _handleBarcode(BarcodeCapture capture) {
    if (_handled) {
      return;
    }
    for (final barcode in capture.barcodes) {
      final raw = barcode.rawValue?.trim() ?? '';
      if (raw.isEmpty) {
        continue;
      }
      _handled = true;
      Navigator.of(context).pop(raw);
      return;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text('扫码连接后端', style: theme.textTheme.titleLarge),
                ),
                IconButton(
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              '扫描 `mobilevc start` 输出的局域网二维码，自动回填 Host、Port 和 Token。',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 16),
            ClipRRect(
              borderRadius: BorderRadius.circular(20),
              child: AspectRatio(
                aspectRatio: 1,
                child: MobileScanner(
                  controller: _scannerController,
                  onDetect: _handleBarcode,
                ),
              ),
            ),
            const SizedBox(height: 12),
            Text(
              '如果扫码失败，仍可返回上一层手动输入连接信息。',
              style: theme.textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}
