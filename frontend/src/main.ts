import { createApp, defineComponent, h, onBeforeUnmount, Teleport, watch, watchEffect } from 'vue';
import { i18n, t } from './i18n';
import { state, boot, disconnectStore, newConversation, saveSettings, stopAll, fail } from './store';
import { Assistant, FuturePage, HistoryPage, ModelsPage, SecurityPage, SettingsPage } from './pages';
import { Inspector } from './inspector';
import { button, errorText, icon, iconButton } from './ui';
import type { Route } from './types';
import './style.css';
import './ai.css';
import './screen.css';
import { ai, refreshAI, startAI, stopAI } from './ai-store';
import { AIProfilesPage, AssistantSelection, RealAssistant, PlaygroundPage } from './ai-pages';
import { UsagePage } from './usage-page';
import { PresetOnboarding, PresetProviders } from './provider-setup';
import { ComputerPage } from './screen-pages';
import { ComputerActivity } from './computer-activity';
import { HomePage, DeveloperPage, WorkspacesPage, TasksPage, SkillsPage, Onboarding } from './product-pages';
import { product, refreshProduct } from './product-store';
import './product.css';
import './workspace.css';
import './unified-agent.css';
import { UnifiedAgentPage, ConnectionsPage } from './unified-agent-page';
import { CognitionSkillsPanel, GrowthPage, LearningPage, MemoryPage } from './cognition-pages';
const primaryRoutes: Route[] = ['home', 'goals', 'assistant', 'computer'];
const managementRoutes: Route[] = ['models', 'connections', 'usage', 'memory', 'skills', 'learning', 'growth', 'security', 'history', 'settings'];
const developerRoutes: Route[] = ['developer', 'workspaces', 'workflows', 'playground'];
const params=new URLSearchParams(location.search);
const stage=params.has('acceptance')?Number(params.get('stage')):0;
const legacyAcceptance=stage>0&&stage<=13;
const cognitionRoutes: Partial<Record<Route,string>> = { memory: 'memoryPage', learning: 'learningPage', growth: 'growthPage' };
const routeLabel=(route:Route)=>['goals','connections'].includes(route)?'unified.'+route:cognitionRoutes[route] ? 'cognition.'+cognitionRoutes[route] : !legacyAcceptance&&route==='models'?'preset.providers':'navigation.'+route;
const showDeveloper = () => legacyAcceptance || state.settings.interfaceMode === 'developer';
function renderPage() {
  switch (state.route) {
    case 'home': return h(HomePage, { developer: showDeveloper() });
    case 'goals': return h(UnifiedAgentPage);
    case 'connections': return h(ConnectionsPage);
    case 'developer': return h(DeveloperPage);
    case 'workspaces': return h(WorkspacesPage);
    case 'workflows': return h(TasksPage);
    case 'memory': return h(MemoryPage);
    case 'learning': return h(LearningPage);
    case 'growth': return h(GrowthPage);
    case 'skills': return h('div',{class:'cognition-skills-route'},[
      h(SkillsPage,{developer:showDeveloper()}),h(CognitionSkillsPanel),
    ]);
    case 'assistant':
      if (!legacyAcceptance) return h(RealAssistant, { developer: showDeveloper() });
      return h('div', { class: 'ai-assistant-shell' }, [
        AssistantSelection(),
        ai.mode === 'mock' ? h(Assistant) : h(RealAssistant, { legacy: true }),
      ]);
    case 'computer': return h(ComputerPage, { developer: showDeveloper() });
    case 'settings': return SettingsPage(legacyAcceptance);
    case 'models':
      if (!legacyAcceptance) return h(PresetProviders);
      return h('div', { class: 'ai-models-route' }, [
        h(AIProfilesPage),
        h('details', { class: 'ai-legacy' }, [
          h('summary', t('ai.offlineCatalog')),
          ModelsPage(),
        ]),
      ]);
    case 'playground': return PlaygroundPage();
    case 'usage': return h(UsagePage, { developer: showDeveloper() });
    case 'security': return SecurityPage();
    case 'history': return HistoryPage(showDeveloper());
    default: return FuturePage(state.route);
  }
}

