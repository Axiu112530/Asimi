import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import {createI18n} from '../../frontend/node_modules/vue-i18n/dist/vue-i18n.mjs';
const folder=new URL('../../frontend/src/',import.meta.url);
const readLocale = async name => JSON.parse(await fs.readFile(new URL('locales/' + name, folder), 'utf8'));
const catalog = createI18n({ legacy: false, messages: {
  'en-US': await readLocale('en-US.json'),
  'zh-CN': await readLocale('zh-CN.json'),
} });
// Mirror the resources merged by i18n.ts, project021-panel.ts and community-setup.ts.
for (const [namespace, englishFile, chineseFile] of [
  [null, 'computer-suggestion.en-US.json', 'computer-suggestion.zh-CN.json'],
  ['project021', 'project021.en-US.json', 'project021.zh-CN.json'],
  ['communitySetup', 'community-setup-en.json', 'community-setup-zh.json'],
]) {
  const english = await readLocale(englishFile);
  const chinese = await readLocale(chineseFile);
  catalog.global.mergeLocaleMessage('en-US', namespace ? { [namespace]: english } : english);
  catalog.global.mergeLocaleMessage('zh-CN', namespace ? { [namespace]: chinese } : chinese);
}
const en = catalog.global.getLocaleMessage('en-US');
const zh = catalog.global.getLocaleMessage('zh-CN');
const flatten=(obj,prefix='')=>Object.entries(obj).flatMap(([k,v])=>typeof v==='object'?flatten(v,prefix+k+'.'):[[prefix+k,v]]);
const english=new Map(flatten(en)),chinese=new Map(flatten(zh));
assert.deepEqual([...english.keys()].sort(),[...chinese.keys()].sort());
for(const [key,value] of english){assert.equal(typeof value,'string');assert.ok(value.length>0,key);assert.ok(chinese.get(key).length>0,key);const placeholders=s=>[...s.matchAll(/\{(\w+)\}/g)].map(m=>m[1]).sort();assert.deepEqual(placeholders(value),placeholders(chinese.get(key)),key)}
let literals=0;
for(const name of await fs.readdir(folder)){if(!name.endsWith('.ts')||name==='i18n.ts')continue;const source=await fs.readFile(new URL(name,folder),'utf8');for(const match of source.matchAll(/(?:\bt|\bbutton|\bdetail|\bsection)\('([\w.-]+)'\s*[,)]/g)){assert.ok(english.has(match[1]),`${name}: missing ${match[1]}`);literals++}}
const fallback=structuredClone(zh);delete fallback.common.unknown;
const i=createI18n({legacy:false,locale:'zh-CN',fallbackLocale:'en-US',messages:{'en-US':en,'zh-CN':fallback},missingWarn:false,fallbackWarn:false});
assert.equal(i.global.t('common.unknown'),en.common.unknown);
console.log(JSON.stringify({localeKeys:english.size,equalKeys:true,parameterParity:true,nonempty:true,literalKeysChecked:literals,missingTranslationFallsBack:true}));
