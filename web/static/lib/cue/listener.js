// web/static/lib/cue/listener.js
// Listens through this device's microphone for a cue (parse.js) and calls
// onCue once for each one said. A thin shell around the browser's
// SpeechRecognition, which is given in so it is tested with a fake.
//
// Three things make it usable rather than merely working:
//  - Recognisers re-send a growing transcript (interim results) and then a
//    final one, so a cue is counted once per result and per position in it,
//    not once per event.
//  - A cooldown: one "hindsight flag" must not become three flags because
//    the recogniser said it three ways.
//  - Recognisers stop on their own (silence, a network blip, a minute's
//    limit). It restarts itself until stop() is called, and gives up with a
//    reason when the browser refuses the microphone.

import { parseCues } from './parse.js';

export const COOLDOWN_MS = 3000;

/** recogniser is the platform's SpeechRecognition constructor, or null. */
export function recogniser(w = globalThis) {
  return w.SpeechRecognition || w.webkitSpeechRecognition || null;
}

/**
 * createListener returns {start(), stop(), running}.
 *   Recognition   the constructor (defaults to the platform's)
 *   onCue(cue)    {action, label}
 *   onState(s)    'listening' | 'stopped' | 'denied' | 'unsupported'
 *   now()         a clock in ms, for the cooldown
 *   lang          a BCP-47 tag, default the page's
 */
export function createListener({ Recognition = recogniser(), onCue, onState = () => {}, now = () => Date.now(), lang, retryMs = 500, setTimer = setTimeout } = {}) {
  let rec = null;
  let want = false;
  let last = -Infinity;
  const seen = new Map(); // result index in this session -> cues already fired from it

  const handle = (event) => {
    for (let i = event.resultIndex; i < event.results.length; i++) {
      const alt = event.results[i]?.[0];
      if (!alt) continue;
      const cues = parseCues(alt.transcript);
      const done = seen.get(i) ?? 0;
      for (let k = done; k < cues.length; k++) {
        const t = now();
        if (t - last < COOLDOWN_MS) continue;
        last = t;
        onCue(cues[k]);
      }
      seen.set(i, Math.max(done, cues.length));
      // A final result is never seen again; keep the map small.
      if (event.results[i].isFinal) seen.delete(i);
    }
  };

  const open = () => {
    rec = new Recognition();
    rec.continuous = true;
    rec.interimResults = true;
    if (lang) rec.lang = lang;
    rec.onresult = handle;
    rec.onerror = (e) => {
      if (e?.error === 'not-allowed' || e?.error === 'service-not-allowed') {
        want = false;
        onState('denied');
      }
      // no-speech, network, aborted: onend restarts.
    };
    rec.onend = () => {
      seen.clear();
      if (want) setTimer(() => { if (want) { try { open(); } catch { /* try again on the next end */ } } }, retryMs);
      else onState('stopped');
    };
    rec.start();
    onState('listening');
  };

  return {
    get running() { return want; },
    start() {
      if (!Recognition) { onState('unsupported'); return false; }
      if (want) return true;
      want = true;
      try { open(); } catch { want = false; onState('stopped'); return false; }
      return true;
    },
    stop() {
      want = false;
      try { rec?.stop(); } catch { /* already stopped */ }
      rec = null;
      onState('stopped');
    },
  };
}
