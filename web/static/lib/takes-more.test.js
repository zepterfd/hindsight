// A row's ⋯ in the Listening Room's List (TakesList moreKey): quick actions on
// the take, so a take can be deleted or starred from the List without opening
// it. The actions are the row's own controls pressed.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fakeDoc } from './play/fake-dom.js';
import { take, click } from './room/harness.js';
import './room/abs-hook.js';

async function list(opts = {}, rows = [take('a', { has_peaks: false })]) {
  const { TakesList } = await import('./takes.js');
  const doc = fakeDoc();
  globalThis.document = doc;
  globalThis.IntersectionObserver = class { observe() {} unobserve() {} };
  const root = doc.createElement('div');
  const toasts = [];
  const l = new TakesList(root, doc.createElement('p'), { spines: true, onToast: (m, k) => toasts.push([m, k]), shape: (all) => [{ label: '', takes: all }], ...opts });
  l.all = rows;
  l.reshape();
  return { l, doc, toasts, row: l.rows.get('jam_a.wav') };
}

test('a row has a ⋯ key only where the list asks for one, named for its take', async () => {
  const none = await list({ openKey: true });
  assert.equal(none.row.el.querySelector('.spine-more'), null);
  assert.equal(none.row.el.classList.contains('mores'), false);
  const some = await list({ openKey: true, moreKey: true });
  const key = some.row.el.querySelector('.spine-more');
  assert.ok(key, 'the key is there');
  assert.equal(some.row.el.classList.contains('mores'), true);
  assert.match(key.getAttribute('aria-label'), /^More for /);
  assert.equal(key.getAttribute('aria-haspopup'), 'menu');
});

test('its menu is Play, Star, Open and the WAV, then Delete last, red, after a line', async () => {
  const { l, row } = await list({ openKey: true, moreKey: true });
  const items = l.rowActions(row);
  const words = items.filter((e) => e.getAttribute('role') === 'menuitem').map((e) => e.textContent.trim());
  assert.deepEqual(words, ['Play', 'Star', 'Open', 'Download WAV', 'Delete']);
  const last = items.at(-1);
  assert.equal(last.classList.contains('danger'), true, 'Delete is red');
  assert.equal(items.at(-2).getAttribute('role'), 'separator', 'after a line');
});

test('Star says Unstar for a starred take, and presses the row’s own star', async () => {
  const { l, row } = await list({ openKey: true, moreKey: true }, [take('a', { has_peaks: false, starred: true })]);
  const star = l.rowActions(row).find((e) => /star/i.test(e.textContent));
  assert.equal(star.textContent.trim(), 'Unstar');
  let pressed = 0;
  row.starBtn.click = () => { pressed++; };
  click(star);
  assert.equal(pressed, 1);
});

test('Delete trashes the take with its Undo, as the row’s own Delete does', async () => {
  const { l, row } = await list({ openKey: true, moreKey: true });
  let trashed = null;
  l.trash = async (names) => { trashed = names; };
  const del = l.rowActions(row).at(-1);
  click(del);
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(trashed, ['jam_a.wav']);
});
