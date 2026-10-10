import tseslint from 'typescript-eslint';

// 只开语义安全、可自动修的规则。eqeqeq 只 warn：
// 把 == 改成 === 是语义改动（全仓 16 处），属非目标。
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
