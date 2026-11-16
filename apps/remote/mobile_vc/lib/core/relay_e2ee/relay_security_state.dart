import 'dart:typed_data';

import 'relay_device_identity.dart';
import 'relay_e2ee_crypto.dart';

enum RelaySecurityMode {
  direct,
  relayTestMode,
  relayE2eeVerified,
  relayNotVerified,
  fingerprintMismatch,
  encryptionUnavailable,
  deviceRevoked,
  plaintextDisabled,
}

class RelaySecurityInput {
  const RelaySecurityInput({
    required this.connectionMode,
    this.expectedNodeFingerprintHex = '',
    this.actualNodePublicKey = const <int>[],
    this.nodeFingerprintConfirmed = false,
    this.handshakeComplete = false,
    this.protocolSupportsE2ee = false,
    this.protocolSupportsTunnel = false,
    this.supportsMultiplexStreams = false,
    this.supportsFileDownload = false,
    this.supportsDeviceManagement = false,
    this.requiresE2ee = false,
    this.plaintextTestMode = false,
    this.deviceRevoked = false,
    this.productionPlaintextRejected = false,
    this.decryptFailed = false,
    this.unsupportedVersion = false,
    this.deviceBound = false,
  });

  final String connectionMode;
  final String expectedNodeFingerprintHex;
  final List<int> actualNodePublicKey;
  final bool nodeFingerprintConfirmed;
  final bool handshakeComplete;
  final bool protocolSupportsE2ee;
  final bool protocolSupportsTunnel;
  final bool supportsMultiplexStreams;
  final bool supportsFileDownload;
  final bool supportsDeviceManagement;
  final bool requiresE2ee;
  final bool plaintextTestMode;
  final bool deviceRevoked;
  final bool productionPlaintextRejected;
  final bool decryptFailed;
  final bool unsupportedVersion;
  final bool deviceBound;
}

class RelaySecurityState {
  const RelaySecurityState({
    required this.mode,
    required this.title,
    required this.detail,
    required this.canShowVerified,
    this.actualFingerprintHex = '',
    this.shortFingerprint = '',
  });

  final RelaySecurityMode mode;
  final String title;
  final String detail;
  final bool canShowVerified;
  final String actualFingerprintHex;
  final String shortFingerprint;

  bool get isBlocking => switch (mode) {
        RelaySecurityMode.fingerprintMismatch ||
        RelaySecurityMode.deviceRevoked ||
        RelaySecurityMode.plaintextDisabled ||
        RelaySecurityMode.encryptionUnavailable =>
          true,
        _ => false,
      };
}

class RelaySecurityStateEvaluator {
  static Future<RelaySecurityState> evaluate(RelaySecurityInput input) async {
    if (input.connectionMode != 'relay') {
      return const RelaySecurityState(
        mode: RelaySecurityMode.direct,
        title: 'LAN 直连',
        detail: '当前未经过 relay。',
        canShowVerified: false,
      );
    }
    final fingerprint = await _fingerprint(input.actualNodePublicKey);
    if (input.deviceRevoked) {
      return _state(
        RelaySecurityMode.deviceRevoked,
        '设备已撤销',
        '此设备已被本地节点撤销，需要重新配对。',
        fingerprint,
      );
    }
    if (_hasFingerprintMismatch(input, fingerprint.fullHex)) {
      return _state(
        RelaySecurityMode.fingerprintMismatch,
        '指纹已变化',
        '节点指纹与已确认记录不一致，请重新确认或重新配对。',
        fingerprint,
      );
    }
    if (input.decryptFailed) {
      return _state(
        RelaySecurityMode.encryptionUnavailable,
        '解密失败',
        'E2EE 消息认证失败，连接已不可信。',
        fingerprint,
      );
    }
    if (input.plaintextTestMode) {
      return _state(
        RelaySecurityMode.relayTestMode,
        'Relay 测试模式',
        '当前 relay 允许明文测试，不能标记为 E2EE 已验证。',
        fingerprint,
      );
    }
    if (input.unsupportedVersion || !_supportsRequiredProtocol(input)) {
      return _state(
        RelaySecurityMode.encryptionUnavailable,
        '加密不可用',
        '当前客户端或本地节点不支持所需的 E2EE 协议能力。',
        fingerprint,
      );
    }
    if (input.requiresE2ee && !input.productionPlaintextRejected) {
      return _state(
        RelaySecurityMode.plaintextDisabled,
        '明文拒绝未启用',
        '生产 relay 必须确认明文已被拒绝后才能显示安全状态。',
        fingerprint,
      );
    }
    final verified = input.nodeFingerprintConfirmed &&
        input.handshakeComplete &&
        input.deviceBound &&
        input.requiresE2ee &&
        input.productionPlaintextRejected;
    if (!verified) {
      return _state(
        RelaySecurityMode.relayNotVerified,
        'Relay 未验证',
        '等待指纹确认、E2EE 握手和设备状态校验完成。',
        fingerprint,
      );
    }
    return RelaySecurityState(
      mode: RelaySecurityMode.relayE2eeVerified,
      title: 'E2EE 已验证',
      detail: '节点指纹、协议能力、E2EE 握手和设备状态均已通过。',
      canShowVerified: true,
      actualFingerprintHex: fingerprint.fullHex,
      shortFingerprint: fingerprint.shortText,
    );
  }

  static bool _supportsRequiredProtocol(RelaySecurityInput input) {
    return input.protocolSupportsE2ee &&
        input.protocolSupportsTunnel &&
        input.supportsMultiplexStreams &&
        input.supportsFileDownload &&
        input.supportsDeviceManagement;
  }

  static bool _hasFingerprintMismatch(
    RelaySecurityInput input,
    String actualFingerprintHex,
  ) {
    final expected = input.expectedNodeFingerprintHex.trim().toLowerCase();
    if (expected.isEmpty || actualFingerprintHex.isEmpty) {
      return false;
    }
    return expected != actualFingerprintHex.toLowerCase();
  }

  static Future<_RelayFingerprint> _fingerprint(List<int> publicKey) async {
    if (publicKey.isEmpty) {
      return const _RelayFingerprint('', '');
    }
    final bytes = await RelayE2eeCrypto.fingerprint(
      Uint8List.fromList(publicKey),
    );
    return _RelayFingerprint(
      RelayDeviceIdentityStore.fingerprintHex(bytes),
      RelayE2eeCrypto.shortFingerprint(bytes),
    );
  }

  static RelaySecurityState _state(
    RelaySecurityMode mode,
    String title,
    String detail,
    _RelayFingerprint fingerprint,
  ) {
    return RelaySecurityState(
      mode: mode,
      title: title,
      detail: detail,
      canShowVerified: false,
      actualFingerprintHex: fingerprint.fullHex,
      shortFingerprint: fingerprint.shortText,
    );
  }
}

class _RelayFingerprint {
  const _RelayFingerprint(this.fullHex, this.shortText);

  final String fullHex;
  final String shortText;
}
