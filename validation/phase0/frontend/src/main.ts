import {createApp,h,nextTick,watchEffect} from 'vue';
import {createI18n} from 'vue-i18n';
import en from './locales/en-US.json';
import zh from './locales/zh-CN.json';

async function run() {
  const config=await fetch('/phase0/config').then(r=>r.json()) as {locale:'en-US'|'zh-CN';stage:number};
  const i=createI18n({legacy:false,locale:config.locale,fallbackLocale:'en-US',missingWarn:false,fallbackWarn:false,messages:{'en-US':en,'zh-CN':zh}});
  const t=i.global.t;
  watchEffect(()=>{document.title=t('probe.title');document.documentElement.lang=i.global.locale.value});
  const app=createApp({render:()=>h('main',{style:'max-width:36rem;margin:2rem auto;font:16px system-ui;overflow-wrap:anywhere;padding:1rem'},[
    h('h1',{id:'title'},t('probe.title')),
    ...['notice','error','empty','loading','approval','hint'].map(k=>h('p',{'data-key':k},t(`probe.${k}`))),
    h('label',{for:'language'},t('probe.language')),
    h('button',{id:'language',title:t('probe.hint'),'aria-label':t('probe.switch'),onClick:()=>{i.global.locale.value=i.global.locale.value==='en-US'?'zh-CN':'en-US'}},t('probe.switch'))
  ])});app.use(i);app.mount('#app');
  const checks:Record<string,boolean>={};
  const expect=(name:string,value:boolean)=>{checks[name]=value;if(!value)throw new Error(name)};
  expect('startupLocale',config.locale===(config.stage===1?'en-US':'zh-CN'));
  const context=Object.freeze({task:'fixture-task',responseLanguage:'auto',history:['retained']});
  const before=JSON.stringify(context);
  i.global.locale.value='zh-CN';document.documentElement.lang='zh-CN';await nextTick();
  expect('liveChinese',document.querySelector('#title')?.textContent===zh.probe.title);
  expect('ariaLocalized',document.querySelector('button')?.getAttribute('aria-label')===zh.probe.switch);
  expect('documentTitleLocalized',document.title===zh.probe.title);
  expect('tooltipLocalized',document.querySelector('button')?.getAttribute('title')===zh.probe.hint);
  expect('chineseNoOverflow',document.documentElement.scrollWidth<=window.innerWidth);
  expect('statusMessagesLocalized',['notice','error','empty','loading','approval','hint'].every(k=>document.querySelector(`[data-key="${k}"]`)?.textContent===zh.probe[k as keyof typeof zh.probe]));
  const incomplete=structuredClone(zh);delete (incomplete.probe as Partial<typeof zh.probe>).error;
  i.global.setLocaleMessage('zh-CN',incomplete);
  expect('missingTranslationFallback',t('probe.error')===en.probe.error);
  i.global.setLocaleMessage('zh-CN',zh);
  i.global.locale.value='en-US';await nextTick();
  expect('returnEnglish',document.querySelector('#title')?.textContent===en.probe.title);
  expect('contextPreserved',JSON.stringify(context)===before);
  expect('intlFormats',new Intl.NumberFormat('zh-CN').format(1234.5).length>0&&new Intl.DateTimeFormat('en-US').format(new Date(0)).length>0);
  expect('noDocumentOverflow',document.documentElement.scrollWidth<=window.innerWidth);
  await fetch('/phase0/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({locale:config.stage===1?'zh-CN':'en-US'})});
  await fetch('/phase0/result',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({stage:config.stage,checks})});
}
run().catch(async e=>{await fetch('/phase0/result',{method:'POST',body:JSON.stringify({error:String(e)})})});
