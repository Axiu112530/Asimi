import { defineComponent, h, ref, watch, nextTick, type VNodeChild } from 'vue';
import { state, newConversation, openConversation, saveSettings, selectTask, send, run, refreshSecurity } from './store';
import { date, known, number, t } from './i18n';
import { badge, button, detail, errorText, icon, iconButton } from './ui';
import { approvalPreview, verificationDetails } from './inspector';
import { Markdown } from './markdown';
import type { Model, Settings, Task } from './types';
import { AISettingsPanel } from './ai-pages';
import { ScreenCaptureActions } from './screen-pages';
import { ConfigurationPanel } from './config021-panel';
export function pageHeading(title: string, subtitle: string, actions?: VNodeChild) { return h('header',{class:'page-heading'},[h('div',[h('h1',t(title)),h('p',t(subtitle))]),actions]); }
export const Assistant = defineComponent({setup(){
  const messages=ref<HTMLElement|null>(null);const composer=ref<HTMLTextAreaElement|null>(null);
  watch(()=>[state.conversation?.id,state.conversation?.messages?.length],async()=>{await nextTick();if(messages.value)messages.value.scrollTop=messages.value.scrollHeight;});
  const prompt=(key:string)=>{state.draft=t(key);nextTick(()=>composer.value?.focus());};
  return ()=>{
    const conversation=state.conversation;const task=state.task;const steps=task?.steps||[];const done=steps.filter(s=>['COMPLETED','DONE','SUCCEEDED'].includes(s.status)).length;
    return h('section',{class:'assistant-workspace'},[
      h('header',{class:'conversation-header'},[h('div',[h('span',{class:'eyebrow'},t('assistant.conversation')),h('h1',conversation?.title||t('assistant.newConversation'))]),h('div',{class:'conversation-actions'},[conversation?.tasks?.length?h('select',{'aria-label':t('assistant.selectTask'),value:task?.id||'',onChange:(e:Event)=>selectTask((e.target as HTMLSelectElement).value)},[h('option',{value:'',disabled:true},t('assistant.selectTask')),...conversation.tasks.map(x=>h('option',{value:x.id},x.title))]):null,!state.inspectorOpen?iconButton('panel','tasks.expand',()=>state.inspectorOpen=true,{'data-testid':'inspector-toggle'}):null])]),
      task?h('div',{class:'task-strip','aria-label':t('assistant.taskProgress')},[
        badge(task.status),h('span',{class:'task-strip-title'},task.title),h('span',{class:'micro muted progress-caption'},t('assistant.progress',{done,total:steps.length})),
        h('div',{class:'mini-progress','aria-hidden':'true'},h('i',{style:{width:steps.length?`${done/steps.length*100}%`:'0%'}})),
        iconButton('chevron','assistant.viewTask',()=>state.inspectorOpen=true),
      ]):null,
      h('div',{class:'message-scroll',ref:messages,'data-testid':'messages','aria-label':t('assistant.messages'),'aria-live':'polite'},[
        !(conversation?.messages?.length)?h('div',{class:'welcome'},[
          h('div',{class:'welcome-mark'},icon('logo',34)),h('span',{class:'eyebrow'},t('app.local')),h('h2',t('assistant.welcome')),h('p',{class:'welcome-intro'},t('assistant.intro')),
          h('div',{class:'suggestion-list'},[
            ['create','file','exampleCreate'],['capabilities','spark','exampleCapabilities'],['plan','workflows','examplePlan'],
          ].map(([key,glyph,example])=>button('assistant.'+key,()=>prompt('assistant.'+example),{class:'suggestion',disabled:!state.connected},[
            h('span',{class:'suggestion-icon'},icon(glyph,21)),h('span',[h('strong',t('assistant.'+key)),h('small',t('assistant.'+key+'Hint'))]),icon('arrow',17),
          ]))),h('p',{class:'scope-note'},[icon('lock',14),t('assistant.scope')]),
        ]):(conversation.messages||[]).map(message=>h('article',{class:['message','message-'+message.role],key:message.id},[
          h('div',{class:'message-avatar','aria-hidden':'true'},message.role==='user'?icon('computer',16):icon('logo',18)),
          h('div',{class:'message-body'},[h('header',[h('strong',t(message.role==='user'?'assistant.you':message.role==='system'?'assistant.system':'assistant.agent')),h('time',{datetime:message.createdAt},date(message.createdAt)),message.taskId?button('assistant.viewTask',()=>selectTask(message.taskId!),{class:'message-task-link',title:t('assistant.viewTask'),'aria-label':t('assistant.viewTask')},icon('workflows',14)):null]),h(Markdown,{content:message.content})]),
        ])),
        state.busy?h('div',{class:'sending-line',role:'status'},[h('span',{class:'spinner'}),t('assistant.sending')]):null,
      ]),
      h('form',{class:'composer-wrap',onSubmit:(e:SubmitEvent)=>{e.preventDefault();send();}},[
        h('div',{class:['composer-box',{focused:!!state.draft}]},[
          h('textarea',{ref:composer,value:state.draft,rows:3,placeholder:t('assistant.placeholder'),'aria-label':t('assistant.inputLabel'),'data-testid':'composer',disabled:!state.connected,onInput:(e:Event)=>{state.draft=(e.target as HTMLTextAreaElement).value;},onKeydown:(e:KeyboardEvent)=>{if(e.key==='Enter'&&!e.shiftKey&&!e.isComposing){e.preventDefault();send();}}}),
          h('div',{class:'composer-bottom screen-mock-composer'},[h('span',{class:'micro muted'},[icon('lock',13),t('assistant.localNotice')]),h(ScreenCaptureActions,{source:'assistant'}),h('button',{type:'submit',class:'send-button',title:t('assistant.send'),'aria-label':t('assistant.send'),'data-testid':'send',disabled:!state.draft.trim()||state.busy||!state.connected},icon('arrow',19))]),
        ]),h('p',{class:'input-hint'},t('assistant.inputHint')),
      ]),
    ]);
  };
}});

