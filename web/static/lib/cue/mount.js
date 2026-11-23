// web/static/lib/cue/mount.js
// Wires the listener to the Pi: a cue marks this moment in the ring, as the
// Flag now key does (POST /api/flag), and says so in a toast. Mounted by
// lib/nav.js on a device that turned it on (lib/device-prefs.js).
//
// POC limits, on purpose: the live flag carries no label on the Pi yet, so a
// spoken "song A" or "note nice chorus" is shown in the toast and the flag is
// a plain one; and recognition is the browser's own (Chrome and Safari send
// the audio to their vendor's speech service), which the setting's note says.

import { createListener, recogniser } from './listener.js';
import { toast as defaultToast } from '../toast.js';
import { named } from '../rooms/words.js';

const SAID = { denied: 'Cue words need the microphone — is it allowed in the browser?', unsupported: 'This browser can\'t listen for cue words.' };

/**
 * flagFromCue posts the flag. Answers {ok} or {ok:false, error} ready to show;
 * never throws, because it runs from a recogniser's callback.
 */
export async function flagFromCue(cue, fetchImpl = globalThis.fetch) {
  try {
    const res = await fetchImpl('/api/flag', { method: 'POST' });
    if (res.ok) return { ok: true };
    const b = (await res.json().catch(() => null)) || {};
    return { ok: false, error: b.error || `HTTP ${res.status}` };
  } catch {
    return { ok: false, error: 'could not reach the Pi' };
  }
}

/** mountCue starts listening. Returns the listener, or null where there is nothing to listen with. */
export function mountCue({ fetchImpl = globalThis.fetch, toast = defaultToast, Recognition = recogniser(), ...rest } = {}) {
  const l = createListener({
    Recognition,
    ...rest,
    onState: (s) => { if (SAID[s]) toast(SAID[s], 'warn'); },
    onCue: async (cue) => {
      const r = await flagFromCue(cue, fetchImpl);
      if (r.ok) toast(cue.label ? `Noted — “${named(cue.label)}”` : 'Noted', 'ok', { ms: 2500 });
      else toast(`Heard the cue, but ${r.error}`, 'bad');
    },
  });
  return l.start() ? l : null;
}
