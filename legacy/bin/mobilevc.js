#!/usr/bin/env node

const fs = require('fs');
const os = require('os');
const path = require('path');
const readline = require('readline');
const { spawn, spawnSync } = require('child_process');
const http = require('http');
const net = require('net');
const qrcode = require('qrcode-terminal');

const PACKAGE_NAME = 'mobilevc';
const SERVER_BINARY_NAME = process.platform === 'win32' ? 'mobilevc-server.exe' : 'mobilevc-server';
const STATE_DIR = path.join(os.homedir(), '.mobilevc', 'launcher');
const LOG_DIR = path.join(os.homedir(), '.mobilevc', 'logs');
const CONFIG_PATH = path.join(STATE_DIR, 'config.json');
const STATE_PATH = path.join(STATE_DIR, 'state.json');
const DEFAULT_PORT = '8001';
const DEFAULT_LANGUAGE = 'zh';
const DEFAULT_RELAY_URL = 'ws://127.0.0.1:9000';
const RELAY_PAIRING_EVENT_PREFIX = 'mobilevc-relay-pairing-';
const RELAY_AGENT_SESSION_PREFIX = 'mobilevc-relay-agent-session-';

const PLATFORM_PACKAGES = {
  'darwin-arm64': '@justprove/mobilevc-server-darwin-arm64',
  'darwin-x64': '@justprove/mobilevc-server-darwin-x64',
  'linux-arm64': '@justprove/mobilevc-server-linux-arm64',
  'linux-x64': '@justprove/mobilevc-server-linux-x64',
  'win32-x64': '@justprove/mobilevc-server-win32-x64',
};

const MESSAGES = {
  zh: {
    helpTitle: '🐱 MobileVC 启动器',
    help: [
      '用法：',
      '  mobilevc           交互式配置并启动 MobileVC 后端',
      '  mobilevc start     启动 MobileVC 后端（默认）',
      '  mobilevc public --relay wss://relay.example.com  保存并启动 LAN + Relay 模式',
      '  mobilevc public --relay wss://relay.example.com --network-exposure-mode relay-only  仅开放 Relay',
      '  mobilevc public    使用已保存 Relay，或本地开发 Relay',
      '  mobilevc restart   重启 MobileVC 后端',
      '  mobilevc stop      停止已保存的后端进程',
      '  mobilevc status    查看保存的状态和健康检查',
      '  mobilevc config    重新配置端口、AUTH_TOKEN 和语言',
      '  mobilevc logs      打印后端日志（加 --follow 跟随）',
      '  mobilevc help      显示帮助',
    ],
    selectLanguage: '选择语言 / Language [1=中文, 2=English]: ',
    backendPort: (current) => `后端端口 [${current}]: `,
    authToken: (current) => current ? 'AUTH_TOKEN [已保存]: ' : 'AUTH_TOKEN: ',
    savedConfig: (filePath) => `🐈 已保存启动器配置到 ${filePath}`,
    launcherStateNotFound: '🐱 未找到启动器状态',
    noSavedBackendProcess: '🐈 没有已保存的后端进程',
    noLogsFound: '🐱 没有找到日志',
    invalidPort: '🐱 端口无效',
    authRequired: '🐈 AUTH_TOKEN 是必填项',
    startingBackend: (port) => `🐱 正在启动 MobileVC 后端，端口 ${port}`,
    logFile: (filePath) => `🐈 日志文件：${filePath}`,
    pid: (pid) => `PID: ${pid}`,
    alreadyRunning: (pid, port) => `MobileVC 已在运行（pid ${pid}，端口 ${port}）`,
    backendNotRunning: '后端进程未运行',
    stoppedBackend: '已停止 MobileVC 后端',
    statusPid: 'pid',
    statusPort: '端口',
    statusAlive: '存活',
    statusHealthz: '健康检查',
    statusStartedAt: '启动时间',
    statusLogPath: '日志路径',
    statusBinaryPath: 'binaryPath',
    statusPlatformTarget: 'platformTarget',
    statusServerVersion: 'serverVersion',
    statusAICliAvailable: 'AI CLI 可用 (Claude/Codex)',
    statusHomeWritable: 'HOME 可写',
    statusPreflight: 'preflight',
    qrTitle: 'Flutter 扫码连接局域网后端',
    qrHint: '用 Flutter 客户端扫码，可自动回填局域网地址、端口和 token',
    localAccess: '本机访问',
    lanAccess: '局域网访问',
    qrUnavailable: '未检测到可用的局域网 IPv4 地址，暂时无法生成二维码',
    preflightTitle: '启动前检查',
    preflightBlocking: '阻塞项',
    preflightWarnings: '提示项',
    preflightOk: '无',
    missingBinaryForPlatform: (target) => `当前平台 ${target} 没有可用的预编译 server 包`,
    binaryMissing: (filePath) => `未找到 server binary：${filePath}`,
    binaryNotExecutable: (filePath) => `server binary 不可执行：${filePath}`,
    homeNotWritable: (homePath) => `HOME 不可写：${homePath}`,
    authTokenMissing: '未配置 AUTH_TOKEN，请先运行 mobilevc config',
    relayURLInvalid: (detail) => `Relay URL 无效：${detail}`,
    relayPairingTimedOut: (filePath) => `Relay 配对信息未就绪，请检查日志：${filePath}`,
    relayQrTitle: '扫码连接 MobileVC Relay',
    relayQrHint: '二维码包含一次性配对 secret，请在过期前使用',
    relayAccess: 'Relay 连接',
    relayLanAccess: '局域网直连仍可用',
    aiCliMissing: '当前启动器未检测到可用的 Claude/Codex CLI；请确认 claude 或 codex 命令可在终端中直接执行。',
    portInUse: (port) => `端口 ${port} 已被其他进程占用`,
    startupFailed: '启动失败，请检查日志和 preflight 提示',
    startupTimedOut: (seconds, filePath) => `后端在 ${seconds} 秒内未就绪，请检查日志：${filePath}`,
    startupExited: (filePath, detail) => `后端启动后很快退出（${detail}），请检查日志：${filePath}`,
    statusUnavailable: '未知',
  },
  en: {
    helpTitle: '🐱 MobileVC launcher',
    help: [
      'Usage:',
      '  mobilevc           Configure interactively and start the MobileVC backend',
      '  mobilevc start     Start the MobileVC backend (default)',
      '  mobilevc public --relay wss://relay.example.com  Save and start LAN + Relay mode',
      '  mobilevc public --relay wss://relay.example.com --network-exposure-mode relay-only  Relay-only exposure',
      '  mobilevc public    Start with the saved Relay, or the local development Relay',
      '  mobilevc restart   Restart the MobileVC backend',
      '  mobilevc stop      Stop the saved backend process',
      '  mobilevc status    Show saved launcher state and health',
      '  mobilevc config    Reconfigure the backend port, AUTH_TOKEN, and language',
      '  mobilevc logs      Print backend logs (use --follow to tail)',
      '  mobilevc help      Show this help',
    ],
    selectLanguage: 'Select language / Language [1=中文, 2=English]: ',
    backendPort: (current) => `Backend port [${current}]: `,
    authToken: (current) => current ? 'AUTH_TOKEN [saved]: ' : 'AUTH_TOKEN: ',
    savedConfig: (filePath) => `🐈 Saved launcher config to ${filePath}`,
    launcherStateNotFound: '🐱 Launcher state not found',
    noSavedBackendProcess: '🐈 No saved backend process',
    noLogsFound: '🐱 No logs found',
    invalidPort: '🐱 Invalid port',
    authRequired: '🐈 AUTH_TOKEN is required',
    startingBackend: (port) => `🐱 Starting MobileVC backend on port ${port}`,
    logFile: (filePath) => `🐈 Log file: ${filePath}`,
    pid: (pid) => `PID: ${pid}`,
    alreadyRunning: (pid, port) => `MobileVC is already running (pid ${pid} on port ${port})`,
    backendNotRunning: 'Backend process is not running',
    stoppedBackend: 'Stopped MobileVC backend',
    statusPid: 'pid',
    statusPort: 'port',
    statusAlive: 'alive',
    statusHealthz: 'healthz',
    statusStartedAt: 'startedAt',
    statusLogPath: 'logPath',
    statusBinaryPath: 'binaryPath',
    statusPlatformTarget: 'platformTarget',
    statusServerVersion: 'serverVersion',
    statusAICliAvailable: 'AI CLI available (Claude/Codex)',
    statusHomeWritable: 'HOME writable',
    statusPreflight: 'preflight',
    qrTitle: 'Flutter scan-to-connect over LAN',
    qrHint: 'Scan with the Flutter client to autofill host, port, and token',
    localAccess: 'Local access',
    lanAccess: 'LAN access',
    qrUnavailable: 'No available LAN IPv4 address was detected, so no QR code was generated',
    preflightTitle: 'Preflight',
    preflightBlocking: 'Blocking',
    preflightWarnings: 'Warnings',
    preflightOk: 'none',
    missingBinaryForPlatform: (target) => `No precompiled server package is available for ${target}`,
    binaryMissing: (filePath) => `Server binary not found: ${filePath}`,
    binaryNotExecutable: (filePath) => `Server binary is not executable: ${filePath}`,
    homeNotWritable: (homePath) => `HOME is not writable: ${homePath}`,
    authTokenMissing: 'AUTH_TOKEN is missing. Run mobilevc config first.',
    relayURLInvalid: (detail) => `Invalid relay URL: ${detail}`,
    relayPairingTimedOut: (filePath) => `Relay pairing data was not ready. Check the log: ${filePath}`,
    relayQrTitle: 'Scan to connect MobileVC Relay',
    relayQrHint: 'The QR contains a one-time pairing secret. Use it before it expires.',
    relayAccess: 'Relay access',
    relayLanAccess: 'LAN direct access is still available',
    aiCliMissing: 'The launcher could not find Claude/Codex CLI. Make sure `claude` or `codex` runs directly in your terminal.',
    portInUse: (port) => `Port ${port} is already in use`,
    startupFailed: 'Startup failed. Check the log and preflight output.',
    startupTimedOut: (seconds, filePath) => `Backend did not become ready within ${seconds} seconds. Check the log: ${filePath}`,
    startupExited: (filePath, detail) => `Backend exited shortly after launch (${detail}). Check the log: ${filePath}`,
    statusUnavailable: 'unknown',
  },
};

