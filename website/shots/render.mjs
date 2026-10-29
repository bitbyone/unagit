// Draws what shoot.py captured as an SVG of a terminal window: the bytes
// unagit wrote, replayed through xterm.js's emulator, cell by cell with
// their colours. No browser and no screenshot: the picture is the
// terminal's own state, so it stays sharp at any size.
//
//   node render.mjs <raw-dir> <out-dir> [cols rows]
import fs from 'node:fs'
import path from 'node:path'
import xterm from '@xterm/headless'

const [rawDir, outDir, colsArg, rowsArg] = process.argv.slice(2)
const cols = Number(colsArg || 160)
const rows = Number(rowsArg || 44)

const cellW = 8.4
const cellH = 18
const pad = 16
const bar = 30
const font = "'JetBrains Mono', 'SF Mono', Menlo, Consolas, 'DejaVu Sans Mono', monospace"
const defaultFg = '#c9c9d4'
const defaultBg = '#14141a'

// xterm's 256 colours, for a cell given by palette index.
const base16 = ['#000000', '#cd3131', '#0dbc79', '#e5e510', '#2472c8', '#bc3fbc', '#11a8cd', '#e5e5e5',
  '#666666', '#f14c4c', '#23d18b', '#f5f543', '#3b8eea', '#d670d6', '#29b8db', '#ffffff']
function palette(i) {
  if (i < 16) return base16[i]
  if (i < 232) {
    const v = [0, 95, 135, 175, 215, 255]
    const n = i - 16
    return rgb(v[Math.floor(n / 36)], v[Math.floor(n / 6) % 6], v[n % 6])
  }
  const g = 8 + (i - 232) * 10
  return rgb(g, g, g)
}
function rgb(r, g, b) {
  return '#' + [r, g, b].map((c) => c.toString(16).padStart(2, '0')).join('')
}
function colour(cell, fg) {
  const mode = fg ? (cell.isFgRGB() ? 'rgb' : cell.isFgPalette() ? 'pal' : 'def') : (cell.isBgRGB() ? 'rgb' : cell.isBgPalette() ? 'pal' : 'def')
  const v = fg ? cell.getFgColor() : cell.getBgColor()
  if (mode === 'rgb') return rgb((v >> 16) & 255, (v >> 8) & 255, v & 255)
  if (mode === 'pal') return palette(v)
  return null
}
// Lines and corners of boxes are drawn, not set in the font: a font's
// glyph rarely spans the whole cell, and the frames would come out dashed.
// So are the round ends of a pill, which few fonts have.
const boxes = {
  '─': 'h', '━': 'h', '│': 'v', '┃': 'v',
  '╭': 'rd', '┌': 'rd', '╮': 'ld', '┐': 'ld', '╰': 'ru', '└': 'ru', '╯': 'lu', '┘': 'lu',
  '├': 'vr', '┤': 'vl', '┬': 'hd', '┴': 'hu', '┼': 'vh',
}
function drawn(ch, x, y, fg) {
  const cx = x + cellW / 2
  const cy = y + cellH / 2
  const r = x + cellW
  const b = y + cellH
  const line = (d) => `<path d="${d}" stroke="${fg}" stroke-width="1.2" fill="none"/>`
  const round = (d) => `<path d="${d}" stroke="${fg}" stroke-width="1.2" fill="none" stroke-linecap="round"/>`
  const k = boxes[ch]
  const rad = Math.min(cellW, cellH) / 2
  switch (k) {
    case 'h': return line(`M${x} ${cy}H${r}`)
    case 'v': return line(`M${cx} ${y}V${b}`)
    case 'rd': return round(`M${r} ${cy}H${cx + rad}Q${cx} ${cy} ${cx} ${cy + rad}V${b}`)
    case 'ld': return round(`M${x} ${cy}H${cx - rad}Q${cx} ${cy} ${cx} ${cy + rad}V${b}`)
    case 'ru': return round(`M${r} ${cy}H${cx + rad}Q${cx} ${cy} ${cx} ${cy - rad}V${y}`)
    case 'lu': return round(`M${x} ${cy}H${cx - rad}Q${cx} ${cy} ${cx} ${cy - rad}V${y}`)
    case 'vr': return line(`M${cx} ${y}V${b}M${cx} ${cy}H${r}`)
    case 'vl': return line(`M${cx} ${y}V${b}M${cx} ${cy}H${x}`)
    case 'hd': return line(`M${x} ${cy}H${r}M${cx} ${cy}V${b}`)
    case 'hu': return line(`M${x} ${cy}H${r}M${cx} ${cy}V${y}`)
    case 'vh': return line(`M${cx} ${y}V${b}M${x} ${cy}H${r}`)
  }
  if (ch === '\ue0b6') return `<path d="M${r} ${y + 1}A${cellH / 2 - 1} ${cellH / 2 - 1} 0 0 0 ${r} ${b - 1}Z" fill="${fg}"/>`
  if (ch === '\ue0b4') return `<path d="M${x} ${y + 1}A${cellH / 2 - 1} ${cellH / 2 - 1} 0 0 1 ${x} ${b - 1}Z" fill="${fg}"/>`
  return null
}

