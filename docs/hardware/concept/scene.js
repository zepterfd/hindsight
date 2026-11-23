// Hindsight Deck: a concept render. A made-up device, drawn from nothing but
// the idea: a multitrack's strips, a loop station's pads, and the Hindsight
// screen, on a Raspberry Pi. Not a product; no maker's design is copied.
import * as THREE from 'three';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js';
import { RoomEnvironment } from 'three/examples/jsm/environments/RoomEnvironment.js';

const canvasEl = document.getElementById('c');
const renderer = new THREE.WebGLRenderer({ canvas: canvasEl, antialias: true, preserveDrawingBuffer: true });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.05;
renderer.outputColorSpace = THREE.SRGBColorSpace;

const scene = new THREE.Scene();
scene.background = new THREE.Color(0x15181d);
scene.fog = new THREE.Fog(0x15181d, 14, 30);
const pmrem = new THREE.PMREMGenerator(renderer);
scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
scene.environmentIntensity = 0.55;

const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 100);
const controls = new OrbitControls(camera, canvasEl);
controls.enableDamping = true;
controls.minDistance = 4; controls.maxDistance = 16;
controls.maxPolarAngle = Math.PI * 0.49;
controls.target.set(0, 0.2, 0.1);

// ------------------------------------------------------------ lights + floor
const key = new THREE.DirectionalLight(0xfff1e0, 2.4);
key.position.set(-4, 7, 5);
key.castShadow = true;
key.shadow.mapSize.set(2048, 2048);
Object.assign(key.shadow.camera, { left: -6, right: 6, top: 6, bottom: -6, near: 1, far: 20 });
key.shadow.bias = -0.0004; key.shadow.radius = 5;
scene.add(key);
const rim = new THREE.DirectionalLight(0x7fb2ff, 1.4); rim.position.set(5, 3, -5); scene.add(rim);
const floor = new THREE.Mesh(new THREE.CircleGeometry(14, 64), new THREE.MeshStandardMaterial({ color: 0x1b1f26, roughness: 0.85, metalness: 0.05 }));
floor.rotation.x = -Math.PI / 2; floor.position.y = -0.62; floor.receiveShadow = true; scene.add(floor);

// ---------------------------------------------------------------- materials
const M = {
  body: new THREE.MeshStandardMaterial({ color: 0x2a2f38, roughness: 0.5, metalness: 0.35 }),
  deck: new THREE.MeshStandardMaterial({ color: 0x1d2128, roughness: 0.62, metalness: 0.25 }),
  trim: new THREE.MeshStandardMaterial({ color: 0xb98a5a, roughness: 0.32, metalness: 0.9 }), // brushed brass
  rubber: new THREE.MeshStandardMaterial({ color: 0x14161a, roughness: 0.95, metalness: 0 }),
  steel: new THREE.MeshStandardMaterial({ color: 0xc9ced6, roughness: 0.28, metalness: 1 }),
  slot: new THREE.MeshStandardMaterial({ color: 0x07080a, roughness: 1 }),
  knurl: new THREE.MeshStandardMaterial({ color: 0x3a4049, roughness: 0.4, metalness: 0.8 }),
  glass: new THREE.MeshPhysicalMaterial({ color: 0x050607, roughness: 0.04, metalness: 0, clearcoat: 1, clearcoatRoughness: 0.03, transmission: 0 }),
};
const led = (hex, i = 2.2) => new THREE.MeshStandardMaterial({ color: 0x111111, emissive: hex, emissiveIntensity: i, roughness: 0.4 });

const rbox = (w, h, d, r, mat) => { const m = new THREE.Mesh(new RoundedBoxGeometry(w, h, d, 4, r), mat); m.castShadow = m.receiveShadow = true; return m; };
const cyl = (rt, rb, h, mat, seg = 48) => { const m = new THREE.Mesh(new THREE.CylinderGeometry(rt, rb, h, seg), mat); m.castShadow = m.receiveShadow = true; return m; };

// The device sits in a group tilted a few degrees toward the player.
const deck = new THREE.Group(); scene.add(deck);
deck.rotation.x = 0.0;

// Body: a base and a raised, bevelled top plate that slopes to the front.
const base = rbox(6.2, 0.62, 3.9, 0.16, M.body); base.position.y = -0.31; deck.add(base);
const plate = rbox(6.0, 0.22, 3.7, 0.1, M.deck); plate.position.y = 0.04; deck.add(plate);
const brass = rbox(6.06, 0.035, 3.76, 0.05, M.trim); brass.position.y = -0.045; deck.add(brass); // an edge of brass between the two
const TOP = 0.15; // the plate's top surface

