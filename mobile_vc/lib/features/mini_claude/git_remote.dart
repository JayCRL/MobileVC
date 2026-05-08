import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

import 'git.dart';

// ─── pkt-line ────────────────────────────────────────────

class PktLine {
  static String encode(String data) {
    final len = (data.length + 4).toRadixString(16).padLeft(4, '0');
    return '$len$data';
  }

  static String flush() => '0000';

  static List<String> split(Uint8List data) {
    final lines = <String>[];
    var i = 0;
    while (i + 4 <= data.length) {
      final hex = utf8.decode(data.sublist(i, i + 4));
      if (hex == '0000') {
        i += 4;
        continue; // flush packet
      }
      final len = int.tryParse(hex, radix: 16) ?? 0;
      if (len <= 4) {
        i += 4;
        continue;
      }
      final end = i + len;
      if (end > data.length) break;
      lines.add(utf8.decode(data.sublist(i + 4, end)));
      i = end;
    }
    return lines;
  }
}

// ─── variable-length int ─────────────────────────────────

class VarInt {
  final int value;
  final int bytesRead;

  VarInt(this.value, this.bytesRead);

  static VarInt read(Uint8List data, int offset) {
    var value = 0;
    var shift = 0;
    var pos = offset;
    while (pos < data.length) {
      final b = data[pos];
      pos++;
      value |= (b & 0x7f) << shift;
      shift += 7;
      if ((b & 0x80) == 0) break;
    }
    return VarInt(value, pos - offset);
  }

  static Uint8List encode(int value) {
    if (value < 0) return Uint8List(0);
    final bytes = <int>[];
    var v = value;
    while (v >= 0x80) {
      bytes.add((v & 0x7f) | 0x80);
      v >>= 7;
    }
    bytes.add(v & 0x7f);
    return Uint8List.fromList(bytes);
  }
}

// ─── packfile parser ─────────────────────────────────────

enum _ObjectType { commit, tree, blob, tag, ofsDelta, refDelta }

class _RawObj {
  final int offset;
  final _ObjectType type;
  final int inflatedSize;
  final Uint8List extraHeader;
  final int? baseOffset;
  _RawObj({required this.offset, required this.type, required this.inflatedSize,
    Uint8List? extraHeader, this.baseOffset}) : extraHeader = extraHeader ?? Uint8List(0);
}

class _PackObject {
  final _ObjectType type;
  final int offset;
  final int inflatedSize;
  final Uint8List rawData;
  final Uint8List extraHeader;
  final int? baseOffset;
  String? sha;

  _PackObject({
    required this.type,
    required this.offset,
    required this.inflatedSize,
    required this.rawData,
    Uint8List? extraHeader,
    this.baseOffset,
  }) : extraHeader = extraHeader ?? Uint8List(0);
}

class PackParser {
  final Uint8List _data;
  int _pos = 0;
  final int _objCount;

  PackParser._(this._data, this._objCount);

  /// Parse a full packfile. Returns [objectSha → decompressed bytes].
  static Map<String, Uint8List> parse(Uint8List packData) {
    if (packData.length < 12 ||
        utf8.decode(packData.sublist(0, 4)) != 'PACK') {
      throw Exception('Not a valid packfile');
    }
    final version = _be32(packData, 4);
    if (version < 2) throw Exception('Unsupported pack version $version');
    final count = _be32(packData, 8);
    if (count <= 0 || count > 100000) throw Exception('Invalid object count: $count');
    final parser = PackParser._(packData, count);
    try {
      return parser._parse();
    } catch (e) {
      throw Exception('Packfile parse error: $e');
    }
  }

