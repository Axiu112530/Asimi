import assert from 'node:assert/strict';
import fs from 'node:fs';

function flat(value, prefix='') {
  return Object.fromEntries(Object.entries(value).flatMap(([key,item]) => {
    const full = prefix ? prefix+'.'+key : key;
    return typeof item==='string' ? [[full,item]] : Object.entries(flat(item,full));
  }));
}
const old = {}, current = {};
for (const locale of ['en-US','zh-CN']) {
  old[locale] = flat(JSON.parse(fs.readFileSync(new URL(`../../docs/mvp01-evidence/locale-baseline-${locale}.json`,import.meta.url),'utf8')));
  current[locale] = flat(JSON.parse(fs.readFileSync(new URL(`../../frontend/src/locales/${locale}.json`,import.meta.url),'utf8')));
  for (const [key,value] of Object.entries(old[locale])) {
    const branded = value.replaceAll('DeskPilot AI','Asimi').replaceAll('DeskPilot','Asimi');
    assert.ok(current[locale][key] === value || current[locale][key] === branded, locale+' '+key);
  }
}
const keys = Object.keys(current['en-US']).sort();
assert.deepEqual(keys,Object.keys(current['zh-CN']).sort());
const placeholders=text=>Array.from(text.matchAll(/\{([\w]+)\}/g),m=>m[1]).sort();
for (const key of keys) assert.deepEqual(placeholders(current['en-US'][key]),placeholders(current['zh-CN'][key]),key);
console.log(JSON.stringify({passed:true,originalKeys:Object.keys(old['en-US']).length,keysPerLocale:keys.length,originalCopyPreservedExceptBrand:true,interpolationParity:true}));
