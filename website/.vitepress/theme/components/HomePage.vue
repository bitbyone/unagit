<script setup lang="ts">
import { ref } from 'vue'
import { withBase } from 'vitepress'

const installs = {
  Homebrew: 'brew tap bitbyone/unagit https://github.com/bitbyone/unagit\nbrew install unagit',
  'From source': 'git clone https://github.com/bitbyone/unagit\ncd unagit && make install',
}
type Way = keyof typeof installs
const way = ref<Way>('Homebrew')
const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(installs[way.value])
    copied.value = true
    setTimeout(() => (copied.value = false), 1600)
  } catch {
    // A browser that refuses the clipboard leaves the command to be selected by hand.
  }
}

// The intro video, once there is one: a file under public/, or an
// address. Until then the hero shows a screenshot in its place.
const introVideo = ''

// Real screenshots of unagit over the made-up world of website/shots
// (make shots), taken in a terminal and drawn as SVG.
const shots = [
  {
    src: '/shots/merge-requests.svg',
    title: 'Every merge request, with what it needs from you',
    text: 'GitLab and GitHub in one list: the pipeline, approvals, threads still open, what is new since your last look. Enter opens the detail; Ctrl-R a review worktree where the whole change is pending edits.',
    link: '/guide/review',
    more: 'The review worktree',
  },
  {
    src: '/shots/sandboxes.svg',
    title: 'Agents in sandboxes, from any row',
    text: 'With unibox, Sandbox with Claude Code… on a repository, a worktree or a merge request starts the agent in a Docker Sandbox. The Sandboxes tab shows what runs where; the context sits on the tab bar.',
    link: '/guide/sandboxes',
    more: 'Sandboxes',
  },
  {
    src: '/shots/standalone.svg',
    title: 'Any repository, no setup: unagit -s',
    text: 'The changes, the log, the branches and the shelf of the repository you are in, with your own git credentials. No server, no passphrase, nothing written.',
    link: '/guide/standalone',
    more: 'One repository alone',
  },
]

const features = [
  {
    title: 'One list, two forges',
    text: 'Any number of GitLab servers and GitHub side by side. A pull request is a merge request here too.',
    link: '/guide/navigating',
  },
  {
    title: 'Reviews as pending edits',
    text: 'The whole merge request as one unstaged change. Your editor\'s gutter and git diff just work.',
    link: '/guide/review',
  },
  {
    title: 'Worktrees, not clones',
    text: 'One clone per repository, a worktree per merge request: three reviews cost three checkouts.',
    link: '/guide/layout',
  },
  {
    title: 'Grouped worktrees',
    text: 'Several repositories on one branch in one folder, opened as one, their merge requests linked.',
    link: '/guide/grouped-worktrees',
  },
  {
    title: 'Agents in sandboxes',
    text: 'Claude Code, Codex or Copilot on any repository or merge request, in a Docker Sandbox through unibox.',
    link: '/guide/sandboxes',
  },
  {
    title: 'One repository alone',
    text: 'unagit -s is a git interface for any checkout: changes, log, branches, shelf. Nothing to set up.',
    link: '/guide/standalone',
  },
  {
    title: 'Watching',
    text: 'Follow a pipeline, a merge request or a branch in the background, and hear when it moves.',
    link: '/guide/watching',
  },
  {
    title: 'Where you already work',
    text: 'Neovim, IntelliJ, VS Code, Zed; Zellij, herdr, Ghostty; Claude Code, Codex and other agents.',
    link: '/guide/integrations',
  },
  {
    title: 'Every rewrite undoable',
    text: 'Squashes, rebases, deleted branches: each is written down, and Rewrite History takes it back.',
    link: '/guide/rewrites',
  },
  {
    title: 'Tokens sealed',
    text: 'Argon2id and AES-256-GCM, a passphrase at every start, and git never sees the token on disk.',
    link: '/reference/security',
  },
  {
    title: 'Twenty-nine themes',
    text: 'From the muted default to gruvbox and catppuccin, Nerd Font icons included, or one of your own.',
    link: '/guide/themes',
  },
  {
    title: 'Try it on made-up data',
    text: 'make demo runs the real unagit over a world of its own: servers, merge requests, sandboxes.',
    link: '/reference/developing',
  },
]
</script>

