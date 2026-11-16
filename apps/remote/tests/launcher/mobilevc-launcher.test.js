const test = require('node:test');
const assert = require('node:assert/strict');
const EventEmitter = require('node:events');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const net = require('net');

const {
  isPortOccupied,
  assertValidRelayURL,
  buildRelayAccessConfig,
  buildRelayPairingUri,
  createRelayAgentSessionStatePath,
  parseInvocation,
  readRelayPairingEventFile,
  removeRelayAgentSessionStateFile,
  removeRelayPairingEventFile,
  resolveBinaryInfo,
} = require('../../bin/mobilevc.js');

test('parseInvocation treats bare mobilevc as guided start', () => {
  const invocation = parseInvocation([]);
  assert.equal(invocation.command, 'start');
  assert.equal(invocation.options.guided, true);
});

test('parseInvocation keeps explicit start non-guided', () => {
  const invocation = parseInvocation(['start']);
  assert.equal(invocation.command, 'start');
  assert.equal(invocation.options.guided, false);
});

test('parseInvocation supports public command relay shorthand', () => {
  const invocation = parseInvocation(['public', 'wss://relay.example.test']);
  assert.equal(invocation.command, 'public');
  assert.equal(invocation.options.public, true);
  assert.equal(invocation.options.relay, 'wss://relay.example.test');
});

test('parseInvocation supports explicit relay-only exposure', () => {
  const invocation = parseInvocation([
    'public',
    'wss://relay.example.test',
    '--network-exposure-mode',
    'relay-only',
  ]);
  assert.equal(invocation.command, 'public');
  assert.equal(invocation.options.relay, 'wss://relay.example.test');
  assert.equal(invocation.options.networkExposureMode, 'relay-only');
});

test('buildRelayAccessConfig defaults public relay to LAN plus Relay', () => {
  const relay = buildRelayAccessConfig({}, { relay: 'wss://relay.example.test' });
  assert.equal(relay.enabled, true);
  assert.equal(relay.networkExposureMode, 'lan');
  assert.equal(relay.env.RELAY_MODE, 'true');
  assert.equal(relay.env.RELAY_URL, 'wss://relay.example.test');
  assert.equal(relay.env.NETWORK_EXPOSURE_MODE, 'lan');
});

test('buildRelayAccessConfig allows explicit relay-only exposure', () => {
  const relay = buildRelayAccessConfig(
    {},
    {
      relay: 'wss://relay.example.test',
      networkExposureMode: 'relay-only',
    },
  );
  assert.equal(relay.networkExposureMode, 'relay-only');
  assert.equal(relay.env.NETWORK_EXPOSURE_MODE, 'relay-only');
});

test('assertValidRelayURL rejects http and public ws relay urls', () => {
  assert.doesNotThrow(() => assertValidRelayURL('wss://relay.example.test'));
  assert.doesNotThrow(() => assertValidRelayURL('ws://127.0.0.1:9000'));
  assert.doesNotThrow(() => assertValidRelayURL('ws://192.168.1.10:9000'));
  assert.throws(() => assertValidRelayURL('https://relay.example.test'), /ws:\/\/ or wss:\/\//);
  assert.throws(() => assertValidRelayURL('ws://relay.example.test'), /loopback or LAN/);
});

test('buildRelayPairingUri does not include direct backend token', () => {
  const uri = buildRelayPairingUri({
    relayUrl: 'wss://relay.example.test',
    sessionId: 'rs_test',
    pairingSecret: 'pair_secret',
    expiresAt: 1760000000,
    nodeFingerprintHex: TEST_NODE_FINGERPRINT,
    capabilities: plaintextCapabilities(),
  });

  const parsed = new URL(uri);
  assert.equal(parsed.protocol, 'mobilevc:');
  assert.equal(parsed.hostname, 'relay');
  assert.equal(parsed.searchParams.get('relay'), 'wss://relay.example.test');
  assert.equal(parsed.searchParams.get('session'), 'rs_test');
  assert.equal(parsed.searchParams.get('secret'), 'pair_secret');
  assert.equal(parsed.searchParams.get('nodeFingerprint'), TEST_NODE_FINGERPRINT);
  assert.equal(parsed.searchParams.get('relayProtocolVersion'), '1');
  assert.equal(parsed.searchParams.get('plaintextTestMode'), 'true');
  assert.equal(parsed.searchParams.has('token'), false);
});

test('relay pairing event file is read locally and removable', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'mobilevc-relay-test-'));
  const eventPath = path.join(dir, 'pairing.json');
  const event = {
    type: 'mobilevc.relay.pairing_ready',
    relayUrl: 'wss://relay.example.test',
    sessionId: 'rs_test',
    pairingSecret: 'pair_secret',
    expiresAt: 1760000000,
    nodeFingerprintHex: TEST_NODE_FINGERPRINT,
    capabilities: plaintextCapabilities(),
  };
  fs.writeFileSync(eventPath, `${JSON.stringify(event)}\n`, { mode: 0o600 });

  assert.deepEqual(readRelayPairingEventFile(eventPath), event);
  removeRelayPairingEventFile(eventPath);
  assert.equal(fs.existsSync(eventPath), false);
});

test('relay agent session state path is per launch and removable', () => {
  const sessionPath = createRelayAgentSessionStatePath();
  assert.match(path.basename(sessionPath), /^mobilevc-relay-agent-session-.+\.json$/);

  fs.mkdirSync(path.dirname(sessionPath), { recursive: true });
  fs.writeFileSync(sessionPath, '{"version":1}\n', { mode: 0o600 });
  removeRelayAgentSessionStateFile(sessionPath);
  assert.equal(fs.existsSync(sessionPath), false);
});

test('isPortOccupied falls back from wildcard probe to IPv4 probe', async () => {
  const originalCreateServer = net.createServer;
  const listenCalls = [];
  let attempts = 0;

  net.createServer = () => {
    const server = new EventEmitter();
    server.listen = (options) => {
      listenCalls.push(options);
      attempts += 1;
      queueMicrotask(() => {
        const code = attempts === 1 ? 'EAFNOSUPPORT' : 'EADDRINUSE';
        server.emit('error', Object.assign(new Error(code), { code }));
      });
    };
    server.close = (callback) => {
      if (callback) {
        callback();
      }
    };
    return server;
  };

  try {
    assert.equal(await isPortOccupied(8123), true);
    assert.deepEqual(listenCalls, [
      { port: 8123 },
      { port: 8123, host: '0.0.0.0' },
    ]);
  } finally {
    net.createServer = originalCreateServer;
  }
});

test('resolveBinaryInfo can fall back to bundled package paths in repo', () => {
  const info = resolveBinaryInfo('darwin-arm64');
  assert.ok(info.binaryPath.endsWith('/packages/server-darwin-arm64/bin/mobilevc-server'));
});

const TEST_NODE_FINGERPRINT = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

function plaintextCapabilities() {
  return {
    relayProtocolVersion: 1,
    e2eeProtocolVersion: 1,
    cryptoSuite: 'p256-ecdsa+p256-ecdh+hkdf-sha256+aes-256-gcm',
    tunnelProtocolVersion: 1,
    supportsMultiplexStreams: true,
    supportsFileDownloadStream: true,
    supportsDeviceManagement: true,
    requiresE2EE: false,
    plaintextTestMode: true,
  };
}
