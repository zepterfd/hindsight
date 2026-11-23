// web/static/lib/wave/sections.js
// The sections a person has picked out of one take: In/Out pairs, any number,
// that POST /api/cut joins into one new take (`ranges`). Pure: no DOM, no
// audio. A list is an array of {start, end} in frames, kept sorted, apart and
// each at least `minLen` long, so what the page holds is always what the
// server will accept. The server checks again; this is so the page never
// offers a cut it knows would be refused.

/** normalize sorts regions, drops those too short or outside the take, clamps the rest, and merges ones that touch or overlap. */
export function normalize(list, total, minLen = 1) {
  const sorted = (list || [])
    .map((r) => ({ start: Math.max(0, Math.round(r.start)), end: Math.min(total, Math.round(r.end)) }))
    .filter((r) => r.end - r.start >= minLen)
    .sort((a, b) => a.start - b.start);
  const out = [];
  for (const r of sorted) {
    const last = out[out.length - 1];
    if (last && r.start <= last.end) last.end = Math.max(last.end, r.end);
    else out.push({ ...r });
  }
  return out;
}

/** add puts one more section in; a section that overlaps another becomes one with it. */
export function add(list, region, total, minLen = 1) {
  return normalize([...(list || []), region], total, minLen);
}

/** remove drops the section at `index`; an index out of range leaves the list as it is. */
export function remove(list, index) {
  return list.filter((_, i) => i !== index);
}

/** at is the index of the section holding `frame`, or -1. */
export function at(list, frame) {
  return list.findIndex((r) => frame >= r.start && frame < r.end);
}

/** frames is how long the new take would be: the sections, end to end. */
export function frames(list) {
  return list.reduce((n, r) => n + (r.end - r.start), 0);
}

/** gaps is what a cut would leave out: the stretches between sections, and before the first and after the last. */
export function gaps(list, total) {
  const out = [];
  let from = 0;
  for (const r of list) {
    if (r.start > from) out.push({ start: from, end: r.start });
    from = r.end;
  }
  if (from < total) out.push({ start: from, end: total });
  return out;
}

/**
 * next is where listening to the sections only should be at `frame`: the
 * frame itself inside a section, the start of the next section from a gap, or
 * null once the last has played.
 */
export function next(list, frame) {
  for (const r of list) {
    if (frame < r.start) return r.start;
    if (frame < r.end) return frame;
  }
  return null;
}

/** body is the JSON for POST /api/cut: one section is the plain cut's body, several are `ranges`. */
export function body(list, label) {
  const withLabel = (b) => (label ? { ...b, label } : b);
  if (list.length === 1) return withLabel({ start_frame: list[0].start, end_frame: list[0].end });
  return withLabel({ ranges: list.map((r) => ({ start_frame: r.start, end_frame: r.end })) });
}
