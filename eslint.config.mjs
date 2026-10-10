import tseslint from 'typescript-eslint';

// 只开语义安全、可自动修的规则；eqeqeq 是语义改动，只 warn。
export default tseslint.config(
  {
    ignores: [
      'node_modules/**',
      'dist/**',
      'bin/**',
      'desktop/dist/**',
      'desktop/build/**',
      'website/dist/**',
    ],
  },
  {
    files: ['**/*.js', '**/*.mjs', '**/*.cjs', '**/*.ts'],
    extends: [tseslint.configs.base],
    rules: {
      'no-var': 'error',
      'prefer-const': 'error',
      eqeqeq: ['warn', 'always'],
    },
  },
);
