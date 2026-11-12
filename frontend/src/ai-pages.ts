import { defineComponent, h, ref, watch, nextTick, cloneVNode, isVNode, onMounted, onUnmounted, type VNodeChild } from 'vue';
import { ai, profile, model, selectProvider, selectModel, refreshAI, resetChatDraft, emptyModel, sendAI, cancelAI, importImages, removeAttachment, clearAI, type ChatSession } from './ai-store';
import { state, fail } from './store';
import { call } from './bridge';
import { ScreenCaptureActions, ScreenCaptureLabel, ScreenResult } from './screen-pages';
import { t, date, number } from './i18n';
import { button, errorText } from './ui';
import { Markdown } from './markdown';
import type { AIModel, AIProfile, AIUsageRecord, Capability } from './ai-types';

function field(key:string,control:VNodeChild){return h('label',{class:'ai-field'},[h('span',t('ai.'+key)),isVNode(control)?cloneVNode(control,{'aria-label':t('ai.'+key),'data-testid':control.props?.['data-testid']||'ai-'+key}):control]);}
function text(value:string,change:(value:string)=>void,options:Record<string,unknown>={}){return h('input',{value,onInput:(e:Event)=>change((e.target as HTMLInputElement).value),...options});}
function numeric(value:number|null,change:(value:number|null)=>void,options:Record<string,unknown>={}){return text(value===null?'':String(value),v=>change(v===''?null:Number(v)),{type:'number',min:0,step:'any',...options});}
function select(value:string,change:(value:string)=>void,options:[string,string][],disabled=false){return h('select',{value,disabled,onChange:(e:Event)=>change((e.target as HTMLSelectElement).value)},options.map(([id,label])=>h('option',{value:id},label)));}
function check(key:string,value:boolean,change:(value:boolean)=>void,disabled=false){return h('label',{class:'ai-checkbox'},[h('input',{type:'checkbox',checked:value,disabled,onChange:(e:Event)=>change((e.target as HTMLInputElement).checked)}),t('ai.'+key)]);}
function action(key:string,work:()=>unknown,disabled=false,primary=false){return button('ai.'+key,work,{class:['button',primary?'primary':'secondary'],disabled,'data-testid':'ai-'+key});}
const unknown=()=>t('common.unknown');
const measured=(n:number|null|undefined)=>n==null?unknown():number(n);
const capKeys=['text','vision','streaming','toolCalling','structuredOutput','computerUse','temperature'] as const;
function capabilities(m:AIModel|undefined){return h('dl',{class:'ai-capabilities'},capKeys.map(key=>h('div',[h('dt',t('ai.'+key)),h('dd',t('ai.'+(m?.capabilities[key]||'UNKNOWN')))])));}
export function AssistantSelection(){return h('div',{class:'ai-assistant-selection'},[field('assistantMode',select(ai.mode,value=>{ai.mode=value;state.inspectorOpen=value==='mock';},[['mock',t('ai.mock')],['real',t('ai.real')]])),ai.mode==='real'?h('span',{class:'micro muted'},t('ai.textVisionOnly')):null]);}
export function AISettingsPanel(){const s=ai.settings;const providers:[string,string][]=[['',t('ai.none')],...ai.profiles.map(p=>[p.id,p.name] as [string,string])];const models=(id:string):[string,string][]=>[['',t('ai.none')],...(ai.profiles.find(p=>p.id===id)?.models||[]).map(m=>[m.id,m.displayName||m.id] as [string,string])];return h('section',{class:'settings-section'},[h('h2',t('ai.defaults')),h('div',{class:'ai-form-grid'},[
  field('defaultProvider',select(s.defaultProviderId,v=>{s.defaultProviderId=v;s.defaultModelId='';},providers)),field('defaultModel',select(s.defaultModelId,v=>s.defaultModelId=v,models(s.defaultProviderId))),field('visionProvider',select(s.visionProviderId,v=>{s.visionProviderId=v;s.visionModelId='';},providers)),field('visionModel',select(s.visionModelId,v=>s.visionModelId=v,models(s.visionProviderId))),field('maxOutputTokens',numeric(s.maxOutputTokens,v=>s.maxOutputTokens=v||1,{min:1,max:65536,step:1})),field('maxContextBytes',numeric(s.maxContextBytes,v=>s.maxContextBytes=v||1024,{min:1024,max:1048576,step:1024})),field('warningThreshold',numeric(s.warningThreshold,v=>s.warningThreshold=v)),
  ]),h('p',{class:'micro muted'},t('ai.budgetNote')),action('saveSettings',async()=>{ai.saving=true;try{ai.settings=await call('SaveAISettings',s);state.notice='ai.saved';}catch(error){fail(error);}finally{ai.saving=false;}},ai.saving||!state.connected)]);}

