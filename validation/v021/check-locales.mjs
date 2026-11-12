import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import '../../validation/mvp01/check-locales.mjs';

const locales = new URL('../../frontend/src/locales/', import.meta.url);
const read = name => JSON.parse(readFileSync(new URL(name, locales), 'utf8'));
const flatten = (value, prefix = '') => Object.entries(value).flatMap(([key, entry]) => {
  const name = prefix + key;
  return typeof entry === 'string' ? [[name, entry]] : flatten(entry, name + '.');
});
const placeholders = text => [...text.matchAll(/\{([\w]+)\}/g)].map(match => match[1]).sort();
const pairs = [
  ['community-setup-en.json', 'community-setup-zh.json'],
  ['project021.en-US.json', 'project021.zh-CN.json'],
  ['computer-suggestion.en-US.json', 'computer-suggestion.zh-CN.json'],
];
for (const [english, chinese] of pairs) {
  const en = Object.fromEntries(flatten(read(english)));
  const zh = Object.fromEntries(flatten(read(chinese)));
  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort(), english);
  for (const key of Object.keys(en)) assert.deepEqual(placeholders(en[key]), placeholders(zh[key]), key);
  console.log(JSON.stringify({ resource: english, keysPerLocale: Object.keys(en).length, interpolationParity: true }));
}
