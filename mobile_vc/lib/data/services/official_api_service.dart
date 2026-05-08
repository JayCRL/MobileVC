import 'dart:convert';
import 'package:http/http.dart' as http;

class NodeInfo {
  final String id;
  final String name;
  final String version;
  final String status;
  final String stunHost;
  final String turnPort;
  final String turnUser;
  final String turnPass;
  final String lastSeen;

  NodeInfo({
    required this.id,
    required this.name,
    required this.version,
    required this.status,
    required this.stunHost,
    required this.turnPort,
    required this.turnUser,
    required this.turnPass,
    required this.lastSeen,
  });

  factory NodeInfo.fromJson(Map<String, dynamic> json) => NodeInfo(
    id: json['id'] ?? '',
    name: json['name'] ?? '',
    version: json['version'] ?? '',
    status: json['status'] ?? 'offline',
    stunHost: json['stunHost'] ?? '',
    turnPort: json['turnPort'] ?? '',
    turnUser: json['turnUser'] ?? '',
    turnPass: json['turnPass'] ?? '',
    lastSeen: json['lastSeen'] ?? '',
  );
}

class OfficialApiService {
  final String baseUrl;
  final String accessToken;

  OfficialApiService({required this.baseUrl, required this.accessToken});

  Map<String, String> get _headers => {
    'Authorization': 'Bearer $accessToken',
    'Content-Type': 'application/json',
  };

  Future<List<NodeInfo>> listNodes() async {
    final uri = Uri.parse('$baseUrl/api/nodes');
    final resp = await http.get(uri, headers: _headers);
    if (resp.statusCode == 200) {
      final data = jsonDecode(resp.body) as Map<String, dynamic>;
      final list = (data['nodes'] as List?) ?? [];
      return list.map((n) => NodeInfo.fromJson(n as Map<String, dynamic>)).toList();
    }
    throw Exception('listNodes failed: ${resp.statusCode}');
  }

  Future<Map<String, dynamic>> getMe() async {
    final uri = Uri.parse('$baseUrl/api/auth/me');
    final resp = await http.get(uri, headers: _headers);
    if (resp.statusCode == 200) {
      return jsonDecode(resp.body) as Map<String, dynamic>;
    }
    throw Exception('getMe failed: ${resp.statusCode}');
  }

  Future<Map<String, dynamic>> refreshToken(String refreshToken) async {
    final uri = Uri.parse('$baseUrl/api/auth/refresh');
    final resp = await http.post(
      uri,
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'refreshToken': refreshToken}),
    );
    if (resp.statusCode == 200) {
      return jsonDecode(resp.body) as Map<String, dynamic>;
    }
    throw Exception('refreshToken failed: ${resp.statusCode}');
  }
}
