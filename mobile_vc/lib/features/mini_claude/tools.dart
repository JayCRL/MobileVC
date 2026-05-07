import 'dart:io';

import 'anthropic_api.dart';
import 'ci.dart';
import 'git.dart';
import 'git_remote.dart';
import 'ish_bridge.dart';

class ToolResult {
  final String content;
  final bool isError;

  const ToolResult({required this.content, this.isError = false});

  factory ToolResult.success(String content) =>
      ToolResult(content: content, isError: false);
  factory ToolResult.error(String content) =>
      ToolResult(content: content, isError: true);
}

class ToolRegistry {
  final String workingDir;
  final String _gitName;
  final String _gitEmail;
  final Map<String, String> _gitCredentials;

  ToolRegistry({
    required this.workingDir,
    String gitName = 'mini-claude',
    String gitEmail = 'mini@claude',
    Map<String, String> gitCredentials = const {},
  })  : _gitName = gitName,
        _gitEmail = gitEmail,
        _gitCredentials = gitCredentials;

  String _safePath(String raw) {
    final resolved = raw.startsWith('/')
        ? raw
        : '${workingDir.replaceAll(RegExp(r'/$'), '')}/$raw';
    String canonical;
    try {
      canonical = Directory(resolved).resolveSymbolicLinksSync();
    } catch (_) {
      // iOS sandbox paths may not support symlink resolution; use as-is
      canonical = Directory(resolved).absolute.path;
    }
    String root;
    try {
      root = Directory(workingDir).resolveSymbolicLinksSync();
    } catch (_) {
      root = Directory(workingDir).absolute.path;
    }
    if (!canonical.startsWith(root + Platform.pathSeparator) &&
        canonical != root) {
      throw Exception('Path escapes working directory: $raw');
    }
    return canonical;
  }