  Map<String, Uint8List> _parse() {
    // Two-pass approach:
    // Pass 1: Scan all object headers and compute zlib boundaries
    // Pass 2: Decompress zlib data and resolve deltas

    // Pass 1
    _pos = 12;
    final boundaries = <int>[];
    final rawObjs = <_RawObj>[];

    for (var i = 0; i < _objCount; i++) {
      if (_pos >= _data.length) break;
      final start = _pos;
      boundaries.add(start);
      var b = _data[_pos++];
      final typeIdx = (b >> 4) & 0x07;
      final typeMap = [null, _ObjectType.commit, _ObjectType.tree, _ObjectType.blob,
        _ObjectType.tag, null, _ObjectType.ofsDelta, _ObjectType.refDelta];
      final type = typeMap[typeIdx] ?? _ObjectType.blob;

      // Read size (with bounds + iteration limit)
      var size = b & 0x0f;
      var shift = 4;
      var sizeIter = 0;
      while ((b & 0x80) != 0 && _pos < _data.length && sizeIter < 10) {
        b = _data[_pos++];
        size |= (b & 0x7f) << shift;
        shift += 7;
        sizeIter++;
      }

      // Read extra header
      Uint8List extraHeader = Uint8List(0);
      if (type == _ObjectType.refDelta && _pos + 20 <= _data.length) {
        extraHeader = _data.sublist(_pos, _pos + 20);
        _pos += 20;
      } else if (typeIdx == 6 && _pos < _data.length) {
        // OFS_DELTA: negative offset
        final extraStart = _pos;
        var negOffset = _data[_pos] & 0x7f;
        var ofsIter = 0;
        while ((_data[_pos++] & 0x80) != 0 && _pos < _data.length && ofsIter < 10) {
          negOffset = ((negOffset + 1) << 7) | (_data[_pos] & 0x7f);
          ofsIter++;
        }
        extraHeader = _data.sublist(extraStart, _pos.clamp(0, _data.length));
        // Store negative offset for base lookup
        final baseOffset = start - negOffset;
        rawObjs.add(_RawObj(
          offset: start, type: type, inflatedSize: size,
          extraHeader: extraHeader, baseOffset: baseOffset));
        continue;
      }

      rawObjs.add(_RawObj(
        offset: start, type: type, inflatedSize: size,
        extraHeader: extraHeader));
    }
    boundaries.add(_data.length - 20); // checksum position = end of last zlib

    // Pass 2: Decompress and resolve
    // For each object, zlib start = offset + size_of_header
    // zlib end = next object's offset
    final objects = <_PackObject>[];

    for (var i = 0; i < rawObjs.length; i++) {
      final raw = rawObjs[i];
      if (raw.offset >= _data.length) continue;
      final zlibEnd = (i + 1 < rawObjs.length)
          ? rawObjs[i + 1].offset.clamp(0, _data.length)
          : (_data.length - 20).clamp(0, _data.length);
      var headerPos = raw.offset;
      if (headerPos >= _data.length) continue;
      var b = _data[headerPos];
      var hdrIter = 0;
      while ((b & 0x80) != 0 && headerPos < _data.length - 1 && hdrIter < 10) {
        headerPos++;
        b = _data[headerPos];
        hdrIter++;
      }
      headerPos++; // skip last size byte
      if (raw.type == _ObjectType.refDelta) headerPos += 20;
      else if (raw.type == _ObjectType.ofsDelta) headerPos += raw.extraHeader.length;

      final zlibStart = headerPos.clamp(0, _data.length);
      final zlibEnd2 = zlibEnd.clamp(0, _data.length);
      if (zlibStart >= zlibEnd2) continue;
      final compressed = _data.sublist(zlibStart, zlibEnd2);
      final decompressed = compressed.isNotEmpty ? zLibDecode(compressed) : Uint8List(0);

      objects.add(_PackObject(
        type: raw.type,
        offset: raw.offset,
        inflatedSize: raw.inflatedSize,
        rawData: decompressed,
        extraHeader: raw.extraHeader,
        baseOffset: raw.baseOffset,
      ));
    }

    // Resolve deltas recursively
    final resolved = <String, Uint8List>{};
    for (final obj in objects) {
      _resolve(obj, objects, resolved);
    }
    return resolved;
  }

  Uint8List _resolve(
      _PackObject obj, List<_PackObject> all, Map<String, Uint8List> cache) {
    if (cache.containsKey(obj.sha)) return cache[obj.sha]!;

    Uint8List data;
    if (obj.type == _ObjectType.ofsDelta || obj.type == _ObjectType.refDelta) {
      Uint8List? baseData;
      if (obj.type == _ObjectType.refDelta) {
        final baseSha = _hex(obj.extraHeader);
        baseData = cache[baseSha] ?? _findResolved(all, cache, baseSha);
      } else {
        baseData = _findResolved(all, cache, null, baseOffset: obj.baseOffset);
      }
      if (baseData == null) {
        throw Exception('Delta base not found');
      }
      data = _applyDelta(baseData, obj.rawData);
    } else {
      data = obj.rawData;
    }

    final typeStr = _typeString(obj.type);
    final header = utf8.encode('$typeStr ${data.length}\0');
    obj.sha = sha1.convert(Uint8List.fromList(header + data)).toString();
    cache[obj.sha!] = data;
    return data;
  }

