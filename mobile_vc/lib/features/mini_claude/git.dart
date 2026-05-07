import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

class GitStatusEntry {
  final String path;
  final String status; // 'M', 'A', 'D', '?', ' '
  final String? oldPath; // for renamed files

  const GitStatusEntry({
    required this.path,
    required this.status,
    this.oldPath,
  });

  String get displayLine {
    if (oldPath != null) return 'R  $oldPath -> $path';
    return '$status $path';
  }
}

class GitLogEntry {
  final String hash;
  final String author;
  final DateTime date;
  final String message;

  const GitLogEntry({
    required this.hash,
    required this.author,
    required this.date,
    required this.message,
  });
}

class GitRepo {
  final String _gitDir;
  final String _workTree;
  String _userName;
  String _userEmail;

  GitRepo({
    required String workTree,
    String userName = 'mini-claude',
    String userEmail = 'mini@claude',
  })  : _workTree = workTree,
        _gitDir = '$workTree/.git',
        _userName = userName,
        _userEmail = userEmail;

  String get gitDir => _gitDir;
  String get workTree => _workTree;
  String get userName => _userName;
  String get userEmail => _userEmail;
  bool get exists => Directory(_gitDir).existsSync();

  void setUser(String name, String email) {
    _userName = name;
    _userEmail = email;
  }

  void init() {
    final git = Directory(_gitDir);
    if (git.existsSync()) return;
    git.createSync(recursive: true);
    Directory('$_gitDir/objects/pack').createSync(recursive: true);
    Directory('$_gitDir/objects/info').createSync(recursive: true);
    Directory('$_gitDir/refs/heads').createSync(recursive: true);
    Directory('$_gitDir/refs/tags').createSync(recursive: true);

    File('$_gitDir/HEAD').writeAsStringSync('ref: refs/heads/main\n');
    File('$_gitDir/config').writeAsStringSync(
      '[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n',
    );
    File('$_gitDir/description')
        .writeAsStringSync('Unnamed repository\n');
    _writeIndex([]);
  }

  // ─── status ───────────────────────────────────────────
  List<GitStatusEntry> status() {
    if (!exists) return [];
    final entries = _readIndex();
    final results = <GitStatusEntry>[];
    final seen = <String>{};

    // Check tracked files for modifications
    for (final entry in entries) {
      seen.add(entry.path);
      final file = File('$_workTree/${entry.path}');
      if (!file.existsSync()) {
        results.add(GitStatusEntry(path: entry.path, status: 'D'));
      } else {
        final currentSha = _hashBlob(file);
        if (currentSha != entry.sha) {
          results.add(GitStatusEntry(path: entry.path, status: 'M'));
        }
      }
    }

    // Find untracked files
    _collectUntracked('', seen, results);

    results.sort((a, b) => a.path.compareTo(b.path));
    return results;
  }

  void _collectUntracked(
      String prefix, Set<String> seen, List<GitStatusEntry> results) {
    final dir = Directory('$_workTree/$prefix');
    if (!dir.existsSync()) return;
    for (final entity in dir.listSync()) {
      final name = entity.path.split('/').last;
      if (name == '.git' || name.startsWith('.')) continue;
      final rel = prefix.isEmpty ? name : '$prefix/$name';
      if (entity is Directory) {
        _collectUntracked(rel, seen, results);
      } else if (entity is File && !seen.contains(rel)) {
        results.add(GitStatusEntry(path: rel, status: '?'));
      }
    }
  }

  // ─── add ──────────────────────────────────────────────
  void add(List<String> paths) {
    var entries = _readIndex();
    final entryMap = <String, IndexEntry>{for (final e in entries) e.path: e};

    for (var path in paths) {
      path = path.replaceAll(RegExp(r'^\./'), '');
      final file = File('$_workTree/$path');
      if (!file.existsSync()) {
        entryMap.remove(path);
        continue;
      }
      final sha = _hashBlob(file);
      final stat = file.statSync();
      entryMap[path] = IndexEntry(
        ctime: stat.changed,
        mtime: stat.modified,
        dev: 0,
        ino: 0,
        mode: 33188, // 0100644 in octal
        uid: 0,
        gid: 0,
        size: stat.size,
        sha: sha,
        path: path,
      );
    }

    entries = entryMap.values.toList();
    entries.sort((a, b) => a.path.compareTo(b.path));
    _writeIndex(entries);
  }

