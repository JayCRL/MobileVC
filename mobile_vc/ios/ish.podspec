Pod::Spec.new do |s|
  s.name             = 'ish'
  s.version          = '0.1.0'
  s.summary          = 'iSH Linux emulator for iOS'
  s.homepage         = 'https://github.com/ish-app/ish'
  s.license          = { :type => 'GPLv3' }
  s.author           = { 'ish-app' => '' }
  s.platform         = :ios, '15.5'
  s.source           = { :git => 'https://github.com/ish-app/ish.git' }
  s.source_files     = 'ish-bridge/*.{h,c}'
  s.public_header_files = 'ish-bridge/ish_bridge.h'
  s.vendored_libraries = 'libish.a', 'libish_emu.a', 'libfakefs.a'
  s.static_framework = true
  s.libraries        = 'sqlite3', 'archive', 'z'
  s.pod_target_xcconfig = {
    'HEADER_SEARCH_PATHS' => '$(PODS_TARGET_SRCROOT)/ish-headers $(PODS_TARGET_SRCROOT)/ish-bridge',
    'OTHER_LDFLAGS' => '-lsqlite3 -larchive -lz',
  }
  s.requires_arc = false
end
