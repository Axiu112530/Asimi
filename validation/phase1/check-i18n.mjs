import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import {createI18n} from '../../frontend/node_modules/vue-i18n/dist/vue-i18n.mjs';
const folder=new URL('../../frontend/src/',import.meta.url);
const en=JSON.parse(await fs.readFile(new URL('locales/en-US.json',folder),'utf8'));
const zh=JSON.parse(await fs.readFile(new URL('locales/zh-CN.json',folder),'utf8'));
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