function main() {
  const { command, options } = parseInvocation(process.argv.slice(2));

  if (options.help || command === 'help' || command === '--help' || command === '-h') {
    printHelp();
    return;
  }

  if (command === 'setup' || command === 'config') {
    runSetup(true).catch(exitWithError);
    return;
  }

  if (command === 'status') {
    runStatus().catch(exitWithError);
    return;
  }

  if (command === 'stop') {
    runStop().catch(exitWithError);
    return;
  }

  if (command === 'restart') {
    runRestart(options).catch(exitWithError);
    return;
  }

  if (command === 'logs') {
    runLogs(options).catch(exitWithError);
    return;
  }

  if (command === 'public') {
    runPublic(options).catch(exitWithError);
    return;
  }

  if (command === 'start') {
    runStart(options).catch(exitWithError);
    return;
  }

  printHelp();
}

function parseInvocation(args) {
  const hasExplicitCommand = Boolean(args[0] && !args[0].startsWith('-'));
  const command = hasExplicitCommand ? args[0] : 'start';
  const optionArgs = args.slice(hasExplicitCommand ? 1 : 0);
  const options = parseOptions(optionArgs);
  if (!hasExplicitCommand) {
    options.guided = true;
  }
  return { command, options };
}

function parseOptions(args) {
  const options = {
    help: false,
    follow: false,
    guided: false,
    public: false,
    relay: '',
    networkExposureMode: '',
  };
  for (let i = 0; i < args.length; i += 1) {
    const arg = args[i];
    if (arg === '--help' || arg === '-h') options.help = true;
    else if (arg === '--follow' || arg === '-f') options.follow = true;
    else if (arg === '--relay') {
      i += 1;
      if (!args[i]) {
        throw new Error('--relay requires a value');
      }
      options.relay = args[i];
    } else if (arg.startsWith('--relay=')) {
      options.relay = arg.slice('--relay='.length);
    } else if (arg === '--network-exposure-mode') {
      i += 1;
      if (!args[i]) {
        throw new Error('--network-exposure-mode requires a value');
      }
      options.networkExposureMode = normalizeNetworkExposureModeOption(args[i]);
    } else if (arg.startsWith('--network-exposure-mode=')) {
      options.networkExposureMode = normalizeNetworkExposureModeOption(
        arg.slice('--network-exposure-mode='.length),
      );
    }
  }
  return options;
}