// Feet
for (const [x, z] of [[-2.7, -1.6], [2.7, -1.6], [-2.7, 1.6], [2.7, 1.6]]) {
  const f = cyl(0.17, 0.2, 0.1, M.rubber, 24); f.position.set(x, -0.67, z); deck.add(f);
}

// ------------------------------------------------------------------- screen
const sc = document.createElement('canvas'); sc.width = 1024; sc.height = 640;
const sx = sc.getContext('2d');
const screenTex = new THREE.CanvasTexture(sc);
screenTex.colorSpace = THREE.SRGBColorSpace; screenTex.anisotropy = 8;
const bezel = rbox(3.5, 0.12, 2.28, 0.07, M.glass);
const screenGroup = new THREE.Group();
screenGroup.position.set(0, TOP + 0.42, -1.12);
screenGroup.rotation.x = 0.62; // the back edge up: it faces the player
bezel.position.y = 0;
screenGroup.add(bezel);
const screenMesh = new THREE.Mesh(new THREE.PlaneGeometry(3.3, 2.06), new THREE.MeshBasicMaterial({ map: screenTex, toneMapped: false }));
screenMesh.rotation.x = -Math.PI / 2; screenMesh.position.y = 0.062;
screenGroup.add(screenMesh);
const stand = rbox(2.6, 0.5, 0.5, 0.08, M.body); stand.position.set(0, TOP + 0.1, -1.45); deck.add(stand);
deck.add(screenGroup);

const TRACKS = [
  { c: '#ff6b6b', seed: 3, name: 'DRUMS' }, { c: '#ffd166', seed: 11, name: 'BASS' },
  { c: '#4ecdc4', seed: 23, name: 'GUITAR' }, { c: '#a78bfa', seed: 37, name: 'KEYS' },
];
function rnd(n) { const x = Math.sin(n * 127.1) * 43758.5453; return x - Math.floor(x); }
function drawScreen(t) {
  const W = 1024, H = 640;
  sx.fillStyle = '#0b0e13'; sx.fillRect(0, 0, W, H);
  // top bar
  sx.fillStyle = '#12161d'; sx.fillRect(0, 0, W, 56);
  sx.fillStyle = '#e8e6e1'; sx.font = '600 26px system-ui, sans-serif'; sx.textBaseline = 'middle';
  sx.fillText('HINDSIGHT', 24, 29);
  sx.fillStyle = '#ff5d5d'; sx.beginPath(); sx.arc(W - 250, 29, 8 + 2 * Math.sin(t * 3), 0, 7); sx.fill();
  sx.fillStyle = '#9aa3b2'; sx.font = '500 20px system-ui, sans-serif';
  sx.fillText('LISTENING   14:52 buffered', W - 232, 30);
  // tracks
  const top = 78, th = 108, gap = 10, left = 90;
  const scroll = t * 60;
  TRACKS.forEach((tr, i) => {
    const y = top + i * (th + gap);
    sx.fillStyle = '#10141b'; sx.fillRect(14, y, W - 28, th);
    sx.fillStyle = tr.c; sx.fillRect(14, y, 5, th);
    sx.fillStyle = '#9aa3b2'; sx.font = '600 15px system-ui, sans-serif'; sx.fillText(tr.name, 30, y + 16);
    sx.strokeStyle = tr.c; sx.lineWidth = 2; sx.beginPath();
    for (let x = left; x < W - 24; x += 2) {
      const k = Math.floor((x + scroll) / 4);
      const burst = 0.45 + 0.4 * Math.sin((x + scroll) * 0.011 + tr.seed) * Math.sin((x + scroll) * 0.0037 + tr.seed * 2) + rnd(k * 0.07 + tr.seed) * 0.15;
      const a = (rnd(k + tr.seed) * 0.6 + 0.2) * burst * (th * 0.42);
      sx.moveTo(x, y + th / 2 - a); sx.lineTo(x, y + th / 2 + a);
    }
    sx.stroke();
  });
  // flags
  [[0.28, 'verse'], [0.55, 'that fill!'], [0.8, 'keep']].forEach(([p, label], j) => {
    const x = left + ((p * (W - 140) - scroll * 0.0) % (W - 140));
    sx.fillStyle = '#ffd166'; sx.fillRect(x, top - 6, 2, 4 * (th + gap) + 4);
    sx.beginPath(); sx.moveTo(x, top - 8); sx.lineTo(x + 18, top - 14); sx.lineTo(x, top - 20); sx.fill();
    sx.font = '600 14px system-ui, sans-serif'; sx.fillText(label, x + 22, top - 14 + (j % 2));
  });
  // NOW line + the reach-back selection
  const nowX = W - 40;
  sx.fillStyle = 'rgba(255,209,102,0.16)'; sx.fillRect(nowX - 300, top - 4, 300, 4 * (th + gap));
  sx.fillStyle = '#fff'; sx.fillRect(nowX, top - 8, 3, 4 * (th + gap) + 8);
  sx.font = '700 14px system-ui, sans-serif'; sx.fillStyle = '#fff'; sx.fillText('NOW', nowX - 38, top - 22);
  // KEEP chip
  const pulse = 0.65 + 0.35 * Math.sin(t * 2.2);
  sx.fillStyle = `rgba(255,170,40,${pulse})`; roundRect(sx, W - 196, H - 62, 170, 44, 12); sx.fill();
  sx.fillStyle = '#1a1206'; sx.font = '800 22px system-ui, sans-serif'; sx.fillText('KEEP last 5:00', W - 182, H - 39);
  sx.fillStyle = '#9aa3b2'; sx.font = '500 17px system-ui, sans-serif';
  sx.fillText('tap a flag to jump · hold to select', 24, H - 40);
  screenTex.needsUpdate = true;
}
function roundRect(c, x, y, w, h, r) { c.beginPath(); c.moveTo(x + r, y); c.arcTo(x + w, y, x + w, y + h, r); c.arcTo(x + w, y + h, x, y + h, r); c.arcTo(x, y + h, x, y, r); c.arcTo(x, y, x + w, y, r); c.closePath(); }

