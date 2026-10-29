// Squeezes the recorded walk (record.py's asciicast) down to what changed
// on the screen. unagit's tcell sends every cell again on every frame -
// tview clears the screen before each draw, and tcell takes a cell set
// again after a clear as a changed one - so a turning pipeline mark, ten
// times a second, was the whole screen each time. Replayed through
// xterm.js's emulator, each moment of the walk is kept as the cells that
// differ from the moment before, with the captions as they came.
//
//   node squeeze.mjs <in.cast> <out.cast>
import fs from 'node:fs'
import xterm from '@xterm/headless'

const [inPath, outPath] = process.argv.slice(2)
const lines = fs.readFileSync(inPath, 'utf8').trim().split('\n')
const header = JSON.parse(lines[0])
const events = lines.slice(1).map((l) => JSON.parse(l))

const term = new xterm.Terminal({ cols: header.width, rows: header.height, allowProposedApi: true })
const write = (data) => new Promise((resolve) => term.write(data, resolve))

// sgr is the escape that sets a cell's look.
function sgr(c) {
  const p = ['0']
  if (c.isBold()) p.push('1')
  if (c.isDim()) p.push('2')
  if (c.isItalic()) p.push('3')
  if (c.isUnderline()) p.push('4')
  if (c.isInverse()) p.push('7')
  if (c.isInvisible()) p.push('8')
  if (c.isStrikethrough()) p.push('9')
  const colour = (rgb, pal, v, base) => {
    if (rgb) return `${base};2;${(v >> 16) & 255};${(v >> 8) & 255};${v & 255}`
    if (pal) return `${base};5;${v}`
    return null
  }
  const fg = colour(c.isFgRGB(), c.isFgPalette(), c.getFgColor(), 38)
  const bg = colour(c.isBgRGB(), c.isBgPalette(), c.getBgColor(), 48)
  if (fg) p.push(fg)
  if (bg) p.push(bg)
  return `\x1b[${p.join(';')}m`
}

// screen is every cell as its look and its text; a wide character's
// second cell is ''.
function screen() {
  const buf = term.buffer.active
  const rows = []
  for (let y = 0; y < term.rows; y++) {
    const line = buf.getLine(buf.viewportY + y)
    const row = []
    for (let x = 0; x < term.cols; x++) {
      const c = line.getCell(x)
      row.push(c.getWidth() === 0 ? ['', ''] : [sgr(c), c.getChars() || ' '])
    }
    rows.push(row)
  }
  return rows
}

// diff is what draws next over last: each run of changed cells, from where
// it starts.
function diff(last, next) {
  let out = ''
  let look = ''
  for (let y = 0; y < next.length; y++) {
    let x = 0
    while (x < next[y].length) {
      const same = last && last[y][x][0] === next[y][x][0] && last[y][x][1] === next[y][x][1]
      if (same || next[y][x][1] === '') {
        x++
        continue
      }
      out += `\x1b[${y + 1};${x + 1}H`
      while (x < next[y].length && !(last && last[y][x][0] === next[y][x][0] && last[y][x][1] === next[y][x][1])) {
        const [s, ch] = next[y][x]
        if (ch !== '') {
          if (s !== look) out += s
          look = s
          out += ch
        }
        x++
      }
    }
  }
  return out
}

const out = [JSON.stringify(header)]
let last = null
// Output that comes within a moment is one change.
const moment = 0.02
for (let i = 0; i < events.length;) {
  const at = events[i][0]
  let data = ''
  const marks = []
  while (i < events.length && events[i][0] - at < moment) {
    if (events[i][1] === 'o') data += events[i][2]
    else marks.push(events[i])
    i++
  }
  for (const m of marks) out.push(JSON.stringify([m[0], m[1], m[2]]))
  if (!data && !last) continue
  await write(data)
  const next = screen()
  const change = diff(last, next)
  // The first moment also hides the cursor, as unagit does.
  const prefix = last ? '' : '\x1b[?25l\x1b[2J'
  if (change || i === events.length) out.push(JSON.stringify([at, 'o', prefix + change]))
  last = next
}
const [head, ...rest] = out
rest.sort((a, b) => JSON.parse(a)[0] - JSON.parse(b)[0])
fs.writeFileSync(outPath, [head, ...rest].join('\n') + '\n')
const size = (p) => (fs.statSync(p).size / 1e6).toFixed(1)
console.log(`squeezed ${inPath} (${size(inPath)} MB) into ${outPath} (${size(outPath)} MB)`)