  List<AnthropicTool> get definitions => [
        AnthropicTool(
          name: 'read',
          description:
              'Read a file from the project. Returns the file content with line numbers (like cat -n). '
              'Use offset/limit for large files.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'file_path': {
                'type': 'string',
                'description': 'Absolute or relative path to the file',
              },
              'offset': {
                'type': 'integer',
                'description': 'Line number to start reading from (1-indexed)',
              },
              'limit': {
                'type': 'integer',
                'description': 'Maximum number of lines to read',
              },
            },
            'required': ['file_path'],
          },
        ),
        AnthropicTool(
          name: 'write',
          description:
              'Create a new file or overwrite an existing file with new content.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'file_path': {
                'type': 'string',
                'description': 'Path to the file to write',
              },
              'content': {
                'type': 'string',
                'description': 'The full content to write to the file',
              },
            },
            'required': ['file_path', 'content'],
          },
        ),
        AnthropicTool(
          name: 'edit',
          description:
              'Make a precise string replacement in an existing file. '
              'The old_string must match exactly one location in the file. '
              'If it matches multiple locations, the edit will fail and you must '
              'include more surrounding context to make it unique.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'file_path': {
                'type': 'string',
                'description': 'Path to the file to edit',
              },
              'old_string': {
                'type': 'string',
                'description': 'The exact text to replace',
              },
              'new_string': {
                'type': 'string',
                'description': 'The text to replace it with',
              },
            },
            'required': ['file_path', 'old_string', 'new_string'],
          },
        ),
        AnthropicTool(
          name: 'grep',
          description:
              'Search for a pattern in files. Returns matching lines with file paths and line numbers.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'pattern': {
                'type': 'string',
                'description': 'The regex pattern to search for',
              },
              'path': {
                'type': 'string',
                'description':
                    'Directory or file to search in. Defaults to project root.',
              },
              'include': {
                'type': 'string',
                'description': 'File pattern to include (e.g. "*.dart")',
              },
            },
            'required': ['pattern'],
          },
        ),
        AnthropicTool(
          name: 'glob',
          description:
              'Find files matching a glob pattern. Returns relative file paths.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'pattern': {
                'type': 'string',
                'description':
                    'Glob pattern (e.g. "**.dart", "lib/**/*.dart", "*.yaml")',
              },
              'path': {
                'type': 'string',
                'description':
                    'Directory to search in. Defaults to project root.',
              },
            },
            'required': ['pattern'],
          },
        ),
        AnthropicTool(
          name: 'ish',
          description:
              'Execute a Linux command inside the Alpine Linux environment (via iSH). '
              'The full Alpine Linux package manager is available: '
              'apk add nodejs npm, apk add go, apk add gcc make, etc. '
              'Use this to install packages, compile code, run scripts, '
              'or any other Linux shell operation.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'command': {
                'type': 'string',
                'description':
                    'The shell command to execute (e.g. "apk add nodejs", "gcc --version", "npm install")',
              },
            },
            'required': ['command'],
          },
        ),
        AnthropicTool(
          name: 'ci',
          description:
              'Check CI/CD build status on GitHub Actions. '
              'Usage: "owner/repo" (e.g. "ci cline/cline"), '
              'or "owner/repo <run-id>" for details.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'repo': {
                'type': 'string',
                'description':
                    'GitHub repository as "owner/repo" (e.g. "flutter/flutter")',
              },
              'run_id': {
                'type': 'integer',
                'description': 'Optional: specific workflow run ID for details',
              },
            },
            'required': ['repo'],
          },
        ),
        AnthropicTool(
          name: 'git',
          description:
              'Run a safe git command. Supported: status, diff, diff --staged, '
              'add <files>, commit -m <message>, log. '
              'Dangerous commands (push --force, reset --hard, etc.) are blocked.',
          inputSchema: {
            'type': 'object',
            'properties': {
              'command': {
                'type': 'string',
                'description':
                    'The git subcommand to run. E.g. "status", "diff", "diff --staged", '
                    '"add lib/foo.dart", \'commit -m "message"\', "log --oneline -10"',
              },
            },
            'required': ['command'],
          },
        ),
      ];

  Future<ToolResult> execute(String toolName, Map<String, dynamic> input) async {
    try {
      switch (toolName) {
        case 'read':
          return _read(input);
        case 'write':
          return _write(input);
        case 'edit':
          return _edit(input);
        case 'grep':
          return _grep(input);
        case 'glob':
          return _glob(input);
        case 'ish':
          return _ish(input);
        case 'ci':
          return _ci(input);
        case 'git':
          return _git(input);
        default:
          return ToolResult.error('Unknown tool: $toolName');
      }
    } catch (e) {
      return ToolResult.error('$e');
    }
  }

  ToolResult _read(Map<String, dynamic> input) {
    final filePath = _safePath((input['file_path'] ?? '').toString());
    final offset = (input['offset'] as int?) ?? 0;
    final limit = (input['limit'] as int?);

    if (filePath.isEmpty) {
      return ToolResult.error('file_path is required');
    }

    final file = File(filePath);
    if (!file.existsSync()) {
      return ToolResult.error('File not found: $filePath');
    }

    final lines = file.readAsLinesSync();
    final start = offset > 0 ? offset - 1 : 0;
    final end = limit != null ? start + limit : lines.length;
    final slice = lines.sublist(
      start.clamp(0, lines.length),
      end.clamp(0, lines.length),
    );

    final buf = StringBuffer();
    for (var i = 0; i < slice.length; i++) {
      final lineNum = start + i + 1;
      buf.writeln('$lineNum\t${slice[i]}');
    }
    if (slice.isEmpty) {
      return ToolResult.success('(empty)');
    }
    return ToolResult.success(buf.toString());
  }

  ToolResult _write(Map<String, dynamic> input) {
    final filePath = _safePath((input['file_path'] ?? '').toString());
    final content = (input['content'] ?? '').toString();

    if (filePath.isEmpty) {
      return ToolResult.error('file_path is required');
    }

    final file = File(filePath);
    file.parent.createSync(recursive: true);
    file.writeAsStringSync(content);

    return ToolResult.success('Wrote ${content.length} bytes to $filePath');
  }

  ToolResult _edit(Map<String, dynamic> input) {
    final filePath = _safePath((input['file_path'] ?? '').toString());
    final oldStr = (input['old_string'] ?? '').toString();
    final newStr = (input['new_string'] ?? '').toString();

    if (filePath.isEmpty) {
      return ToolResult.error('file_path is required');
    }

    final file = File(filePath);
    if (!file.existsSync()) {
      return ToolResult.error('File not found: $filePath');
    }

    final content = file.readAsStringSync();
    final count = oldStr.allMatches(content).length;

    if (count == 0) {
      return ToolResult.error(
        'old_string not found in file. Make sure the text matches exactly '
        '(including whitespace).',
      );
    }
    if (count > 1) {
      return ToolResult.error(
        'old_string matches $count locations in the file. '
        'Include more surrounding context to make it unique.',
      );
    }

    final newContent = content.replaceFirst(oldStr, newStr);
    file.writeAsStringSync(newContent);

    return ToolResult.success('Successfully edited $filePath');
  }

  ToolResult _grep(Map<String, dynamic> input) {
    final pattern = (input['pattern'] ?? '').toString();
    final searchPath = input['path'] != null
        ? _safePath(input['path'].toString())
        : workingDir;
    final include = (input['include'] as String?)?.trim();

    if (pattern.isEmpty) {
      return ToolResult.error('pattern is required');
    }

    final regex = RegExp(pattern);
    final results = <String>[];
    final dir = Directory(searchPath);

    if (!dir.existsSync()) {
      return ToolResult.error('Path not found: $searchPath');
    }

    void searchDir(Directory d) {
      for (final entity in d.listSync(recursive: false)) {
        if (entity is File) {
          if (include != null && !_globMatch(entity.path, include)) continue;
          // Skip binary-ish files
          final name = entity.path.toLowerCase();
          if (name.endsWith('.png') ||
              name.endsWith('.jpg') ||
              name.endsWith('.jpeg') ||
              name.endsWith('.gif') ||
              name.endsWith('.ico') ||
              name.endsWith('.pdf') ||
              name.endsWith('.zip') ||
              name.endsWith('.tar') ||
              name.endsWith('.gz')) {
            continue;
          }
          try {
            final lines = entity.readAsLinesSync();
            for (var i = 0; i < lines.length; i++) {
              if (regex.hasMatch(lines[i])) {
                results.add('${entity.path}:${i + 1}: ${lines[i]}');
              }
            }
          } catch (_) {
            // Skip files that can't be read as text
          }
        } else if (entity is Directory) {
          final dirName = entity.path.split('/').last;
          if (dirName.startsWith('.') &&
              dirName != '.' &&
              dirName != '..') {
            continue;
          }
          searchDir(entity);
        }
      }
    }

    searchDir(dir);

    if (results.isEmpty) {
      return ToolResult.success('No matches found for "$pattern"');
    }
    return ToolResult.success(results.join('\n'));
  }

  ToolResult _glob(Map<String, dynamic> input) {
    final pattern = (input['pattern'] ?? '').toString();
    final searchPath = input['path'] != null
        ? _safePath(input['path'].toString())
        : workingDir;

    if (pattern.isEmpty) {
      return ToolResult.error('pattern is required');
    }

    final results = <String>[];
    final dir = Directory(searchPath);

    if (!dir.existsSync()) {
      return ToolResult.error('Path not found: $searchPath');
    }

    for (final entity
        in dir.listSync(recursive: true, followLinks: false)) {
      if (entity is! File) continue;
      final relPath = entity.path.replaceFirst('$searchPath/', '');
      if (_globMatch(relPath, pattern)) {
        results.add(relPath);
      }
    }

    if (results.isEmpty) {
      return ToolResult.success('No files match "$pattern"');
    }
    results.sort();
    return ToolResult.success(results.join('\n'));
  }

  Future<ToolResult> _ish(Map<String, dynamic> input) async {
    final command = (input['command'] ?? '').toString().trim();
    if (command.isEmpty) {
      return ToolResult.error('command is required');
    }

    try {
      await IshBridge.ensureInitialized();
      if (!IshBridge.ready) {
        return ToolResult.error('iSH Linux not ready. Try again in a moment.');
      }
      final result = await IshBridge.exec(command);
      if (result.ok) {
        final out = result.output.trim();
        return ToolResult.success(
            out.isNotEmpty ? out : '(completed)');
      }
      return ToolResult.error(
          result.output.isNotEmpty ? result.output : 'exit code ${result.exitCode}');
    } catch (e) {
      return ToolResult.error('iSH failed: $e');
    }
  }

  Future<ToolResult> _ci(Map<String, dynamic> input) async {
    final repo = (input['repo'] ?? '').toString().trim();
    final runId = (input['run_id'] as int?) ?? 0;

    if (repo.isEmpty) {
      return ToolResult.error('repo is required (e.g. "owner/repo")');
    }

    final parts = repo.split('/');
    if (parts.length != 2) {
      return ToolResult.error('repo must be "owner/repo" format');
    }

    // Use GitHub token from credentials if available
    final token = _gitCredentials['github.com'];
    final ci = GitHubCI(token: token);

    try {
      if (runId > 0) {
        final summary =
            await ci.getRunSummary(parts[0], parts[1], runId);
        return ToolResult.success(summary);
      }

      final runs =
          await ci.getRuns(parts[0], parts[1]);
      if (runs.isEmpty) {
        return ToolResult.success('No workflow runs found');
      }
      final buf = StringBuffer();
      for (final r in runs) {
        buf.writeln(r.displayLine);
        if (r.url.isNotEmpty) buf.writeln('  ${r.url}');
      }
      return ToolResult.success(buf.toString());
    } catch (e) {
      return ToolResult.error('CI check failed: $e');
    } finally {
      ci.close();
    }
  }

  Future<ToolResult> _git(Map<String, dynamic> input) async {
    final command = (input['command'] ?? '').toString().trim();

    if (command.isEmpty) {
      return ToolResult.error('command is required');
    }

    final lower = command.toLowerCase();
    final blocked = [
      'reset',
      'checkout -',
      'clean',
      'rebase',
      'stash',
      'merge',
      'revert',
      'cherry-pick',
      'bisect',
    ];

    for (final b in blocked) {
      if (lower.startsWith(b)) {
        return ToolResult.error(
          'Git "$b" is blocked. '
          'Use only: init, status, diff, add, commit, log.',
        );
      }
    }

    try {
      final repo = GitRepo(workTree: workingDir, userName: _gitName, userEmail: _gitEmail);

      if (lower == 'init') {
        repo.init();
        return ToolResult.success('Initialized empty Git repository in ${repo.exists ? workingDir : workingDir}');
      }

      if (!repo.exists) {
        return ToolResult.error(
          'Not a git repository. Run "git init" first.',
        );
      }

      if (lower == 'status' || lower == 'status --short' || lower == 'status -s') {
        final entries = repo.status();
        if (entries.isEmpty) {
          return ToolResult.success('nothing to commit, working tree clean');
        }
        return ToolResult.success(entries.map((e) => e.displayLine).join('\n'));
      }

      if (lower == 'diff' || lower.startsWith('diff ')) {
        final parts = command.split(' ');
        String? path;
        if (parts.length > 1 && !parts[1].startsWith('-')) {
          path = parts[1];
        }
        final diff = repo.diff(path: path);
        if (diff.trim().isEmpty) return ToolResult.success('(no changes)');
        return ToolResult.success(diff);
      }

      if (lower.startsWith('add ')) {
        final files = _parseAddFiles(command);
        repo.add(files);
        return ToolResult.success('Staged ${files.length} file(s)');
      }

      if (lower.startsWith('commit ')) {
        // Extract message: commit -m "msg" or commit -m"msg"
        final msgMatch = RegExp(r'''commit\s+-m\s*['"](.+?)['"]''').firstMatch(command);
        final msg = msgMatch?.group(1) ?? command.replaceFirst(RegExp(r'commit\s+-m\s*'), '');
        if (msg.isEmpty) {
          return ToolResult.error('commit requires a -m "message"');
        }
        final sha = repo.commit(msg);
        return ToolResult.success('[$sha] $msg');
      }

      if (lower.startsWith('log')) {
        final countMatch = RegExp(r'-(\d+)').firstMatch(command);
        final count = countMatch != null ? int.parse(countMatch.group(1)!) : 10;
        final entries = repo.log(count: count);
        if (entries.isEmpty) return ToolResult.success('(no commits)');
        final buf = StringBuffer();
        for (final e in entries) {
          buf.writeln('commit ${e.hash}');
          if (e.author.isNotEmpty) buf.writeln('Author: ${e.author}');
          buf.writeln('Date:   ${e.date}');
          buf.writeln();
          buf.writeln('    ${e.message}');
          buf.writeln();
        }
        return ToolResult.success(buf.toString());
      }

      if (lower.startsWith('clone ')) {
        final parts = command.split(' ');
        final url = parts.length > 1 ? parts[1] : '';
        if (url.isEmpty) return ToolResult.error('clone requires a URL');
        final network = GitNetwork(repo: repo, credentials: _gitCredentials);
        await network.clone(url);
        return ToolResult.success('Cloned $url');
      }

      if (lower == 'fetch' || lower.startsWith('fetch ')) {
        final parts = command.split(' ');
        final remote = parts.length > 1 ? parts[1] : 'origin';
        if (!repo.exists) {
          return ToolResult.error('Not a git repository. Run "git init" first.');
        }
        final network = GitNetwork(repo: repo, credentials: _gitCredentials);
        await network.fetch(remote);
        return ToolResult.success('Fetched from $remote');
      }

      if (lower == 'push' || lower.startsWith('push ')) {
        final parts = command.split(' ');
        var remote = 'origin';
        var branch = 'main';
        if (parts.length > 1 && !parts[1].startsWith('-')) {
          remote = parts[1];
        }
        if (parts.length > 2 && !parts[2].startsWith('-')) {
          branch = parts[2];
        }
        if (!repo.exists) {
          return ToolResult.error('Not a git repository. Run "git init" first.');
        }
        final network = GitNetwork(repo: repo, credentials: _gitCredentials);
        final result = await network.push(remote, branch);
        return ToolResult.success('Push result: $result');
      }

      if (lower == 'pull') {
        if (!repo.exists) {
          return ToolResult.error('Not a git repository. Run "git init" first.');
        }
        // pull = fetch + no merge (just update tracking refs)
        final network = GitNetwork(repo: repo, credentials: _gitCredentials);
        await network.fetch('origin');
        return ToolResult.success('Pulled from origin');
      }

      // Remote management
      if (lower.startsWith('remote ')) {
        final remotes = GitRemote(repo.gitDir);
        final args = command.substring('remote'.length).trim();

        if (args == '' || args == '-v' || args == 'list') {
          final list = remotes.list();
          if (list.isEmpty) return ToolResult.success('(no remotes)');
          return ToolResult.success(
              list.map((r) => '${r.name}\t${r.url}').join('\n'));
        }

        if (args.startsWith('add ')) {
          final addArgs = args.substring(4).trim().split(' ');
          if (addArgs.length < 2) {
            return ToolResult.error('remote add <name> <url>');
          }
          remotes.add(addArgs[0], addArgs[1]);
          return ToolResult.success('Added remote "${addArgs[0]}"');
        }

        if (args.startsWith('remove ') || args.startsWith('rm ')) {
          final name = args.split(' ').last.trim();
          remotes.remove(name);
          return ToolResult.success('Removed remote "$name"');
        }

        return ToolResult.error('remote: use add/remove/list');
      }

      return ToolResult.error(
        'Unsupported git command: "$command". '
        'Supported: init, status, diff, add <files>, commit -m "msg", log, '
        'clone <url>, fetch, push, pull, remote add/remove/list',
      );
    } catch (e) {
      return ToolResult.error('Git failed: $e');
    }
  }

  List<String> _parseAddFiles(String command) {
    final withoutAdd = command.substring('add'.length).trim();
    if (withoutAdd == '.' || withoutAdd == '-A' || withoutAdd == '--all') {
      // Add all untracked and modified
      final repo = GitRepo(workTree: workingDir, userName: _gitName, userEmail: _gitEmail);
      return repo.status().where((e) => e.status != ' ').map((e) => e.path).toList();
    }
    return withoutAdd.split(' ').where((f) => f.trim().isNotEmpty).toList();
  }

  bool _globMatch(String path, String pattern) {
    final regex = RegExp(
      '^${pattern.split('*').map((p) => RegExp.escape(p)).join('.*')}\$',
      caseSensitive: true,
    );
    return regex.hasMatch(path);
  }
}
