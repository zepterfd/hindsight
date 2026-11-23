// J2.12 Skip a selection to hear what is either side of it: on a take's page,
// with an In and Out chosen, "Skip it" beside Clear leaves that section out
// of playback (the preview jumps from In to Out; lib/wave/playwindow.js).
// It is on the screen at every size, it lights when on, and it gives way
// while Repeat is on, since a loop is the selection. docs/flows.md#j212
export default {
  id: 'J2.12',
  title: 'Skip the selection to hear what is either side of it',
  profiles: ['tablet', 'desktop', 'phone'],
  async run(t) {
    const { page, api } = t;
    const take = await t.given('a take on the shelf', () => api.keep(10));
    await t.given('it has a selection', () => api.patch(`/api/take?file=${encodeURIComponent(take.name)}`,
      { trim: { start_frame: 96000, end_frame: 192000 } }));
    await t.given('its page is open', () => page.goto(`${t.base}/wave.html?file=${encodeURIComponent(take.name)}`));
    await page.locator('#sel-row').waitFor({ state: 'visible', timeout: 10_000 });

    const box = await page.evaluate(() => {
      const b = document.getElementById('sel-skip').getBoundingClientRect();
      const c = document.getElementById('sel-clear').getBoundingClientRect();
      return { left: Math.round(b.left), right: Math.round(b.right), W: innerWidth, scrollW: document.scrollingElement.scrollWidth, clearRight: Math.round(c.right), w: Math.round(b.width) };
    });
    t.expect('Skip it is on the screen beside Clear, and the page has no sideways scroll', box.left >= 0 && box.right <= box.W && box.clearRight <= box.W && box.scrollW <= box.W && box.w > 0, box);
    t.expect('it starts off', (await page.getAttribute('#sel-skip', 'aria-pressed')) === 'false');

    await t.step('tap Skip it', () => t.tap(page.locator('#sel-skip')));
    t.expect('it lights', await t.poll(() => document.getElementById('sel-skip')?.getAttribute('aria-pressed') === 'true'));

    // Repeat sits in the bar's modes, which the phone folds away until the
    // player is up; the pairing is the same page code at every size.
    if (t.profile !== 'phone') {
      await t.step('tap Repeat', () => t.tap(page.locator('#loop')));
      t.expect('while Repeat is on there is nothing to skip: Skip it is off the table', await t.poll(() => document.getElementById('sel-skip')?.disabled === true));
      await t.step('tap Repeat again', () => t.tap(page.locator('#loop')));
      t.expect('and back, still lit', await t.poll(() => {
        const b = document.getElementById('sel-skip');
        return !b.disabled && b.getAttribute('aria-pressed') === 'true';
      }));
    }

    await t.step('tap Skip it again', () => t.tap(page.locator('#sel-skip')));
    t.expect('it goes dark', await t.poll(() => document.getElementById('sel-skip')?.getAttribute('aria-pressed') === 'false'));
  },
};
