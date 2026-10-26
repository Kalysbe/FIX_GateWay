const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

function load(file, imports) {
  const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src', file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  const context = { exports: {}, JSON, Map, Set, Array, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const raw = load('lib/rawOrders.ts', {});
function component(role, settings, classes) {
  return load('components/MarketClassesSettings.tsx', {
    react: React, 'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': {
      useQuery: ({ queryKey }) => ({ data: queryKey[0] === 'market-data-settings' ? settings : classes, error: null }),
      useMutation: () => ({ isPending: false, mutate() {} }),
      useQueryClient: () => ({ invalidateQueries() {} }),
    },
    '../api': { api() { throw Error('unexpected request'); }, apiWithHeaders() { throw Error('unexpected request'); }, getSessionUser: () => ({ role }) },
    './Toast': { useToast: () => ({ push() {} }) },
    '../lib/rawOrders': raw,
  });
}

test('chosen classes are checked among QUIK Classes; a vanished class is still shown', () => {
  const classes = raw.parseDBJSON('[{"ClassCode":"LISTBND","ClassName":"Листинговые:облигации"},{"ClassCode":"AUCT_GBA","ClassName":null}]');
  const m = component('ADMIN', { classes: ['LISTBND', 'OLD'], default_classes: ['LISTBND'] }, classes);
  const html = renderToStaticMarkup(React.createElement(m.MarketClassesSettings));
  assert.match(html, /Классы текущих торгов: LISTBND, OLD/);
  assert.match(html, /checked=""[^>]*\/><span class="font-mono">LISTBND<\/span><span class="text-muted">Листинговые:облигации/);
  assert.doesNotMatch(html, /checked=""[^>]*\/><span class="font-mono">AUCT_GBA/);
  assert.match(html, /OLD<\/span><span class="text-muted">нет в QUIK Classes/);
  assert.match(html, /Сохранить/);
});

test('read-only roles see the classes but cannot change them', () => {
  const m = component('AUDITOR', { classes: ['LISTBND'], default_classes: ['LISTBND'] }, []);
  const html = renderToStaticMarkup(React.createElement(m.MarketClassesSettings));
  assert.match(html, /disabled=""/);
  assert.doesNotMatch(html, /Сохранить/);
  assert.equal(m.canEditClasses('operator'), true);
  assert.equal(m.canEditClasses('AUDITOR'), false);
});