  Uint8List? _findResolved(List<_PackObject> all, Map<String, Uint8List> cache,
      String? sha, {int? baseOffset}) {
    for (final o in all) {
      if (sha != null && o.sha == sha) return _resolve(o, all, cache);
      if (baseOffset != null && o.offset == baseOffset) return _resolve(o, all, cache);
    }
    return null;
  }

  static Uint8List _applyDelta(Uint8List base, Uint8List delta) {
    var pos = 0;

    // Skip source size
    while (pos < delta.length) {
      if ((delta[pos++] & 0x80) == 0) break;
    }

    // Read target size
    var targetSize = 0;
    var shift = 0;
    while (pos < delta.length) {
      final b = delta[pos++];
      targetSize |= (b & 0x7f) << shift;
      shift += 7;
      if ((b & 0x80) == 0) break;
    }

    if (targetSize <= 0 || targetSize > 100 * 1024 * 1024) {
      return Uint8List(0);
    }
    final result = Uint8List(targetSize);
    var outPos = 0;

    while (pos < delta.length && outPos < targetSize) {
      if (pos >= delta.length) break;
      final cmd = delta[pos++];

      if ((cmd & 0x80) == 0) {
        final len = cmd;
        if (len > 0 && pos + len <= delta.length && outPos + len <= targetSize) {
          result.setRange(outPos, outPos + len, delta.sublist(pos, pos + len));
          outPos += len;
          pos += len;
        }
      } else {
        var copyOffset = 0;
        var size = 0;

        if ((cmd & 0x01) != 0 && pos < delta.length) copyOffset |= delta[pos++];
        if ((cmd & 0x02) != 0 && pos < delta.length) copyOffset |= (delta[pos++] << 8);
        if ((cmd & 0x04) != 0 && pos < delta.length) copyOffset |= (delta[pos++] << 16);
        if ((cmd & 0x08) != 0 && pos < delta.length) copyOffset |= (delta[pos++] << 24);

        if ((cmd & 0x10) != 0 && pos < delta.length) size |= delta[pos++];
        if ((cmd & 0x20) != 0 && pos < delta.length) size |= (delta[pos++] << 8);
        if ((cmd & 0x40) != 0 && pos < delta.length) size |= (delta[pos++] << 16);
        if (size == 0) size = 0x10000;

        if (copyOffset + size <= base.length && outPos + size <= targetSize) {
          result.setRange(outPos, outPos + size, base.sublist(copyOffset, copyOffset + size));
          outPos += size;
        }
      }
    }
    return result;
  }

  static int _be32(Uint8List data, int offset) =>
      (data[offset] << 24) |
      (data[offset + 1] << 16) |
      (data[offset + 2] << 8) |
      data[offset + 3];

  static Uint8List _be32Bytes(int v) {
    final b = Uint8List(4);
    b[0] = (v >> 24) & 0xFF;
    b[1] = (v >> 16) & 0xFF;
    b[2] = (v >> 8) & 0xFF;
    b[3] = v & 0xFF;
    return b;
  }

  static String _hex(Uint8List bytes) {
    return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  static String _typeString(_ObjectType type) => switch (type) {
        _ObjectType.commit => 'commit',
        _ObjectType.tree => 'tree',
        _ObjectType.blob => 'blob',
        _ObjectType.tag => 'tag',
        _ObjectType.ofsDelta => 'blob',
        _ObjectType.refDelta => 'blob',
      };
}

// ─── packfile writer ─────────────────────────────────────

class PackWriter {
  final List<_PackObj> _objects = [];
  final Set<String> _added = {};

  void addBlob(String sha, Uint8List content) {
    _add(3, sha, content);
  }

  void addTree(String sha, Uint8List content) {
    _add(2, sha, content);
  }

  void addCommit(String sha, Uint8List content) {
    _add(1, sha, content);
  }