const futureItems: Record<string,string[]> = {computer:['screen','mouse','keyboard','uia','monitors'],developer:['workspace','assistant','terminal','git','environment'],workflows:['library','saved','community','create']};
export function FuturePage(route: string) {
  return h('div',{class:'page-scroll'},h('div',{class:'page future-page'},[
    h('div',{class:'future-hero'},[h('div',{class:'future-icon'},icon(route,36)),h('span',{class:'eyebrow'},t('navigation.'+route)),h('h1',t('future.'+route+'Title')),h('p',t('future.'+route+'Description')),h('span',{class:'badge neutral'},t('future.badge'))]),
    h('div',{class:'feature-list'},futureItems[route].map((key,index)=>h('div',{class:'feature-row'},[
      h('span',{class:'feature-number','aria-hidden':'true'},String(index+1).padStart(2,'0')),h('div',[h('h3',t(`future.${route}.${key}`)),h('p',t(`future.${route}.${key}Note`))]),
      h('button',{class:'button disabled-feature',disabled:true,title:t('future.badge')},t(route==='computer'?'future.badge':'future.button')),
    ]))),
  ]));
}

function settingSelect<K extends keyof Settings>(key:K, options: [Settings[K],string][],testid?:string) {
  return h('select',{value:state.settings[key],disabled:state.saving||!state.connected,'aria-label':t('settings.'+(key==='appLanguage'?'language':key)),'data-testid':testid||key,onChange:(e:Event)=>saveSettings({[key]:(e.target as HTMLSelectElement).value})},options.map(([value,label])=>h('option',{value},t(label))));
}
function settingRow(label:string,hint:string,control:VNodeChild) { return h('div',{class:'settings-row'},[h('div',[h('h3',t(label)),h('p',t(hint))]),control]); }
export function SettingsPage(legacyAcceptance = false) {
  return h('div',{class:'page-scroll'},h('div',{class:'page settings-page'},[
    pageHeading('settings.title','settings.subtitle',state.saving?h('span',{class:'micro muted',role:'status'},t('settings.saving')):undefined),
    h('section',{class:'settings-section'},[h('h2',t('settings.general')),
      settingRow('settings.startup','settings.startupHint',settingSelect('startup',[['welcome','settings.welcome'],['last','settings.last']],'startup')),
    ]),
    h('section',{class:'settings-section'},[h('h2',t('community.providers')),settingRow('settings.models','settings.modelsHint',button('settings.manageModels',()=>state.route='models',{class:'button secondary'}))]),
    h('section',{class:'settings-section'},[h('h2',t('community.appearance')),
      settingRow('settings.theme','settings.themeHint',settingSelect('theme',[['light','settings.light'],['dark','settings.dark'],['system','settings.system']],'theme')),
    ]),
    h('section',{class:'settings-section'},[h('h2',t('community.language')),
      settingRow('settings.language','settings.languageHint',settingSelect('appLanguage',[['en-US','settings.english'],['zh-CN','settings.chinese']],'app-language')),
      settingRow('settings.agentLanguage','settings.agentLanguageHint',settingSelect('agentLanguage',[['follow-app','settings.followApp'],['auto','settings.auto'],['en-US','settings.english'],['zh-CN','settings.chinese']],'agent-language')),
    ]),
    h('section',{class:'settings-section'},[h('h2',t('community.privacy')),settingRow('security.scoped','settings.securityHint',button('settings.openSecurity',()=>state.route='security',{class:'button secondary'}))]),
    legacyAcceptance ? AISettingsPanel() : h('details',{class:'settings-section community-advanced','data-testid':'community-advanced'},[h('summary',t('community.advanced')), AISettingsPanel(), h(ConfigurationPanel)]),
    h('section',{class:'settings-section about-section'},[h('div',{class:'about-brand'},[h('div',{class:'welcome-mark small'},icon('logo',24)),h('div',[h('h2',t('settings.about')),h('p',t('app.description'))])]),h('dl',{class:'about-details'},[detail('settings.version',state.info?.version||t('common.unknown')),detail('settings.go',state.info?.goVersion||t('common.unknown')),detail('settings.wails',state.info?.wailsVersion||t('common.unknown')),detail('security.dataDir',state.info?.dataDir||t('common.unknown'),true)])]),
  ]));
}