<template>
  <div class="home">
    <section class="hero">
      <p class="kicker"><b>U</b>niversal <b>Nav</b>igator <b>A</b>round <b>GIT</b></p>
      <h1>Every repository you work with.<br /><span>One keystroke to be inside it.</span></h1>
      <p class="lead">
        A terminal navigator over every repository on GitLab and GitHub: where each lands on
        disk, getting into it, and merge requests reviewed as pending edits in your own editor.
      </p>

      <div class="install">
        <div class="install-tabs" role="tablist">
          <button
            v-for="(_, name) in installs"
            :key="name"
            role="tab"
            :class="{ on: way === name }"
            :aria-selected="way === name"
            @click="way = name as Way"
          >{{ name }}</button>
          <button class="copy" :aria-label="copied ? 'Copied' : 'Copy the commands'" @click="copy">
            {{ copied ? 'Copied' : 'Copy' }}
          </button>
        </div>
        <pre><code><span v-for="line in installs[way].split('\n')" :key="line" class="line"><span class="prompt">$</span> {{ line }}</span></code></pre>
      </div>

      <div class="actions">
        <a class="btn brand" :href="withBase('/guide/')">Get Started</a>
        <a class="btn alt" :href="withBase('/tutorials/first-review')">Your First Review</a>
        <a class="btn alt" href="https://github.com/bitbyone/unagit">GitHub</a>
      </div>
    </section>

    <section v-if="introVideo" class="screen" aria-label="unagit in a terminal">
      <video class="intro" :src="introVideo.startsWith('/') ? withBase(introVideo) : introVideo" controls playsinline preload="metadata" />
    </section>

    <section v-for="(shot, i) in shots" :key="shot.src" class="showcase" :class="{ flip: i % 2 === 1 }">
      <div class="copy-col">
        <h2>{{ shot.title }}</h2>
        <p>{{ shot.text }}</p>
        <a :href="withBase(shot.link)">{{ shot.more }} →</a>
      </div>
      <img class="shot" :src="withBase(shot.src)" :alt="shot.title" loading="lazy" />
    </section>

    <section class="arrangement">
      <div class="copy-col">
        <h2>The review is one unstaged change</h2>
        <p>
          Editors draw their gutter by comparing the file with git's index. So unagit puts the
          index and HEAD on the merge base and the files on the merge request's head: gitsigns,
          gitgutter, <code>]c</code>, <code>:DiffviewOpen</code> and plain <code>git diff</code>
          all show the merge request as a whole, with nothing to configure.
        </p>
        <a :href="withBase('/guide/review')">How the review worktree is built →</a>
      </div>
      <pre class="formula"><span>HEAD</span>         = merge base
<span>index</span>        = merge base
<span>working tree</span> = merge request head

<span class="dim">$</span> git diff     <span class="dim"># the entire merge request</span>
<span class="dim">$</span> git status   <span class="dim"># every file it touches</span></pre>
    </section>

    <section class="features">
      <h2>Everything the job asks for</h2>
      <div class="grid">
        <a v-for="f in features" :key="f.title" class="card" :href="withBase(f.link)">
          <h3>{{ f.title }}</h3>
          <p>{{ f.text }}</p>
        </a>
      </div>
    </section>

    <section class="closing">
      <h2>Start in Settings, then never edit a file</h2>
      <p>Add a server, paste a token, pick your groups and say where they go. That is the whole setup.</p>
      <a class="btn brand" :href="withBase('/guide/setup')">First Setup</a>
    </section>
  </div>
</template>

<style scoped>
.home {
  max-width: 1440px;
  margin: 0 auto;
}
section {
  padding: 72px 24px;
  border-bottom: 1px solid var(--ug-frame);
}
@media (min-width: 768px) {
  section { padding: 96px 64px; }
}

.hero {
  text-align: center;
  padding-top: 96px;
}
.kicker {
  display: inline-block;
  font-size: 13px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--vp-c-text-2);
  border: 1px solid var(--ug-frame);
  border-radius: 999px;
  padding: 4px 14px;
  margin-bottom: 28px;
}
.kicker b { color: var(--ug-gold); font-weight: 600; }
.hero h1 {
  font-size: clamp(36px, 6vw, 64px);
  line-height: 1.08;
  letter-spacing: -0.035em;
  font-weight: 600;
  color: var(--vp-c-text-1);
}
.hero h1 span { color: var(--ug-brand); }
.lead {
  max-width: 680px;
  margin: 24px auto 0;
  font-size: 18px;
  line-height: 1.7;
  color: var(--vp-c-text-2);
}