// ------------------------------------------------------------ channel strips
const pads = [];
const faders = [];
const knobs = [];
const stripX = [-1.95, -1.15, -0.35, 0.45];
TRACKS.forEach((tr, i) => {
  const x = stripX[i];
  const colour = new THREE.Color(tr.c);
  // encoder
  const enc = cyl(0.14, 0.15, 0.17, M.knurl, 40); enc.position.set(x, TOP + 0.085, 0.2); deck.add(enc);
  const cap = cyl(0.115, 0.115, 0.02, M.steel, 40); cap.position.set(x, TOP + 0.18, 0.2); deck.add(cap);
  const tick = rbox(0.02, 0.012, 0.09, 0.005, led(colour, 1.6)); tick.position.set(0, 0.013, -0.04); cap.add(tick);
  knobs.push(cap);
  // pad
  const padMat = led(colour, 0.5);
  const pad = rbox(0.62, 0.1, 0.62, 0.1, M.rubber); pad.position.set(x, TOP + 0.05, 0.82); deck.add(pad);
  const lens = rbox(0.5, 0.02, 0.5, 0.07, padMat); lens.position.set(0, 0.056, 0); pad.add(lens);
  pads.push({ mat: padMat, i });
  // fader slot + cap
  const slot = rbox(0.1, 0.02, 1.2, 0.04, M.slot); slot.position.set(x, TOP + 0.005, 1.55); deck.add(slot);
  const f = rbox(0.28, 0.16, 0.16, 0.03, M.steel); f.position.set(x, TOP + 0.08, 1.35); deck.add(f);
  const fl = rbox(0.2, 0.012, 0.02, 0.004, led(colour, 2)); fl.position.set(0, 0.082, 0); f.add(fl);
  faders.push(f);
});

// ----------------------------------------------------------- scrub ring + KEEP
const ringGroup = new THREE.Group(); ringGroup.position.set(2.15, TOP, 0.75); deck.add(ringGroup);
const well = cyl(0.98, 1.02, 0.06, M.slot, 64); well.position.y = 0.0; ringGroup.add(well);
const wheel = new THREE.Group(); ringGroup.add(wheel);
const rim_ = new THREE.Mesh(new THREE.TorusGeometry(0.82, 0.12, 24, 96), M.knurl); rim_.rotation.x = Math.PI / 2; rim_.position.y = 0.09; rim_.castShadow = true; wheel.add(rim_);
for (let i = 0; i < 48; i++) { // knurling
  const a = (i / 48) * Math.PI * 2;
  const g = rbox(0.025, 0.05, 0.1, 0.008, M.steel); g.position.set(Math.cos(a) * 0.93, 0.115, Math.sin(a) * 0.93); g.rotation.y = -a; wheel.add(g);
}
// the LED ring inside the wheel: the buffer, 15 minutes round
const LEDS = 60; const ringLeds = [];
for (let i = 0; i < LEDS; i++) {
  const a = (i / LEDS) * Math.PI * 2;
  const m = led(0xffb347, 0.1);
  const d = new THREE.Mesh(new THREE.SphereGeometry(0.028, 12, 8), m);
  d.position.set(Math.cos(a) * 0.62, 0.075, Math.sin(a) * 0.62); ringGroup.add(d); ringLeds.push(m);
}
const keepMat = led(0xffa51f, 1.6);
const keepBtn = cyl(0.42, 0.45, 0.12, M.rubber, 48); keepBtn.position.y = 0.09; ringGroup.add(keepBtn);
const keepTop = cyl(0.36, 0.36, 0.03, keepMat, 48); keepTop.position.y = 0.07; keepBtn.add(keepTop);

