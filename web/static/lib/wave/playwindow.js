// web/static/lib/wave/playwindow.js
// The part of a take that plays by default: its selection, In to Out, or the
// whole take when it has none. Opening a take, Play, |◂ and the end of
// playback all ask this one object, so the page's clock and the bar's take
// source cannot disagree about where a take starts and stops. Seeking is
// free: the window only decides where Play begins and when it stops.
//
// With `skip`, the selection is not what plays but what is left out: the
// window is the whole take, and playback that reaches In jumps to Out, so the
// pieces either side of a section being clipped can be heard run together.
//
// Pure: no DOM, no audio. `region` is {start, end} in frames, as the page
// keeps its selection, or null.

export class PlayWindow {
  constructor(region, { skip = false } = {}) {
    const r = region && region.end > region.start ? { start: region.start, end: region.end } : null;
    this.skip = !!(skip && r);
    // A skipped section is not a window: the whole take plays around it.
    this.region = this.skip ? null : r;
    this.hole = this.skip ? r : null;
  }

  /** of reads a take's saved trim ({start_frame, end_frame}), as the API names it. */
  static of(take) {
    const t = take && take.trim;
    return new PlayWindow(t ? { start: t.start_frame, end: t.end_frame } : null);
  }

  /** clipped is true when a selection narrows the take. */
  get clipped() { return !!this.region; }

  /** opening is where a take's playhead rests when it is opened. */
  get opening() { return this.region ? this.region.start : 0; }

  /** contains is whether `frame` is inside the window; a whole take contains every frame. */
  contains(frame) { return !this.region || (frame >= this.region.start && frame < this.region.end); }

  /** entry is where Play begins from `frame`: there if inside, else at In. */
  entry(frame) { return this.contains(frame) ? frame : this.region.start; }

  /** finished is whether playback at `frame` has run out of the window: only a selection ends it. */
  finished(frame) { return !!this.region && frame >= this.region.end; }

  /**
   * jump is where playback at `frame` has to go to leave the skipped section
   * out: its Out when `frame` is inside it, else null. Always null without
   * skip. Playback that lands inside the section from a seek is moved on the
   * same way, so scrubbing into it cannot play it.
   */
  jump(frame) {
    return this.hole && frame >= this.hole.start && frame < this.hole.end ? this.hole.end : null;
  }
}