const esc = (t) => t.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

function render(raw, title) {
  const term = new xterm.Terminal({ cols, rows, allowProposedApi: true })
  return new Promise((resolve) => {
    term.write(raw, () => {
      const buf = term.buffer.active
      const width = cols * cellW + pad * 2
      const height = rows * cellH + pad * 2 + bar
      // The screen's own background is the colour most of its cells have.
      const counts = new Map()
      for (let y = 0; y < rows; y++) {
        const line = buf.getLine(buf.viewportY + y)
        for (let x = 0; x < cols; x++) {
          const c = line.getCell(x)
          const bg = colour(c, false) || defaultBg
          counts.set(bg, (counts.get(bg) || 0) + 1)
        }
      }
      const screenBg = [...counts.entries()].sort((a, b) => b[1] - a[1])[0][0]
      const rects = []
      const texts = []
      for (let y = 0; y < rows; y++) {
        const line = buf.getLine(buf.viewportY + y)
        const top = bar + pad + y * cellH
        let x = 0
        while (x < cols) {
          const c = line.getCell(x)
          const w = c.getWidth() || 1
          let fg = colour(c, true) || defaultFg
          let bg = colour(c, false)
          if (c.isInverse()) {
            const f = fg
            fg = bg || screenBg
            bg = f
          }
          if (bg && bg !== screenBg) {
            rects.push(`<rect x="${(pad + x * cellW).toFixed(1)}" y="${top}" width="${(w * cellW + 0.4).toFixed(1)}" height="${cellH}" fill="${bg}"/>`)
          }
          const ch = c.getChars()
          const shape = ch ? drawn(ch, pad + x * cellW, top, fg) : null
          if (shape) {
            rects.push(shape)
          } else if (ch && ch !== ' ') {
            const attrs = [`x="${(pad + x * cellW).toFixed(1)}"`, `y="${top + 13.5}"`, `fill="${c.isDim() ? fg + '99' : fg}"`]
            if (c.isBold()) attrs.push('font-weight="700"')
            if (c.isItalic()) attrs.push('font-style="italic"')
            if (c.isUnderline()) attrs.push('text-decoration="underline"')
            texts.push(`<text ${attrs.join(' ')}>${esc(ch)}</text>`)
          }
          x += w
        }
      }
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" role="img" aria-label="${esc(title)}">
<rect width="${width}" height="${height}" rx="10" fill="${screenBg}"/>
<rect width="${width}" height="${bar}" rx="10" fill="#1c1c24"/><rect y="${bar - 10}" width="${width}" height="10" fill="#1c1c24"/>
<circle cx="18" cy="15" r="5.5" fill="#ff5f57"/><circle cx="36" cy="15" r="5.5" fill="#febc2e"/><circle cx="54" cy="15" r="5.5" fill="#28c840"/>
<text x="${width / 2}" y="19" fill="#8a8a96" font-family="${font}" font-size="12" text-anchor="middle">${esc(title)}</text>
<g>${rects.join('')}</g>
<g font-family="${font}" font-size="14" xml:space="preserve">${texts.join('')}</g>
</svg>
`
      resolve(svg)
    })
  })
}

fs.mkdirSync(outDir, { recursive: true })
for (const file of fs.readdirSync(rawDir).filter((f) => f.endsWith('.raw')).sort()) {
  const name = path.basename(file, '.raw')
  const svg = await render(fs.readFileSync(path.join(rawDir, file)), name === 'standalone' ? 'unagit -s' : 'unagit')
  fs.writeFileSync(path.join(outDir, name + '.svg'), svg)
  console.log('drew', name)
}