function normalizeNetworkExposureModeOption(raw) {
  const value = String(raw || '').trim().toLowerCase();
  if (value === '' || value === 'lan' || value === 'lan-enabled') {
    return 'lan';
  }
  if (value === 'relay' || value === 'relay-only') {
    return 'relay-only';
  }
  throw new Error('--network-exposure-mode must be lan or relay-only');
}

function printHelp() {
  const language = readJson(CONFIG_PATH, null)?.language || DEFAULT_LANGUAGE;
  const bundle = MESSAGES[language] || MESSAGES[DEFAULT_LANGUAGE];
  console.log(bundle.helpTitle);
  console.log('');
  console.log(bundle.help.join('\n'));
}

async function runSetup(promptAll = true) {
  ensureDir(STATE_DIR, 0o700);
  ensureDir(LOG_DIR, 0o700);

  const current = readJson(CONFIG_PATH, null) || null;
  const currentLanguage = current?.language || DEFAULT_LANGUAGE;
  const language = promptAll || !current ? await askLanguage(currentLanguage) : currentLanguage;
  const port = promptAll || !current ? await askPort(language, current?.port || DEFAULT_PORT) : String(current?.port || DEFAULT_PORT);
  const authToken = promptAll || !current ? await askToken(language, current?.authToken || '') : String(current?.authToken || '').trim();

  writeJson(CONFIG_PATH, { language, port, authToken });
  console.log(message(language, 'savedConfig', CONFIG_PATH));
}

async function runStart(options = {}) {
  ensureDir(STATE_DIR, 0o700);
  ensureDir(LOG_DIR, 0o700);

  let config = readJson(CONFIG_PATH, null);
  if (!config) {
    await runSetup(true);
    config = readJson(CONFIG_PATH, null);
  }

  const language = config?.language || DEFAULT_LANGUAGE;
  const relayAccess = buildRelayAccessConfig(config, options);
  const existingState = readJson(STATE_PATH, null);
  if (existingState?.pid && isPidAlive(existingState.pid)) {
    if (isStateConfigMatch(existingState, config, relayAccess)) {
      console.log(message(language, 'alreadyRunning', existingState.pid, existingState.port));
      if (options.guided) {
        await printLanQr(language, existingState.port, existingState.authToken, process.cwd());
      }
      return;
    }
    await runStop({ silent: true, language });
  }

  const platformTarget = getPlatformTarget();
  const binaryInfo = resolveBinaryInfo(platformTarget);
  const preflight = await runPreflightChecks({ config, language, platformTarget, binaryInfo });
  printPreflight(language, preflight);

  if (preflight.blocking.length > 0) {
    const state = buildStateSkeleton(config, language, binaryInfo, platformTarget, preflight, null);
    writeJson(STATE_PATH, state);
    throw new Error(preflight.blocking[0]);
  }

  const logPath = path.join(LOG_DIR, `mobilevc-${timestampForFile()}.log`);
  const pairingEventPath = relayAccess.enabled ? createRelayPairingEventPath() : '';
  const relayAgentSessionStatePath = relayAccess.enabled ? createRelayAgentSessionStatePath() : '';
  fs.writeFileSync(logPath, '', { mode: 0o600 });
  try {
    fs.chmodSync(logPath, 0o600);
  } catch (_) {}
  const lanHost = relayAccess.enabled && relayAccess.networkExposureMode === 'lan'
    ? await detectLanHost()
    : '';

  const env = {
    ...process.env,
    PORT: String(config.port),
    AUTH_TOKEN: String(config.authToken),
    RUNTIME_WORKSPACE_ROOT: process.cwd(),
    MOBILEVC_LAN_HOST: lanHost,
    ...relayAccess.env,
  };
  if (relayAccess.enabled) {
    env.RELAY_PAIRING_EVENT_PATH = pairingEventPath;
    env.RELAY_AGENT_SESSION_STATE_PATH = relayAgentSessionStatePath;
  }

  fs.appendFileSync(logPath, `launcher starting binary=${binaryInfo.binaryPath} target=${platformTarget}\n`);
  const logFd = fs.openSync(logPath, 'a');
  const child = spawn(binaryInfo.binaryPath, [], {
    detached: true,
    stdio: ['ignore', logFd, logFd],
    env,
  });
  fs.closeSync(logFd);

  child.on('error', (err) => {
    fs.appendFileSync(logPath, `launcher error: ${err.stack || err.message}\n`);
  });

  const startup = await waitForServerReady({ child, port: config.port, timeoutMs: 10000 });
  if (!startup.ok) {
    if (child.pid && isPidAlive(child.pid)) {
      killProcessGroup(child.pid, 'SIGTERM');
      await waitForExit(child.pid, 2000);
    }
    clearState();
    removeRelayPairingEventFile(pairingEventPath);
    removeRelayAgentSessionStateFile(relayAgentSessionStatePath);
    throw new Error(formatStartupFailure(language, startup, logPath));
  }

  const pairing = relayAccess.enabled ? await waitForRelayPairing(pairingEventPath, 10000) : null;
  if (relayAccess.enabled && !pairing) {
    if (child.pid && isPidAlive(child.pid)) {
      killProcessGroup(child.pid, 'SIGTERM');
      await waitForExit(child.pid, 2000);
    }
    clearState();
    removeRelayPairingEventFile(pairingEventPath);
    removeRelayAgentSessionStateFile(relayAgentSessionStatePath);
    throw new Error(message(language, 'relayPairingTimedOut', logPath));
  }

  const state = {
    ...buildStateSkeleton(config, language, binaryInfo, platformTarget, preflight, logPath),
    pid: child.pid,
    startedAt: new Date().toISOString(),
    serverVersion: formatVersionInfo(startup.versionInfo),
    serverVersionRaw: startup.versionInfo,
    relayMode: relayAccess.enabled,
    relayUrl: relayAccess.url || '',
    networkExposureMode: relayAccess.networkExposureMode || '',
    relayAgentSessionStatePath,
  };
  writeJson(STATE_PATH, state);

  child.unref();
  console.log(message(language, 'startingBackend', state.port));
  console.log(message(language, 'logFile', logPath));
  console.log(message(language, 'pid', child.pid));
  if (!await checkHealth(state.port)) {
    throw new Error(message(language, 'startupFailed'));
  }
  if (relayAccess.enabled) {
    if (relayAccess.networkExposureMode === 'lan') {
      printLanQrWithHost(language, lanHost, state.port, state.authToken, state.cwd || process.cwd());
      console.log('');
      console.log(message(language, 'relayLanAccess'));
    }
    printRelayQr(language, pairing);
  } else {
    await printLanQr(language, state.port, state.authToken, state.cwd || process.cwd());
  }
}