  void _add(int type, String sha, Uint8List content) {
    if (_added.contains(sha)) return;
    _added.add(sha);
    _objects.add(_PackObj(type: type, sha: sha, data: content));
  }

  Uint8List build() {
    final buf = BytesBuilder();

    // Header
    buf.add(utf8.encode('PACK'));
    buf.add(PackParser._be32Bytes(2)); // version
    buf.add(PackParser._be32Bytes(_objects.length));

    for (final obj in _objects) {
      final compressed = zLibEncode(obj.data);
      final size = obj.data.length;

      // Type byte
      final typeBits = (obj.type & 0x7) << 4;
      final lowNibble = size & 0x0f;
      int firstByte = typeBits | lowNibble;
      if (size >= 16) firstByte |= 0x80;
      buf.add([firstByte]);

      // Extended size
      var remaining = size >> 4;
      while (remaining >= 0x80) {
        buf.add([(remaining & 0x7f) | 0x80]);
        remaining >>= 7;
      }
      if (size >= 16) {
        buf.add([remaining & 0x7f]);
      }

      buf.add(compressed);
    }

    // SHA1 checksum
    final data = buf.toBytes();
    final checksum = sha1.convert(data);
    buf.add(Uint8List.fromList(checksum.bytes));
    return buf.toBytes();
  }
}

class _PackObj {
  final int type;
  final String sha;
  final Uint8List data;
  _PackObj({required this.type, required this.sha, required this.data});
}

// ─── ref data ────────────────────────────────────────────

class _RemoteRef {
  final String sha;
  final String name;
  _RemoteRef(this.sha, this.name);
}

// ─── git HTTP protocol ───────────────────────────────────

class GitHTTP {
  final http.Client _client = http.Client();
  String? _authHeader;

  GitHTTP({String? token}) {
    if (token != null && token.isNotEmpty) {
      _authHeader = 'Basic ${base64Encode(utf8.encode('token:$token'))}';
    }
  }

  // Parse auth from URL like https://token@host/repo
  static ({String url, GitHTTP client}) fromURL(String rawUrl) {
    final uri = Uri.parse(rawUrl);
    final client = GitHTTP();
    if (uri.userInfo.isNotEmpty) {
      final token = uri.userInfo;
      client._authHeader = 'Basic ${base64Encode(utf8.encode('token:$token'))}';
    }
    return (url: rawUrl.split('@').last, client: client);
  }

  Map<String, String> get _headers => {
        'User-Agent': 'mini-claude-mobile',
        if (_authHeader != null) 'Authorization': _authHeader!,
      };

  /// Discover refs from a remote
  Future<List<_RemoteRef>> discoverRefs(String url) async {
    final uri = Uri.parse('$url/info/refs?service=git-upload-pack');
    final resp = await _client.get(uri, headers: _headers);
    if (resp.statusCode != 200) {
      throw Exception('Failed to discover refs: ${resp.statusCode} ${resp.body}');
    }
    return _parseRefAdvert(Uint8List.fromList(resp.bodyBytes));
  }

  /// Discover refs for push
  Future<List<_RemoteRef>> discoverPushRefs(String url) async {
    final uri = Uri.parse('$url/info/refs?service=git-receive-pack');
    final resp = await _client.get(uri, headers: _headers);
    if (resp.statusCode != 200) {
      throw Exception('Failed to discover refs: ${resp.statusCode} ${resp.body}');
    }
    return _parseRefAdvert(Uint8List.fromList(resp.bodyBytes));
  }

  /// Fetch objects (clone or fetch)
  Future<Uint8List> uploadPack(String url, List<String> wants,
      [List<String> haves = const []]) async {
    final uri = Uri.parse('$url/git-upload-pack');
    final body = BytesBuilder();
    for (final sha in wants) {
      body.add(utf8.encode(PktLine.encode('want $sha\n')));
    }
    if (wants.isNotEmpty) {
      body.add(utf8.encode(
          PktLine.encode('want ${wants.first} side-band-64k thin-pack ofs-delta\n')));
    }
    for (final sha in haves) {
      body.add(utf8.encode(PktLine.encode('have $sha\n')));
    }
    body.add(utf8.encode(PktLine.flush()));
    body.add(utf8.encode('0009done\n'));

    final resp = await _client.post(
      uri,
      headers: {
        ..._headers,
        'Content-Type': 'application/x-git-upload-pack-request',
      },
      body: body.toBytes(),
    );

    if (resp.statusCode != 200) {
      throw Exception('upload-pack failed: ${resp.statusCode}');
    }

    final bytes = Uint8List.fromList(resp.bodyBytes);
    // Handle side-band multiplexing (band 1 = pack data)
    return _extractPackData(bytes);
  }

