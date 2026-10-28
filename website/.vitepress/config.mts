import { defineConfig } from 'vitepress'

// The site is served by GitHub Pages from the repository's own address,
// https://bitbyone.github.io/unagit/, so every link is under /unagit/.
const base = '/unagit/'
const repo = 'https://github.com/bitbyone/unagit'

const guideSidebar = [
  {
    text: 'Introduction',
    items: [
      { text: 'Getting Started', link: '/guide/' },
      { text: 'Why unagit', link: '/guide/why' },
      { text: 'First Setup', link: '/guide/setup' },
    ],
  },
  {
    text: 'Navigating',
    items: [
      { text: 'The Lists', link: '/guide/navigating' },
      { text: 'Actions and Keys', link: '/guide/actions' },
      { text: 'Where Things Land', link: '/guide/layout' },
      {
        text: 'Repositories',
        link: '/guide/repositories',
        items: [
          { text: 'Branches', link: '/guide/branches' },
          { text: 'Changes and Commits', link: '/guide/changes' },
          { text: 'Pipelines', link: '/guide/pipelines' },
        ],
      },
      { text: 'Following into a Shell', link: '/guide/shell' },
      { text: 'One Repository Alone', link: '/guide/standalone' },
    ],
  },
  {
    text: 'Reviewing',
    items: [
      { text: 'The Review Worktree', link: '/guide/review' },
      { text: 'The Commit Log', link: '/guide/commit-log' },
      {
        text: 'Rewriting History',
        link: '/guide/rewrites',
        items: [{ text: 'The Shelf', link: '/guide/shelf' }],
      },
      { text: 'Finishing a Review', link: '/guide/finishing' },
    ],
  },
  {
    text: 'Worktrees',
    items: [
      { text: 'Grouped Worktrees', link: '/guide/grouped-worktrees' },
      { text: 'Keeping Worktrees Current', link: '/guide/keeping-current' },
    ],
  },
  {
    text: 'In the Background',
    items: [{ text: 'Watching and Activity', link: '/guide/watching' }],
  },
  {
    text: 'Integrations',
    items: [
      { text: 'Overview', link: '/guide/integrations' },
      { text: 'Editors', link: '/guide/editors' },
      { text: 'Coding Agents', link: '/guide/agents' },
      { text: 'Sandboxes', link: '/guide/sandboxes' },
      { text: 'Zellij, herdr, Ghostty', link: '/guide/multiplexers' },
      { text: 'Incomm and Hunk', link: '/guide/incomm-hunk' },
      { text: 'chezmoi', link: '/guide/chezmoi' },
      { text: 'Yazi and zoxide', link: '/guide/yazi-zoxide' },
    ],
  },
  {
    text: 'Customising',
    items: [{ text: 'Themes', link: '/guide/themes' }],
  },
]

const tutorialSidebar = [
  {
    text: 'Tutorials',
    items: [
      { text: 'Overview', link: '/tutorials/' },
      { text: 'Your First Review', link: '/tutorials/first-review' },
      { text: 'A Change Across Repositories', link: '/tutorials/cross-repo-change' },
      { text: 'Waiting for a Pipeline', link: '/tutorials/watch-a-pipeline' },
      { text: 'Hand a Review to an Agent', link: '/tutorials/agent-review' },
      { text: 'Make Your Own Theme', link: '/tutorials/own-theme' },
    ],
  },
]

const referenceSidebar = [
  {
    text: 'Reference',
    items: [
      { text: 'Keys', link: '/reference/keys' },
      { text: 'Command Line', link: '/reference/cli' },
      { text: 'Files on Disk', link: '/reference/files' },
      { text: 'Security', link: '/reference/security' },
      { text: 'Developing unagit', link: '/reference/developing' },
    ],
  },
]

export default defineConfig({
  base,
  title: 'unagit',
  titleTemplate: ':title | unagit',
  description: 'The universal navigator around git: every repository you work with, and merge requests reviewed as pending edits.',
  cleanUrls: true,
  lastUpdated: true,
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: `${base}favicon.svg` }],
    ['meta', { name: 'theme-color', content: '#4f30e8' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'unagit' }],
  ],
  markdown: {
    theme: { light: 'github-light', dark: 'github-dark-dimmed' },
  },
  themeConfig: {
    logo: { light: '/logo-light.svg', dark: '/logo-dark.svg', alt: 'unagit' },
    siteTitle: false,
    nav: [
      { text: 'Guide', link: '/guide/', activeMatch: '^/guide/' },
      { text: 'Tutorials', link: '/tutorials/', activeMatch: '^/tutorials/' },
      { text: 'Reference', link: '/reference/keys', activeMatch: '^/reference/' },
      {
        text: 'Resources',
        items: [
          { text: 'GitHub', link: repo },
          { text: 'Releases', link: `${repo}/releases` },
          { text: 'Issues', link: `${repo}/issues` },
          { text: 'Incomm', link: 'https://github.com/bitbyone/incomm' },
        ],
      },
    ],
    sidebar: {
      '/guide/': guideSidebar,
      '/tutorials/': tutorialSidebar,
      '/reference/': referenceSidebar,
    },
    socialLinks: [{ icon: 'github', link: repo }],
    outline: { level: [2, 3], label: 'On this page' },
    search: { provider: 'local' },
    editLink: {
      pattern: `${repo}/edit/main/website/:path`,
      text: 'Suggest changes to this page',
    },
    footer: {
      message: 'Released under the MIT License.',
      copyright: `© ${new Date().getFullYear()} unagit contributors`,
    },
  },
})