async function runPublic(options = {}) {
  ensureDir(STATE_DIR, 0o700);
  ensureDir(LOG_DIR, 0o700);

  let config = readJson(CONFIG_PATH, null);
  if (!config) {
    await runSetup(true);
    config = readJson(CONFIG_PATH, null);
  }
  const relayUrl = resolveRelayURL(config, options);
  assertValidRelayURL(relayUrl);
  writeJson(CONFIG_PATH, { ...config, relayUrl });
  await runStart({ ...options, public: false, relay: relayUrl });
}

async function runStatus() {
  const state = readJson(STATE_PATH, null);
  const config = readJson(CONFIG_PATH, null);
  const language = state?.language || config?.language || DEFAULT_LANGUAGE;
  if (!state) {
    console.log(message(language, 'launcherStateNotFound'));
    return;
  }

  const platformTarget = state.platformTarget || getPlatformTarget();
  const binaryInfo = resolveBinaryInfo(platformTarget);
  const alive = state.pid ? isPidAlive(state.pid) : false;
  const healthy = alive ? await checkHealth(state.port) : false;
  const versionInfo = healthy ? await fetchServerVersion(state.port) : null;
  const aiCliAvailability = detectAICliAvailability();
  const homeWritable = isHomeWritable();

  console.log(`${message(language, 'statusPid')}: ${state.pid || '-'}`);
  console.log(`${message(language, 'statusPort')}: ${state.port || '-'}`);
  console.log(`${message(language, 'statusAlive')}: ${alive}`);
  console.log(`${message(language, 'statusHealthz')}: ${healthy}`);
  console.log(`${message(language, 'statusStartedAt')}: ${state.startedAt || '-'}`);
  console.log(`${message(language, 'statusLogPath')}: ${state.logPath || '-'}`);
  console.log(`${message(language, 'statusBinaryPath')}: ${state.binaryPath || binaryInfo.binaryPath || '-'}`);
  console.log(`${message(language, 'statusPlatformTarget')}: ${platformTarget}`);
  console.log(`${message(language, 'statusServerVersion')}: ${formatVersionInfo(versionInfo) || state.serverVersion || '-'}`);
  console.log(`${message(language, 'statusAICliAvailable')}: claude=${aiCliAvailability.claude}, codex=${aiCliAvailability.codex}`);
  console.log(`${message(language, 'statusHomeWritable')}: ${homeWritable}`);
  console.log(`${message(language, 'statusPreflight')}: ${summarizePreflight(state.preflight, language)}`);
}

async function runStop(options = {}) {
  const state = readJson(STATE_PATH, null);
  const language = options.language || state?.language || DEFAULT_LANGUAGE;
  if (!state?.pid) {
    if (!options.silent) {
      console.log(message(language, 'noSavedBackendProcess'));
    }
    return false;
  }

  if (!isPidAlive(state.pid)) {
    removeRelayAgentSessionStateFile(state.relayAgentSessionStatePath);
    clearState();
    if (!options.silent) {
      console.log(message(language, 'backendNotRunning'));
    }
    return false;
  }

  killProcessGroup(state.pid, 'SIGTERM');

  await waitForExit(state.pid, 4000);
  if (isPidAlive(state.pid)) {
    killProcessGroup(state.pid, 'SIGKILL');
    await waitForExit(state.pid, 2000);
  }

  removeRelayAgentSessionStateFile(state.relayAgentSessionStatePath);
  clearState();
  if (!options.silent) {
    console.log(message(language, 'stoppedBackend'));
  }
  return true;
}

async function runRestart(options) {
  const state = readJson(STATE_PATH, null);
  const language = state?.language || readJson(CONFIG_PATH, null)?.language || DEFAULT_LANGUAGE;
  await runStop({ silent: true, language });
  await runStart(options);
}

async function runLogs(options) {
  ensureDir(LOG_DIR, 0o700);
  const files = fs.readdirSync(LOG_DIR)
    .filter((file) => file.endsWith('.log'))
    .map((file) => path.join(LOG_DIR, file))
    .sort((a, b) => fs.statSync(b).mtimeMs - fs.statSync(a).mtimeMs);

  const state = readJson(STATE_PATH, null);
  const language = state?.language || DEFAULT_LANGUAGE;
  if (files.length === 0) {
    console.log(message(language, 'noLogsFound'));
    return;
  }

  const latest = files[0];
  if (options.follow) {
    followFile(latest);
    return;
  }

  process.stdout.write(fs.readFileSync(latest, 'utf8'));
}

function resolveBinaryInfo(platformTarget) {
  const packageName = PLATFORM_PACKAGES[platformTarget] || null;
  const packageRoot = packageName ? resolveInstalledPackageRoot(packageName) : null;
  const bundledPackageRoot = resolveBundledPackageRoot(platformTarget);
  const resolvedPackageRoot = packageRoot || bundledPackageRoot;
  const binaryPath = resolvedPackageRoot ? path.join(resolvedPackageRoot, 'bin', SERVER_BINARY_NAME) : null;
  return {
    packageName,
    packageRoot: resolvedPackageRoot,
    binaryPath,
    source: packageRoot ? 'installed' : bundledPackageRoot ? 'bundled' : null,
  };
}

function resolveInstalledPackageRoot(packageName) {
  const packageJsonSuffix = packageName.split('/').join(path.sep);
  const candidatePackageJsonPaths = [
    safeResolve(() => require.resolve(`${packageName}/package.json`)),
    safeResolve(() => require.resolve(`${packageName}/package.json`, { paths: [__dirname, process.cwd()] })),
    path.join(__dirname, '..', 'node_modules', packageJsonSuffix, 'package.json'),
    path.join(__dirname, '..', '..', 'node_modules', packageJsonSuffix, 'package.json'),
    path.join(__dirname, '..', '..', packageJsonSuffix, 'package.json'),
    path.join(getGlobalNodeModulesRoot(), packageJsonSuffix, 'package.json'),
  ].filter(Boolean);

  for (const packageJsonPath of candidatePackageJsonPaths) {
    if (fs.existsSync(packageJsonPath)) {
      return path.dirname(packageJsonPath);
    }
  }

  return null;
}

