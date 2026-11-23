import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Relay, chunker } from './relay.js';

const f32 = (...v) => new Float32Array(v).buffer;

test('chunker cuts whole chunks and keeps the remainder', () => {
  const c = chunker(2); // 2 frames = 4 samples
  assert.equal(c.push(f32(1, 2, 3)).length, 0);
  const out = c.push(f32(4, 5, 6, 7, 8, 9));
  assert.deepEqual(out.map((x) => [...x]), [[1, 2, 3, 4], [5, 6, 7, 8]]);
  assert.equal(c.flush(), null, 'a lone sample is not a stereo frame');
});

test('chunker joins a float32 torn across two pieces', () => {
  const c = chunker(1); // 1 frame = 2 samples
  const b = new Uint8Array(f32(0.5, -0.25));
  assert.equal(c.push(b.slice(0, 3).buffer).length, 0);
  const out = c.push(b.slice(3).buffer);
  assert.deepEqual([...out[0]], [0.5, -0.25]);
});

test('flush returns whole stereo frames left over, once', () => {
  const c = chunker(10);
  c.push(f32(1, 2, 3, 4));
  assert.deepEqual([...c.flush()], [1, 2, 3, 4]);
  assert.equal(c.flush(), null);
});

// A fake tap, and a fake uploader that records what it was given.
class FakeTap {
  constructor() { this.sockets = []; }
  get WS() {
    const tap = this;
    return class {
      constructor(url) { this.url = url; tap.sockets.push(this); this.closed = false; }
      close() { this.closed = true; }
      hello(rate) { this.onmessage({ data: JSON.stringify({ type: 'hello', rate }) }); }
      audio(...v) { this.onmessage({ data: f32(...v) }); }
      drop() { this.onclose(); }
    };
  }
}
function fakeUploader() {
  const u = { pushed: [], started: false, stopped: false,
    start() { u.started = true; }, push(c) { u.pushed.push([...c]); },
    stop: async () => { u.stopped = true; return { name: 'jam_x.wav', seconds: 1, partial: false, reason: 'stop' }; } };
  return u;
}

function setup(over = {}) {
  const tap = new FakeTap();
  const up = fakeUploader();
  const states = [];
  const timers = [];
  let rate = 0;
  const r = new Relay({ tapUrl: 'ws://tap.local:9000', WebSocketImpl: tap.WS, schedule: (f) => timers.push(f),
    makeUploader: (x) => { rate = x; return up; }, onState: (s) => states.push(s), ...over });
  return { r, tap, up, states, timers, get rate() { return rate; } };
}

test('audio waits for the tap to say its rate, then goes up in tenth-second chunks', () => {
  const t = setup();
  t.r.start();
  const ws = t.tap.sockets[0];
  ws.audio(9, 9); // before hello: ignored
  assert.equal(t.up.pushed.length, 0);
  ws.hello(20); // 2 frames per chunk = 4 samples
  assert.equal(t.rate, 20);
  assert.ok(t.up.started);
  ws.audio(1, 2, 3, 4, 5, 6);
  assert.deepEqual(t.up.pushed, [[1, 2, 3, 4]]);
  assert.deepEqual(t.states, ['connecting', 'relaying']);
});

test('a tap that drops is reconnected to, and the recording stays open', () => {
  const t = setup();
  t.r.start();
  t.tap.sockets[0].hello(20);
  t.tap.sockets[0].audio(1, 2, 3); // a torn chunk
  t.tap.sockets[0].drop();
  assert.equal(t.states.at(-1), 'tap-lost');
  assert.equal(t.up.stopped, false, 'a WiFi blink must not end the take');
  t.timers.shift()();
  assert.equal(t.tap.sockets.length, 2);
  t.tap.sockets[1].hello(20);
  t.tap.sockets[1].audio(7, 8, 9, 10);
  assert.deepEqual(t.up.pushed, [[7, 8, 9, 10]], 'the torn piece is a gap, not glued to the new stream');
  assert.equal(t.states.at(-1), 'relaying');
});

test('a tap that comes back at another rate is refused: it is a different recording', async () => {
  const t = setup();
  t.r.start();
  t.tap.sockets[0].hello(20);
  t.tap.sockets[0].drop();
  t.timers.shift()();
  t.tap.sockets[1].hello(40);
  assert.ok(t.states.includes('failed'));
  assert.equal(t.up.stopped, true);
});

test('stop flushes what is left and resolves with the take', async () => {
  const t = setup();
  t.r.start();
  const ws = t.tap.sockets[0];
  ws.hello(20);
  ws.audio(1, 2); // half a chunk
  const take = await t.r.stop();
  assert.equal(take.name, 'jam_x.wav');
  assert.deepEqual(t.up.pushed, [[1, 2]]);
  assert.ok(ws.closed);
  assert.deepEqual(t.states.slice(-2), ['saving', 'saved']);
  // Late events from the old socket do nothing.
  ws.audio(5, 6, 7, 8);
  assert.equal(t.up.pushed.length, 1);
});

test('stop with a tap that never spoke makes no recording', async () => {
  const t = setup();
  t.r.start();
  assert.equal(await t.r.stop(), null);
  assert.equal(t.up.started, false);
});

test('a hello that is not one, or is not JSON, is ignored', () => {
  const t = setup();
  t.r.start();
  const ws = t.tap.sockets[0];
  ws.onmessage({ data: 'not json' });
  ws.onmessage({ data: JSON.stringify({ type: 'hello', rate: 0 }) });
  ws.onmessage({ data: JSON.stringify({ type: 'other', rate: 48000 }) });
  assert.equal(t.up.started, false);
});
