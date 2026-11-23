import { test } from 'node:test';
import assert from 'node:assert/strict';
import { normalize, add, remove, at, frames, gaps, next, body } from './sections.js';

test('normalize sorts, merges what touches or overlaps, and drops what is too short', () => {
  const got = normalize([{ start: 500, end: 700 }, { start: 0, end: 100 }, { start: 90, end: 200 }, { start: 200, end: 250 }, { start: 900, end: 905 }], 1000, 10);
  assert.deepEqual(got, [{ start: 0, end: 250 }, { start: 500, end: 700 }]);
});

test('normalize clamps to the take and rounds to whole frames', () => {
  assert.deepEqual(normalize([{ start: -5, end: 40.4 }, { start: 980, end: 2000 }], 1000, 1), [{ start: 0, end: 40 }, { start: 980, end: 1000 }]);
  assert.deepEqual(normalize([{ start: 1500, end: 2000 }], 1000, 1), []);
  assert.deepEqual(normalize(null, 1000), []);
});

test('add joins a region that overlaps one already there, and keeps the list in order', () => {
  let l = add([], { start: 600, end: 800 }, 1000, 10);
  l = add(l, { start: 100, end: 200 }, 1000, 10);
  l = add(l, { start: 150, end: 300 }, 1000, 10);
  assert.deepEqual(l, [{ start: 100, end: 300 }, { start: 600, end: 800 }]);
  assert.deepEqual(add(l, { start: 400, end: 405 }, 1000, 10), l, 'too short to add');
});

test('remove drops one section and ignores a bad index', () => {
  const l = [{ start: 0, end: 10 }, { start: 20, end: 30 }];
  assert.deepEqual(remove(l, 0), [{ start: 20, end: 30 }]);
  assert.deepEqual(remove(l, 5), l);
  assert.equal(l.length, 2, 'the input is not changed');
});

test('at finds the section holding a frame; the end is outside', () => {
  const l = [{ start: 10, end: 20 }, { start: 30, end: 40 }];
  assert.equal(at(l, 10), 0);
  assert.equal(at(l, 20), -1);
  assert.equal(at(l, 35), 1);
});

test('frames is the length of the new take', () => {
  assert.equal(frames([{ start: 10, end: 20 }, { start: 30, end: 45 }]), 25);
  assert.equal(frames([]), 0);
});

test('gaps are what a cut leaves out', () => {
  assert.deepEqual(gaps([{ start: 10, end: 20 }, { start: 30, end: 40 }], 50),
    [{ start: 0, end: 10 }, { start: 20, end: 30 }, { start: 40, end: 50 }]);
  assert.deepEqual(gaps([{ start: 0, end: 50 }], 50), []);
  assert.deepEqual(gaps([], 50), [{ start: 0, end: 50 }]);
});

test('next steers listening to the sections only', () => {
  const l = [{ start: 10, end: 20 }, { start: 30, end: 40 }];
  assert.equal(next(l, 0), 10);
  assert.equal(next(l, 15), 15);
  assert.equal(next(l, 20), 30);
  assert.equal(next(l, 39), 39);
  assert.equal(next(l, 40), null);
  assert.equal(next([], 5), null);
});

test('body is the plain cut for one section and ranges for several', () => {
  assert.deepEqual(body([{ start: 1, end: 9 }]), { start_frame: 1, end_frame: 9 });
  assert.deepEqual(body([{ start: 1, end: 9 }], 'x'), { start_frame: 1, end_frame: 9, label: 'x' });
  assert.deepEqual(body([{ start: 1, end: 9 }, { start: 20, end: 30 }]), { ranges: [{ start_frame: 1, end_frame: 9 }, { start_frame: 20, end_frame: 30 }] });
});
