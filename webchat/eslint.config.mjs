import { defineConfig, globalIgnores } from 'eslint/config'
import nextVitals from 'eslint-config-next/core-web-vitals'

// Next.js 16 removed `next lint`; lint via the ESLint CLI with a flat config
// (see docs/01-app/03-api-reference/05-config/03-eslint.mdx).
const eslintConfig = defineConfig([
  ...nextVitals,
  globalIgnores([
    '.next/**',
    'out/**',
    'build/**',
    'next-env.d.ts',
  ]),
])

export default eslintConfig