  // ─── commit ───────────────────────────────────────────
  String commit(String message) {
    final entries = _readIndex();
    if (entries.isEmpty) throw Exception('Nothing to commit');

    // Build tree recursively
    final treeSha = _writeTree(entries);
    final parents = _readHeadCommit();

    final now = DateTime.now();
    final timestamp = now.millisecondsSinceEpoch ~/ 1000;
    final tz = _tzOffset(now);

    final commitObj = StringBuffer();
    commitObj.writeln('tree $treeSha');
    for (final parent in parents) {
      commitObj.writeln('parent $parent');
    }
    commitObj.writeln('author $_userName <$_userEmail> $timestamp $tz');
    commitObj.writeln('committer $_userName <$_userEmail> $timestamp $tz');
    commitObj.writeln();
    commitObj.write(message);

    final commitSha = _writeObject('commit', commitObj.toString());

    // Update ref
    final head = _readHead();
    if (head is String) {
      // Detached HEAD
      File('$_gitDir/HEAD').writeAsStringSync(commitSha);
    } else {
      File('$_gitDir/$head').writeAsStringSync('$commitSha\n');
    }

    return commitSha;
  }

  // ─── log ──────────────────────────────────────────────
  List<GitLogEntry> log({int count = 20}) {
    final results = <GitLogEntry>[];
    var sha = _readHeadCommit().isNotEmpty ? _readHeadCommit().first : null;
    if (sha == null) return results;

    for (var i = 0; i < count && sha != null; i++) {
      final obj = _readObject(sha);
      if (obj == null) break;
      final lines = utf8.decode(obj).split('\n');
      String author = '';
      String date = '';
      final messageLines = <String>[];
      var inMessage = false;

      for (final line in lines) {
        if (inMessage) {
          messageLines.add(line);
        } else if (line.isEmpty) {
          inMessage = true;
        } else if (line.startsWith('author ')) {
          final parts = line.substring(7).split(' ');
          // author name <email> timestamp tz
          author = parts.sublist(0, parts.length - 2).join(' ');
          final ts = int.tryParse(parts[parts.length - 2]) ?? 0;
          date = DateTime.fromMillisecondsSinceEpoch(ts * 1000).toString();
        } else if (line.startsWith('parent ')) {
          sha = line.substring(7);
        }
      }

      results.add(GitLogEntry(
        hash: sha!,
        author: author,
        date: DateTime.tryParse(date) ?? DateTime.now(),
        message: messageLines.join('\n').trim(),
      ));

      // Find next parent
      final obj2 = _readObject(sha);
      if (obj2 == null) break;
      for (final line in utf8.decode(obj2).split('\n')) {
        if (line.startsWith('parent ')) {
          sha = line.substring(7);
          break;
        }
      }
    }
    return results;
  }

  // ─── diff ─────────────────────────────────────────────
  String diff({String? path}) {
    final entries = _readIndex();
    final buf = StringBuffer();

    for (final entry in entries) {
      if (path != null && entry.path != path) continue;
      final file = File('$_workTree/${entry.path}');
      if (!file.existsSync()) {
        // File deleted
        final oldContent =
            _readObject(entry.sha) != null ? utf8.decode(_readObject(entry.sha)!) : '';
        buf.writeln('diff --git a/${entry.path} b/${entry.path}');
        buf.writeln('deleted file');
        buf.writeln('--- a/${entry.path}');
        buf.writeln('+++ /dev/null');
        for (final line in oldContent.split('\n')) {
          buf.writeln('-$line');
        }
        continue;
      }

      final currentSha = _hashBlob(file);
      if (currentSha == entry.sha && path == null) continue;

      final newContent = file.readAsStringSync();
      final oldContent =
          _readObject(entry.sha) != null ? utf8.decode(_readObject(entry.sha)!) : '';

      buf.writeln('diff --git a/${entry.path} b/${entry.path}');
      buf.writeln('--- a/${entry.path}');
      buf.writeln('+++ b/${entry.path}');
      buf.write(_unifiedDiff(oldContent, newContent, entry.path));
    }
    return buf.toString();
  }