.install {
  max-width: 600px;
  margin: 40px auto 0;
  text-align: left;
  border: 1px solid var(--ug-frame);
  border-radius: 12px;
  overflow: hidden;
  background: var(--ug-code-bg);
}
.install-tabs {
  display: flex;
  gap: 4px;
  padding: 6px;
  border-bottom: 1px solid var(--ug-frame);
  background: var(--vp-c-bg);
}
.install-tabs button {
  font-size: 13px;
  padding: 4px 12px;
  border-radius: 6px;
  color: var(--vp-c-text-2);
}
.install-tabs button.on {
  background: var(--ug-brand-soft);
  color: var(--ug-brand);
}
.install pre {
  margin: 0;
  padding: 18px 20px;
  overflow-x: auto;
  font-family: var(--vp-font-family-mono);
  font-size: 14px;
  line-height: 1.9;
}
.install .line { display: block; white-space: pre; }
.install .prompt { color: var(--vp-c-text-3); user-select: none; }
.copy {
  margin-left: auto;
  font-size: 12px;
  padding: 4px 10px;
  border: 1px solid var(--ug-frame);
  border-radius: 6px;
  background: var(--vp-c-bg);
  color: var(--vp-c-text-2);
}
.copy:hover { color: var(--ug-brand); }

.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
  margin-top: 32px;
}
.btn {
  display: inline-block;
  padding: 10px 22px;
  border-radius: 999px;
  font-size: 15px;
  font-weight: 500;
  border: 1px solid transparent;
  transition: background-color 0.2s, border-color 0.2s;
}
.btn.brand { background: var(--ug-brand); color: #fff; }
.dark .btn.brand { color: #0c0c10; }
.btn.brand:hover { background: var(--ug-brand-hover); }
.btn.alt { border-color: var(--ug-frame); color: var(--vp-c-text-1); }
.btn.alt:hover { border-color: var(--vp-c-text-3); }

.screen { display: flex; justify-content: center; background: var(--ug-code-bg); }
.shot {
  display: block;
  width: 100%;
  max-width: 1100px;
  height: auto;
  border-radius: 12px;
  box-shadow: 0 24px 60px -24px rgba(20, 20, 40, 0.35);
}
.intro {
  width: 100%;
  max-width: 1100px;
  border-radius: 12px;
  background: #14141a;
}
.showcase {
  display: grid;
  gap: 32px;
  align-items: center;
}
@media (min-width: 1100px) {
  .showcase { grid-template-columns: 2fr 3fr; gap: 56px; }
  .showcase.flip .copy-col { order: 2; }
}
.showcase p { margin: 16px 0; font-size: 17px; line-height: 1.75; color: var(--vp-c-text-2); }
.showcase a { color: var(--ug-brand); font-weight: 500; }

.arrangement {
  display: grid;
  gap: 40px;
  align-items: center;
}
@media (min-width: 960px) {
  .arrangement { grid-template-columns: 1fr 1fr; gap: 64px; }
}
h2 {
  font-size: clamp(26px, 3.4vw, 36px);
  line-height: 1.2;
  letter-spacing: -0.025em;
  font-weight: 600;
  color: var(--vp-c-text-1);
}
.arrangement p { margin: 16px 0; font-size: 17px; line-height: 1.75; color: var(--vp-c-text-2); }
.arrangement code {
  font-size: 0.88em;
  padding: 2px 6px;
  border-radius: 5px;
  background: var(--ug-brand-soft);
  color: var(--ug-brand);
}
.arrangement a { color: var(--ug-brand); font-weight: 500; }
.formula {
  margin: 0;
  padding: 28px;
  border-radius: 12px;
  background: var(--ug-code-bg);
  border: 1px solid var(--ug-frame);
  font-family: var(--vp-font-family-mono);
  font-size: 14px;
  line-height: 1.9;
  overflow-x: auto;
  color: var(--vp-c-text-1);
}
.formula span { color: var(--ug-brand); }
.formula .dim { color: var(--vp-c-text-3); }

.features h2 { text-align: center; margin-bottom: 48px; }
.grid {
  display: grid;
  grid-template-columns: 1fr;
  border: 1px solid var(--ug-frame);
  border-radius: 14px;
  overflow: hidden;
}
@media (min-width: 640px) { .grid { grid-template-columns: repeat(2, 1fr); } }
@media (min-width: 1024px) { .grid { grid-template-columns: repeat(3, 1fr); } }
.card {
  padding: 28px;
  margin: 0 -1px -1px 0;
  border-right: 1px solid var(--ug-frame);
  border-bottom: 1px solid var(--ug-frame);
  transition: background-color 0.2s;
}
.card:hover { background: var(--ug-code-bg); }
.card h3 { font-size: 17px; font-weight: 600; color: var(--vp-c-text-1); }
.card p { margin-top: 8px; font-size: 15px; line-height: 1.65; color: var(--vp-c-text-2); }

.closing { text-align: center; border-bottom: none; }
.closing p { margin: 16px auto 28px; max-width: 560px; font-size: 17px; color: var(--vp-c-text-2); }
</style>
