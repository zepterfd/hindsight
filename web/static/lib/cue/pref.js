// web/static/lib/cue/pref.js
// Whether this device listens for cue words. Per device (lib/device-prefs.js):
// the microphone is this phone's, and the other kits' are none of its business.
// Off until someone turns it on, and the browser still asks for the microphone.

export const CUE_KEY = 'hindsight.cue';

/** cueOn says whether this device listens for cue words. */
export function cueOn() {
  try { return localStorage.getItem(CUE_KEY) === '1'; } catch { return false; }
}

/** setCueOn turns it on or off and says whether that stuck. */
export function setCueOn(v) {
  try {
    if (v) localStorage.setItem(CUE_KEY, '1');
    else localStorage.removeItem(CUE_KEY);
    return cueOn() === !!v;
  } catch { return false; }
}