  String _unifiedDiff(String oldText, String newText, String path) {
    final oldLines = oldText.split('\n');
    final newLines = newText.split('\n');
    final hunks = _diffHunks(oldLines, newLines);
    final buf = StringBuffer();

    for (var h = 0; h < hunks.length; h++) {
      final hunk = hunks[h];
      int oldStart = hunk.oldStart;
      int newStart = hunk.newStart;

      // Include context lines
      const ctx = 3;
      final oldSlice = oldLines
          .sublist((oldStart - ctx).clamp(0, oldLines.length),
              oldStart + hunk.oldLines + ctx)
          .toList();
      final newSlice = newLines
          .sublist((newStart - ctx).clamp(0, newLines.length),
              newStart + hunk.newLines + ctx)
          .toList();

      buf.writeln(
          '@@ -${oldStart},${oldSlice.length} +${newStart},${newSlice.length} @@');

      // Simple line-by-line diff within the hunk context
      // For a proper implementation, use Myers diff, but this is good enough for v1
      var oi = oldStart - ctx;
      var ni = newStart - ctx;
      while (oi < oldStart + hunk.oldLines + ctx ||
          ni < newStart + hunk.newLines + ctx) {
        if (oi < oldStart) {
          // Context before
          if (ni < newStart) {
            // Both in context
            if (oi < oldLines.length && ni < newLines.length &&
                oldLines[oi] == newLines[ni]) {
              buf.writeln(' ${oldLines[oi]}');
            }
            oi++;
            ni++;
            continue;
          }
          oi++;
          continue;
        }
        if (ni < newStart) {
          ni++;
          continue;
        }

        if (oi >= oldStart + hunk.oldLines &&
            ni >= newStart + hunk.newLines) {
          // Context after
          if (oi < oldLines.length &&
              ni < newLines.length &&
              oi < oldStart + hunk.oldLines + ctx &&
              ni < newStart + hunk.newLines + ctx &&
              oldLines[oi] == newLines[ni]) {
            buf.writeln(' ${oldLines[oi]}');
          }
          oi++;
          ni++;
          continue;
        }

        if (oi < oldStart + hunk.oldLines) {
          buf.writeln('-${oi < oldLines.length ? oldLines[oi] : ''}');
          oi++;
        }
        if (ni < newStart + hunk.newLines) {
          buf.writeln('+${ni < newLines.length ? newLines[ni] : ''}');
          ni++;
        }
      }
    }
    return buf.toString();
  }

  // ─── index read/write ─────────────────────────────────
  List<IndexEntry> _readIndex() {
    final file = File('$_gitDir/index');
    if (!file.existsSync()) return [];
    final data = file.readAsBytesSync();
    if (data.length < 12 || utf8.decode(data.sublist(0, 4)) != 'DIRC') {
      return [];
    }

    final reader = _ByteReader(data);
    reader.skip(4); // signature
    final version = reader.u32();
    if (version != 2) return []; // Only v2 for now
    final count = reader.u32();

    final entries = <IndexEntry>[];
    for (var i = 0; i < count; i++) {
      final ctimeSec = reader.u32();
      reader.skip(4); // ctime nsec
      final mtimeSec = reader.u32();
      reader.skip(4); // mtime nsec
      final dev = reader.u32();
      final ino = reader.u32();
      final mode = reader.u32();
      final uid = reader.u32();
      final gid = reader.u32();
      final size = reader.u32();
      final sha = reader.bytes(20);
      final flags = reader.u16();
      final nameLen = flags & 0xFFF;
      final path = utf8.decode(reader.bytes(nameLen));
      // Skip null terminator
      reader.skip(1);
      // Skip padding to 8-byte boundary
      final padLen = (8 - ((reader.pos + 1) % 8)) % 8;
      reader.skip(padLen);

      entries.add(IndexEntry(
        ctime: DateTime.fromMillisecondsSinceEpoch(ctimeSec * 1000,
            isUtc: true),
        mtime: DateTime.fromMillisecondsSinceEpoch(mtimeSec * 1000,
            isUtc: true),
        dev: dev,
        ino: ino,
        mode: mode,
        uid: uid,
        gid: gid,
        size: size,
        sha: _hex(sha),
        path: path,
      ));
    }
    return entries;
  }