// ------------------------------------------------------------------ transport
const trans = new THREE.Group(); trans.position.set(-2.55, TOP, -0.35); deck.add(trans);
[['rec', 0xff4d4d], ['play', 0x59d98e], ['stop', 0xdfe3ea]].forEach(([n, c], i) => {
  const b = rbox(0.34, 0.1, 0.34, 0.08, M.rubber); b.position.set(0, 0.05, i * 0.5 - 0.2); trans.add(b);
  const l = new THREE.Mesh(new THREE.SphereGeometry(0.045, 16, 10), led(c, n === 'rec' ? 2.4 : 0.6)); l.position.set(0, 0.105, 0); b.add(l);
});

// ---------------------------------------------------- back panel: jacks, USB-C
const back = new THREE.Group(); back.position.set(0, -0.3, -1.96); deck.add(back);
for (let i = 0; i < 4; i++) { // combo jacks
  const x = -2.3 + i * 0.62;
  const o = cyl(0.17, 0.17, 0.2, M.steel, 36); o.rotation.x = Math.PI / 2; o.position.set(x, 0.0, 0); back.add(o);
  const h = cyl(0.11, 0.11, 0.21, M.slot, 36); h.rotation.x = Math.PI / 2; h.position.set(x, 0.0, 0.01); back.add(h);
}
for (let i = 0; i < 2; i++) { // outputs
  const o = cyl(0.11, 0.11, 0.2, M.steel, 28); o.rotation.x = Math.PI / 2; o.position.set(0.5 + i * 0.4, 0.0, 0); back.add(o);
}
const usb = rbox(0.34, 0.12, 0.22, 0.05, M.steel); usb.position.set(1.95, -0.02, 0.0); back.add(usb);
const usb2 = rbox(0.28, 0.07, 0.24, 0.03, M.slot); usb2.position.set(1.95, -0.02, 0.02); back.add(usb2);
// the Pi's vents, down the right side
for (let i = 0; i < 9; i++) { const v = rbox(0.02, 0.05, 0.9 - 0.02 * Math.abs(i - 4), 0.01, M.slot); v.position.set(3.12, -0.28, 0.2 + (i - 4) * 0.0); v.position.z = -1.2 + i * 0.3; v.scale.set(1, 1, 0.1); deck.add(v); v.rotation.y = 0; v.position.x = 3.08; v.rotation.z = 0; v.scale.set(1, 1, 0.12); }
// headphone knob on the front-left
const phones = cyl(0.13, 0.14, 0.15, M.knurl, 36); phones.position.set(-2.55, TOP + 0.075, 1.25); deck.add(phones);
const pcap = cyl(0.1, 0.1, 0.02, M.steel, 36); pcap.position.y = 0.085; phones.add(pcap);

// ---------------------------------------------------------------- shadow blob
{
  const c = document.createElement('canvas'); c.width = c.height = 256;
  const g = c.getContext('2d'); const gr = g.createRadialGradient(128, 128, 20, 128, 128, 128);
  gr.addColorStop(0, 'rgba(0,0,0,0.65)'); gr.addColorStop(1, 'rgba(0,0,0,0)'); g.fillStyle = gr; g.fillRect(0, 0, 256, 256);
  const t = new THREE.CanvasTexture(c);
  const s = new THREE.Mesh(new THREE.PlaneGeometry(10, 7), new THREE.MeshBasicMaterial({ map: t, transparent: true, depthWrite: false }));
  s.rotation.x = -Math.PI / 2; s.position.y = -0.615; scene.add(s);
}

