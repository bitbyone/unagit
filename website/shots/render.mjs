// Draws what shoot.py captured as SVG screenshots: the bytes unagit wrote,
// replayed through xterm.js's emulator.
//
//   node render.mjs <raw-dir> <out-dir> [cols rows]
import fs from 'node:fs'
import path from 'node:path'
import xterm from '@xterm/headless'
import subsetFont from 'subset-font'
import { svgOf } from './term-svg.mjs'

const [rawDir, outDir, colsArg, rowsArg] = process.argv.slice(2)
const cols = Number(colsArg || 160)
const rows = Number(rowsArg || 44)

// The screenshots are pictures on the page, which load no fonts of the
// page's: each carries unagit mono, cut down to the characters it shows.
const fonts = { 400: 'unagit-mono-regular.ttf', 700: 'unagit-mono-bold.ttf' }
async function embedFont(svg) {
  const text = [...new Set(svg.replace(/<[^>]*>/g, ''))].join('')
  let faces = ''
  for (const [weight, file] of Object.entries(fonts)) {
    const woff2 = await subsetFont(fs.readFileSync(new URL('./fonts/' + file, import.meta.url)), text, { targetFormat: 'woff2' })
    faces += `@font-face{font-family:'unagit mono';font-weight:${weight};src:url(data:font/woff2;base64,${woff2.toString('base64')}) format('woff2')}`
  }
  return svg.replace(/(<svg[^>]*>)/, `$1\n<style>${faces}</style>`)
}

function render(raw, title) {
  const term = new xterm.Terminal({ cols, rows, allowProposedApi: true })
  return new Promise((resolve) => term.write(raw, () => resolve(svgOf(term, title))))
}

fs.mkdirSync(outDir, { recursive: true })
for (const file of fs.readdirSync(rawDir).filter((f) => f.endsWith('.raw')).sort()) {
  const name = path.basename(file, '.raw')
  const svg = await embedFont(await render(fs.readFileSync(path.join(rawDir, file)), name === 'standalone' ? 'unagit -s' : 'unagit'))
  fs.writeFileSync(path.join(outDir, name + '.svg'), svg)
  console.log('drew', name)
}