export function HistoryPage() {
  return h('div',{class:'page-scroll'},h('div',{class:'page history-page'},[
    pageHeading('history.title','history.subtitle',button('navigation.newTask',newConversation,{class:'button primary',disabled:!state.connected},[icon('plus',16),t('navigation.newTask')])),
    h('section',{class:'history-section'},[h('div',{class:'section-title'},[h('h2',t('history.conversations')),h('span',{class:'count'},number(state.conversations.length))]),
      state.conversations.length?h('div',{class:'record-list'},state.conversations.map(c=>h('button',{class:'history-conversation',onClick:()=>openConversation(c.id),'aria-label':t('history.openConversation',{title:c.title}),key:c.id,'data-conversation-id':c.id},[
        h('span',{class:'record-icon'},icon('assistant',21)),h('span',{class:'history-content'},[h('strong',c.title||t('assistant.newConversation')),h('span',{class:'micro muted'},[t('history.taskCount',{count:c.tasks?.length||state.tasks.filter(x=>x.conversationId===c.id).length})])]),h('time',{class:'micro muted',datetime:c.updatedAt},date(c.updatedAt)),icon('chevron',16),
      ]))):h('div',{class:'list-empty'},[icon('history',28),h('p',t('history.noConversations'))]),
    ]),
    h('section',{class:'history-section'},[h('div',{class:'section-title'},[h('h2',t('history.tasks')),h('span',{class:'count'},number(state.tasks.length))]),state.tasks.length?h('div',{class:'record-list'},state.tasks.map(task=>h('button',{class:'history-task',onClick:()=>openConversation(task.conversationId,task.id),'aria-label':t('tasks.select',{id:task.id}),key:task.id,'data-task-id':task.id},[
      h('span',{class:'record-icon'},icon('file',19)),h('span',{class:'history-content'},[h('strong',task.title),h('code',{class:'micro muted'},task.id)]),badge(task.status),h('time',{class:'micro muted',datetime:task.updatedAt},date(task.updatedAt)),icon('chevron',16),
    ]))):h('p',{class:'list-empty muted'},t('history.noTasks'))]),
  ]));
}