function resolveBundledPackageRoot(platformTarget) {
  const candidates = [
    path.join(__dirname, '..', 'packages', `server-${platformTarget}`),
    path.join(process.cwd(), 'packages', `server-${platformTarget}`),
  ];

  for (const candidate of candidates) {
    if (fs.existsSync(path.join(candidate, 'package.json'))) {
      return candidate;
    }
  }

  return null;
}

function safeResolve(fn) {
  try {
    return fn();
  } catch (_) {
    return null;
  }
}

function getGlobalNodeModulesRoot() {
  const npmRoot = spawnSync('npm', ['root', '-g'], { encoding: 'utf8' });
  if (npmRoot.status === 0) {
    return String(npmRoot.stdout || '').trim();
  }

  if (process.platform === 'win32') {
    return path.join(process.env.APPDATA || '', 'npm', 'node_modules');
  }

  return '/usr/local/lib/node_modules';
}

function getPlatformTarget() {
  return `${process.platform}-${process.arch}`;
}

async function runPreflightChecks({ config, language, platformTarget, binaryInfo }) {
  const blocking = [];
  const warnings = [];

  if (!binaryInfo.packageName) {
    blocking.push(message(language, 'missingBinaryForPlatform', platformTarget));
  } else if (!binaryInfo.binaryPath || !fs.existsSync(binaryInfo.binaryPath)) {
    blocking.push(message(language, 'binaryMissing', binaryInfo.binaryPath || `${binaryInfo.packageName}/bin/${SERVER_BINARY_NAME}`));
  } else if (!isBinaryExecutable(binaryInfo.binaryPath)) {
    blocking.push(message(language, 'binaryNotExecutable', binaryInfo.binaryPath));
  }

  if (!String(config?.authToken || '').trim()) {
    blocking.push(message(language, 'authTokenMissing'));
  }

  if (!isHomeWritable()) {
    blocking.push(message(language, 'homeNotWritable', os.homedir()));
  }

  if (await isPortOccupied(String(config?.port || DEFAULT_PORT))) {
    blocking.push(message(language, 'portInUse', String(config?.port || DEFAULT_PORT)));
  }

  const aiCliAvailability = detectAICliAvailability();
  if (!aiCliAvailability.claude && !aiCliAvailability.codex) {
    warnings.push(message(language, 'aiCliMissing'));
  }

  return { blocking, warnings };
}

function printPreflight(language, preflight) {
  console.log(`${message(language, 'preflightTitle')}:`);
  console.log(`  ${message(language, 'preflightBlocking')}: ${formatList(preflight.blocking, language)}`);
  console.log(`  ${message(language, 'preflightWarnings')}: ${formatList(preflight.warnings, language)}`);
}

function formatList(items, language) {
  if (!items || items.length === 0) {
    return message(language, 'preflightOk');
  }
  return items.join(' | ');
}

function buildRelayAccessConfig(config, options) {
  const url = String(options.relay || '').trim();
  if (!url) {
    return { enabled: false, env: {}, url: '', networkExposureMode: '' };
  }
  assertValidRelayURL(url);
  const networkExposureMode = options.networkExposureMode || 'lan';
  return {
    enabled: true,
    url,
    networkExposureMode,
    env: {
      RELAY_MODE: 'true',
      RELAY_URL: url,
      NETWORK_EXPOSURE_MODE: networkExposureMode,
    },
  };
}

function resolveRelayURL(config, options) {
  return String(options.relay || config.relayUrl || DEFAULT_RELAY_URL).trim();
}

function assertValidRelayURL(raw) {
  const url = new URL(String(raw || '').trim());
  if (url.protocol === 'wss:') {
    validateRelayURLShape(url);
    return;
  }
  if (url.protocol !== 'ws:') {
    throw new Error(message(DEFAULT_LANGUAGE, 'relayURLInvalid', 'must use ws:// or wss://'));
  }
  validateRelayURLShape(url);
  if (!isDevelopmentRelayHost(url.hostname)) {
    throw new Error(message(DEFAULT_LANGUAGE, 'relayURLInvalid', 'ws:// is only allowed for loopback or LAN hosts'));
  }
}

function validateRelayURLShape(url) {
  if (!url.hostname || url.username || url.password) {
    throw new Error(message(DEFAULT_LANGUAGE, 'relayURLInvalid', 'expected scheme://host[:port]'));
  }
  if (url.pathname !== '/' || url.search || url.hash) {
    throw new Error(message(DEFAULT_LANGUAGE, 'relayURLInvalid', 'path, query, and fragment are not allowed'));
  }
}

function isDevelopmentRelayHost(host) {
  const normalized = String(host || '').toLowerCase();
  if (normalized === 'localhost') {
    return true;
  }
  if (net.isIP(normalized) === 0) {
    return false;
  }
  if (normalized === '127.0.0.1' || normalized === '::1') {
    return true;
  }
  return isPrivateIPv4(normalized) || normalized.startsWith('fc') || normalized.startsWith('fd') || normalized.startsWith('fe80');
}

function isPrivateIPv4(host) {
  const parts = host.split('.').map((part) => Number(part));
  if (parts.length !== 4 || parts.some((part) => Number.isNaN(part))) {
    return false;
  }
  return parts[0] === 10 ||
    (parts[0] === 192 && parts[1] === 168) ||
    (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31);
}

function createRelayPairingEventPath() {
  const suffix = `${process.pid}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return path.join(STATE_DIR, `${RELAY_PAIRING_EVENT_PREFIX}${suffix}.json`);
}

function createRelayAgentSessionStatePath() {
  const suffix = `${process.pid}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return path.join(STATE_DIR, `${RELAY_AGENT_SESSION_PREFIX}${suffix}.json`);
}

function waitForRelayPairing(eventPath, timeoutMs) {
  return new Promise((resolve) => {
    const started = Date.now();
    const timer = setInterval(() => {
      const pairing = readRelayPairingEventFile(eventPath);
      if (pairing) {
        clearInterval(timer);
        removeRelayPairingEventFile(eventPath);
        resolve(pairing);
        return;
      }
      if (Date.now() - started >= timeoutMs) {
        clearInterval(timer);
        resolve(null);
      }
    }, 250);
  });
}

