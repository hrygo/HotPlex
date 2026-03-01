import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'HotPlex',
  description: 'The Strategic Bridge for AI Agent Engineering - Stateful, Secure, and High-Performance.',
  lang: 'en-US',
  base: '/hotplex/',

  head: [
    ['link', { rel: 'icon', href: '/hotplex/favicon.ico' }],
    ['meta', { name: 'theme-color', content: '#00ADD8' }],
    ['meta', { name: 'google', content: 'notranslate' }],
  ],

  themeConfig: {
    logo: '/logo.svg',
    siteTitle: 'HotPlex',

    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Ecosystem', link: '/ecosystem/' },
      { text: 'Reference', link: '/reference/architecture' },
      { text: 'Blog', link: '/blog/' },
      { text: 'GitHub', link: 'https://github.com/hrygo/hotplex' }
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Introduction',
          collapsed: false,
          items: [
            { text: 'Philosophy & Vision', link: '/guide/introduction' },
            { text: 'Quick Start Journey', link: '/guide/getting-started' },
          ]
        },
        {
          text: 'Core Concepts',
          collapsed: false,
          items: [
            { text: 'Architecture of the Bridge', link: '/guide/architecture' },
            { text: 'State & Persistence', link: '/guide/state' },
            { text: 'The Hooks System', link: '/guide/hooks' },
          ]
        },
        {
          text: 'Ecosystem & Integration',
          collapsed: false,
          items: [
            { text: 'Ecosystem Manifesto', link: '/guide/chatapps' },
            { text: 'Slack Mastery Guide', link: '/guide/chatapps-slack' },
            { text: 'Observability & Telemetry', link: '/guide/observability' },
            { text: 'Production Deployment', link: '/guide/deployment' },
          ]
        },
        {
          text: 'SDK Mastery Guides',
          collapsed: false,
          items: [
            { text: 'Go SDK', link: '/sdks/go-sdk' },
            { text: 'Python SDK', link: '/sdks/python-sdk' },
            { text: 'TypeScript SDK', link: '/sdks/typescript-sdk' },
          ]
        }
      ],
      '/reference/': [
        {
          text: 'Technical Soul',
          items: [
            { text: 'The Anatomy of Agency', link: '/reference/architecture' },
            { text: 'API Specification', link: '/reference/api' },
            { text: 'Protocol: The Conversation', link: '/reference/protocol' },
            { text: 'Hooks Specification', link: '/reference/hooks-api' },
          ]
        }
      ],
      '/blog/': [
        {
          text: 'Updates & Engineering',
          items: [
            { text: 'Latest Updates', link: '/blog/' },
            { text: 'Roadmap 2026', link: '/blog/roadmap-2026' },
          ]
        }
      ]
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/hrygo/hotplex' }
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © 2026 HotPlex Team'
    },

    search: {
      provider: 'local'
    },

    editLink: {
      pattern: 'https://github.com/hrygo/hotplex/edit/main/docs-site/:path',
      text: 'Edit this page on GitHub'
    },

    lastUpdated: {
      text: 'Last updated',
      formatOptions: {
        dateStyle: 'medium',
        timeStyle: 'short'
      }
    }
  }
})