  /// Push packfile
  Future<String> receivePack(String url, List<_PushRef> refs, Uint8List pack,
      {String? ref}) async {
    final uri = Uri.parse('$url/git-receive-pack');
    final body = BytesBuilder();

    // Send old/new refs + capabilities
    final capLine = 'report-status report-status-v2 delete-refs side-band-64k';
    for (final r in refs) {
      if (r.oldSha.isEmpty) {
        body.add(utf8.encode(PktLine.encode('${r.newSha} ${r.name}\0$capLine')));
      } else {
        body.add(utf8.encode(
            PktLine.encode('${r.oldSha} ${r.newSha} ${r.name}')));
      }
    }
    body.add(utf8.encode(PktLine.flush()));
    body.add(pack);

    final resp = await _client.post(
      uri,
      headers: {
        ..._headers,
        'Content-Type': 'application/x-git-receive-pack-request',
      },
      body: body.toBytes(),
    );

    if (resp.statusCode != 200) {
      throw Exception('receive-pack failed: ${resp.statusCode}');
    }

    final lines = PktLine.split(Uint8List.fromList(resp.bodyBytes));
    for (final line in lines) {
      if (line.startsWith('unpack ')) {
        if (line != 'unpack ok') {
          throw Exception('Unpack failed: $line');
        }
      }
    }
    return 'ok';
  }

  List<_RemoteRef> _parseRefAdvert(Uint8List data) {
    // Strip the smart HTTP wrapper
    final text = utf8.decode(data);
    final refs = <_RemoteRef>[];
    for (final line in text.split('\n')) {
      final parts = line.trim().split('\0')[0].split(' ');
      if (parts.length >= 2 && parts[0].length == 40) {
        final name = parts[1].trim();
        if (!name.endsWith('^{}')) {
          refs.add(_RemoteRef(parts[0], name));
        }
      }
    }
    return refs;
  }

  Uint8List _extractPackData(Uint8List data) {
    // Find the PACK signature
    for (var i = 0; i < data.length - 4; i++) {
      if (utf8.decode(data.sublist(i, i + 4)) == 'PACK') {
        // Extract to end minus 20-byte checksum
        final pack = data.sublist(i, data.length - 20);
        return pack;
      }
    }
    // If no PACK header found, try parsing side-band
    // Side-band: first byte is band number, rest is data
    final packData = BytesBuilder();
    var pos = 0;
    while (pos < data.length) {
      final hex = utf8.decode(data.sublist(pos, min(pos + 4, data.length)));
      pos += 4;
      if (hex == '0000' || hex == '0001') continue;
      final len = int.tryParse(hex, radix: 16) ?? 0;
      if (len <= 4 || pos + len - 4 > data.length) break;
      final chunk = data.sublist(pos, pos + len - 4);
      pos += len - 4;
      if (chunk.isNotEmpty && chunk[0] == 0x01) {
        // Band 1 = pack data
        packData.add(chunk.sublist(1));
      }
    }
    return packData.toBytes();
  }

  void close() => _client.close();
}

class _PushRef {
  final String name;
  final String oldSha;
  final String newSha;
  _PushRef(this.name, this.oldSha, this.newSha);
}

// ─── remote management ───────────────────────────────────

class GitRemoteEntry {
  final String name;
  final String url;
  GitRemoteEntry(this.name, this.url);
}

class GitRemote {
  final String _gitDir;

  GitRemote(this._gitDir);

