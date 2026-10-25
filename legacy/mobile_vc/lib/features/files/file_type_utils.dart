import 'package:flutter/material.dart';

class FileTypeInfo {
  const FileTypeInfo({
    required this.label,
    required this.icon,
    required this.color,
    this.isImage = false,
  });

  final String label;
  final IconData icon;
  final Color color;
  final bool isImage;
}

const Set<String> _imageExtensions = {
  'png',
  'jpg',
  'jpeg',
  'webp',
  'gif',
  'bmp',
  'heic',
  'heif',
};

String fileExtensionOf(String name) {
  final normalized = name.replaceAll('\\', '/');
  final fileName = normalized.split('/').last.toLowerCase();
  final index = fileName.lastIndexOf('.');
  if (index <= 0 || index == fileName.length - 1) {
    return '';
  }
  return fileName.substring(index + 1);
}

FileTypeInfo fileTypeInfoFor(String name, {bool isDir = false}) {
  if (isDir) {
    return const FileTypeInfo(
      label: '目录',
      icon: Icons.folder_rounded,
      color: Color(0xFFF59E0B),
    );
  }

  final extension = fileExtensionOf(name);
  final fileName = name.replaceAll('\\', '/').split('/').last.toLowerCase();
  if (fileName == 'dockerfile' ||
      fileName == 'makefile' ||
      fileName == 'gemfile' ||
      fileName == 'rakefile') {
    return const FileTypeInfo(
      label: '代码',
      icon: Icons.code_rounded,
      color: Color(0xFF38BDF8),
    );
  }
  if (fileName == 'package.json' ||
      fileName == 'pubspec.yaml' ||
      fileName == 'go.mod' ||
      fileName == 'cargo.toml' ||
      fileName.startsWith('.env')) {
    return const FileTypeInfo(
      label: '配置',
      icon: Icons.tune_rounded,
      color: Color(0xFFA78BFA),
    );
  }
  if (fileName == 'readme' || fileName == 'license') {
    return const FileTypeInfo(
      label: '文本',
      icon: Icons.article_rounded,
      color: Color(0xFF60A5FA),
    );
  }
  if (_imageExtensions.contains(extension)) {
    return const FileTypeInfo(
      label: '图片',
      icon: Icons.image_rounded,
      color: Color(0xFF22C55E),
      isImage: true,
    );
  }

  switch (extension) {
    case 'dart':
    case 'go':
    case 'rs':
    case 'py':
    case 'java':
    case 'kt':
    case 'swift':
    case 'c':
    case 'cc':
    case 'cpp':
    case 'h':
    case 'hpp':
    case 'js':
    case 'jsx':
    case 'ts':
    case 'tsx':
    case 'html':
    case 'css':
    case 'scss':
    case 'sh':
    case 'bash':
    case 'zsh':
    case 'sql':
      return const FileTypeInfo(
        label: '代码',
        icon: Icons.code_rounded,
        color: Color(0xFF38BDF8),
      );
    case 'json':
    case 'jsonc':
    case 'yaml':
    case 'yml':
    case 'toml':
    case 'xml':
    case 'plist':
    case 'env':
    case 'ini':
    case 'conf':
    case 'config':
      return const FileTypeInfo(
        label: '配置',
        icon: Icons.tune_rounded,
        color: Color(0xFFA78BFA),
      );
    case 'md':
    case 'markdown':
    case 'txt':
    case 'log':
    case 'csv':
      return const FileTypeInfo(
        label: '文本',
        icon: Icons.article_rounded,
        color: Color(0xFF60A5FA),
      );
    case 'pdf':
    case 'doc':
    case 'docx':
    case 'ppt':
    case 'pptx':
    case 'xls':
    case 'xlsx':
      return const FileTypeInfo(
        label: '文档',
        icon: Icons.description_rounded,
        color: Color(0xFFFB7185),
      );
    case 'zip':
    case 'tar':
    case 'gz':
    case 'tgz':
    case 'rar':
    case '7z':
      return const FileTypeInfo(
        label: '压缩包',
        icon: Icons.inventory_2_rounded,
        color: Color(0xFFF97316),
      );
    case 'mp3':
    case 'wav':
    case 'm4a':
    case 'aac':
    case 'flac':
    case 'ogg':
      return const FileTypeInfo(
        label: '音频',
        icon: Icons.audio_file_rounded,
        color: Color(0xFF14B8A6),
      );
    case 'mp4':
    case 'mov':
    case 'mkv':
    case 'webm':
    case 'avi':
      return const FileTypeInfo(
        label: '视频',
        icon: Icons.video_file_rounded,
        color: Color(0xFFEC4899),
      );
    case 'apk':
    case 'ipa':
    case 'dmg':
    case 'exe':
    case 'msi':
    case 'deb':
    case 'rpm':
      return const FileTypeInfo(
        label: '安装包',
        icon: Icons.install_mobile_rounded,
        color: Color(0xFF10B981),
      );
    default:
      return const FileTypeInfo(
        label: '文件',
        icon: Icons.insert_drive_file_rounded,
        color: Color(0xFF94A3B8),
      );
  }
}
