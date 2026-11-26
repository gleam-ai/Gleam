'use strict';

const fs = require('node:fs');
const Module = require('node:module');
const ts = require('typescript');

// Load a TypeScript source file as CommonJS with a stubbed 'electron' module.
// Both hooks are restored in finally so one harness cannot poison the next.
function loadTsModule(file, options = {}) {
  const { electron, mocks = {}, env = {}, reset = [] } = options;
  const entry = require.resolve(file);
  for (const p of [entry, ...reset.map((r) => require.resolve(r))]) {
    delete require.cache[p];
  }

  const originalLoad = Module._load;
  const originalTsLoader = require.extensions['.ts'];
  const savedEnv = new Map();
  for (const [key, value] of Object.entries(env)) {
    savedEnv.set(key, process.env[key]);
    process.env[key] = value;
  }

  require.extensions['.ts'] = (module, filename) => {
    const output = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
        esModuleInterop: true,
      },
    }).outputText;
    module._compile(output, filename);
  };
  Module._load = function (request) {
    if (request === 'electron') return electron;
    if (Object.prototype.hasOwnProperty.call(mocks, request))
      return mocks[request];
    return originalLoad.apply(this, arguments);
  };

  try {
    return require(entry);
  } finally {
    Module._load = originalLoad;
    if (originalTsLoader) require.extensions['.ts'] = originalTsLoader;
    else delete require.extensions['.ts'];
    for (const [key, value] of savedEnv) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

module.exports = { loadTsModule };