  List<GitRemoteEntry> list() {
    final entries = <GitRemoteEntry>[];
    final config = File('$_gitDir/config');
    if (!config.existsSync()) return entries;

    final lines = config.readAsLinesSync();
    String? currentName;
    for (final line in lines) {
      final trimmed = line.trim();
      if (trimmed.startsWith('[remote "')) {
        final match =
            RegExp(r'\[remote\s+"(.+?)"\]').firstMatch(trimmed);
        currentName = match?.group(1);
      } else if (trimmed.startsWith('url = ') && currentName != null) {
        final url = trimmed.substring(6).trim();
        entries.add(GitRemoteEntry(currentName, url));
        currentName = null;
      }
    }
    return entries;
  }

  void add(String name, String url) {
    final config = File('$_gitDir/config');
    var content = '';
    if (config.existsSync()) content = config.readAsStringSync();

    // Remove existing remote with same name
    final lines = content.split('\n');
    final filtered = <String>[];
    var skipNext = false;
    for (final line in lines) {
      if (line.trim().startsWith('[remote "$name"')) {
        skipNext = true;
        continue;
      }
      if (skipNext && line.trim().startsWith('url ')) {
        skipNext = false;
        continue;
      }
      skipNext = false;
      filtered.add(line);
    }
    filtered.add('\n[remote "$name"]\n\turl = $url\n');
    config.writeAsStringSync(filtered.join('\n'));
  }

  void remove(String name) {
    final config = File('$_gitDir/config');
    if (!config.existsSync()) return;
    final lines = config.readAsLinesSync();
    final filtered = <String>[];
    var skipNext = false;
    for (final line in lines) {
      if (line.trim().startsWith('[remote "$name"')) {
        skipNext = true;
        continue;
      }
      if (skipNext && line.trim().startsWith('url ')) {
        skipNext = false;
        continue;
      }
      skipNext = false;
      filtered.add(line);
    }
    config.writeAsStringSync(filtered.join('\n'));
  }

  String? getURL(String name) {
    return list().where((r) => r.name == name).map((r) => r.url).firstOrNull;
  }
}

// ─── network operations: clone / fetch / push ────────────

class GitNetwork {
  final GitRepo _repo;
  final String _gitDir;
  final String _workTree;
  final Map<String, String> _credentials;

  GitNetwork({
    required GitRepo repo,
    Map<String, String> credentials = const {},
  })  : _repo = repo,
        _gitDir = repo.gitDir,
        _workTree = repo.workTree,
        _credentials = credentials;

  String? _tokenForURL(String url) {
    try {
      final host = Uri.parse(url).host;
      for (final entry in _credentials.entries) {
        if (host.contains(entry.key) || entry.key.contains(host)) {
          return entry.value;
        }
      }
    } catch (_) {}
    return null;
  }

  /// Build GitHTTP with auto-injected credentials
  ({String url, GitHTTP client}) _httpClient(String url) {
    final parsed = GitHTTP.fromURL(url);
    final token = _tokenForURL(url);
    if (token != null) {
      parsed.client._authHeader = 'Basic ${base64Encode(utf8.encode('token:$token'))}';
    }
    return parsed;
  }

  /// Clone a repository
  Future<void> clone(String url, {String branch = 'main'}) async {
    final parsed = _httpClient(url);
    final http = parsed.client;
    final cleanUrl = parsed.url;

    // Discover refs
    final refs = await http.discoverRefs(cleanUrl);
    if (refs.isEmpty) throw Exception('No refs found at $url');

    // Find the target branch ref
    var targetRef = refs.where((r) => r.name == 'refs/heads/$branch').firstOrNull;
    targetRef ??= refs.where((r) => r.name == 'HEAD').firstOrNull;
    if (targetRef == null) {
      targetRef = refs.first;
    }

    // Fetch all objects
    final wants = [targetRef.sha];
    for (final r in refs) {
      if (r.sha != targetRef.sha) wants.add(r.sha);
    }

    final packData = await http.uploadPack(cleanUrl, wants.take(50).toList());
    final objects = PackParser.parse(packData);

    // Write objects
    for (final entry in objects.entries) {
      _writeRawObject(entry.key, entry.value);
    }

    // Set up refs
    final refDir = Directory('$_gitDir/refs/remotes/origin');
    refDir.createSync(recursive: true);

    for (final ref in refs) {
      final localName = ref.name.replaceFirst('refs/heads/', '');
      if (_isHead(ref.name)) {
        // Checkout
        final objData = objects.entries
            .where((e) => e.key.startsWith(ref.sha))
            .firstOrNull;
        if (objData != null) {
          _checkoutTree(objData.key, _workTree);
        }
        // Set HEAD
        File('$_gitDir/HEAD')
            .writeAsStringSync('ref: refs/heads/$localName\n');
        // Set local branch
        final branchDir = Directory('$_gitDir/refs/heads');
        branchDir.createSync(recursive: true);
        File('$_gitDir/refs/heads/$localName').writeAsStringSync('${ref.sha}\n');
      }
      // Save remote ref
      File('$_gitDir/refs/remotes/origin/$localName')
          .writeAsStringSync('${ref.sha}\n');
    }

    http.close();
  }

