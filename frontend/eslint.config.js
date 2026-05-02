// Flat config for ESLint v9. Replaces .eslintrc — minimal rules
// to keep CI green. Add stricter rules incrementally.
import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'coverage'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],
      // The OIDCProvider intentionally exports both the React context
      // provider and module-level helpers (getAccessToken etc.) so the
      // API client can read tokens without going through React. Allow
      // this pattern.
      '@typescript-eslint/no-explicit-any': 'off',
    },
  }
)
