import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PlayWindow } from './playwindow.js';

const clipped = new PlayWindow({ start: 100, end: 200 });
const whole = new PlayWindow(null);

test('a take with no selection plays whole: it opens at 0, contains everything and never finishes', () => {
  assert.equal(whole.clipped, false);
  assert.equal(whole.opening, 0);
  assert.equal(whole.entry(5000), 5000);
  assert.equal(whole.finished(1e9), false);
});

test('a take with a selection opens at In and plays In to Out', () => {
  assert.equal(clipped.clipped, true);
  assert.equal(clipped.opening, 100);
  assert.equal(clipped.entry(150), 150);   // inside: carry on from here
  assert.equal(clipped.entry(0), 100);     // before In: start at In
  assert.equal(clipped.entry(200), 100);   // at Out: start over at In
  assert.equal(clipped.entry(900), 100);   // past Out
  assert.equal(clipped.finished(199), false);
  assert.equal(clipped.finished(200), true);
});

test('an empty or backwards selection is no selection', () => {
  assert.equal(new PlayWindow({ start: 50, end: 50 }).clipped, false);
  assert.equal(new PlayWindow({ start: 90, end: 10 }).clipped, false);
});

test('of reads the API trim', () => {
  assert.deepEqual(PlayWindow.of({ trim: { start_frame: 3, end_frame: 9 } }).region, { start: 3, end: 9 });
  assert.equal(PlayWindow.of({ trim: null }).clipped, false);
  assert.equal(PlayWindow.of({}).clipped, false);
});

const skipping = new PlayWindow({ start: 100, end: 200 }, { skip: true });

test('skipping a selection plays the whole take and jumps from In to Out', () => {
  assert.equal(skipping.clipped, false, 'the whole take is the window');
  assert.equal(skipping.opening, 0);
  assert.equal(skipping.finished(1e9), false);
  assert.equal(skipping.jump(99), null);
  assert.equal(skipping.jump(100), 200);
  assert.equal(skipping.jump(150), 200, 'a seek into the section is moved on too');
  assert.equal(skipping.jump(199), 200);
  assert.equal(skipping.jump(200), null);
});

test('without skip, or without a selection, nothing is jumped', () => {
  assert.equal(clipped.jump(150), null);
  assert.equal(whole.jump(150), null);
  assert.equal(new PlayWindow(null, { skip: true }).jump(150), null);
  assert.equal(new PlayWindow(null, { skip: true }).skip, false, 'nothing to skip is not skipping');
});
