// web/static/lib/cue/parse.js
// Cue words: what a person says to the room that means "mark this".
//
// Speech recognition hands over a transcript; this decides whether it holds
// a cue. Pure: no browser, no network, so it is tested alone and the
// listener (listener.js) stays a thin shell around the platform's recogniser.
//
// A cue is the wake word, then a verb:
//
//   hindsight flag           a flag, no label
//   hindsight note [words]   a flag, labelled with the words after it
//   hindsight song a         a flag labelled "Song A" (a letter, a number or a word)
//   hindsight mark / bookmark / keep this    synonyms of flag
//
// The wake word is required so that a rehearsal's chatter ("let's flag
// that bar") never marks anything. Recognisers mishear a made-up word
// ("hind sight", "hindsite"), so the wake word is matched loosely.

export const WAKE = 'hindsight';

// How recognisers write the wake word. All lower-case, spaces collapsed.
const WAKE_FORMS = ['hindsight', 'hind sight', 'hindsite', 'hind site', 'hines sight', 'hindsides'];

const FLAG_VERBS = ['flag', 'flags', 'mark', 'bookmark', 'keep this', 'keep that', 'remember this', 'remember that'];

/** normalise lower-cases, drops punctuation and collapses spaces. */
export function normalise(s) {
  return String(s ?? '').toLowerCase().replace(/[^\p{L}\p{N}\s']/gu, ' ').replace(/\s+/g, ' ').trim();
}

/** titleCase makes a spoken label read as a label: "song a" is "Song A". */
function titleCase(s) {
  return s.replace(/(^|\s)(\p{L})/gu, (_, sp, c) => sp + c.toUpperCase());
}

/**
 * parseCues finds every cue in a transcript, in the order said:
 * [{ action: 'flag', label, at }] where `at` is the index in the normalised
 * transcript (so a caller can tell a cue it has seen from a new one).
 * `label` is '' for a bare flag. A transcript with no wake word has none.
 */
export function parseCues(transcript, { wake = WAKE_FORMS } = {}) {
  const text = normalise(transcript);
  if (!text) return [];
  const forms = [...wake].sort((a, b) => b.length - a.length);
  const out = [];
  let from = 0;
  while (from < text.length) {
    let hit = -1;
    let len = 0;
    for (const w of forms) {
      const i = text.indexOf(w, from);
      if (i < 0) continue;
      // A whole word only: "hindsights" is not the wake word.
      const before = i === 0 || text[i - 1] === ' ';
      const after = i + w.length === text.length || text[i + w.length] === ' ';
      if (!before || !after) continue;
      if (hit < 0 || i < hit) { hit = i; len = w.length; }
    }
    if (hit < 0) break;
    const rest = text.slice(hit + len).trim();
    const cue = verb(rest);
    if (cue) out.push({ ...cue, at: hit });
    from = hit + len;
  }
  return out;
}

/** parseCue is the first cue in a transcript, or null. */
export function parseCue(transcript, opts) {
  return parseCues(transcript, opts)[0] ?? null;
}

function verb(rest) {
  if (!rest) return null;
  for (const v of FLAG_VERBS) {
    if (rest === v || rest.startsWith(v + ' ')) return { action: 'flag', label: '' };
  }
  if (rest === 'note' || rest.startsWith('note ') || rest === 'notes' || rest.startsWith('notes ')) {
    const words = rest.replace(/^notes?/, '').trim();
    // A note's words stop at the next wake word, so "hindsight note nice
    // chorus hindsight flag" is one note and one flag.
    return { action: 'flag', label: titleCase(cut(words)) };
  }
  if (rest.startsWith('song ')) {
    const words = cut(rest);
    return { action: 'flag', label: titleCase(words.split(' ').slice(0, 3).join(' ')) };
  }
  return null;
}

function cut(words) {
  let end = words.length;
  for (const w of WAKE_FORMS) {
    const i = words.indexOf(` ${w}`);
    if (i >= 0 && i < end) end = i;
  }
  return words.slice(0, end).trim();
}