  /// Fetch from a remote
  Future<void> fetch(String remoteName) async {
    final remotes = GitRemote(_gitDir);
    final url = remotes.getURL(remoteName);
    if (url == null) throw Exception('Remote "$remoteName" not found');

    final parsed = _httpClient(url);
    final http = parsed.client;
    final cleanUrl = parsed.url;

    // Discover remote refs
    final remoteRefs = await http.discoverRefs(cleanUrl);

    // Collect local refs for "have" lines
    final haves = <String>[];
    final localRefsDir = Directory('$_gitDir/refs/remotes/$remoteName');
    if (localRefsDir.existsSync()) {
      for (final file in localRefsDir.listSync(recursive: true)) {
        if (file is File) {
          final sha = file.readAsStringSync().trim();
          if (sha.length == 40) haves.add(sha);
        }
      }
    }

    // Also add HEAD
    final head = _repo.readHeadCommit();
    haves.addAll(head);

    // Wants are all remote refs we don't have
    final wants = remoteRefs.map((r) => r.sha).toList();

    if (wants.isEmpty) return;

    final packData = await http.uploadPack(cleanUrl, wants, haves);
    if (packData.isEmpty || packData.length <= 20) return;

    final objects = PackParser.parse(packData);

    for (final entry in objects.entries) {
      _writeRawObject(entry.key, entry.value);
    }

    // Update remote refs
    final refDir = Directory('$_gitDir/refs/remotes/$remoteName');
    refDir.createSync(recursive: true);

    for (final ref in remoteRefs) {
      final localName = ref.name.replaceFirst('refs/heads/', '');
      File('$_gitDir/refs/remotes/$remoteName/$localName')
          .writeAsStringSync('${ref.sha}\n');
    }

    http.close();
  }

  /// Push to a remote
  Future<String> push(String remoteName, String branch) async {
    final remotes = GitRemote(_gitDir);
    final url = remotes.getURL(remoteName);
    if (url == null) throw Exception('Remote "$remoteName" not found');

    final parsed = _httpClient(url);
    final http = parsed.client;
    final cleanUrl = parsed.url;

    // Read local ref
    final refFile = File('$_gitDir/refs/heads/$branch');
    if (!refFile.existsSync()) {
      throw Exception('Branch "$branch" not found');
    }
    final newSha = refFile.readAsStringSync().trim();

    // Discover remote refs
    final remoteRefs = await http.discoverPushRefs(cleanUrl);
    final remoteRef = remoteRefs
        .where((r) => r.name == 'refs/heads/$branch')
        .firstOrNull;
    final oldSha = remoteRef?.sha ?? '0000000000000000000000000000000000000000';

    // Collect objects to push: all objects reachable from newSha
    // but not reachable from oldSha
    final packWriter = PackWriter();
    final seen = <String>{};
    _collectObjects(newSha, packWriter, seen, stopAt: oldSha == '0000000000000000000000000000000000000000' ? null : oldSha);

    final pack = packWriter.build();

    final pushRef = _PushRef('refs/heads/$branch', oldSha, newSha);
    final result = await http.receivePack(cleanUrl, [pushRef], pack);

    // Update local remote ref
    final remoteRefDir = Directory('$_gitDir/refs/remotes/$remoteName');
    remoteRefDir.createSync(recursive: true);
    File('$_gitDir/refs/remotes/$remoteName/$branch')
        .writeAsStringSync('$newSha\n');

    http.close();
    return result;
  }

