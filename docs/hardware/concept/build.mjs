// Bundles scene.js and three.js into one self-contained HTML file.
import { build } from 'esbuild';
import { readFileSync, writeFileSync } from 'node:fs';
const out = await build({ entryPoints: ['src/scene.js'], bundle: true, minify: true, format: 'iife', write: false, target: 'es2020' });
const js = out.outputFiles[0].text.replace(/<\/script/gi, '<\\/script');
const html = readFileSync('src/page.html', 'utf8').replace('/*SCENE*/', () => js);
writeFileSync(process.argv[2] || 'deck.html', html);
console.log('wrote', (html.length / 1024).toFixed(0), 'KB');