const App=defineComponent({setup(){
  let noticeTimer: ReturnType<typeof setTimeout> | undefined;
  watch(() => state.notice, value => {
    clearTimeout(noticeTimer);
    if (value) noticeTimer = setTimeout(() => { state.notice = ''; }, 3500);
  });
  onBeforeUnmount(() => clearTimeout(noticeTimer));
  watch(() => [showDeveloper(), state.route] as const, ([enabled, route]) => {
    if (!enabled && route !== 'workflows' && developerRoutes.includes(route)) state.route = 'home';
  });
  watch(() => state.route, route => {
    state.notice = '';
    if (route === 'settings' && state.connected) void refreshAI().catch(fail);
  });
  watchEffect(()=>{document.documentElement.lang=i18n.global.locale.value;document.title=t('app.title',{page:t(routeLabel(state.route))});});
  return ()=>!state.ready?h('div',{class:'boot-screen neutral-boot'},[h('span',{class:'welcome-mark'},icon('logo',36)),h('span',{class:'spinner'})]):h('div',{class: ['desktop-app', {
    'inspector-visible': legacyAcceptance && state.route === 'assistant' && state.inspectorOpen,
    'setup-active': !legacyAcceptance && product.showOnboarding,
  }], inert: !legacyAcceptance && product.showOnboarding ? true : undefined},[
    h('aside',{class:'sidebar'},[
      h('div',{class:'brand'},[h('span',{class:'brand-mark'},icon('logo',29)),h('div',[h('strong',t('app.name'))])]),
      button('navigation.newChat',newConversation,{class:'new-task-button',disabled:!state.connected,'data-testid':'new-task',title:t('navigation.newChat')},[icon('plus',19),h('span',t('navigation.newChat'))]),
      h('nav',{'aria-label':t('accessibility.navigation')},[
        h('p',{class:'nav-group'},t('navigation.workspace')),
        ...[...primaryRoutes, ...(showDeveloper() ? developerRoutes : []), ...managementRoutes].map(route=>h('div',{class:'nav-item-wrap',key:route},[
          route==='models'?h('p',{class:'nav-group manage-label'},t('navigation.manage')):null,
          button(routeLabel(route),()=>state.route=route,{class:['nav-item',{active:state.route===route}],title:t(routeLabel(route)),'aria-current':state.route===route?'page':undefined,'data-testid':'nav-'+route},[
            icon(route,19),h('span',t(routeLabel(route))),
            route==='security'&&state.approvals.length?h('span',{class:'nav-count'},String(state.approvals.length)):null,
          ]),
        ])),
      ]),
      state.info ? h('span', { class: 'sidebar-version' }, t('app.version', { version: state.info.version })) : null,
    ]),
    h('div',{class:'app-shell'},[
      h('header',{class:'topbar'},[
        h('div',{class:'topbar-route'},[icon(state.route,17),h('span',t(routeLabel(state.route)))]),
        h('div',{class:'topbar-actions'},[
          h('label',{class:'topbar-language'},[icon('globe',16),h('select',{'aria-label':t('accessibility.language'),title:t('settings.language'),value:state.settings.appLanguage,disabled:state.saving||!state.connected,onChange:(e:Event)=>saveSettings({appLanguage:(e.target as HTMLSelectElement).value as 'en-US'|'zh-CN'})},[h('option',{value:'en-US'},t('settings.english')),h('option',{value:'zh-CN'},t('settings.chinese'))])]),
          button('tasks.stopAll',stopAll,{class:'stop-all',title:t('tasks.stopHint'),'aria-label':t('tasks.stopHint'),'data-testid':'stop-all',disabled:!state.connected},[icon('stop',14),h('span',t('tasks.stopAll'))]),
        ]),
      ]),
      h(ComputerActivity),
      state.error?h('div',{class:'notification error-notification',role:'alert'},[icon('warning',18),h('div',[h('strong',errorText(state.error,state.errorParams)),!state.connected?h('p',t('app.bridgeNote')):null]),!state.connected?button('app.retry',boot,{class:'button secondary',disabled:state.loading}):null,iconButton('close','accessibility.dismissError',()=>state.error='')]):null,
      state.notice?h('div',{class:'notification success-notification',role:'status'},[icon('check',17),h('span',t(state.notice)),iconButton('close','accessibility.dismissNotice',()=>state.notice='')]):null,
      h('main',{class:'workspace','aria-label':t('accessibility.workspace')},[
        !state.ready?h('div',{class:'boot-screen',role:'status'},[h('span',{class:'welcome-mark'},icon('logo',36)),h('span',{class:'spinner'}),h('p',t('app.connecting'))]):
        renderPage(),
        legacyAcceptance && state.route === 'assistant' && state.inspectorOpen && ai.mode === 'mock' ? h(Inspector) : null,
      ]),
    ]),
    legacyAcceptance ? h(Onboarding) : h(Teleport, { to: 'body' }, [h(PresetOnboarding)]),
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
