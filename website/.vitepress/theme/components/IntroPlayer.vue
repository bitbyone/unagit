<script setup lang="ts">
// The landing page's walk through unagit: the asciicast record.py made of
// the real unagit over the demo world, played by asciinema-player in a
// loop, with each scene's caption - the cast's markers - above it. The
// player is loaded in the browser alone: it needs a DOM, which the build's
// rendering of the page has not.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { withBase } from 'vitepress'

const props = defineProps<{ src: string }>()
const host = ref<HTMLElement>()
const opening = 'unagit - the universal navigator around git'
const caption = ref(opening)
let player: { getCurrentTime(): Promise<number> | number; dispose(): void } | undefined
let timer: ReturnType<typeof setInterval> | undefined

onMounted(async () => {
  const [{ create }, text] = await Promise.all([
    import('asciinema-player'),
    fetch(withBase(props.src)).then((r) => r.text()),
    import('asciinema-player/dist/bundle/asciinema-player.css'),
  ])
  const markers: [number, string][] = text
    .split('\n')
    .slice(1)
    .filter(Boolean)
    .map((l) => JSON.parse(l))
    .filter((e) => e[1] === 'm')
    .map((e) => [e[0], e[2]])
  if (!host.value) return
  player = create({ data: text }, host.value, {
    autoPlay: true,
    loop: true,
    controls: false,
    fit: 'width',
    theme: 'retro-block',
    terminalFontFamily: "'JetBrains Mono', 'SF Mono', Menlo, Consolas, 'DejaVu Sans Mono', monospace",
  })
  timer = setInterval(async () => {
    const t = await player!.getCurrentTime()
    let now = opening
    for (const [at, text] of markers) if (at <= t) now = text
    caption.value = now
  }, 200)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  player?.dispose()
})
</script>

<template>
  <div class="intro-window">
    <div class="bar"><i /><i /><i /><span>unagit</span></div>
    <p class="caption" aria-live="polite">{{ caption }}</p>
    <div ref="host" class="player" aria-label="A walk through unagit in a terminal" />
  </div>
</template>

<style scoped>
.intro-window {
  width: 100%;
  max-width: 1100px;
  border-radius: 12px;
  overflow: hidden;
  background: #282527;
  box-shadow: 0 24px 60px -24px rgba(29, 27, 28, 0.45);
  border: 1px solid #3a3537;
}
.bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  background: #1c1a1b;
  font-size: 12px;
  color: #8a8078;
}
.bar i { width: 11px; height: 11px; border-radius: 50%; }
.bar i:nth-child(1) { background: #ff5f57; }
.bar i:nth-child(2) { background: #febc2e; }
.bar i:nth-child(3) { background: #28c840; }
.bar span { flex: 1; text-align: center; margin-right: 50px; }
.caption {
  margin: 0;
  padding: 10px 18px;
  background: #222021;
  color: #c2985d;
  font-family: var(--vp-font-family-mono);
  font-weight: 700;
  font-size: 15px;
  min-height: 1.6em;
}
.player { min-height: 200px; }
.player :deep(.ap-wrapper) { background: #282527; }
.player :deep(.ap-player) { border-radius: 0; }
</style>

<style>
/* retro-block's palette for what the terminal draws in its own colours: the
   shell, and anything unagit leaves at the terminal's defaults. */
.asciinema-player-theme-retro-block {
  --term-color-foreground: #cbcbcb;
  --term-color-background: #282527;
  --term-color-0: #1d1b1c;
  --term-color-1: #e37575;
  --term-color-2: #848867;
  --term-color-3: #c2985d;
  --term-color-4: #8fb3c4;
  --term-color-5: #b48ead;
  --term-color-6: #a5d0c0;
  --term-color-7: #cbcbcb;
  --term-color-8: #7a716a;
  --term-color-9: #e37575;
  --term-color-10: #a0a37f;
  --term-color-11: #d4ab6e;
  --term-color-12: #a6c6d4;
  --term-color-13: #c9a3c2;
  --term-color-14: #bfe0d3;
  --term-color-15: #eaeaea;
}
</style>
