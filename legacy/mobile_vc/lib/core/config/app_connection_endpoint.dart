const _httpScheme = 'http';
const _httpsScheme = 'https';
const _maxPortNumber = 65535;

class AppConnectionEndpoint {
  const AppConnectionEndpoint({
    required this.host,
    required this.port,
    required this.secureTransport,
  });

  final String host;
  final String port;
  final bool? secureTransport;

  static AppConnectionEndpoint parse(
    String raw, {
    String fallbackPort = '',
    bool preferEmbeddedPort = true,
  }) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty) {
      throw const FormatException('host is required');
    }
    final uriEndpoint = _parseUriEndpoint(
      trimmed,
      fallbackPort,
      preferEmbeddedPort,
    );
    if (uriEndpoint != null) {
      return uriEndpoint;
    }
    final hostPort = _splitHostPort(trimmed);
    return AppConnectionEndpoint(
      host: _unbracketHost(hostPort.host),
      port: _selectPort(hostPort.port, fallbackPort, preferEmbeddedPort),
      secureTransport: secureTransportFromScheme(hostPort.scheme),
    );
  }

  int? get portNumber {
    final trimmed = port.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    final parsed = int.tryParse(trimmed);
    if (parsed == null || parsed <= 0 || parsed > _maxPortNumber) {
      throw FormatException('invalid port: $port');
    }
    return parsed;
  }
}

AppConnectionEndpoint? _parseUriEndpoint(
  String raw,
  String fallbackPort,
  bool preferEmbeddedPort,
) {
  final uri = Uri.tryParse(raw);
  if (uri == null || uri.host.trim().isEmpty) {
    return null;
  }
  final scheme = uri.scheme.toLowerCase();
  if (scheme != _httpScheme && scheme != _httpsScheme) {
    return null;
  }
  return AppConnectionEndpoint(
    host: _unbracketHost(uri.host),
    port: _selectPort(
      uri.hasPort ? uri.port.toString() : '',
      fallbackPort,
      preferEmbeddedPort,
    ),
    secureTransport: scheme == _httpsScheme,
  );
}

_HostPort _splitHostPort(String raw) {
  final normalized = raw.endsWith('/') ? raw.substring(0, raw.length - 1) : raw;
  final schemeSeparator = normalized.indexOf('://');
  if (schemeSeparator > 0) {
    return _HostPort(
      normalized.substring(schemeSeparator + 3),
      '',
      normalized.substring(0, schemeSeparator),
    );
  }
  return _splitNormalizedHostPort(normalized);
}

_HostPort _splitNormalizedHostPort(String raw) {
  if (raw.startsWith('[')) {
    final end = raw.indexOf(']');
    if (end > 0 && raw.length > end + 1 && raw[end + 1] == ':') {
      return _HostPort(raw.substring(1, end), raw.substring(end + 2), '');
    }
    return _HostPort(_unbracketHost(raw), '', '');
  }
  final colon = raw.lastIndexOf(':');
  if (colon <= 0 || raw.indexOf(':') != colon) {
    return _HostPort(raw, '', '');
  }
  final port = raw.substring(colon + 1);
  if (int.tryParse(port) == null) {
    return _HostPort(raw, '', '');
  }
  return _HostPort(raw.substring(0, colon), port, '');
}

String _selectPort(
  String embeddedPort,
  String fallbackPort,
  bool preferEmbeddedPort,
) {
  final embedded = embeddedPort.trim();
  final fallback = fallbackPort.trim();
  if (preferEmbeddedPort && embedded.isNotEmpty) {
    return embedded;
  }
  return fallback.isNotEmpty ? fallback : embedded;
}

String _unbracketHost(String raw) {
  final trimmed = raw.trim();
  if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
    return trimmed.substring(1, trimmed.length - 1);
  }
  return trimmed;
}

class _HostPort {
  const _HostPort(this.host, this.port, this.scheme);

  final String host;
  final String port;
  final String scheme;
}

bool? parseSecureTransport(Object? value) {
  if (value is bool) {
    return value;
  }
  return value is String ? secureTransportFromScheme(value) : null;
}

bool? secureTransportFromScheme(String value) {
  switch (value.trim().toLowerCase()) {
    case _httpsScheme:
    case 'wss':
    case 'true':
      return true;
    case _httpScheme:
    case 'ws':
    case 'false':
      return false;
    default:
      return null;
  }
}