export const AIProfilesPage=defineComponent({props:{embedded:Boolean},setup(props){
  const draft=ref<AIProfile|null>(null);const saving=ref(false);const secret=ref<HTMLInputElement|null>(null);onMounted(()=>{if(state.connected)void refreshAI().catch(fail);});onUnmounted(()=>{if(secret.value)secret.value.value='';});
  const create=()=>{draft.value={id:'',name:'',protocol:'responses',baseUrl:'https://api.openai.com/v1',enabled:true,trustedHttp:false,hasCredential:false,models:[emptyModel()]};};
  function deepSeekPreset() {
    if(secret.value)secret.value.value='';
    const m=emptyModel();
    m.id='deepseek-flash';
    m.displayName='deepseek-flash';
    m.capabilities.vision='SUPPORTED';
    m.capabilities.streaming='SUPPORTED';
    m.capabilities.structuredOutput='SUPPORTED';
    m.capabilities.contextLimit=1048576;
    m.capabilities.imageDetails=['auto','low','high','original'];
    // Conservative peak USD snapshot. Users can edit their configured prices.
    m.pricing={inputPerMillion:0.30,outputPerMillion:1.20,cachedInputPerMillion:0.006,currency:'USD',effectiveAt:'2026-11-11'};
    draft.value={id:'',name:'DeepSeek',protocol:'chat-completions',baseUrl:'https://api.deepseek.com',enabled:true,trustedHttp:false,hasCredential:false,models:[m]};
  }
  const save=async()=>{if(!draft.value||saving.value)return;if(draft.value.trustedHttp&&!window.confirm(t('ai.httpConfirm')))return;saving.value=true;try{const hadCredential=draft.value.hasCredential;draft.value=await call<AIProfile>('SaveAIProfile',{profile:draft.value});await refreshAI();state.notice=hadCredential&&!draft.value.hasCredential?'ai.endpointCredentialReset':'ai.saved';}catch(error){fail(error);}finally{saving.value=false;}};
  async function storeSecret(){if(saving.value||!draft.value?.id||!secret.value?.value)return;const input=secret.value;const providerId=draft.value.id;saving.value=true;try{const promise=call<{hasCredential:boolean}>('StoreCredential',{providerId,secret:input.value});input.value='';const result=await promise;if(draft.value?.id===providerId)draft.value.hasCredential=result.hasCredential;await refreshAI();state.notice='ai.credentialSaved';}catch(error){fail(error);}finally{input.value='';saving.value=false;}}
  const edit=(p:AIProfile)=>{if(secret.value)secret.value.value='';draft.value=JSON.parse(JSON.stringify(p));};
  function modelEditor(m:AIModel,index:number){const c=m.capabilities;return h('section',{class:'ai-model-editor',key:index},[h('div',{class:'ai-row'},[h('h3',t('ai.modelNumber',{number:index+1})),action('removeModel',()=>draft.value!.models.splice(index,1),saving.value)]),h('div',{class:'ai-form-grid'},[
    field('modelId',text(m.id,v=>m.id=v)),field('displayName',text(m.displayName,v=>m.displayName=v)),...capKeys.map(key=>field(key,select(c[key],v=>c[key]=v as Capability,['SUPPORTED','UNSUPPORTED','UNKNOWN'].map(v=>[v,t('ai.'+v)]),key==='computerUse'))),field('contextLimit',numeric(c.contextLimit,v=>c.contextLimit=v,{step:1,min:1})),field('imageDetails',text(c.imageDetails.join(', '),v=>c.imageDetails=v.split(',').map(x=>x.trim()).filter(Boolean))),
  ]),h('p',{class:'micro muted'},t('ai.capabilityNote')),check('configurePricing',!!m.pricing,v=>m.pricing=v?{inputPerMillion:0,outputPerMillion:0,cachedInputPerMillion:null,currency:'USD',effectiveAt:new Date().toISOString().slice(0,10)}:null),m.pricing?h('div',{class:'ai-form-grid'},[
    field('inputPrice',numeric(m.pricing.inputPerMillion,v=>m.pricing!.inputPerMillion=v??0)),field('outputPrice',numeric(m.pricing.outputPerMillion,v=>m.pricing!.outputPerMillion=v??0)),field('cachedPrice',numeric(m.pricing.cachedInputPerMillion,v=>m.pricing!.cachedInputPerMillion=v)),field('currency',text(m.pricing.currency,v=>m.pricing!.currency=v)),field('effectiveAt',text(m.pricing.effectiveAt,v=>m.pricing!.effectiveAt=v)),
  ]):h('p',{class:'micro muted'},t('ai.priceUnknown'))]);}
  return ()=>{const p=draft.value;const editor=h('div',{class:props.embedded?'ai-profiles':'page ai-profiles'},[
    h('header',{class:'page-heading'},[props.embedded?null:h('div',[h('h1',t('ai.providers')),h('p',t('ai.providersNote'))]),h('div',{class:'ai-row'},[action('refresh',()=>refreshAI().catch(fail),!state.connected),props.embedded?null:action('deepSeekPreset',deepSeekPreset,saving.value||!state.connected),action('addProvider',create,!state.connected)])]),
    h('div',{class:'ai-profile-list'},ai.profiles.length?ai.profiles.map(p=>h('button',{class:'history-conversation',onClick:()=>edit(p),'data-testid':'ai-profile-'+p.id},[h('strong',p.name),h('span',p.protocol),h('span',t(p.hasCredential?'ai.keyStored':'ai.noKey')),h('span',t(p.enabled?'ai.enabled':'ai.disabled'))])):h('p',t('ai.noProviders'))),
    p?h('section',{class:'settings-section'},[h('h2',t('ai.editProvider')),h('div',{class:'ai-form-grid'},[
      field('providerName',text(p.name,v=>p.name=v)),
      field('protocol',select(p.protocol,v=>p.protocol=v,[['responses',t('ai.responses')],['chat-completions',t('ai.chatCompletions')]])),
      field('baseUrl',text(p.baseUrl,v=>p.baseUrl=v,{type:'url',placeholder:'https://api.example.com/v1'})),
      props.embedded?h('label',{class:'ai-field'},[
        h('span',t('communitySetup.timeout')),
        numeric(p.timeoutSeconds??null,v=>p.timeoutSeconds=v??undefined,{min:1,max:120,step:1,'data-testid':'ai-timeoutSeconds'}),
        h('small',{class:'muted'},t('communitySetup.timeoutNote')),
      ]):null,
      check('enabled',p.enabled,v=>p.enabled=v),
      check('trustedHttp',p.trustedHttp,v=>p.trustedHttp=v),
    ]),h('p',{class:'micro muted'},t('ai.endpointNote')),
      ...p.models.map(modelEditor),action('addModel',()=>p.models.push(emptyModel()),saving.value),h('div',{class:'ai-row ai-actions'},[action('saveProvider',save,saving.value||!p.name||!p.models.length||p.models.some(m=>!m.id),true),p.id?action('deleteProvider',async()=>{if(!window.confirm(t('ai.deleteProviderConfirm')))return;try{await call('DeleteAIProfile',{id:p.id,confirm:true});draft.value=null;await refreshAI();}catch(error){fail(error);}},saving.value):null]),
      p.id?h('section',{class:'ai-vault'},[h('h3',t('ai.vault')),h('p',t(p.hasCredential?'ai.keyStored':'ai.noKey')),field('apiKey',h('input',{key:p.id,ref:secret,type:'password',autocomplete:'new-password',spellcheck:false,'data-testid':'api-key',onChange:storeSecret})),h('p',{class:'micro muted'},t('ai.keyHint')),h('div',{class:'ai-row'},[action('saveKey',storeSecret,saving.value),action('deleteKey',async()=>{if(!window.confirm(t('ai.deleteKeyConfirm')))return;try{const result=await call<{hasCredential:boolean}>('DeleteCredential',{providerId:p.id});p.hasCredential=result.hasCredential;await refreshAI();}catch(error){fail(error);}},saving.value||!p.hasCredential),action('testConnection',async()=>{const s=ai.playground;if(s.busy||s.starting)return;resetChatDraft(s);selectProvider(s,p.id);s.conversation=null;await sendAI(s,true,true);state.route='playground';},saving.value||ai.playground.busy||ai.playground.starting)]),h('p',{class:'micro muted'},t('ai.vaultNote'))]):h('p',t('ai.saveBeforeKey')),
    ]):null,props.embedded?null:AISettingsPanel(),
  ]);return props.embedded?editor:h('div',{class:'page-scroll'},editor);};
}});

