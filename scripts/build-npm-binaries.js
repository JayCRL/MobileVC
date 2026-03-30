#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const MAIN_PACKAGE = require(path.join(ROOT, 'package.json'));
const VERSION = process.env.MOBILEVC_VERSION || MAIN_PACKAGE.version;
const COMMIT = process.env.MOBILEVC_COMMIT || 'unknown';
const BUILD_DATE = process.env.MOBILEVC_BUILD_DATE || new Date().toISOString();
const SOURCE_DIR = path.join(ROOT, 'web');
const TARGET_DIR = path.join(ROOT, 'cmd', 'server', 'web');

syncEmbeddedWeb();

const TARGETS = [
  { target: 'darwin-arm64', goos: 'darwin', goarch: 'arm64', packageName: '@justprove/mobilevc-server-darwin-arm64' },
  { target: 'darwin-x64', goos: 'darwin', goarch: 'amd64', packageName: '@justprove/mobilevc-server-darwin-x64' },
  { target: 'linux-arm64', goos: 'linux', goarch: 'arm64', packageName: '@justprove/mobilevc-server-linux-arm64' },
  { target: 'linux-x64', goos: 'linux', goarch: 'amd64', packageName: '@justprove/mobilevc-server-linux-x64' },
  { target: 'win32-x64', goos: 'windows', npmOs: 'win32', goarch: 'amd64', packageName: '@justprove/mobilevc-server-win32-x64', extension: '.exe' },
];

for (const target of TARGETS) {
  buildTarget(target);
}

syncMainOptionalDependencies();

function syncEmbeddedWeb() {
  if (!fs.existsSync(SOURCE_DIR)) {
    throw new Error(`Source web directory not found: ${SOURCE_DIR}`);
  }

  fs.rmSync(TARGET_DIR, { recursive: true, force: true });
  fs.mkdirSync(TARGET_DIR, { recursive: true });
  copyRecursive(SOURCE_DIR, TARGET_DIR);
}

function copyRecursive(sourceDir, targetDir) {
  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const sourcePath = path.join(sourceDir, entry.name);
    const targetPath = path.join(targetDir, entry.name);

    if (entry.isDirectory()) {
      fs.mkdirSync(targetPath, { recursive: true });
      copyRecursive(sourcePath, targetPath);
      continue;
    }

    if (entry.isFile()) {
      fs.copyFileSync(sourcePath, targetPath);
    }
  }
}

function buildTarget(target) {
  const packageDir = path.join(ROOT, 'packages', `server-${target.target}`);
  const binDir = path.join(packageDir, 'bin');
  fs.mkdirSync(binDir, { recursive: true });

  const binaryName = `mobilevc-server${target.extension || ''}`;
  const binaryPath = path.join(binDir, binaryName);
  const ldflags = [
    `-X main.version=${VERSION}`,
    `-X main.commit=${COMMIT}`,
    `-X main.buildDate=${BUILD_DATE}`,
  ].join(' ');

  const result = spawnSync('go', ['build', '-trimpath', '-ldflags', ldflags, '-o', binaryPath, './cmd/server'], {
    cwd: ROOT,
    stdio: 'inherit',
    env: {
      ...process.env,
      GOOS: target.goos,
      GOARCH: target.goarch,
      CGO_ENABLED: '0',
    },
  });

  if (result.status !== 0) {
    process.exit(result.status || 1);
  }

  if (target.goos !== 'windows') {
    fs.chmodSync(binaryPath, 0o755);
  }

  fs.writeFileSync(path.join(packageDir, 'package.json'), `${JSON.stringify({
    name: target.packageName,
    version: VERSION,
    description: `Precompiled MobileVC server binary for ${target.target}`,
    license: MAIN_PACKAGE.license,
    os: [target.npmOs || target.goos],
    cpu: [target.goarch === 'amd64' ? 'x64' : target.goarch],
    files: ['bin/', 'README.md'],
  }, null, 2)}\n`);

  fs.writeFileSync(path.join(packageDir, 'README.md'), `# ${target.packageName}\n\nPrecompiled MobileVC server binary for ${target.target}.\n`);
}

function syncMainOptionalDependencies() {
  const packageJsonPath = path.join(ROOT, 'package.json');
  const packageJson = JSON.parse(fs.readFileSync(packageJsonPath, 'utf8'));
  const optionalDependencies = { ...(packageJson.optionalDependencies || {}) };

  for (const target of TARGETS) {
    optionalDependencies[target.packageName] = VERSION;
  }

  packageJson.optionalDependencies = optionalDependencies;
  fs.writeFileSync(packageJsonPath, `${JSON.stringify(packageJson, null, 2)}\n`);
}