  void _writeIndex(List<IndexEntry> entries) {
    // Sort by path (git sorts with a specific algorithm, but simple string sort is close enough)
    entries.sort((a, b) => a.path.compareTo(b.path));

    final buf = BytesBuilder();
    // Header
    buf.add(utf8.encode('DIRC'));
    buf.add(_u32Be(2)); // version
    buf.add(_u32Be(entries.length)); // count

    for (final entry in entries) {
      final ctimeMs = entry.ctime.millisecondsSinceEpoch ~/ 1000;
      final mtimeMs = entry.mtime.millisecondsSinceEpoch ~/ 1000;
      buf.add(_u32Be(ctimeMs));
      buf.add(_u32Be(0)); // nsec
      buf.add(_u32Be(mtimeMs));
      buf.add(_u32Be(0)); // nsec
      buf.add(_u32Be(entry.dev));
      buf.add(_u32Be(entry.ino));
      buf.add(_u32Be(entry.mode));
      buf.add(_u32Be(entry.uid));
      buf.add(_u32Be(entry.gid));
      buf.add(_u32Be(entry.size));
      buf.add(_shaBytes(entry.sha));
      final pathBytes = utf8.encode(entry.path);
      final nameLen = pathBytes.length;
      buf.add(_u16Be(nameLen & 0xFFF));
      buf.add(pathBytes);
      buf.add([0]); // null terminator
      // Pad to 8-byte boundary
      final pos = 12 + buf.toBytes().length % 8;
      final pad = (8 - (pos % 8)) % 8;
      if (pad > 0) buf.add(Uint8List(pad));
    }

    // SHA1 checksum
    final content = buf.toBytes();
    buf.add(sha1.convert(content).bytes);

    File('$_gitDir/index').writeAsBytesSync(buf.toBytes());
  }

  // ─── object read/write ────────────────────────────────
  String _writeObject(String type, String content) {
    final raw = utf8.encode('$type ${content.length}\0$content');
    final hash = sha1.convert(raw).toString();
    final dir = '$_gitDir/objects/${hash.substring(0, 2)}';
    final file = File('$dir/${hash.substring(2)}');
    if (!file.existsSync()) {
      Directory(dir).createSync(recursive: true);
      file.writeAsBytesSync(zLibEncode(raw));
    }
    return hash;
  }

  Uint8List? _readObject(String sha) {
    if (sha.length < 40) return null;
    final path = '$_gitDir/objects/${sha.substring(0, 2)}/${sha.substring(2)}';
    final file = File(path);
    if (!file.existsSync()) return null;
    final compressed = file.readAsBytesSync();
    final decompressed = zLibDecode(compressed);
    // Split at null byte to get header
    final nullIdx = decompressed.indexOf(0);
    if (nullIdx < 0) return null;
    return decompressed.sublist(nullIdx + 1);
  }

  // ─── tree handling ────────────────────────────────────
  String _writeTree(List<IndexEntry> entries) {
    // Build tree hierarchy
    final tree = <String, List<IndexEntry>>{};
    for (final entry in entries) {
      final slashIdx = entry.path.indexOf('/');
      if (slashIdx < 0) {
        tree[entry.path] = [entry];
      } else {
        final prefix = entry.path.substring(0, slashIdx);
        final rest = entry.path.substring(slashIdx + 1);
        tree.putIfAbsent(prefix, () => []).add(IndexEntry(
              ctime: entry.ctime,
              mtime: entry.mtime,
              dev: entry.dev,
              ino: entry.ino,
              mode: entry.mode,
              uid: entry.uid,
              gid: entry.gid,
              size: entry.size,
              sha: entry.sha,
              path: rest,
            ));
      }
    }

    final buf = StringBuffer();
    final keys = tree.keys.toList()..sort();
    for (final key in keys) {
      final subEntries = tree[key]!;
      String sha;
      int mode;
      if (subEntries.length == 1 && !subEntries.first.path.contains('/')) {
        sha = subEntries.first.sha;
        mode = subEntries.first.mode;
      } else {
        sha = _writeTree(subEntries);
        mode = 16384; // 040000 in octal (tree)
      }
      buf.write('$mode $key\0');
      buf.write(_shaBytes(sha));
    }
    return _writeObject('tree', buf.toString());
  }

  // ─── HEAD handling ────────────────────────────────────
  Object _readHead() {
    final headFile = File('$_gitDir/HEAD');
    if (!headFile.existsSync()) return '';
    final content = headFile.readAsStringSync().trim();
    if (content.startsWith('ref: ')) {
      return content.substring(5);
    }
    return content;
  }

  List<String> readHeadCommit() => _readHeadCommit();

  List<String> _readHeadCommit() {
    final head = _readHead();
    if (head == '') return [];
    if (head is String && head.length == 40) return [head];
    if (head is String) {
      final refFile = File('$_gitDir/$head');
      if (!refFile.existsSync()) return [];
      return [refFile.readAsStringSync().trim()];
    }
    return [];
  }