// ------------------------------------------------------------------- callouts
const callouts = [
  { text: 'Touchscreen: the Hindsight Listening Room, the last 15 minutes always on', obj: screenGroup, off: [0, 0.3, 0] },
  { text: 'Four strips: encoder, pad, fader per track. The pad is the track\'s KEEP / arm', obj: pads[1] && deck, pos: new THREE.Vector3(-0.8, TOP + 0.1, 0.9) },
  { text: 'Scrub ring: the buffer, as a ring. Turn to reach back; press KEEP to save the last pass', obj: ringGroup, off: [0, 0.2, 0] },
  { text: '4 combo inputs + 2 outs, USB-C: a Raspberry Pi and an audio codec inside', obj: back, off: [0, 0.2, 0] },
  { text: 'Transport: record, play, stop', obj: trans, off: [0, 0.2, 0] },
];
const labels = document.getElementById('labels');
callouts.forEach((c) => { const d = document.createElement('div'); d.className = 'lab'; d.textContent = c.text; labels.appendChild(d); c.el = d; });
let showLabels = false;
const v3 = new THREE.Vector3();
function placeLabels() {
  const r = canvasEl.getBoundingClientRect();
  callouts.forEach((c) => {
    c.el.style.display = showLabels ? 'block' : 'none';
    if (!showLabels) return;
    if (c.pos) v3.copy(c.pos).applyMatrix4(deck.matrixWorld); else { c.obj.getWorldPosition(v3); v3.add(new THREE.Vector3(...c.off)); }
    v3.project(camera);
    c.el.style.left = ((v3.x * 0.5 + 0.5) * r.width) + 'px';
    c.el.style.top = ((-v3.y * 0.5 + 0.5) * r.height) + 'px';
  });
}

// --------------------------------------------------------------------- views
const VIEWS = {
  hero: [-5.2, 5.2, 7.0, 0, 0.35, 0.05],
  top: [0, 11.5, 2.4, 0, 0, 0.15],
  front: [0, 1.6, 8.6, 0, 0.2, 0],
  back: [3.8, 2.4, -7.0, 0, 0, 0],
};
let tween = null;
export function view(name) {
  const v = VIEWS[name]; if (!v) return;
  tween = { t: 0, from: [camera.position.clone(), controls.target.clone()], to: [new THREE.Vector3(v[0], v[1], v[2]), new THREE.Vector3(v[3], v[4], v[5])] };
}
function setView(name) { const v = VIEWS[name]; camera.position.set(v[0], v[1], v[2]); controls.target.set(v[3], v[4], v[5]); controls.update(); }
setView('hero');
window.__hindsightDeck = { view, setView, labels: (on) => { showLabels = on; }, spin: (on) => { controls.autoRotate = on; } };

function resize() {
  const w = canvasEl.clientWidth, h = canvasEl.clientHeight;
  if (canvasEl.width !== Math.floor(w * renderer.getPixelRatio()) || canvasEl.height !== Math.floor(h * renderer.getPixelRatio())) {
    renderer.setSize(w, h, false); camera.aspect = w / h; camera.updateProjectionMatrix();
  }
}
const clock = new THREE.Clock();
const still = new URLSearchParams(location.search).has('still');
let frozenT = still ? 7.3 : null;
function frame() {
  const dt = clock.getDelta();
  const t = frozenT ?? clock.elapsedTime;
  resize();
  drawScreen(t);
  // playing: pads breathe, ring LEDs fill like a buffer with a bright head, knobs and faders drift
  pads.forEach((p) => { p.mat.emissiveIntensity = 0.3 + 0.35 * Math.max(0, Math.sin(t * 2.4 + p.i * 1.3)); });
  const fill = 0.82 + 0.02 * Math.sin(t * 0.5);
  ringLeds.forEach((m, i) => {
    const u = i / LEDS; const head = (t * 0.25) % 1;
    const d = (u - head + 1) % 1;
    m.emissiveIntensity = u < fill ? 0.35 + (d < 0.06 ? 3.2 * (1 - d / 0.06) : 0) : 0.05;
  });
  keepMat.emissiveIntensity = 0.75 + 0.35 * Math.sin(t * 2.2);
  knobs.forEach((k, i) => { k.rotation.y = Math.sin(t * 0.6 + i) * 0.8; });
  faders.forEach((f, i) => { f.position.z = 1.28 + 0.26 * (0.5 + 0.5 * Math.sin(t * 0.45 + i * 1.7)); });
  if (tween) {
    tween.t = Math.min(1, tween.t + dt / 0.9); const e = 1 - Math.pow(1 - tween.t, 3);
    camera.position.lerpVectors(tween.from[0], tween.to[0], e); controls.target.lerpVectors(tween.from[1], tween.to[1], e);
    if (tween.t >= 1) tween = null;
  }
  controls.update();
  deck.updateMatrixWorld(true);
  renderer.render(scene, camera);
  placeLabels();
  requestAnimationFrame(frame);
}
frame();
window.__ready = true;
