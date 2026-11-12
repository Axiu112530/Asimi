import { createApp, defineComponent, h, watch, watchEffect } from 'vue';
import { i18n, t } from './i18n';
import { state, boot, disconnectStore, newConversation, saveSettings, stopAll, fail } from './store';
import { Assistant, FuturePage, HistoryPage, ModelsPage, SecurityPage, SettingsPage } from './pages';
import { Inspector } from './inspector';
import { button, errorText, icon, iconButton } from './ui';
import type { Route } from './types';
import './style.css';
import './ai.css';
import './screen.css';
import { ai, profile, startAI, stopAI } from './ai-store';
import { AIProfilesPage, AssistantSelection, RealAssistant, PlaygroundPage, UsagePage } from './ai-pages';
import { PresetOnboarding, PresetProviders } from './provider-setup';
import { ComputerPage } from './screen-pages';
import { HomePage, DeveloperPage, WorkspacesPage, TasksPage, SkillsPage, Onboarding } from './product-pages';
import { product, refreshProduct } from './product-store';
import './product.css';
const routes: Route[]=['home','assistant','computer','developer','workspaces','workflows','models','playground','usage','skills','security','history','settings'];
const params=new URLSearchParams(location.search);
const stage=params.has('acceptance')?Number(params.get('stage')):0;
const legacyAcceptance=stage>0&&stage<=13;
const routeLabel=(route:Route)=>!legacyAcceptance&&route==='models'?'preset.providers':'navigation.'+route;
const realSurface=()=>state.route==='playground'||ai.mode==='real';
const activeSession=()=>state.route==='playground'?ai.playground:ai.assistant;
const App=defineComponent({setup(){
  watchEffect(()=>{document.documentElement.lang=i18n.global.locale.value;document.title=t('app.title',{page:t(routeLabel(state.route))});});
  return ()=>!state.ready?h('div',{class:'boot-screen neutral-boot'},[h('span',{class:'welcome-mark'},icon('logo',36)),h('span',{class:'spinner'})]):h('div',{class:['desktop-app',{'inspector-visible':state.route==='assistant'&&state.inspectorOpen}]},[
    h('aside',{class:'sidebar'},[
      h('div',{class:'brand'},[h('span',{class:'brand-mark'},icon('logo',29)),h('div',[h('strong',t('app.name'))])]),
      button('navigation.newTask',newConversation,{class:'new-task-button',disabled:!state.connected,'data-testid':'new-task',title:t('navigation.newTask')},[icon('plus',19),h('span',t('navigation.newTask'))]),
      h('nav',{'aria-label':t('accessibility.navigation')},[
        h('p',{class:'nav-group'},t('navigation.workspace')),
        ...routes.map((route,index)=>h('div',{class:'nav-item-wrap'},[
          index===4?h('p',{class:'nav-group manage-label'},t('navigation.manage')):null,
          button(routeLabel(route),()=>state.route=route,{class:['nav-item',{active:state.route===route}],title:t(routeLabel(route)),'aria-current':state.route===route?'page':undefined,'data-testid':'nav-'+route},[
            icon(route,19),h('span',t(routeLabel(route))),
            route==='security'&&state.approvals.length?h('span',{class:'nav-count'},String(state.approvals.length)):null,
          ]),
        ])),
      ]),
      h('div',{class:'sidebar-footer'},[h('div',{class:'local-indicator'},[icon('lock',15),h('span',t(realSurface()?'ai.localHistory':'app.local')),h('i',{class:['online-dot',{offline:!state.connected}]})]),h('p',t('app.tagline')),h('span',{class:'sidebar-version'},state.info?t('app.version',{version:state.info.version}) : '')]),
    ]),
    h('div',{class:'app-shell'},[
      h('header',{class:'topbar'},[
        h('div',{class:'topbar-route'},[icon(state.route,17),h('span',t(routeLabel(state.route)))]),
        h('div',{class:'runtime-info','aria-label':t('accessibility.status')},[
          h('span',{class:'mode-tag'},t(realSurface()?'ai.real':legacyAcceptance?'app.mode':'preset.offlineMode')),h('span',{class:'topbar-provider',title:t('app.provider')},realSurface()?(profile(activeSession())?.name||t('common.notConfigured')):state.info?.provider||t('models.mock')),h('code',{class:'topbar-model',title:t('app.model')},realSurface()?activeSession().modelId||t('common.dash'):state.info?.model||t('common.dash')),
          h('span',{class:'runtime-status'},[h('i',{class:['online-dot',{offline:!state.connected,working:!!state.security?.activeTasks||ai.assistant.busy||ai.playground.busy}]}),t(!state.connected?'common.unavailable':state.security?.activeTasks||ai.assistant.busy||ai.playground.busy?'app.running':'app.ready')]),
        ]),
        h('div',{class:'topbar-actions'},[
          h('label',{class:'topbar-language'},[icon('globe',16),h('select',{'aria-label':t('accessibility.language'),title:t('settings.language'),value:state.settings.appLanguage,disabled:state.saving||!state.connected,onChange:(e:Event)=>saveSettings({appLanguage:(e.target as HTMLSelectElement).value as 'en-US'|'zh-CN'})},[h('option',{value:'en-US'},t('settings.english')),h('option',{value:'zh-CN'},t('settings.chinese'))])]),
          button('tasks.stopAll',stopAll,{class:'stop-all',title:t('tasks.stopHint'),'aria-label':t('tasks.stopHint'),'data-testid':'stop-all',disabled:!state.connected},[icon('stop',14),h('span',t('tasks.stopAll'))]),
        ]),
      ]),
      state.error?h('div',{class:'notification error-notification',role:'alert'},[icon('warning',18),h('div',[h('strong',errorText(state.error,state.errorParams)),!state.connected?h('p',t('app.bridgeNote')):null]),!state.connected?button('app.retry',boot,{class:'button secondary',disabled:state.loading}):null,iconButton('close','accessibility.dismissError',()=>state.error='')]):null,
      state.notice?h('div',{class:'notification success-notification',role:'status'},[icon('check',17),h('span',t(state.notice)),iconButton('close','accessibility.dismissNotice',()=>state.notice='')]):null,
      h('main',{class:'workspace','aria-label':t('accessibility.workspace')},[
        !state.ready?h('div',{class:'boot-screen',role:'status'},[h('span',{class:'welcome-mark'},icon('logo',36)),h('span',{class:'spinner'}),h('p',t('app.connecting'))]):
          state.route==='home'?h(HomePage):state.route==='developer'?h(DeveloperPage):state.route==='workspaces'?h(WorkspacesPage):state.route==='workflows'?h(TasksPage):state.route==='skills'?h(SkillsPage):state.route==='assistant'?h('div',{class:'ai-assistant-shell'},[h('div',{class:'product-row'},[AssistantSelection(),button('product.agentMode',()=>state.route='workflows',{class:'button secondary'})]),ai.mode==='mock'?h(Assistant):h(RealAssistant)]):state.route==='computer'?h(ComputerPage):state.route==='settings'?SettingsPage(legacyAcceptance):state.route==='models'?h('div',{class:'ai-models-route'},[legacyAcceptance?h(AIProfilesPage):h(PresetProviders),h('details',{class:'ai-legacy'},[h('summary',t('ai.offlineCatalog')),ModelsPage()])]):state.route==='playground'?PlaygroundPage():state.route==='usage'?UsagePage():state.route==='security'?SecurityPage():state.route==='history'?HistoryPage():FuturePage(state.route),
        state.ready&&state.route==='assistant'&&state.inspectorOpen&&ai.mode==='mock'?h(Inspector):null,
      ]),
    ]),
    legacyAcceptance?h(Onboarding):h(PresetOnboarding),
  ]);
}});
const app=createApp(App);app.use(i18n);app.mount('#app');
let productBooted=false;
watch(()=>state.connected,async connected=>{if(!connected)return;try{await refreshProduct();if(!productBooted){productBooted=true;if(!legacyAcceptance||stage===9||stage===10){product.showOnboarding=!product.data.onboarding.completed;if(!state.conversation)state.route='home';}}}catch(e){fail(e);}},{immediate:true});
const compact=matchMedia('(max-width:850px)');
const dark=matchMedia('(prefers-color-scheme: dark)');
if(compact.matches)state.inspectorOpen=false;
function onCompact(event: MediaQueryListEvent) {
  if (event.matches) state.inspectorOpen = false;
}
function onTheme(event: MediaQueryListEvent) {
  if (state.settings.theme === 'system') document.documentElement.dataset.theme = event.matches ? 'dark' : 'light';
}
function dispose() {
  stopAI();
  stopConnection();
  disconnectStore();
  compact.removeEventListener('change', onCompact);
  dark.removeEventListener('change', onTheme);
  window.removeEventListener('pagehide', dispose);
}
compact.addEventListener('change', onCompact);
dark.addEventListener('change', onTheme);
window.addEventListener('pagehide', dispose, {once:true});
app.onUnmount(dispose);
const stopConnection=watch(()=>state.connected,connected=>{if(connected)void startAI(!legacyAcceptance).catch(fail);else stopAI();});
await boot();
// The application never loads acceptance code during an ordinary launch.
if(new URLSearchParams(location.search).has('acceptance')) {
  await import(/* @vite-ignore */ '/acceptance.js').catch(()=>{ /* Harness absent; no product state changes. */ });
}