  // ─── diff hunk detection ──────────────────────────────
  List<_DiffHunk> _diffHunks(List<String> oldLines, List<String> newLines) {
    // Simple line-by-line diff
    final hunks = <_DiffHunk>[];
    var oi = 0;
    var ni = 0;

    while (oi < oldLines.length || ni < newLines.length) {
      if (oi < oldLines.length && ni < newLines.length && oldLines[oi] == newLines[ni]) {
        oi++;
        ni++;
        continue;
      }

      // Found a difference
      final oldStart = oi;
      final newStart = ni;

      // Fast-forward to find the next equal line
      final oldRemaining = oldLines.length - oi;
      final newRemaining = newLines.length - ni;
      var bestOld = oldRemaining;
      var bestNew = newRemaining;
      var bestDist = oldRemaining + newRemaining;

      // Look ahead for the next matching line
      for (var dO = 0; dO < 50 && oi + dO < oldLines.length; dO++) {
        for (var dN = 0; dN < 50 && ni + dN < newLines.length; dN++) {
          if (oldLines[oi + dO] == newLines[ni + dN]) {
            final dist = dO + dN;
            if (dist < bestDist) {
              bestDist = dist;
              bestOld = dO;
              bestNew = dN;
            }
            break;
          }
        }
      }

      hunks.add(_DiffHunk(
        oldStart: oldStart + 1, // 1-indexed
        newStart: newStart + 1,
        oldLines: bestOld,
        newLines: bestNew,
      ));

      oi += bestOld;
      ni += bestNew;
    }

    return hunks;
  }

  // ─── blob hashing ─────────────────────────────────────
  String _hashBlob(File file) {
    final content = file.readAsBytesSync();
    final blob = Uint8List.fromList(
        utf8.encode('blob ${content.length}\0') + content);
    return sha1.convert(blob).toString();
  }

  // ─── helpers ──────────────────────────────────────────
  static String _hex(Uint8List bytes) {
    return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  static Uint8List _shaBytes(String hex) {
    final bytes = <int>[];
    for (var i = 0; i < hex.length; i += 2) {
      bytes.add(int.parse(hex.substring(i, i + 2), radix: 16));
    }
    return Uint8List.fromList(bytes);
  }

  static Uint8List _u32Be(int v) {
    final b = Uint8List(4);
    b[0] = (v >> 24) & 0xFF;
    b[1] = (v >> 16) & 0xFF;
    b[2] = (v >> 8) & 0xFF;
    b[3] = v & 0xFF;
    return b;
  }

  static Uint8List _u16Be(int v) {
    return Uint8List(2)..[0] = (v >> 8) & 0xFF..[1] = v & 0xFF;
  }

  static String _tzOffset(DateTime dt) {
    final offset = dt.timeZoneOffset;
    final sign = offset.isNegative ? '-' : '+';
    final hours = offset.inHours.abs().toString().padLeft(2, '0');
    final mins = (offset.inMinutes.abs() % 60).toString().padLeft(2, '0');
    return '$sign$hours$mins';
  }
}

// ─── helpers ────────────────────────────────────────────
Uint8List zLibEncode(Uint8List data) {
  final compressed = ZLibEncoder().convert(data);
  return Uint8List.fromList(compressed);
}

Uint8List zLibDecode(Uint8List data) {
  return Uint8List.fromList(ZLibDecoder().convert(data));
}

class IndexEntry {
  final DateTime ctime;
  final DateTime mtime;
  final int dev;
  final int ino;
  final int mode;
  final int uid;
  final int gid;
  final int size;
  final String sha;
  final String path;

  const IndexEntry({
    required this.ctime,
    required this.mtime,
    required this.dev,
    required this.ino,
    required this.mode,
    required this.uid,
    required this.gid,
    required this.size,
    required this.sha,
    required this.path,
  });
}

class _ByteReader {
  final Uint8List _data;
  int pos = 0;

  _ByteReader(this._data);

  void skip(int n) => pos += n;

  int u32() {
    final v = (_data[pos] << 24) |
        (_data[pos + 1] << 16) |
        (_data[pos + 2] << 8) |
        _data[pos + 3];
    pos += 4;
    return v;
  }

  int u16() {
    final v = (_data[pos] << 8) | _data[pos + 1];
    pos += 2;
    return v;
  }

  Uint8List bytes(int n) {
    final v = _data.sublist(pos, pos + n);
    pos += n;
    return v;
  }
}

class _DiffHunk {
  final int oldStart;
  final int newStart;
  final int oldLines;
  final int newLines;

  const _DiffHunk({
    required this.oldStart,
    required this.newStart,
    required this.oldLines,
    required this.newLines,
  });
}
