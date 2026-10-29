import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'

import HomePage from './components/HomePage.vue'
import IntroPlayer from './components/IntroPlayer.vue'
import './style.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('HomePage', HomePage)
    app.component('IntroPlayer', IntroPlayer)
  },
} satisfies Theme