export function ModelsPage() {
  const provider=state.providers.find(x=>x.id===state.selectedProvider)||state.providers[0];
  const models=provider?.models||[];const capabilities: (keyof Model)[]=['text','vision','toolCalling','streaming','computerUse','structuredOutput'];
  return h('div',{class:'page-scroll'},h('div',{class:'page models-page'},[
    pageHeading('models.title','models.subtitle'),
    h('div',{class:'models-layout'},[
      h('aside',{class:'provider-list','aria-label':t('accessibility.providerList')},[
        h('h2',{class:'eyebrow'},t('models.active')),...state.providers.filter(p=>p.enabled).map(p=>h('button',{class:['provider-button',{selected:provider?.id===p.id}],onClick:()=>state.selectedProvider=p.id,title:t('models.inspectProvider',{provider:p.name})},[h('span',{class:'provider-monogram'},icon('models',18)),h('span',[h('strong',p.name),h('small',t('app.local'))]),h('span',{class:'online-dot'})])),
        h('h2',{class:'eyebrow provider-future-title'},t('models.reserved')),...state.providers.filter(p=>!p.enabled).map(p=>h('button',{class:['provider-button',{selected:provider?.id===p.id}],onClick:()=>state.selectedProvider=p.id,title:t('models.inspectProvider',{provider:p.name})},[h('span',{class:'provider-initial','aria-hidden':'true'},p.name.slice(0,1)),h('span',[h('strong',p.name),h('small',t('models.notConnected'))]),icon('lock',13)])),
      ]),
      provider?h('section',{class:'provider-detail'},[
        h('div',{class:'provider-detail-heading'},[h('div',[h('span',{class:'eyebrow'},t(provider.enabled?'models.active':'models.reserved')),h('h2',provider.name)]),h('span',{class:['badge',provider.enabled?'state-completed':'neutral']},t(provider.enabled?'common.enabled':'models.notConnected'))]),
        h('p',{class:'muted'},t(provider.enabled?'models.mockNote':'models.futureNote')),
        h('dl',{class:'provider-fields'},[detail('models.providerId',provider.id,true),detail('models.baseUrl',provider.baseUrl||t('common.notConfigured'),true)]),
        !provider.enabled?h('div',{class:'provider-connect'},[h('span',{class:'micro muted'},t('models.reservedEndpoint')),h('button',{disabled:true,class:'button secondary',title:t('models.futureNote')},[icon('lock',14),t('models.connect')])]):null,
        ...models.map(model=>h('section',{class:'model-section',key:model.id},[
          h('div',{class:'section-title'},[h('h3',t('models.capabilities')),h('code',{class:'model-id'},model.id)]),
          h('table',{class:'data-table capability-table'},[
            h('thead',h('tr',[h('th',t('models.capability')),h('th',t('models.support'))])),
            h('tbody',capabilities.map(cap=>h('tr',[h('td',t('models.capabilityNames.'+cap)),h('td',h('span',{class:['capability','capability-'+String(model[cap]).toLowerCase()]},[icon(model[cap]==='SUPPORTED'?'check':model[cap]==='UNSUPPORTED'?'close':'info',14),known('capabilities.'+model[cap],'capabilities.UNKNOWN')]))]))),
          ]),h('dl',{class:'model-metadata'},[detail('models.context',model.contextLength>0?t('models.tokens',{count:number(model.contextLength)}):t('common.unknown')),detail('models.pricing',t(provider.enabled?'models.noPricing':'models.unknownPricing'))]),
          h('p',{class:'micro muted'},[icon('info',14),t('models.honest')]),
        ])),
        !models.length?h('p',{class:'list-empty muted'},t('models.noModels')):null,
      ]):h('p',{class:'list-empty muted'},t('common.empty')),
    ]),
  ]));
}

