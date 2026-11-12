import test from 'node:test';
import assert from 'node:assert/strict';
import {createI18n} from 'vue-i18n';
import {createSSRApp, h} from 'vue';
import {renderToString} from 'vue/server-renderer';
import en from './src/locales/en-US.json' with {type:'json'};
import zh from './src/locales/zh-CN.json' with {type:'json'};
const keys = (v,p='') => Object.entries(v).flatMap(([k,x])=>typeof x==='object'?keys(x,p+k+'.'):[p+k]).sort();
test('locale key parity',()=>assert.deepEqual(keys(en),keys(zh)));
test('English default, reactive Chinese, English fallback',async()=>{
  const i=createI18n({legacy:false,locale:'en-US',fallbackLocale:'en-US',missingWarn:false,fallbackWarn:false,messages:{'en-US':{...en,onlyEnglish:'Fallback'},'zh-CN':zh}});
  const render=()=>{const app=createSSRApp({render:()=>h('h1',i.global.t('probe.title'))}); app.use(i);return renderToString(app)};
  assert.match(await render(),/validation/);
  i.global.locale.value='zh-CN';
  assert.match(await render(),/验证/);
  assert.equal(i.global.t('onlyEnglish'),'Fallback');
  i.global.locale.value='en-US';assert.match(await render(),/validation/);
});
test('every fixture UI message comes from equal locale keys',()=>{
  for (const locale of [en,zh]) for (const text of Object.values(locale.probe)) assert.ok(text.length>0);
});
