import 'app_connection_endpoint.dart';

const _httpScheme = 'http';
const _httpsScheme = 'https';
const _wsScheme = 'ws';
const _wssScheme = 'wss';
const _wsPath = '/ws';
const _downloadPath = '/download';

class AppConnectionUrls {
  const AppConnectionUrls({
    required this.host,
    required this.port,
    required this.token,
    required this.secureTransport,
  });

  final String host;
  final String port;
  final String token;
  final bool secureTransport;

  String get baseHttpUrl => _baseUri(httpScheme).toString();

  String get displayEndpoint => _baseUri(httpScheme).toString();

  String get displayHost => Uri(
        scheme: httpScheme,
        host: _endpoint.host,
      ).toString();

  String get wsUrl => _baseUri(wsScheme).replace(
      path: _wsPath,
      queryParameters: <String, String>{'token': token}).toString();

  Uri downloadUri(String path) => _baseUri(httpScheme).replace(
        path: _downloadPath,
        queryParameters: <String, String>{'token': token, 'path': path},
      );

  String get httpScheme => secureTransport ? _httpsScheme : _httpScheme;

  String get wsScheme => secureTransport ? _wssScheme : _wsScheme;

  Uri _baseUri(String scheme) {
    final endpoint = _endpoint;
    return Uri(
      scheme: scheme,
      host: endpoint.host,
      port: endpoint.portNumber,
    );
  }

  AppConnectionEndpoint get _endpoint {
    final endpoint = AppConnectionEndpoint.parse(
      host,
      fallbackPort: port,
      preferEmbeddedPort: false,
    );
    return endpoint;
  }
}