function readRelayPairingEventFile(eventPath) {
  if (!eventPath || !fs.existsSync(eventPath)) {
    return null;
  }
  try {
    const parsed = JSON.parse(fs.readFileSync(eventPath, 'utf8'));
    if (parsed.type === 'mobilevc.relay.pairing_ready') {
      return parsed;
    }
  } catch (_) {
    return null;
  }
  return null;
}

function removeRelayPairingEventFile(eventPath) {
  if (!eventPath) {
    return;
  }
  try {
    fs.unlinkSync(eventPath);
  } catch (_) {}
}

function removeRelayAgentSessionStateFile(filePath) {
  if (!filePath) {
    return;
  }
  try {
    fs.unlinkSync(filePath);
  } catch (_) {}
}

function printRelayQr(language, pairing) {
  const uri = buildRelayPairingUri(pairing);
  console.log('');
  console.log(message(language, 'relayQrTitle'));
  renderTerminalQr(uri);
  console.log('');
  console.log(message(language, 'relayQrHint'));
  console.log(`${message(language, 'relayAccess')}: ${uri}`);
}

function buildRelayPairingUri(pairing) {
  const uri = new URL('mobilevc://relay/v1');
  uri.searchParams.set('relay', pairing.relayUrl);
  uri.searchParams.set('session', pairing.sessionId);
  uri.searchParams.set('secret', pairing.pairingSecret);
  uri.searchParams.set('exp', String(pairing.expiresAt));
  if (pairing.nodeFingerprintHex) {
    uri.searchParams.set('nodeFingerprint', pairing.nodeFingerprintHex);
  }
  appendRelayCapabilityParams(uri, pairing.capabilities);
  return uri.toString();
}

function appendRelayCapabilityParams(uri, capabilities) {
  if (!capabilities || typeof capabilities !== 'object') {
    return;
  }
  for (const key of [
    'relayProtocolVersion',
    'e2eeProtocolVersion',
    'cryptoSuite',
    'tunnelProtocolVersion',
    'supportsMultiplexStreams',
    'supportsFileDownloadStream',
    'supportsDeviceManagement',
    'requiresE2EE',
    'plaintextTestMode',
  ]) {
    if (Object.prototype.hasOwnProperty.call(capabilities, key)) {
      uri.searchParams.set(key, String(capabilities[key]));
    }
  }
}

function redactRelaySecret(rawUri) {
  const uri = new URL(rawUri);
  if (uri.searchParams.has('secret')) {
    uri.searchParams.set('secret', '<redacted>');
  }
  return uri.toString();
}

function buildStateSkeleton(config, language, binaryInfo, platformTarget, preflight, logPath) {
  return {
    pid: null,
    port: String(config.port),
    authToken: String(config.authToken),
    cwd: process.cwd(),
    language,
    startedAt: null,
    logPath,
    binaryPath: binaryInfo.binaryPath,
    platformTarget,
    serverVersion: null,
    preflight,
  };
}

function detectAICliAvailability() {
  return {
    claude: detectCommandAvailability('claude'),
    codex: detectCommandAvailability('codex'),
  };
}

function detectCommandAvailability(commandName) {
  const checks = process.platform === 'win32'
    ? [
      () => commandSucceeds(commandName, ['--version']),
      () => commandSucceeds(`${commandName}.cmd`, ['--version']),
      () => commandSucceeds(`${commandName}.exe`, ['--version']),
      () => commandSucceeds('where', [commandName]),
      () => commandSucceeds('where', [`${commandName}.cmd`]),
      () => windowsShimExists(commandName),
      () => pathCommandExists(commandName),
    ]
    : [
      () => commandSucceeds(commandName, ['--version']),
      () => commandSucceeds('which', [commandName]),
      () => pathCommandExists(commandName),
    ];

  return checks.some((check) => check());
}

function commandSucceeds(command, args) {
  const result = spawnSync(command, args, { stdio: 'ignore' });
  return result.status === 0;
}

function windowsShimExists(commandName) {
  const appData = process.env.APPDATA || '';
  if (!appData) {
    return false;
  }

  const candidates = [
    path.join(appData, 'npm', `${commandName}.cmd`),
    path.join(appData, 'npm', commandName),
    path.join(appData, 'npm', `${commandName}.exe`),
    path.join(appData, 'npm', `${commandName}.ps1`),
  ];
  return candidates.some((candidate) => fs.existsSync(candidate));
}

function pathCommandExists(commandName) {
  const home = os.homedir();
  const pathParts = String(process.env.PATH || '').split(path.delimiter).filter(Boolean);
  const extraParts = process.platform === 'win32'
    ? [
      path.join(process.env.APPDATA || '', 'npm'),
      path.join(home, 'AppData', 'Roaming', 'npm'),
      path.join(home, '.codex', 'bin'),
      path.join(home, '.claude', 'local'),
    ]
    : [
      '/opt/homebrew/bin',
      '/usr/local/bin',
      '/usr/bin',
      path.join(home, '.local', 'bin'),
      path.join(home, '.npm-global', 'bin'),
      path.join(home, '.npm', 'bin'),
      path.join(home, '.yarn', 'bin'),
      path.join(home, '.pnpm'),
      path.join(home, '.codex', 'bin'),
      path.join(home, '.claude', 'local'),
    ];
  const npmGlobalBin = getNpmGlobalBinPath();
  if (npmGlobalBin) {
    extraParts.push(npmGlobalBin);
  }

  const dirs = Array.from(new Set([...pathParts, ...extraParts].filter(Boolean)));
  const suffixes = process.platform === 'win32' ? ['', '.cmd', '.exe', '.bat', '.ps1'] : [''];

  for (const dir of dirs) {
    for (const suffix of suffixes) {
      const candidate = path.join(dir, `${commandName}${suffix}`);
      if (!fs.existsSync(candidate)) {
        continue;
      }
      if (process.platform === 'win32' || isBinaryExecutable(candidate)) {
        return true;
      }
    }
  }

  return false;
}

function getNpmGlobalBinPath() {
  const prefix = spawnSync('npm', ['prefix', '-g'], { encoding: 'utf8' });
  if (prefix.status !== 0) {
    return null;
  }
  const root = String(prefix.stdout || '').trim();
  if (!root) {
    return null;
  }
  return process.platform === 'win32' ? root : path.join(root, 'bin');
}

