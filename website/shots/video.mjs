// Renders the recorded walk (record.py's asciicast) as an MP4: the cast
// replayed through xterm.js's emulator, each frame drawn as the
// screenshots are, its scene's caption above the terminal, and the frames
// piped to ffmpeg. The page plays the cast itself; the MP4 is for where a
// player cannot go - a README, a post, a message.
//
//   node video.mjs <intro.cast> <out.mp4> [fps]
import fs from 'node:fs'
import { spawn } from 'node:child_process'
import xterm from '@xterm/headless'
import { Resvg } from '@resvg/resvg-js'
import ffmpeg from 'ffmpeg-static'
import { svgOf } from './term-svg.mjs'

const [castPath, out, fpsArg] = process.argv.slice(2)
const fps = Number(fpsArg || 25)
const lines = fs.readFileSync(castPath, 'utf8').trim().split('\n')
const header = JSON.parse(lines[0])
const events = lines.slice(1).map((l) => JSON.parse(l))
const end = events[events.length - 1][0] + 1.5

// unagit mono first (fonts.py), DejaVu for what it lacks.
const own = new URL('./fonts/', import.meta.url).pathname
const dejavu = new URL('./node_modules/dejavu-fonts-ttf/ttf/', import.meta.url).pathname
const fontFiles = [...['unagit-mono-regular.ttf', 'unagit-mono-bold.ttf'].map((f) => own + f),
  ...['DejaVuSansMono.ttf', 'DejaVuSansMono-Bold.ttf', 'DejaVuSans.ttf'].map((f) => dejavu + f)]

const term = new xterm.Terminal({ cols: header.width, rows: header.height, allowProposedApi: true })
const write = (data) => new Promise((resolve) => term.write(data, resolve))

const enc = spawn(ffmpeg, ['-y', '-loglevel', 'error', '-f', 'image2pipe', '-framerate', String(fps), '-i', '-',
  '-vf', 'scale=1920:-2:flags=lanczos', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-preset', 'slow', '-crf', '20',
  '-movflags', '+faststart', out], { stdio: ['pipe', 'inherit', 'inherit'] })

let at = 0
let caption = 'unagit - the universal navigator around git'
let last = null
let lastPng = null
for (let frame = 0; frame * (1 / fps) < end; frame++) {
  const t = frame / fps
  let data = ''
  while (at < events.length && events[at][0] <= t) {
    const [, kind, text] = events[at]
    if (kind === 'o') data += text
    if (kind === 'm') caption = text
    at++
  }
  if (data) await write(data)
  const svg = svgOf(term, 'unagit', caption)
  // An unchanged screen is the same picture: draw it once.
  if (svg !== last) {
    lastPng = new Resvg(svg, { font: { fontFiles, loadSystemFonts: false, defaultFontFamily: 'unagit mono' }, fitTo: { mode: 'width', value: 1920 } }).render().asPng()
    last = svg
  }
  if (!enc.stdin.write(lastPng)) await new Promise((r) => enc.stdin.once('drain', r))
  if (frame % (fps * 5) === 0) process.stdout.write(`${Math.round(t)}s `)
}
enc.stdin.end()
await new Promise((r) => enc.on('close', r))
console.log('\nwrote', out)
