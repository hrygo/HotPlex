import { defineConfig } from 'vitepress'

const guideSidebar = [
  {
    text: 'Getting Started',
    collapsed: false,
    items: [
      { text: 'Quick Start', link: '/guide/getting-started' },
      { text: 'Philosophy', link: '/guide/introduction' },
    ]
  },
  {
    text: 'Core Concepts',
    collapsed: false,
    items: [
      { text: 'Architecture', link: '/guide/architecture' },
      { text: 'State Management', link: '/guide/state' },
      { text: 'Hooks System', link: '/guide/hooks' },
    ]
  },
  {
    text: 'Security',
    collapsed: false,
    items: [
      { text: 'Security Overview', link: '/guide/security' },
    ]
  },
  {
    text: 'Integration',
    collapsed: false,
    items: [
      { text: 'ChatApps Overview', link: '/guide/chatapps' },
      { text: 'Slack Integration', link: '/guide/chatapps-slack' },
    ]
  },
  {
    text: 'Operations',
    collapsed: false,
    items: [
      { text: 'Observability', link: '/guide/observability' },
      { text: 'Deployment', link: '/guide/deployment' },
      { text: 'Troubleshooting', link: '/guide/troubleshooting' },
    ]
  },
  {
    text: 'SDKs',
    collapsed: false,
    items: [
      { text: 'Go SDK', link: '/sdks/go-sdk' },
      { text: 'Python SDK', link: '/sdks/python-sdk' },
      { text: 'TypeScript SDK', link: '/sdks/typescript-sdk' },
    ]
  }
]

export default defineConfig({
  title: 'HotPlex',
  description: 'The Strategic Bridge for AI Agent Engineering - Stateful, Secure, and High-Performance.',
  lang: 'en-US',
  base: '/hotplex/',

  head: [
    ['link', { rel: 'icon', href: '/hotplex/favicon.ico' }],
    ['meta', { name: 'theme-color', content: '#00ADD8' }],
    ['meta', { name: 'google', content: 'notranslate' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'HotPlex - Strategic Bridge for AI Agent Engineering' }],
    ['meta', { property: 'og:description', content: 'Stateful, Secure, and High-Performance Agent Infrastructure.' }],
    ['meta', { property: 'og:image', content: 'https://hrygo.github.io/hotplex/assets/hotplex-og.png' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:image', content: 'https://hrygo.github.io/hotplex/assets/hotplex-og.png' }],
  ],

  themeConfig: {
    logo: '/logo.svg',
    siteTitle: 'HotPlex',

    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Ecosystem', link: '/guide/chatapps' },
      {
        text: 'Manuals',
        items: [
          { text: 'ChatApps Manual', link: '/reference/chatapps' },
          { text: 'Engine Manual', link: '/reference/engine' },
          { text: 'Provider Manual', link: '/reference/provider' },
          { text: 'Internal Reference', link: '/reference/internal' },
          { text: 'API Specification', link: '/reference/api' },
        ]
      },
      { text: 'Blog', link: '/blog/' },
      { text: 'GitHub', link: 'https://github.com/hrygo/hotplex' }
    ],

    sidebar: {
      '/guide/': guideSidebar,
      '/sdks/': guideSidebar,
      '/reference/': [
        {
          text: 'Technical Reference',
          items: [
            { text: 'API Specification', link: '/reference/api' },
            { text: 'Protocol', link: '/reference/protocol' },
            { text: 'Hooks API', link: '/reference/hooks-api' },
          ]
        },
        {
          text: 'Package Manuals',
          items: [
            { text: 'ChatApps', link: '/reference/chatapps' },
            { text: 'Engine', link: '/reference/engine' },
            { text: 'Provider', link: '/reference/provider' },
            { text: 'Internal Subsystems', link: '/reference/internal' },
          ]
        }
      ],
      '/blog/': [
        {
          text: 'Updates',
          items: [
            { text: 'Latest', link: '/blog/' },
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