function chatControls(s:ChatSession){const p=profile(s),m=model(s);const disabled=s.busy||s.starting;const details=m?.capabilities.imageDetails||[];return h('div',{class:'ai-form-grid'},[
  field('provider',select(s.providerId,v=>selectProvider(s,v),[['',t('ai.selectProvider')],...ai.profiles.filter(p=>p.enabled).map(p=>[p.id,p.name] as [string,string])],disabled)),field('model',select(s.modelId,v=>selectModel(s,v),(p?.models||[]).map(m=>[m.id,m.displayName||m.id]),disabled)),field('baseUrl',h('code',{class:'ai-base-url'},p?.baseUrl||unknown())),field('maxOutputTokens',numeric(s.maxOutputTokens,v=>s.maxOutputTokens=v||1,{min:1,max:65536,step:1,disabled})),field('temperature',numeric(s.temperature,v=>s.temperature=v,{min:0,max:2,step:0.1,disabled:disabled||m?.capabilities.temperature!=='SUPPORTED'})),field('imageDetail',select(s.imageDetail,v=>s.imageDetail=v,details.map(v=>[v,t('ai.detail'+v)]),disabled||!details.length)),check('streaming',s.stream,v=>s.stream=v,disabled||m?.capabilities.streaming!=='SUPPORTED'),ai.settings.visionProviderId&&ai.settings.visionModelId?action('useVisionDefault',()=>{selectProvider(s,ai.settings.visionProviderId);selectModel(s,ai.settings.visionModelId);},disabled):null,
]);}
function attachments(items:import('./ai-types').AIAttachment[],remove?:(id:string)=>unknown){return h('div',{class:'ai-attachments'},items.map(a=>h('figure',{key:a.id},[a.previewDataUrl?h('img',{src:a.previewDataUrl,alt:a.name,loading:'lazy'}):h('span',t('ai.image')),h('figcaption',[h('span',a.name),h('small',`${a.width} × ${a.height} · ${number(a.bytes)} B`)]),remove?action('removeImage',()=>remove(a.id)):null])));}
function messages(s: ChatSession) {
  const items=s.conversation?.messages||[];
  const screenRequests=new Set(items.filter(message=>message.screenCapture).map(message=>message.requestId||message.screenCapture!.requestId));
  return items.map(message=>{
    const screenResult=message.role==='assistant'&&(message.vision||message.requestId&&
      (s.screenRequestId===message.requestId||screenRequests.has(message.requestId)));
    const pending=s.busy&&s.requestId===message.requestId;
    const usage=message.usage;
    const record=screenResult?ai.usage.find(entry=>entry.requestId===message.requestId):undefined;
    return h('article',{class:['message','message-'+message.role],key:message.id,'data-message-id':message.id},[
      h('div',{class:'message-body'},[
        h('header',[h('strong',t(message.role==='user'?'assistant.you':'assistant.agent')),h('time',date(message.createdAt))]),
        h('p',{class:'micro muted'},[message.providerId||'',message.modelId?' · '+message.modelId:'',message.requestId?' · '+message.requestId:'']),
        message.attachments?.length?attachments(message.attachments):null,
        message.screenCapture?ScreenCaptureLabel(message.screenCapture):null,
        screenResult?(pending||message.vision||message.content?ScreenResult(message.vision,message.content,pending):null):h(Markdown,{content:message.content}),
        message.status&&message.role==='assistant'?h('p',{class:'micro muted'},t('ai.status',{status:localizedStatus(message.status)})):null,
        message.errorCode?h('p',{class:'ai-error',role:'alert'},errorText(message.errorCode)):null,
        screenResult&&message.status&&!pending?h('details',{class:'screen-result-usage'},[
          h('summary',t('navigation.usage')),metrics(record,message.requestId===s.requestId?s:undefined),
        ]):usage?h('p',{class:'micro muted'},t('ai.messageUsage',{input:measured(usage.inputTokens),output:measured(usage.outputTokens),source:usageSource(usage.source)})):null,
      ]),
    ]);
  });
}
function localizedStatus(status:string){const key='ai.status'+status.toUpperCase();const value=t(key);return value===key?unknown():value;}
function usageSource(source:string){const key='ai.source'+source;const value=t(key);return value===key?unknown():value;}
function composer(s:ChatSession,playground:boolean){const m=model(s);const canVision=m?.capabilities.vision==='SUPPORTED';return h('form',{class:'composer-wrap',onSubmit:(e:SubmitEvent)=>{e.preventDefault();void sendAI(s,playground);}},[s.attachments.length?attachments(s.attachments,id=>removeAttachment(s,id)):null,h('div',{class:'composer-box'},[h('textarea',{rows:3,value:s.draft,'aria-label':t('ai.prompt'),'data-testid':'ai-composer',placeholder:t('ai.prompt'),disabled:!state.connected||s.busy,onInput:(e:Event)=>s.draft=(e.target as HTMLTextAreaElement).value}),h('div',{class:'composer-bottom ai-row'},[field(playground?'addImage':'attachImage',h('input',{type:'file',accept:'image/png,image/jpeg,image/webp,image/gif',multiple:true,disabled:!canVision||s.busy||s.importing||!state.connected,onChange:(e:Event)=>importImages(s,e.target as HTMLInputElement),'data-testid':playground?'playground-images':'assistant-images'})),playground?null:h(ScreenCaptureActions,{source:'assistant'}),s.busy?action('cancel',()=>cancelAI(s),!s.requestId):h('button',{type:'submit',class:'button primary',disabled:!state.connected||s.importing||s.starting||!profile(s)?.enabled||m?.capabilities.text!=='SUPPORTED'||(!s.draft.trim()&&!s.attachments.length)||(!!s.attachments.length&&!canVision),'data-testid':playground?'playground-send':'real-send'},t('ai.send'))])]),s.importing?h('p',{role:'status'},t('ai.importing')):null,s.error?h('p',{class:'ai-error',role:'alert'},errorText(s.error)):null,h('p',{class:'input-hint'},t('ai.privacyNotice'))]);}

