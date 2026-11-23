// J2.13 Select a take and press ▶: it plays the take. With a tape in the
// bar, ▶ used to play the tape (its reels turned; nothing came out) because
// selecting a take puts nothing in the bar. Now ▶ plays the take chosen by
// selecting it; Open still opens its page. docs/flows.md#j213
import { aTake } from './room.mjs';

export default {
  id: 'J2.13',
  title: 'Select a take, press ▶, and hear that take',
  profiles: ['tablet', 'phone'],
  async run(t) {
    const { page, api } = t;
    await t.given('a tape in the bar', () => api.tape());
    await t.given('a take on the shelf', () => api.keep(20));
    await page.addInitScript(() => {
      window.__plays = [];
      const play = HTMLMediaElement.prototype.play;
      HTMLMediaElement.prototype.play = function (...a) { window.__plays.push(this.src); return play.apply(this, a); };
    });
    await t.given('the Listening Room is open', () => page.goto(`${t.base}/takes.html`));
    await aTake(page).waitFor({ state: 'visible' });
    t.expect('the bar holds the tape', await t.poll(() => document.getElementById('np')?.dataset.source === 'tape'));

    await t.step('hold the take to select it', () => t.hold(aTake(page)));
    t.expect('the select bar comes up', await t.poll(() => !document.getElementById('select-bar')?.hidden));

    // The select bar sits over the bar's ▶ on a phone: it is the button's own click that is asked.
    await t.step('press ▶ in the bar', () => page.evaluate(() => document.getElementById('np-play').click()));
    t.expect('the bar plays a take, not the tape', await t.poll(() => document.getElementById('np')?.dataset.source === 'take' && document.getElementById('np-play')?.classList.contains('playing'), null, 8000));
    const plays = await page.evaluate(() => window.__plays);
    t.expect('its preview is what plays', plays.some((s) => /_preview\.mp3/.test(s)), plays);
  },
};
