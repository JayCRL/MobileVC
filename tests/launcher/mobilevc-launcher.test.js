const test = require('node:test');
const assert = require('node:assert/strict');
const EventEmitter = require('node:events');
const net = require('net');

const {
  isPortOccupied,
  parseInvocation,
  resolveBinaryInfo,
} = require('../bin/mobilevc.js');

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
