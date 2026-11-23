import { test } from 'node:test';
import assert from 'node:assert/strict';

import { parseCue, parseCues, normalise } from './parse.js';
import { createListener, COOLDOWN_MS } from './listener.js';
import { flagFromCue, mountCue } from './mount.js';
import { cueOn, setCueOn, CUE_KEY } from './pref.js';

test('a wake word and a verb is a cue', () => {
  assert.deepEqual(parseCue('Hindsight flag'), { action: 'flag', label: '', at: 0 });
  assert.equal(parseCue('okay hindsight, mark!').action, 'flag');
  assert.equal(parseCue('HINDSIGHT bookmark').action, 'flag');
});

test('a made-up word is heard loosely', () => {
  for (const s of ['hind sight flag', 'hindsite flag', 'Hind-sight, flag']) {
    assert.ok(parseCue(s), s);
  }
});

test('no wake word, no cue: rehearsal chatter marks nothing', () => {
  assert.equal(parseCue('let us flag that bar'), null);
  assert.equal(parseCue('flag'), null);
  assert.equal(parseCue(''), null);
  assert.equal(parseCue(null), null);
});

test('the wake word alone, or with an unknown verb, is not a cue', () => {
  assert.equal(parseCue('hindsight'), null);
  assert.equal(parseCue('hindsight is twenty twenty'), null);
  assert.equal(parseCue('hindsights flag'), null);
});

test('song A and note words become a label', () => {
  assert.equal(parseCue('hindsight song a').label, 'Song A');
  assert.equal(parseCue('hindsight song two verse').label, 'Song Two Verse');
  assert.equal(parseCue('hindsight note nice chorus here').label, 'Nice Chorus Here');
  assert.equal(parseCue('hindsight note').label, '');
});

test('several cues in one transcript are each found, and a note stops at the next wake word', () => {
  const cues = parseCues('hindsight note nice chorus hindsight flag');
  assert.deepEqual(cues.map((c) => c.label), ['Nice Chorus', '']);
  assert.ok(cues[1].at > cues[0].at);
});

test('normalise drops punctuation and keeps apostrophes and letters', () => {
  assert.equal(normalise("  Hind-sight,   FLAG!! don't "), "hind sight flag don't");
});

// ---------------------------------------------------------------- listener

function fakeRecognition() {
  const made = [];
  class R {
    constructor() { made.push(this); this.started = false; this.stopped = false; }
    start() { this.started = true; }
    stop() { this.stopped = true; this.onend?.(); }
  }
  R.made = made;
  return R;
}
const say = (rec, results, resultIndex = 0) => rec.onresult({ resultIndex, results });
const res = (transcript, isFinal = false) => Object.assign([{ transcript }], { isFinal });

test('an interim then a final result of the same words fires once', () => {
  const R = fakeRecognition();
  const cues = [];
  const l = createListener({ Recognition: R, onCue: (c) => cues.push(c), now: () => 0 });
  assert.equal(l.start(), true);
  const rec = R.made[0];
  say(rec, [res('hindsight')]);
  say(rec, [res('hindsight flag')]);
  say(rec, [res('hindsight flag', true)]);
  assert.equal(cues.length, 1);
});

test('the cooldown holds off a repeat, then lets the next cue through', () => {
  const R = fakeRecognition();
  let t = 0;
  const cues = [];
  createListener({ Recognition: R, onCue: (c) => cues.push(c), now: () => t }).start();
  const rec = R.made[0];
  say(rec, [res('hindsight flag', true)]);
  t = COOLDOWN_MS - 1;
  say(rec, [res('hindsight flag', true)], 0);
  assert.equal(cues.length, 1);
  t = COOLDOWN_MS + 1;
  say(rec, [res('hindsight flag', true)], 0);
  assert.equal(cues.length, 2);
});

test('it restarts when the recogniser ends on its own, and not after stop()', () => {
  const R = fakeRecognition();
  const timers = [];
  const states = [];
  const l = createListener({ Recognition: R, onCue() {}, onState: (s) => states.push(s), setTimer: (f) => timers.push(f), now: () => 0 });
  l.start();
  R.made[0].onend();
  assert.equal(timers.length, 1);
  timers[0]();
  assert.equal(R.made.length, 2);
  assert.ok(R.made[1].started);
  l.stop();
  assert.equal(l.running, false);
  assert.equal(states.at(-1), 'stopped');
  // a restart already queued must not reopen it
  timers.push(() => {});
  R.made[1].onend();
  assert.equal(R.made.length, 2);
});

test('a refused microphone stops it for good and says so', () => {
  const R = fakeRecognition();
  const states = [];
  const timers = [];
  const l = createListener({ Recognition: R, onCue() {}, onState: (s) => states.push(s), setTimer: (f) => timers.push(f) });
  l.start();
  R.made[0].onerror({ error: 'not-allowed' });
  R.made[0].onend();
  assert.ok(states.includes('denied'));
  assert.equal(l.running, false);
  assert.equal(timers.length, 0, 'no retry after a refusal');
});

test('a browser with no recogniser says so', () => {
  const states = [];
  const l = createListener({ Recognition: null, onCue() {}, onState: (s) => states.push(s) });
  assert.equal(l.start(), false);
  assert.deepEqual(states, ['unsupported']);
});

// ------------------------------------------------------------------- mount

test('a cue posts the live flag and says so', async () => {
  const R = fakeRecognition();
  const calls = [];
  const toasts = [];
  const fetchImpl = async (url, init) => { calls.push([url, init.method]); return { ok: true }; };
  const l = mountCue({ fetchImpl, toast: (m, k) => toasts.push([m, k]), Recognition: R, now: () => 0 });
  assert.ok(l);
  say(R.made[0], [res('hindsight song a', true)]);
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(calls, [['/api/flag', 'POST']]);
  assert.deepEqual(toasts, [['Noted — “\uE000Song A\uE001”', 'ok']]);
});

test('a flag the Pi refuses is told, not swallowed', async () => {
  const r = await flagFromCue({}, async () => ({ ok: false, status: 409, json: async () => ({ error: 'no audio buffered yet' }) }));
  assert.deepEqual(r, { ok: false, error: 'no audio buffered yet' });
  const n = await flagFromCue({}, async () => { throw new Error('offline'); });
  assert.equal(n.ok, false);
});

test('no recogniser: mountCue answers null and tells the person', () => {
  const toasts = [];
  assert.equal(mountCue({ Recognition: null, toast: (m) => toasts.push(m) }), null);
  assert.equal(toasts.length, 1);
});

// -------------------------------------------------------------------- pref

test('the switch is per device and off until turned on', () => {
  const store = {};
  globalThis.localStorage = { getItem: (k) => store[k] ?? null, setItem: (k, v) => { store[k] = String(v); }, removeItem: (k) => { delete store[k]; } };
  try {
    assert.equal(cueOn(), false);
    assert.equal(setCueOn(true), true);
    assert.equal(store[CUE_KEY], '1');
    assert.equal(cueOn(), true);
    assert.equal(setCueOn(false), true);
    assert.equal(cueOn(), false);
  } finally { delete globalThis.localStorage; }
});
