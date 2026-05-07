import 'dart:convert';

import 'package:http/http.dart' as http;

class CIStatus {
  final String name;
  final String status;
  final String conclusion;
  final String url;
  final String? logSnippet;

  const CIStatus({
    required this.name,
    required this.status,
    required this.conclusion,
    required this.url,
    this.logSnippet,
  });

  String get displayLine {
    final icon = conclusion == 'success'
        ? '✓'
        : conclusion == 'failure'
            ? '✗'
            : status == 'in_progress'
                ? '◎'
                : '○';
    return '$icon $name: ${conclusion.isNotEmpty ? conclusion : status}';
  }
}

class GitHubCI {
  final String? token;
  final http.Client _client = http.Client();

  GitHubCI({this.token});

  Map<String, String> get _headers => {
        'Accept': 'application/vnd.github+json',
        'User-Agent': 'mini-claude-mobile',
        if (token != null) 'Authorization': 'Bearer $token',
      };

  /// Get latest workflow runs for a repo
  Future<List<CIStatus>> getRuns(String owner, String repo,
      {int count = 5}) async {
    final uri = Uri.parse(
        'https://api.github.com/repos/$owner/$repo/actions/runs?per_page=$count');
    final resp = await _client.get(uri, headers: _headers);

    if (resp.statusCode != 200) {
      throw Exception('GitHub API error: ${resp.statusCode} ${resp.body}');
    }

    final data = jsonDecode(resp.body);
    final runs = (data['workflow_runs'] as List?) ?? [];
    return runs.map((r) {
      return CIStatus(
        name: (r['name'] ?? r['workflow_name'] ?? 'CI').toString(),
        status: (r['status'] ?? '').toString(),
        conclusion: (r['conclusion'] ?? '').toString(),
        url: (r['html_url'] ?? '').toString(),
      );
    }).toList();
  }

  /// Get logs for a specific run (summary only)
  Future<String> getRunSummary(String owner, String repo, int runId) async {
    final uri = Uri.parse(
        'https://api.github.com/repos/$owner/$repo/actions/runs/$runId/jobs');
    final resp = await _client.get(uri, headers: _headers);

    if (resp.statusCode != 200) {
      throw Exception('GitHub API error: ${resp.statusCode}');
    }

    final data = jsonDecode(resp.body);
    final jobs = (data['jobs'] as List?) ?? [];
    final buf = StringBuffer();

    for (final job in jobs) {
      final name = (job['name'] ?? job['id']).toString();
      final status = (job['status'] ?? '').toString();
      final conclusion = (job['conclusion'] ?? '').toString();
      final url = (job['html_url'] ?? '').toString();
      buf.writeln('$name: ${conclusion.isNotEmpty ? conclusion : status}');
      if (url.isNotEmpty) buf.writeln('  $url');
      for (final step in (job['steps'] as List?) ?? []) {
        final stepName = (step['name'] ?? '').toString();
        final stepStatus = (step['status'] ?? '').toString();
        final stepConclusion = (step['conclusion'] ?? '').toString();
        if (stepConclusion == 'failure' || stepStatus == 'in_progress') {
          buf.writeln(
              '  ${stepConclusion == 'failure' ? '✗' : '◎'} $stepName');
        }
      }
    }

    return buf.toString();
  }

  void close() => _client.close();
}