  void _collectObjects(String sha, PackWriter writer, Set<String> seen,
      {String? stopAt}) {
    if (sha == stopAt || seen.contains(sha)) return;
    seen.add(sha);

    final data = _readRawObject(sha);
    if (data == null) return;

    final type = _objectType(data);
    final content = _objectContent(data);

    switch (type) {
      case 'blob':
        writer.addBlob(sha, content);
        break;
      case 'tree':
        writer.addTree(sha, Uint8List.fromList(data));
        _collectTreeObjects(content, writer, seen, stopAt: stopAt);
        break;
      case 'commit':
        writer.addCommit(sha, Uint8List.fromList(data));
        _collectCommitObjects(content, writer, seen, stopAt: stopAt);
        break;
    }
  }

  void _collectTreeObjects(Uint8List content, PackWriter writer,
      Set<String> seen, {String? stopAt}) {
    var pos = 0;
    while (pos < content.length) {
      // Read mode + name
      final space = content.indexWhere((b) => b == 0x20, pos);
      if (space < 0) break;
      final nul = content.indexWhere((b) => b == 0x00, space + 1);
      if (nul < 0) break;
      final sha = content.sublist(nul + 1, nul + 21);
      final shaStr = _hex(sha);
      _collectObjects(shaStr, writer, seen, stopAt: stopAt);
      pos = nul + 21;
    }
  }

  void _collectCommitObjects(Uint8List content, PackWriter writer,
      Set<String> seen, {String? stopAt}) {
    final text = utf8.decode(content);
    for (final line in text.split('\n')) {
      if (line.startsWith('tree ') || line.startsWith('parent ')) {
        final sha = line.substring(5).trim();
        if (sha.length == 40) {
          _collectObjects(sha, writer, seen, stopAt: stopAt);
        }
      }
    }
  }

  /// Write raw object to .git/objects
  void _writeRawObject(String sha, Uint8List data) {
    final dir = '$_gitDir/objects/${sha.substring(0, 2)}';
    final file = File('$dir/${sha.substring(2)}');
    if (!file.existsSync()) {
      Directory(dir).createSync(recursive: true);
      file.writeAsBytesSync(zLibEncode(data));
    }
  }

  /// Read raw (compressed) object from .git/objects
  Uint8List? _readRawObject(String sha) {
    if (sha.length < 40) return null;
    final path = '$_gitDir/objects/${sha.substring(0, 2)}/${sha.substring(2)}';
    final file = File(path);
    if (!file.existsSync()) return null;
    return file.readAsBytesSync();
  }

  String? _objectType(Uint8List decompressed) {
    final nul = decompressed.indexWhere((b) => b == 0);
    if (nul < 0) return null;
    final header = utf8.decode(decompressed.sublist(0, nul));
    return header.split(' ').first;
  }

  Uint8List _objectContent(Uint8List decompressed) {
    final nul = decompressed.indexWhere((b) => b == 0);
    if (nul < 0) return decompressed;
    return decompressed.sublist(nul + 1);
  }

  /// Checkout a tree into the working directory
  void _checkoutTree(String treeSha, String target) {
    final data = _readRawObject(treeSha);
    if (data == null) return;
    final decompressed = zLibDecode(data);
    final content = _objectContent(decompressed);

    var pos = 0;
    while (pos < content.length) {
      final space = content.indexWhere((b) => b == 0x20, pos);
      if (space < 0) break;
      final mode = utf8.decode(content.sublist(pos, space));
      final nul = content.indexWhere((b) => b == 0x00, space + 1);
      if (nul < 0) break;
      final name = utf8.decode(content.sublist(space + 1, nul));
      final sha = _hex(content.sublist(nul + 1, nul + 21));

      final fullPath = '$target/$name';
      if (mode == '40000') {
        // Tree (subdirectory)
        Directory(fullPath).createSync(recursive: true);
        _checkoutTree(sha, fullPath);
      } else {
        // Blob
        final objData = _readRawObject(sha);
        if (objData != null) {
          final blobData = zLibDecode(objData);
          final blobContent = _objectContent(blobData);
          final file = File(fullPath);
          file.parent.createSync(recursive: true);
          file.writeAsBytesSync(blobContent);
        }
      }
      pos = nul + 21;
    }
  }

  bool _isHead(String ref) =>
      ref == 'HEAD' ||
      ref == 'refs/heads/HEAD';

  static String _hex(Uint8List bytes) {
    return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }
}
