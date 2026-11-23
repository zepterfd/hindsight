// J2.14 A take in the Listening Room's List has a ⋯ at its end: a menu of
// quick actions (play, star, open, the WAV, then Delete, last and red), so a
// take can be starred or deleted without opening it. The ⋯ is a key of its own
// beside the case, so a tap on it never plays the take. Delete goes to the
// trash with its Undo. docs/flows.md#j214
import { aTake } from './room.mjs';

export default {
  id: 'J2.14',
  title: 'Star or delete a take from its ⋯ in the List',
  profiles: ['phone', 'tablet'],
  async run(t) {
    const { page, api } = t;
    const take = await t.given('a take on the shelf', () => api.keep(8));
    await t.given('the Listening Room is open', () => page.goto(`${t.base}/takes.html`));
    await aTake(page).waitFor({ state: 'visible' });
    if (t.profile !== 'phone') await t.step('switch to the List', () => t.tap(page.locator('.shelf-view button', { hasText: 'List' })));
    const row = page.locator(`#takes .take.spine[data-name="${take.name}"]`);
    await row.waitFor({ state: 'visible' });
    const more = row.locator('.spine-more');

    const box = await page.evaluate((name) => {
      const b = document.querySelector(`#takes .take.spine[data-name="${name}"] .spine-more`).getBoundingClientRect();
      const o = document.querySelector(`#takes .take.spine[data-name="${name}"] .spine-open`).getBoundingClientRect();
      return { left: Math.round(b.left), right: Math.round(b.right), W: innerWidth, scrollW: document.scrollingElement.scrollWidth, w: Math.round(b.width), openLeft: Math.round(o.left) };
    }, take.name);
    t.expect('⋯ is on the screen before ›, a key a finger wide, and the page has no sideways scroll',
      box.left >= 0 && box.right <= box.openLeft && box.w >= 36 && box.scrollW <= box.W, box);

    await t.step('tap ⋯', () => t.tap(more));
    const words = await t.poll(() => {
      const m = document.querySelector('.menu:not([hidden]) [role="menuitem"]') && [...document.querySelectorAll('.menu:not([hidden]) [role="menuitem"]')].map((e) => e.textContent.trim());
      return m && m.length ? m : null;
    });
    t.expect('its menu is Play, Star, Open, the WAV, and Delete last', JSON.stringify(words) === JSON.stringify(['Play', 'Star', 'Open', 'Download WAV', 'Delete']), words);
    t.expect('tapping ⋯ did not start the take', await page.evaluate(() => document.getElementById('np')?.dataset.source !== 'take'));

    await t.step('pick Star', () => t.tap(page.locator('.menu:not([hidden]) [role="menuitem"]', { hasText: /^Star$/ })));
    const starred = await api.until(async () => (await api.takes()).find((x) => x.name === take.name && x.starred), 'the star to reach the Pi', 5000).catch(() => null);
    t.expect('the Pi has the star', !!starred);

    await t.step('tap ⋯ again', () => t.tap(more));
    await t.poll(() => !!document.querySelector('.menu:not([hidden]) [role="menuitem"]'));
    await t.step('pick Delete', () => t.tap(page.locator('.menu:not([hidden]) [role="menuitem"].danger')));
    const gone = await api.until(async () => !(await api.takes()).some((x) => x.name === take.name), 'the take to leave the shelf', 5000).catch(() => false);
    t.expect('the take is deleted', !!gone);
    const toast = page.locator('#toasts .toast').filter({ hasText: /Deleted/ }).first();
    await toast.waitFor({ timeout: 5000 });
    await t.step('Undo', () => toast.getByRole('button', { name: /Undo/ }).click());
    const back = await api.until(async () => (await api.takes()).some((x) => x.name === take.name), 'the take back', 5000).catch(() => false);
    t.expect('Undo brings it back', !!back);
  },
};
