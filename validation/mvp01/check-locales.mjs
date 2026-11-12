import assert from 'node:assert/strict';
import fs from 'node:fs';

function flat(value, prefix='') {
  return Object.fromEntries(Object.entries(value).flatMap(([key,item]) => {
    const full = prefix ? prefix+'.'+key : key;
    return typeof item==='string' ? [[full,item]] : Object.entries(flat(item,full));
  }));
}
const old = {}, current = {};
const chatCopy = {
  'en-US': { 'ai.prompt': 'Write a message…', 'ai.send': 'Send' },
  'zh-CN': { 'ai.prompt': '输入消息…', 'ai.send': '发送' },
};
for (const locale of ['en-US','zh-CN']) {
  old[locale] = flat(JSON.parse(fs.readFileSync(new URL(`../../docs/mvp01-evidence/locale-baseline-${locale}.json`,import.meta.url),'utf8')));
  current[locale] = flat(JSON.parse(fs.readFileSync(new URL(`../../frontend/src/locales/${locale}.json`,import.meta.url),'utf8')));
  for (const [key,value] of Object.entries(old[locale])) {
    const branded = value.replaceAll('DeskPilot AI','Asimi').replaceAll('DeskPilot','Asimi').replaceAll('Community workflows','Shared workflows').replaceAll('社区工作流','共享工作流');
    const actual = current[locale][key.replaceAll('community','preset')];
    assert.ok(actual === value || actual === branded || (key in chatCopy[locale] && actual === chatCopy[locale][key]), locale+' '+key);
  }
}
const keys = Object.keys(current['en-US']).sort();
assert.deepEqual(keys,Object.keys(current['zh-CN']).sort());
const placeholders=text=>Array.from(text.matchAll(/\{([\w]+)\}/g),m=>m[1]).sort();
for (const key of keys) assert.deepEqual(placeholders(current['en-US'][key]),placeholders(current['zh-CN'][key]),key);
console.log(JSON.stringify({passed:true,originalKeys:Object.keys(old['en-US']).length,keysPerLocale:keys.length,originalCopyPreservedExceptBrandAndChat:true,interpolationParity:true}));