function isHomeWritable() {
  try {
    fs.accessSync(os.homedir(), fs.constants.W_OK);
    return true;
  } catch (_) {
    return false;
  }
}

function isBinaryExecutable(filePath) {
  try {
    fs.accessSync(filePath, fs.constants.X_OK);
    return true;
  } catch (_) {
    return process.platform === 'win32' && fs.existsSync(filePath);
  }
}

function isPortOccupied(port) {
  return tryListenPort({ port: Number(port) }).then((result) => {
    if (result === true || result === false) {
      return result;
    }
    return tryListenPort({ port: Number(port), host: '0.0.0.0' }).then((fallback) => fallback === true);
  });
}

function waitForServerReady({ child, port, timeoutMs }) {
  return new Promise((resolve) => {
    let settled = false;
    let polling = false;
    const started = Date.now();

    const finish = (result) => {
      if (settled) {
        return;
      }
      settled = true;
      clearInterval(timer);
      child.removeListener('exit', onExit);
      child.removeListener('error', onError);
      resolve(result);
    };

    const onExit = (code, signal) => {
      finish({ ok: false, reason: 'exit', code, signal });
    };
    const onError = (error) => {
      finish({ ok: false, reason: 'error', error });
    };

    const poll = async () => {
      if (settled || polling) {
        return;
      }
      polling = true;
      try {
        const versionInfo = await fetchServerVersion(port);
        if (versionInfo) {
          finish({ ok: true, versionInfo });
          return;
        }
        if (Date.now() - started >= timeoutMs) {
          finish({ ok: false, reason: 'timeout', timeoutMs });
        }
      } finally {
        polling = false;
      }
    };

    child.once('exit', onExit);
    child.once('error', onError);

    const timer = setInterval(() => {
      poll().catch((error) => finish({ ok: false, reason: 'error', error }));
    }, 400);

    poll().catch((error) => finish({ ok: false, reason: 'error', error }));
  });
}

function fetchServerVersion(port) {
  return new Promise((resolve) => {
    const req = http.get({ hostname: '127.0.0.1', port: Number(port), path: '/version', timeout: 1500 }, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => {
        if (res.statusCode !== 200) {
          resolve(null);
          return;
        }
        try {
          resolve(JSON.parse(body));
        } catch (_) {
          resolve(null);
        }
      });
    });
    req.on('timeout', () => {
      req.destroy();
      resolve(null);
    });
    req.on('error', () => resolve(null));
  });
}

function formatVersionInfo(info) {
  if (!info || !info.version) {
    return null;
  }
  const extras = [];
  if (info.commit && info.commit !== 'unknown') extras.push(info.commit);
  if (info.buildDate && info.buildDate !== 'unknown') extras.push(info.buildDate);
  return extras.length > 0 ? `${info.version} (${extras.join(', ')})` : info.version;
}

function summarizePreflight(preflight, language) {
  if (!preflight) {
    return message(language, 'statusUnavailable');
  }
  return `blocking=${preflight.blocking?.length || 0}, warnings=${preflight.warnings?.length || 0}`;
}

function followFile(filePath) {
  let lastSize = 0;
  process.stdout.write(fs.readFileSync(filePath, 'utf8'));
  lastSize = fs.statSync(filePath).size;
  const timer = setInterval(() => {
    if (!fs.existsSync(filePath)) return;
    const stat = fs.statSync(filePath);
    if (stat.size > lastSize) {
      const stream = fs.createReadStream(filePath, { start: lastSize, end: stat.size });
      stream.pipe(process.stdout, { end: false });
      lastSize = stat.size;
    }
  }, 1000);

  process.on('SIGINT', () => {
    clearInterval(timer);
    process.exit(0);
  });
}

async function askLanguage(currentLanguage) {
  const selection = await promptInput(message(DEFAULT_LANGUAGE, 'selectLanguage'));
  const normalized = String(selection || '').trim().toLowerCase();
  if (!normalized) {
    return currentLanguage || DEFAULT_LANGUAGE;
  }
  if (normalized === '2' || normalized === 'en' || normalized === 'english') {
    return 'en';
  }
  return 'zh';
}

async function askPort(language, currentPort) {
  const prompt = message(language, 'backendPort', currentPort || DEFAULT_PORT);
  const value = await promptInput(prompt);
  const port = String((value || currentPort || DEFAULT_PORT).trim());
  if (!/^\d+$/.test(port) || Number(port) <= 0 || Number(port) > 65535) {
    throw new Error(message(language, 'invalidPort'));
  }
  return port;
}

async function askToken(language, currentToken) {
  const prompt = currentToken ? message(language, 'authToken', true) : message(language, 'authToken', false);
  const value = await promptInput(prompt, true);
  const token = String((value || currentToken || '').trim());
  if (!token) {
    throw new Error(message(language, 'authRequired'));
  }
  return token;
}

function message(language, key, ...args) {
  const bundle = MESSAGES[language] || MESSAGES[DEFAULT_LANGUAGE];
  const value = bundle[key];
  return typeof value === 'function' ? value(...args) : value;
}

async function printLanQr(language, port, authToken = '', cwd = process.cwd()) {
  const host = await detectLanHost();
  printLanQrWithHost(language, host, port, authToken, cwd);
}

function printLanQrWithHost(language, host, port, authToken = '', cwd = process.cwd()) {
  const localUrl = buildLaunchUrl('127.0.0.1', port, authToken, cwd);
  console.log('');
  console.log(`${message(language, 'localAccess')}: ${localUrl}`);

  if (!host) {
    console.log(message(language, 'qrUnavailable'));
    return;
  }

  const url = buildLaunchUrl(host, port, authToken, cwd);
  console.log(`${message(language, 'lanAccess')}: ${url}`);
  console.log('');
  console.log(message(language, 'qrTitle'));
  renderTerminalQr(url);
  console.log(message(language, 'qrHint'));
}

function renderTerminalQr(text) {
  qrcode.generate(text, { small: true }, (qr) => {
    const output = String(qr || '').replace(/\s+$/, '');
    console.log(output);
  });
}

function buildLaunchUrl(host, port, authToken = '', cwd = '') {
  const url = new URL(`http://${host}:${port}/`);
  if (authToken) {
    url.searchParams.set('token', authToken);
  }
  const normalizedCwd = String(cwd || '').trim();
  if (normalizedCwd) {
    url.searchParams.set('cwd', normalizedCwd);
  }
  return url.toString();
}

