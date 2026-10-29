// Draws what shoot.py captured as SVG screenshots: the bytes unagit wrote,
// replayed through xterm.js's emulator.
//
//   node render.mjs <raw-dir> <out-dir> [cols rows]
import fs from 'node:fs'
import path from 'node:path'
import xterm from '@xterm/headless'
import { svgOf } from './term-svg.mjs'

const [rawDir, outDir, colsArg, rowsArg] = process.argv.slice(2)
const cols = Number(colsArg || 160)
const rows = Number(rowsArg || 44)

function render(raw, title) {
  const term = new xterm.Terminal({ cols, rows, allowProposedApi: true })
  return new Promise((resolve) => term.write(raw, () => resolve(svgOf(term, title))))
}

fs.mkdirSync(outDir, { recursive: true })
for (const file of fs.readdirSync(rawDir).filter((f) => f.endsWith('.raw')).sort()) {
  const name = path.basename(file, '.raw')
  const svg = await render(fs.readFileSync(path.join(rawDir, file)), name === 'standalone' ? 'unagit -s' : 'unagit')
  fs.writeFileSync(path.join(outDir, name + '.svg'), svg)
  console.log('drew', name)
}