export const RealAssistant=defineComponent({setup(){const scroll=ref<HTMLElement|null>(null);watch(()=>ai.assistant.conversation?.messages.map(m=>m.content).join(''),async()=>{await nextTick();if(scroll.value)scroll.value.scrollTop=scroll.value.scrollHeight;});return ()=>{const s=ai.assistant;return h('section',{class:'assistant-workspace ai-chat','data-testid':'ai-assistant'},[h('header',{class:'conversation-header'},[h('div',[h('h1',s.conversation?.title||t('ai.real')),h('p',{class:'micro muted'},t('ai.textVisionOnly'))]),h('div',{class:'ai-row'},[action('clear',()=>clearAI(s),s.busy||!s.conversation),action('deleteConversation',()=>clearAI(s,true),s.busy||!s.conversation)])]),h('details',{class:'ai-chat-options',open:!s.conversation},[h('summary',t('ai.configuration')),chatControls(s)]),h('div',{class:'message-scroll',ref:scroll,'aria-live':'polite'},s.conversation?.messages.length?messages(s):[h('div',{class:'list-empty'},[h('h2',t('ai.ready')),h('p',t('ai.noAutoSend'))])]),composer(s,false)]);};}});

function metrics(record:AIUsageRecord|undefined,s?:ChatSession){const response=s?.response;const u=record?.usage||response?.usage;return h('dl',{class:'ai-metrics'},[
  ['provider',record?.providerId||s?.providerId||unknown()],['model',record?.modelId||s?.modelId||unknown()],['requestId',record?.requestId||s?.requestId||unknown()],['httpStatus',measured(record?.httpStatus||response?.httpStatus||null)],['firstTokenLatency',measured(record?.firstTokenLatencyMs??response?.firstTokenLatencyMs)],['duration',measured(record?.durationMs??response?.durationMs)],['inputTokens',measured(u?.inputTokens)],['outputTokens',measured(u?.outputTokens)],['totalTokens',measured(u?.totalTokens)],['cachedTokens',measured(u?.cachedInputTokens)],['reasoningTokens',measured(u?.reasoningTokens)],['usageSource',u?usageSource(u.source):unknown()],['estimatedCost',record?.estimatedCost==null?unknown():`${record.estimatedCost.toFixed(6)} ${record.pricing?.currency||unknown()}`],['requestStatus',record?localizedStatus(record.status):response?localizedStatus(response.status):unknown()],
].map(([key,value])=>h('div',[h('dt',t('ai.'+key)),h('dd',value)])));}
export function PlaygroundPage(){const s=ai.playground;const latest=ai.usage.find(x=>x.requestId===s.requestId);return h('div',{class:'page-scroll'},h('div',{class:'page ai-playground','data-testid':'ai-playground'},[h('header',{class:'page-heading'},[h('div',[h('h1',t('navigation.playground')),h('p',t('ai.playgroundNote'))])]),h('div',{class:'ai-playground-grid'},[h('aside',{class:'settings-section'},[h('h2',t('ai.configuration')),chatControls(s),capabilities(model(s))]),h('section',{class:'ai-playground-chat'},[h('div',{class:'ai-row'},[action('clear',()=>clearAI(s),s.busy||!s.conversation),action('deleteConversation',()=>clearAI(s,true),s.busy||!s.conversation)]),h('div',{class:'ai-playground-messages','aria-live':'polite'},messages(s)),composer(s,true)]),h('aside',{class:'settings-section'},[h('h2',t('ai.results')),metrics(latest,s)])]) ]));}