function isStateConfigMatch(
  state,
  config,
  relayAccess = { enabled: false, url: '', networkExposureMode: '' },
) {
  return String(state?.port || '') === String(config?.port || '') &&
    String(state?.authToken || '') === String(config?.authToken || '') &&
    Boolean(state?.relayMode) === Boolean(relayAccess.enabled) &&
    String(state?.relayUrl || '') === String(relayAccess.url || '') &&
    String(state?.networkExposureMode || '') === String(relayAccess.networkExposureMode || '');
}

function formatStartupFailure(language, startup, logPath) {
  if (startup.reason === 'timeout') {
    return message(language, 'startupTimedOut', Math.round((startup.timeoutMs || 10000) / 1000), logPath);
  }
  if (startup.reason === 'exit') {
    const detail = startup.signal ? `signal ${startup.signal}` : `code ${startup.code ?? 'unknown'}`;
    return message(language, 'startupExited', logPath, detail);
  }
  if (startup.error) {
    return `${message(language, 'startupFailed')}: ${startup.error.message || startup.error}`;
  }
  return message(language, 'startupFailed');
}

function tryListenPort(options) {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.once('error', (err) => {
      if (!err) {
        resolve(false);
        return;
      }
      if (err.code === 'EADDRINUSE' || err.code === 'EACCES') {
        resolve(true);
        return;
      }
      if (err.code === 'EAFNOSUPPORT') {
        resolve(null);
        return;
      }
      resolve(false);
    });
    server.once('listening', () => {
      server.close(() => resolve(false));
    });
    server.listen(options);
  });
}

async function detectLanHost() {
  const interfaces = os.networkInterfaces();
  const wifi = [];
  const wired = [];
  const privateLan = [];
  const other = [];

  for (const [name, entries] of Object.entries(interfaces)) {
    if (!entries) continue;
    if (isVirtualNetworkInterfaceName(name)) {
      continue;
    }
    for (const entry of entries) {
      if (!entry || entry.family !== 'IPv4' || entry.internal) {
        continue;
      }
      if (isLinkLocalIpv4(entry.address) || isVpnLikeIpv4(entry.address)) {
        continue;
      }

      const lowered = name.toLowerCase();
      if (/^(en0|wl|wlan|wifi|wi-?fi)/.test(lowered)) {
        wifi.push(entry.address);
      } else if (/^(en|eth)/.test(lowered)) {
        wired.push(entry.address);
      } else if (isPrivateLanIpv4(entry.address)) {
        privateLan.push(entry.address);
      } else {
        other.push(entry.address);
      }
    }
  }

  return wifi[0] || wired[0] || privateLan[0] || other[0] || null;
}

function isLinkLocalIpv4(address) {
  return /^169\.254\./.test(address);
}

function isVirtualNetworkInterfaceName(name) {
  const lowered = String(name || '').toLowerCase();
  return /(tailscale|utun|tun|tap|wg|wireguard|zerotier|clash|mihomo|sing-box|v2ray|vpn|ppp|docker|br-|veth|vmware|virtualbox|hyper-v|wintun|loopback)/.test(lowered);
}

function isPrivateLanIpv4(address) {
  return /^192\.168\./.test(address) ||
    /^172\.(1[6-9]|2\d|3[0-1])\./.test(address) ||
    /^10\./.test(address);
}

function isVpnLikeIpv4(address) {
  return /^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(address);
}

function promptInput(question, silent = false) {
  return new Promise((resolve) => {
    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
      terminal: true,
    });
    if (silent) {
      rl.stdoutMuted = true;
      rl._writeToOutput = function _writeToOutput(stringToWrite) {
        if (!rl.stdoutMuted) {
          rl.output.write(stringToWrite);
          return;
        }
        const text = String(stringToWrite || '');
        if (!text) return;
        if (/\r?\n$/.test(text) || /:\s*$/.test(text) || /\]\s*$/.test(text)) {
          rl.output.write(text);
        }
      };
    }
    rl.question(question, (answer) => {
      rl.close();
      process.stdout.write('\n');
      resolve(answer);
    });
  });
}

function ensureDir(dir, mode) {
  fs.mkdirSync(dir, { recursive: true, mode });
  try {
    fs.chmodSync(dir, mode);
  } catch (_) {}
}

function writeJson(filePath, value) {
  fs.writeFileSync(filePath, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  try {
    fs.chmodSync(filePath, 0o600);
  } catch (_) {}
}

function readJson(filePath, fallback) {
  try {
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
  } catch (_) {
    return fallback;
  }
}

function clearState() {
  try {
    fs.unlinkSync(STATE_PATH);
  } catch (_) {}
}

function isPidAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return err.code !== 'ESRCH';
  }
}

function killProcessGroup(pid, signal) {
  const targets = process.platform === 'win32' ? [pid] : [-pid, pid];
  for (const target of targets) {
    try {
      process.kill(target, signal);
      return;
    } catch (err) {
      if (err.code !== 'ESRCH' && err.code !== 'EINVAL') {
        throw err;
      }
    }
  }
}

function waitForExit(pid, timeoutMs) {
  return new Promise((resolve) => {
    const started = Date.now();
    const timer = setInterval(() => {
      if (!isPidAlive(pid) || Date.now() - started >= timeoutMs) {
        clearInterval(timer);
        resolve();
      }
    }, 250);
  });
}

function checkHealth(port) {
  return new Promise((resolve) => {
    const req = http.get({ hostname: '127.0.0.1', port: Number(port), path: '/healthz', timeout: 1500 }, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => resolve(res.statusCode === 200 && body.trim() === 'ok'));
    });
    req.on('timeout', () => {
      req.destroy();
      resolve(false);
    });
    req.on('error', () => resolve(false));
  });
}

function timestampForFile() {
  return new Date().toISOString().replace(/[:.]/g, '-');
}

function exitWithError(err) {
  console.error(err.message || err);
  process.exit(1);
}

if (require.main === module) {
  main();
} else {
  module.exports = {
    buildLaunchUrl,
    assertValidRelayURL,
    buildRelayAccessConfig,
    buildRelayPairingUri,
    createRelayAgentSessionStatePath,
    readRelayPairingEventFile,
    removeRelayAgentSessionStateFile,
    isPortOccupied,
    removeRelayPairingEventFile,
    parseInvocation,
    resolveBinaryInfo,
    resolveBundledPackageRoot,
  };
}
