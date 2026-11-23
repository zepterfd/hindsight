// web/static/lib/phone/relay.js
// The phone as a relay: audio from a box that can reach the phone but not the
// Pi (an inline tap on the room's WiFi or its own, docs/plan/inline-tap.md)
// goes on to the Pi over the phone's own connection, which may be a tailnet
// away (docs/plan/tailscale-routing.md).
//
//   tap ──ws (local)──► this page ──Uploader──► /api/phone ──► the Pi's take
//
// It adds nothing to the Pi's end: the Uploader is the one the recorder uses,
// so numbering, acks, resend after a dropout and the partial-take rules are
// the same. This module owns only the first hop: the tap's stream, in; whole
// tenth-of-a-second chunks, out.
//
// The tap's stream (what `hindsight-tap -listen` serves):
//   text   {"type":"hello","rate":48000}            once, first
//   binary interleaved stereo float32 LE, any length
// A tap that drops is reconnected to; the Uploader keeps what it has queued
// meanwhile, and the recording stays open: a tap's WiFi blink does not end a
// take. stop() ends it.

const RETRY_MIN_MS = 500;
const RETRY_MAX_MS = 5000;

/**
 * chunker turns arbitrary binary pieces into chunks of exactly `frames`
 * stereo frames, keeping the remainder. push(ArrayBuffer) returns the whole
 * chunks that completed (Float32Arrays, interleaved); a piece that is not a
 * whole number of stereo float32 frames has its torn tail kept for the next.
 */
export function chunker(frames) {
  const per = frames * 2;
  let carry = new Float32Array(0);
  let bytes = new Uint8Array(0); // a torn float32
  return {
    push(buf) {
      let u8 = new Uint8Array(buf);
      if (bytes.length) { const m = new Uint8Array(bytes.length + u8.length); m.set(bytes); m.set(u8, bytes.length); u8 = m; bytes = new Uint8Array(0); }
      const whole = u8.length - (u8.length % 4);
      if (whole < u8.length) bytes = u8.slice(whole);
      const f = new Float32Array(whole / 4);
      new Uint8Array(f.buffer).set(u8.subarray(0, whole));
      const all = new Float32Array(carry.length + f.length);
      all.set(carry); all.set(f, carry.length);
      const out = [];
      let at = 0;
      while (all.length - at >= per) { out.push(all.slice(at, at + per)); at += per; }
      carry = all.slice(at);
      return out;
    },
    /** flush is what is left, as a short last chunk of whole stereo frames. */
    flush() {
      const n = carry.length - (carry.length % 2);
      const rest = n ? carry.slice(0, n) : null;
      carry = new Float32Array(0); bytes = new Uint8Array(0);
      return rest;
    },
  };
}

export class Relay {
  /**
   * @param {object} o
   * @param {string} o.tapUrl      ws:// address of the tap's stream
   * @param {(rate:number)=>object} o.makeUploader  makes the Uploader once the
   *   tap has said its sample rate (the Pi is told the rate in its start message)
   * @param {(state:string, info?:object)=>void} [o.onState]
   *   'connecting' | 'relaying' | 'tap-lost' | 'saving' | 'saved' | 'failed'
   */
  constructor({ tapUrl, makeUploader, WebSocketImpl = globalThis.WebSocket, schedule = (f, ms) => setTimeout(f, ms), onState }) {
    this.tapUrl = tapUrl;
    this.makeUploader = makeUploader;
    this.WS = WebSocketImpl;
    this.schedule = schedule;
    this.onState = onState;
    this.uploader = null;
    this.rate = 0;
    this.ws = null;
    this.gen = 0;
    this.retryMs = RETRY_MIN_MS;
    this.stopped = false;
    this.chunks = null;
    this.state = '';
  }

  start() { this.connect(); }

  setState(s, info) {
    if (s === this.state && !info) return;
    this.state = s;
    this.onState?.(s, info);
  }

  connect() {
    if (this.stopped) return;
    const gen = ++this.gen;
    this.setState(this.uploader ? 'tap-lost' : 'connecting');
    let ws;
    try { ws = new this.WS(this.tapUrl); } catch { this.retry(gen); return; }
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    let hello = false;
    ws.onmessage = (ev) => {
      if (gen !== this.gen || this.stopped) return;
      if (typeof ev.data === 'string') {
        let m;
        try { m = JSON.parse(ev.data); } catch { return; }
        if (m.type !== 'hello' || !(m.rate > 0)) return;
        if (this.uploader && m.rate !== this.rate) {
          // The recording on the Pi has a rate; a tap that changed it is a different recording.
          this.setState('failed', { error: `the tap's rate changed from ${this.rate} to ${m.rate}` });
          this.stop();
          return;
        }
        if (!this.uploader) {
          this.rate = m.rate;
          this.uploader = this.makeUploader(m.rate);
          this.uploader.start?.();
        }
        this.chunks = chunker(Math.round(this.rate / 10));
        hello = true;
        this.retryMs = RETRY_MIN_MS;
        this.setState('relaying');
        return;
      }
      if (!hello) return; // audio before the tap said its rate: ignored
      for (const c of this.chunks.push(ev.data)) this.uploader.push(c);
    };
    ws.onclose = () => { if (gen === this.gen && !this.stopped) this.retry(gen); };
    ws.onerror = () => { /* onclose follows */ };
  }

  retry(gen) {
    if (this.stopped || gen !== this.gen) return;
    gen = ++this.gen;
    // A torn last chunk is not carried across a reconnect: the tap's gap is a gap.
    this.chunks = null;
    this.setState(this.uploader ? 'tap-lost' : 'connecting');
    const ms = this.retryMs;
    this.retryMs = Math.min(RETRY_MAX_MS, this.retryMs * 2);
    this.schedule(() => { if (gen === this.gen) this.connect(); }, ms);
  }

  /**
   * stop ends the relay and the recording: what the tap sent is flushed, the
   * Uploader is stopped, and the take is what resolves (or null if the tap
   * never said a word).
   */
  async stop() {
    if (this.stopped) return this.result ?? null;
    this.stopped = true;
    this.gen++;
    try { this.ws?.close(); } catch { /* gone */ }
    if (!this.uploader) { this.setState('saved', {}); return null; }
    const rest = this.chunks?.flush();
    if (rest) this.uploader.push(rest);
    this.setState('saving');
    this.result = await this.uploader.stop();
    this.setState(this.result?.error ? 'failed' : 'saved', this.result);
    return this.result;
  }
}