function taskLink(id: string) { const task=state.tasks.find(t=>t.id===id);return task?button('security.openTask',()=>openConversation(task.conversationId,task.id),{class:'text-button',title:t('security.openTask')},h('code',id)):h('code',id); }
export function SecurityPage() {
  const s=state.security;const tab=state.securityTab;
  const tabs=['overview','operations','approvals','permissions','audit','privacy'];
  const privacy=()=>h('section',{class:'security-content'},[
    h('h2',t('security.privacy')),h('p',{class:'muted'},t('security.localData')),
    h('dl',{class:'security-dl'},[detail('security.network',t(s?.externalNetwork?'common.enabled':'common.disabled')),detail('security.desktop',t(s?.desktopControl?'common.enabled':'common.disabled')),detail('security.stop',t(s?.stopAvailable?'common.enabled':'common.unavailable')),detail('security.dataDir',state.info?.dataDir||t('common.unknown'),true)]),h('p',{class:'policy-note'},[icon('lock',18),t('security.retention')]),
  ]);
  return h('div',{class:'page-scroll'},h('div',{class:'page security-page'},[
    pageHeading('security.title','security.subtitle',iconButton('refresh','common.refresh',()=>run(refreshSecurity),{disabled:!state.connected})),
    h('nav',{class:'tabs','aria-label':t('security.title')},tabs.map(key=>button('security.'+key,()=>state.securityTab=key,{class:['tab',{selected:tab===key}],'aria-current':tab===key?'page':undefined}))),
    tab==='overview'?h('div',[
      h('section',{class:'security-content'},[
        h('div',{class:'security-summary'},[h('div',[h('span',{class:'eyebrow'},t('security.workspace')),h('p',{class:'mono workspace-path'},s?.workspace||t('common.unknown'))]),h('div',{class:'security-stat'},[h('strong',number(s?.activeTasks||0)),h('span',t('security.active'))]),h('div',{class:'security-stat'},[h('strong',number(s?.pendingApprovals||0)),h('span',t('security.pending'))])]),
        h('dl',{class:'security-dl'},[detail('security.maxBytes',t('common.bytes',{count:number(s?.maxFileBytes||0)})),detail('security.tools',(s?.toolNames||[]).map(name=>h('code',{class:'tool-name'},name))),detail('security.stop',t(s?.stopAvailable?'common.enabled':'common.unavailable'))]),
        h('h2',t('security.levels')),h('table',{class:'data-table policy-table'},[h('thead',h('tr',[h('th',t('security.level')),h('th',t('security.policy')),h('th',t('security.availability'))])),h('tbody',['L0','L1','L2','L3'].map(level=>h('tr',{class:{'enabled-policy':level==='L2'}},[h('td',h('strong',t('security.'+level))),h('td',t('security.'+level+'Note')),h('td',h('span',{class:level==='L2'?'scope-tag':'muted'},t(level==='L2'?'security.scoped':'common.disabled')))])))]),h('p',{class:'policy-note'},[icon('security',17),t('security.noGrant')]),
      ]),
    ]):null,
    tab==='operations'?h('section',{class:'security-content'},[h('h2',t('security.operations')),state.executions.length?state.executions.map(ex=>h('details',{class:'evidence-record',key:ex.id},[
      h('summary',[icon('file',18),h('span',{class:'evidence-title'},[h('strong',ex.toolName),h('span',{class:'mono micro'},ex.path)]),badge(ex.status),h('time',{class:'micro muted'},date(ex.updatedAt))]),
      h('div',{class:'evidence-body'},[h('dl',[detail('common.id',ex.id,true),detail('common.task',taskLink(ex.taskId)),detail('common.path',ex.path,true)]),ex.errorCode?h('p',{class:'inline-error'},errorText(ex.errorCode)):null,ex.result?verificationDetails(ex.result,ex.status==='RECOVERY_REQUIRED'):null]),
    ])):h('p',{class:'list-empty muted'},t('security.noOperations'))]):null,
    tab==='approvals'?h('section',{class:'security-content'},[h('h2',t('approvals.history')),state.approvalHistory.length?state.approvalHistory.map(a=>h('details',{class:'evidence-record',key:a.id},[
      h('summary',[icon('lock',17),h('span',{class:'evidence-title'},[h('strong',a.operation.fileName),h('code',{class:'micro'},a.id)]),badge(a.status),h('time',{class:'micro muted'},date(a.createdAt))]),
      h('div',{class:'evidence-body'},[taskLink(a.operation.taskId),approvalPreview(a),a.decidedAt?h('p',{class:'micro muted'},t('approvals.decided')+': '+date(a.decidedAt)):null]),
    ])):h('p',{class:'list-empty muted'},t('approvals.noHistory'))]):null,
    tab==='permissions'?h('section',{class:'security-content'},[h('h2',t('security.permissions')),h('p',{class:'muted'},t('security.currentHint')),state.approvals.length?state.approvals.map(a=>h('details',{class:'evidence-record',key:a.id},[
      h('summary',[icon('lock',17),h('strong',{class:'evidence-title'},a.operation.fileName),badge(a.status)]),h('div',{class:'evidence-body'},[taskLink(a.operation.taskId),approvalPreview(a),button('approvals.review',()=>{const task=state.tasks.find(t=>t.id===a.operation.taskId);if(task)openConversation(task.conversationId,task.id);},{class:'button primary'})]),
    ])):h('p',{class:'list-empty muted'},t('approvals.noPending'))]):null,
    tab==='audit'?h('section',{class:'security-content'},[h('h2',t('security.audit')),state.audits.length?h('div',{class:'audit-list'},state.audits.map(event=>h('div',{class:'audit-row',key:event.id},[
      h('span',{class:'audit-dot','aria-hidden':'true'}),h('div',[h('strong',known('audit.'+event.code,'security.auditFallback')),h('code',{class:'micro'},event.code),event.taskId?taskLink(event.taskId):null,event.params?h('dl',{class:'audit-params'},Object.entries(event.params).filter(([,value])=>value!=='').map(([key,value])=>h('div',[h('dt',known('audit.fields.'+key,'common.details')),h('dd',key==='errorCode'?errorText(value):key==='reason'&&value==='user'?t('audit.user'):key==='created'?t(value==='true'?'common.yes':'common.no'):value)]))):null]),h('time',{class:'micro muted',datetime:event.createdAt},date(event.createdAt)),
    ]))):h('p',{class:'list-empty muted'},t('security.noAudit'))]):null,
    tab==='privacy'?privacy():null,
  ]));
}