function aggregate(records:AIUsageRecord[]){const sum=(key:'inputTokens'|'outputTokens'|'totalTokens')=>{const known=records.filter(r=>r.usage[key]!=null);return `${known.length||!records.length?number(known.reduce((n,r)=>n+(r.usage[key]||0),0)):''}${known.length<records.length?(known.length?' + ':'')+t('ai.unknownRecords',{count:records.length-known.length}):''}`;};const currencies=new Map<string,{cost:number;unknown:number;known:number}>();for(const r of records){const currency=r.pricing?.currency||unknown();const group=currencies.get(currency)||{cost:0,unknown:0,known:0};if(r.estimatedCost==null)group.unknown++;else {group.cost+=r.estimatedCost;group.known++};currencies.set(currency,group);}return h('dl',{class:'ai-metrics ai-summary'},[
  ['requests',number(records.length)],['inputTokens',sum('inputTokens')],['outputTokens',sum('outputTokens')],['totalTokens',sum('totalTokens')],['estimatedCost',[...currencies].map(([currency,g])=>`${g.known?g.cost.toFixed(6)+' '+currency:''}${g.unknown?(g.known?' + ':'')+t('ai.unknownRecords',{count:g.unknown})+' '+currency:''}`).join(' · ')||unknown()],
].map(([key,value])=>h('div',[h('dt',t('ai.'+key)),h('dd',value)])));}
function groups(key:'providerId'|'modelId'){const grouped=new Map<string,AIUsageRecord[]>();for(const r of ai.usage){const id=key==='modelId'?`${r.providerId} / ${r.modelId}`:r.providerId;grouped.set(id,[...(grouped.get(id)||[]),r]);}return [...grouped].map(([id,records])=>h('details',[h('summary',id),aggregate(records)]));}
export function UsagePage(){const today=new Date().toDateString();const current=ai.usage.filter(r=>new Date(r.createdAt).toDateString()===today);return h('div',{class:'page-scroll'},h('div',{class:'page ai-usage','data-testid':'ai-usage'},[
  h('header',{class:'page-heading'},[h('div',[h('h1',t('navigation.usage')),h('p',t('ai.usageNote'))]),action('refresh',()=>refreshAI().catch(fail),!state.connected)]),h('section',{class:'settings-section'},[h('h2',t('ai.today')),aggregate(current)]),h('section',{class:'settings-section'},[h('h2',t('ai.allTime')),aggregate(ai.usage)]),h('section',{class:'settings-section'},[h('h2',t('ai.byProvider')),groups('providerId')]),h('section',{class:'settings-section'},[h('h2',t('ai.byModel')),groups('modelId')]),h('section',{class:'settings-section'},[h('h2',t('ai.requestHistory')),ai.usage.length?[...ai.usage].sort((a,b)=>b.createdAt.localeCompare(a.createdAt)).map(r=>h('details',{key:r.requestId},[h('summary',`${date(r.createdAt)} · ${r.providerId} / ${r.modelId} · ${localizedStatus(r.status)}`),metrics(r),r.errorCode?h('p',{class:'ai-error'},errorText(r.errorCode)):null])):h('p',t('ai.noUsage'))]),
 ]));}
