import { defineComponent, h, onUnmounted, reactive, ref, watch, type PropType, type VNodeChild } from 'vue';
import { call, subscribe } from './bridge';
import { date, number, t } from './i18n';
import { state, openConversation } from './store';
import { ai } from './ai-store';
import { product } from './product-store';
import { button, errorText, icon } from './ui';
import type { CognitionSkill, Connection, ConnectionKind, ConnectorAction, EvolutionInsights, GoalKind, MemoryMaintenance, SkillCandidate, UnifiedRun, UnifiedSummary } from './unified-agent-types';

const textValue = (event:Event) => (event.target as HTMLInputElement).value;
const pretty = (value:unknown) => typeof value === 'string' ? value : JSON.stringify(value, null, 2);
const label = (key:string, input:VNodeChild) => h('label', {class:'product-field'}, [h('span',t('unified.'+key)),input]);
const status = (value:string) => h('span',{class:['badge','state-'+value.toLowerCase()]},t('unified.status.'+value));
const control = (key:string, action:()=>unknown, disabled=false, id?:string, primary=false) => button('unified.'+key, action, {class:['button',primary?'primary':'secondary'],disabled:disabled||!state.connected,'data-testid':id});
function readableResult(value:unknown,id?:string) {
  if(!value||typeof value!=='object'||Array.isArray(value))return h('pre',pretty(value));
  const facts=value as Record<string,unknown>;
  const name=typeof facts.full_name==='string'?facts.full_name:typeof facts.path_with_namespace==='string'?facts.path_with_namespace:undefined;
  if(!name){return typeof facts.content==='string'?h('div',[h('p',facts.content),h('details',[h('summary',t('unified.rawResult')),h('pre',pretty(value))])]):h('pre',pretty(value));}
  return h('section',{'data-testid':id},[
    h('h4',name),typeof facts.description==='string'?h('p',facts.description):null,
    h('div',{class:'product-row'},[
      typeof facts.language==='string'?h('span',t('unified.repositoryLanguage',{language:facts.language})):null,
      typeof facts.stargazers_count==='number'?h('span',t('unified.repositoryStars',{count:facts.stargazers_count})):null,
      typeof facts.forks_count==='number'?h('span',t('unified.repositoryForks',{count:facts.forks_count})):null,
    ]),typeof facts.html_url==='string'?h('code',facts.html_url):null,
    h('details',[h('summary',t('unified.rawResult')),h('pre',pretty(value))]),
  ]);
}
const errorCode = (error:unknown) => error && typeof error==='object' && 'code' in error ? String(error.code) : error instanceof Error ? error.message : String(error);

// A disconnected or superseded read cannot replace the current review state.
function useLive<T>(read:()=>Promise<T>) {
  const data=ref<T>(),loading=ref(false),busy=ref(false),error=ref('');
  let version=0,off:(()=>void)|undefined,timer:ReturnType<typeof setInterval>|undefined,debounce:ReturnType<typeof setTimeout>|undefined;
  async function load() {
    if(!state.connected)return;
    const current=++version;loading.value=true;
    try {const value=await read();if(current===version){data.value=value;if(!busy.value)error.value='';}}
    catch(reason){if(current===version)error.value=errorCode(reason);}
    finally{if(current===version)loading.value=false;}
  }
  async function work(action:()=>Promise<unknown>) {
    if(busy.value||!state.connected)return;
    busy.value=true;error.value='';
    try {await action();await load();}catch(reason){error.value=errorCode(reason);await load();}finally{busy.value=false;}
  }
  async function interrupt(action:()=>Promise<unknown>) {
    if(!state.connected)return;
    try{await action();await load();}catch(reason){error.value=errorCode(reason);}
  }
  function stop(){version++;off?.();off=undefined;clearInterval(timer);clearTimeout(debounce);}
  watch(()=>state.connected,connected=>{
    stop();data.value=undefined;
    if(connected){void load();off=subscribe('asimi:changed',()=>{clearTimeout(debounce);debounce=setTimeout(()=>void load(),120);});timer=setInterval(()=>void load(),3000);}
  },{immediate:true});
  onUnmounted(stop);
  return {data,loading,busy,error,load,work,interrupt};
}
function remoteState(loading:boolean,error:string) {
  return [loading?h('p',{class:'micro',role:'status'},t('common.loading')):null,error?h('p',{class:'ai-error',role:'alert'},errorText(error)):null,!state.connected?h('p',{role:'status'},t('app.connecting')):null];
}
function workspacePicker(value:string,change:(value:string)=>void,disabled=false) {
  return label('workspace',h('select',{value,disabled,'data-testid':'unified-workspace',onChange:(e:Event)=>change(textValue(e))},[
    h('option',{value:''},t('unified.personal')),...product.data.workspaces.map(w=>h('option',{value:w.id},w.name)),
  ]));
}
function modelPicker(disabled:boolean) {
  const profile=ai.profiles.find(p=>p.id===ai.assistant.providerId);
  return h('div',{class:'product-row'},[
    label('provider',h('select',{value:ai.assistant.providerId,disabled,onChange:(e:Event)=>{ai.assistant.providerId=textValue(e);ai.assistant.modelId='';}},[
      h('option',{value:''},t('unified.noProvider')),...ai.profiles.filter(p=>p.enabled).map(p=>h('option',{value:p.id},p.name)),
    ])),label('model',h('select',{value:ai.assistant.modelId,disabled,onChange:(e:Event)=>{ai.assistant.modelId=textValue(e);}},[
      h('option',{value:''},t('unified.chooseModel')),...(profile?.models??[]).filter(m=>m.capabilities.text==='SUPPORTED').map(m=>h('option',{value:m.id},m.displayName||m.id)),
    ])),
  ]);
}

const ConnectorActionCard = defineComponent({
  props:{action:{type:Object as PropType<ConnectorAction>,required:true},connection:Object as PropType<Connection>,busy:Boolean},
  emits:['approve','reject','cancel'],
  setup(props,{emit}) {
    const consent=ref(false),consumed=ref(false),cancelling=ref(false);
    watch(()=>[props.action.id,props.action.hash,props.connection?.revision],()=>{consent.value=false;});
    const valid=()=>props.action.status==='NEEDS_APPROVAL'&&Date.parse(props.action.expiresAt)>Date.now()&&props.connection?.enabled&&props.connection.revision===props.action.connectionRevision&&!consumed.value;
    function decide(approve:boolean){if(props.busy||!state.connected||consumed.value||approve&&(!valid()||!consent.value))return;consumed.value=true;consent.value=false;emit(approve?'approve':'reject',props.action);}
    return ()=>{const a=props.action;return h('article',{class:'product-card unified-action','data-testid':'connector-action-'+a.id},[
      h('div',{class:'split'},[h('h3',t('unified.externalAction')),status(a.status)]),
      h('dl',{class:'product-details'},[
        h('dt',t('unified.account')),h('dd',props.connection?.name||a.connectionId),
        h('dt',t('unified.target')),h('dd',[h('code',a.method+' '+a.origin+a.path)]),
        h('dt',t('unified.resource')),h('dd',a.resource),h('dt',t('unified.operation')),h('dd',a.operation),
        h('dt',t('unified.revision')),h('dd',String(a.connectionRevision)),h('dt',t('unified.expires')),h('dd',date(a.expiresAt)),
      ]),
      h('details',{open:a.status==='NEEDS_APPROVAL'},[h('summary',t('unified.exactRequest')),h('pre',pretty(a.request)),h('code',{class:'product-hash'},a.hash)]),
      a.status==='NEEDS_APPROVAL'?[
        !valid()&&!consumed.value?h('p',{class:'product-warning'},t('unified.staleApproval')):null,
        h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:consent.value,disabled:props.busy||!valid(),'data-testid':'connector-consent-'+a.id,onChange:(e:Event)=>{consent.value=(e.target as HTMLInputElement).checked;}}),t('unified.networkConsent')]),
        h('div',{class:'product-row'},[control('approve',()=>decide(true),props.busy||!consent.value||!valid(),'connector-approve-'+a.id,true),control('reject',()=>decide(false),props.busy||consumed.value,'connector-reject-'+a.id)]),
      ]:null,
      a.status==='RUNNING'?control('cancel',()=>{if(!cancelling.value&&state.connected){cancelling.value=true;emit('cancel',a);}},cancelling.value,'connector-cancel-'+a.id):null,
      ['UNKNOWN','RECOVERY_REQUIRED'].includes(a.status)?h('p',{class:'product-warning','data-testid':'connector-unknown-'+a.id},t('unified.noReplay')):null,
      a.errorCode?h('p',{class:'ai-error',role:'alert'},errorText(a.errorCode)):null,
      a.result!==undefined?h('div',{'data-testid':'connector-result-'+a.id},[h('h4',t(a.verified?'unified.verified':'unified.unverified')),h('p',{class:'micro'},t('unified.untrusted')),readableResult(a.result,'connector-readable-'+a.id)]):null,
    ]);};
  },
});
function actionCards(actions:ConnectorAction[],connections:Connection[],busy:boolean,work:(fn:()=>Promise<unknown>)=>Promise<void>,interrupt:(fn:()=>Promise<unknown>)=>Promise<void>) {
  return actions.map(action=>h(ConnectorActionCard,{key:action.id,action,connection:connections.find(c=>c.id===action.connectionId),busy,
    onApprove:(a:ConnectorAction)=>work(()=>call('ApproveConnectorAction',{id:a.id,hash:a.hash})),
    onReject:(a:ConnectorAction)=>work(()=>call('RejectConnectorAction',{id:a.id,hash:a.hash})),
    onCancel:(a:ConnectorAction)=>interrupt(()=>call('CancelConnectorAction',{id:a.id})),
  }));
}

export const UnifiedAgentPage=defineComponent({setup(){
  const goal=ref(''),kind=ref<GoalKind>('auto'),paths=ref(''),confirm=ref(false),preview=ref<UnifiedSummary>(),summaryConsent=ref(false);
  const remote=useLive(async()=>{const [runs,actions,connections]=await Promise.all([call<UnifiedRun[]>('GetUnifiedRuns'),call<ConnectorAction[]>('ListConnectorActions'),call<Connection[]>('ListConnections')]);return {runs:runs??[],actions:actions??[],connections:connections??[]};});
  watch([goal,kind,paths,()=>product.selectedWorkspace,()=>ai.assistant.providerId,()=>ai.assistant.modelId],()=>{confirm.value=false;preview.value=undefined;summaryConsent.value=false;});
  watch(()=>state.connected,()=>{confirm.value=false;preview.value=undefined;summaryConsent.value=false;});
  const submit=()=>remote.work(async()=>{
    const run=await call<UnifiedRun>('SubmitAgentGoal',{goal:goal.value.trim(),kind:kind.value,providerId:ai.assistant.providerId||undefined,modelId:ai.assistant.modelId||undefined,workspaceId:product.selectedWorkspace||undefined,paths:paths.value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),...(confirm.value?{confirmSend:true}:{})});
    if(run){goal.value='';confirm.value=false;}
  });
  const reviewSummary=(run:UnifiedRun)=>remote.work(async()=>{summaryConsent.value=false;preview.value=await call<UnifiedSummary>('PreviewUnifiedSummary',{id:run.id,providerId:ai.assistant.providerId||undefined,modelId:ai.assistant.modelId||undefined});});
  const sendSummary=()=>remote.work(async()=>{const p=preview.value;if(!p||!summaryConsent.value)return;summaryConsent.value=false;preview.value=undefined;await call('SummarizeUnifiedRun',{id:p.id,contextHash:p.contextHash,providerId:p.providerId,modelId:p.modelId,confirmSend:true});});
  return ()=>{const {data,busy,loading,error}=remote;const modelReady=!!ai.assistant.providerId&&!!ai.assistant.modelId;const needsModel=['chat','development'].includes(kind.value);
    return h('section',{class:'product-page unified-page','data-testid':'unified-page'},[
      h('header',{class:'product-heading'},[h('span',{class:'eyebrow'},t('unified.eyebrow')),h('h1',t('unified.goals')),h('p',t('unified.intro'))]),
      h('section',{class:'product-card unified-compose'},[
        label('goal',h('textarea',{value:goal.value,rows:3,maxlength:16000,'data-testid':'unified-goal',placeholder:t('unified.goalPlaceholder'),disabled:busy.value,onInput:(e:Event)=>{goal.value=textValue(e);}})),
        h('div',{class:'product-row'},[label('taskType',h('select',{value:kind.value,disabled:busy.value,'data-testid':'unified-kind',onChange:(e:Event)=>{kind.value=textValue(e) as GoalKind;}},(['auto','chat','development','desktop','communication'] as const).map(k=>h('option',{value:k},t('unified.kind.'+k))))),workspacePicker(product.selectedWorkspace,value=>{product.selectedWorkspace=value;},busy.value)]),
        h('p',{class:'micro muted'},t('unified.routeHint')),
        h('details',[h('summary',t('unified.modelOptions')),modelPicker(busy.value),label('paths',h('textarea',{value:paths.value,rows:2,disabled:busy.value,onInput:(e:Event)=>{paths.value=textValue(e);}})),
          h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:confirm.value,disabled:busy.value||!modelReady,'data-testid':'unified-model-consent',onChange:(e:Event)=>{confirm.value=(e.target as HTMLInputElement).checked;}}),t('unified.modelConsent',{provider:ai.profiles.find(p=>p.id===ai.assistant.providerId)?.name||'—',model:ai.assistant.modelId||'—'})]),
        ]),
        kind.value==='desktop'?h('p',{class:'product-warning'},t('unified.observeFirst')):null,
        h('div',{class:'product-row'},[
          control('submit',submit,busy.value||!goal.value.trim()||kind.value==='desktop'||needsModel&&(!modelReady||!confirm.value)||kind.value==='development'&&!product.selectedWorkspace,'unified-submit',true),
          control('connections',()=>{state.route='connections';},busy.value),control('computer',()=>{state.route='computer';},busy.value),control('refresh',remote.load,busy.value,'unified-refresh'),
        ]),
      ]),...remoteState(loading.value,error.value),
      data.value&&!data.value.runs.length?h('p',{class:'product-empty','data-testid':'unified-empty'},t('unified.noGoals')):null,
      ...(data.value?.runs??[]).map(run=>h('article',{class:'product-card unified-run',key:run.id,'data-testid':'unified-run-'+run.id},[
        h('div',{class:'split'},[h('h2',run.goal),status(run.status)]),h('p',{class:'micro'},t('unified.kind.'+run.kind)+' · '+date(run.updatedAt)),
        run.error?h('p',{class:'ai-error',role:'alert'},errorText(run.error)):null,
        ['UNKNOWN','UNKNOWN_RESULT','RECOVERY_REQUIRED'].includes(run.status)?h('p',{class:'product-warning'},t('unified.noReplay')):null,
        run.result!==undefined?h('details',{open:true},[h('summary',t('unified.result')),readableResult(run.result)]):null,
        ...actionCards((data.value?.actions??[]).filter(a=>a.id===run.actionId),data.value?.connections??[],busy.value,remote.work,remote.interrupt),
        h('div',{class:'product-row'},[
          !['SUCCEEDED','COMPLETED','FAILED','CANCELLED','REJECTED','EXPIRED','UNKNOWN','UNKNOWN_RESULT','RECOVERY_REQUIRED'].includes(run.status)?control('cancel',()=>remote.work(()=>call('CancelUnifiedRun',{id:run.id})),busy.value,'unified-cancel-'+run.id):null,
          run.kind==='development'&&run.taskId?control('reviewDevelopment',()=>{product.selectedTask=run.taskId!;state.route='workflows';},busy.value,'unified-development-'+run.id):null,
          run.kind==='desktop'?control('computer',()=>{state.route='computer';},busy.value):null,
          run.conversationId?control('conversation',()=>openConversation(run.conversationId!),busy.value):null,
          run.kind==='communication'&&['SUCCEEDED','COMPLETED'].includes(run.status)?control('previewSummary',()=>reviewSummary(run),busy.value||!modelReady,'unified-preview-'+run.id):null,
        ]),
      ])),
      preview.value?h('section',{class:'product-card unified-summary','data-testid':'unified-summary'},[
        h('h2',t('unified.reviewSummary')),h('p',t('unified.summaryDestination',{provider:preview.value.providerId,model:preview.value.modelId})),h('pre',preview.value.content),h('code',{class:'product-hash'},preview.value.contextHash),
        h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:summaryConsent.value,'data-testid':'unified-summary-consent',disabled:busy.value,onChange:(e:Event)=>{summaryConsent.value=(e.target as HTMLInputElement).checked;}}),t('unified.summaryConsent')]),
        h('div',{class:'product-row'},[control('sendSummary',sendSummary,busy.value||!summaryConsent.value,'unified-summary-send',true),control('close',()=>{preview.value=undefined;summaryConsent.value=false;},busy.value)]),
      ]):null,
      h(EvolutionPanel),
    ]);
  };
}});

const catalog:Record<ConnectionKind,{origin:string;operations:string[];resource:string}>={
  github:{origin:'https://api.github.com',operations:['repo.read','issues.list','issues.create','identity.read'],resource:'owner/repository'},
  gitlab:{origin:'https://gitlab.com',operations:['repo.read','issues.list','issues.create','identity.read'],resource:'owner/repository'},
  graph:{origin:'https://graph.microsoft.com',operations:['mail.list','mail.send','calendar.list','calendar.create','identity.read'],resource:'me'},
  slack:{origin:'https://slack.com',operations:['messages.list','messages.send','identity.read'],resource:'C0123456789'},
  mcp:{origin:'',operations:['tools.list','tools.call'],resource:'$catalog'},
};
const ConnectionOperationPanel=defineComponent({
  props:{connection:{type:Object as PropType<Connection>,required:true},busy:Boolean},emits:['prepare'],
  setup(props,{emit}) {
    const scope=ref(0),operation=ref(props.connection.scopes[0]?.operations[0]??'');
    const values=reactive({title:'',body:'',subject:'',to:'',start:'',end:'',text:'',arguments:'{}'});
    function reset(){Object.assign(values,{title:'',body:'',subject:'',to:'',start:'',end:'',text:'',arguments:'{}'});}
    watch(()=>props.connection.revision,()=>{scope.value=0;operation.value=props.connection.scopes[0]?.operations[0]??'';reset();});
    watch(operation,reset);
    const argumentsObject=()=>{try{const value=JSON.parse(values.arguments);return value&&typeof value==='object'&&!Array.isArray(value)?value:null;}catch{return null;}};
    const dateTime=(value:string)=>value.length===16?value+':00':value;
    function request():Record<string,unknown>{switch(operation.value){
      case 'issues.create':return {title:values.title,body:values.body};
      case 'mail.send':return {to:values.to,subject:values.subject,body:values.body};
      case 'calendar.create':return {subject:values.subject,start:dateTime(values.start),end:dateTime(values.end),timeZone:'UTC'};
      case 'messages.send':return {text:values.text};
      case 'tools.call':return {arguments:argumentsObject()};
      default:return {};
    }}
    function valid(){switch(operation.value){case 'issues.create':return !!values.title.trim();case 'mail.send':return !!values.to.trim()&&!!values.subject.trim()&&!!values.body.trim();case 'calendar.create':return !!values.subject.trim()&&!!values.start&&!!values.end;case 'messages.send':return !!values.text.trim();case 'tools.call':return !!argumentsObject();default:return !!operation.value;}}
    const input=(key:keyof typeof values,multiline=false,type='text')=>label('requestField.'+key,h(multiline?'textarea':'input',{type:multiline?undefined:type,step:type==='datetime-local'?1:undefined,value:values[key],rows:multiline?3:undefined,disabled:props.busy,'data-testid':'connection-request-'+key+'-'+props.connection.id,onInput:(e:Event)=>{values[key]=textValue(e);}}));
    return ()=>{const c=props.connection;const grant=c.scopes[scope.value];return h('details',{class:'unified-operation',open:true},[
      h('summary',t('unified.prepareAction')),h('p',{class:'micro muted'},t('unified.prepareHint')),
      label('resource',h('select',{value:String(scope.value),disabled:props.busy,'data-testid':'connection-action-resource-'+c.id,onChange:(e:Event)=>{scope.value=Number(textValue(e));operation.value=c.scopes[scope.value]?.operations[0]??'';reset();}},c.scopes.map((entry,index)=>h('option',{value:String(index)},entry.resource)))),
      label('operation',h('select',{value:operation.value,disabled:props.busy,'data-testid':'connection-action-operation-'+c.id,onChange:(e:Event)=>{operation.value=textValue(e);}},(grant?.operations??[]).map(op=>h('option',{value:op},t('unified.operations.'+op.replaceAll('.','_'))+' · '+op)))),
      operation.value==='issues.create'?[input('title'),input('body',true)]:null,
      operation.value==='mail.send'?[input('to',false,'email'),input('subject'),input('body',true)]:null,
      operation.value==='calendar.create'?[input('subject'),input('start',false,'datetime-local'),input('end',false,'datetime-local'),h('p',{class:'micro'},t('unified.calendarUTC'))]:null,
      operation.value==='messages.send'?input('text',true):null,
      operation.value==='tools.call'?h('details',{open:true},[h('summary',t('unified.toolArguments')),input('arguments',true),h('p',{class:'micro'},t('unified.mcpUnknown'))]):null,
      control('prepareAction',()=>{if(props.busy||!c.enabled||!state.connected||!valid()||!grant?.operations.includes(operation.value))return;emit('prepare',{connectionId:c.id,operation:operation.value,resource:grant.resource,request:request()});},props.busy||!c.enabled||!valid(),'connection-prepare-'+c.id),
    ]);};
  },
});

export const ConnectionsPage=defineComponent({setup(){
  const remote=useLive(async()=>{const [connections,actions]=await Promise.all([call<Connection[]>('ListConnections'),call<ConnectorAction[]>('ListConnectorActions')]);return {connections:connections??[],actions:actions??[]};});
  const additionalScopes=ref<Connection['scopes']>([]);
  const editing=ref<Connection>(),form=reactive({name:'',kind:'github' as ConnectionKind,origin:catalog.github.origin,resource:'',operations:['repo.read'],credential:'',enabled:true});
  function reset(){editing.value=undefined;additionalScopes.value=[];Object.assign(form,{name:'',kind:'github',origin:catalog.github.origin,resource:'',operations:['repo.read'],credential:'',enabled:true});}
  function edit(c:Connection){editing.value=c;additionalScopes.value=c.scopes.slice(1).map(scope=>({resource:scope.resource,operations:[...scope.operations]}));Object.assign(form,{name:c.name,kind:c.kind,origin:c.origin,resource:c.scopes[0]?.resource??'',operations:[...(c.scopes[0]?.operations??[])],credential:'',enabled:c.enabled});}
  const save=()=>remote.work(async()=>{await call('SaveConnection',{...(editing.value?{id:editing.value.id,revision:editing.value.revision}:{}),name:form.name.trim(),kind:form.kind,origin:form.origin.trim(),scopes:[{resource:form.resource.trim(),operations:[...form.operations]},...additionalScopes.value],enabled:form.enabled,...(form.credential?{credential:form.credential}:{})});reset();});
  onUnmounted(()=>{form.credential='';});
  return ()=>h('section',{class:'product-page unified-page','data-testid':'connections-page'},[
    h('header',{class:'product-heading'},[h('h1',t('unified.connections')),h('p',t('unified.connectionsIntro'))]),
    control('refresh',remote.load,remote.busy.value,'connections-refresh'),...remoteState(remote.loading.value,remote.error.value),
    remote.data.value&&!remote.data.value.connections.length?h('p',{class:'product-empty'},t('unified.noConnections')):null,
    h('div',{class:'product-columns'},(remote.data.value?.connections??[]).map(c=>h('article',{class:'product-card',key:c.id},[
      h('div',{class:'split'},[h('h2',c.name),h('span',{class:'badge'},t(c.enabled?'unified.enabled':'unified.revoked'))]),h('code',c.origin),h('p',t('unified.credential.'+c.credentialStatus)),
      c.identity&&c.identity.revision===c.revision?h('p',{'data-testid':'connection-identity-'+c.id},t('unified.verifiedIdentity',{name:c.identity.displayName,id:c.identity.id,at:date(c.identity.verifiedAt)})):h('p',{class:'micro'},t('unified.identityNotVerified')),
      h('ul',c.scopes.map(scope=>h('li',[h('strong',scope.resource),h('p',scope.operations.join(', '))]))),
      h('div',{class:'product-row'},[control('edit',()=>edit(c),remote.busy.value,'connection-edit-'+c.id),control('revoke',()=>remote.work(async()=>{await call('RevokeConnection',{id:c.id});if(editing.value?.id===c.id)reset();}),remote.busy.value||!c.enabled,'connection-revoke-'+c.id)]),
      h(ConnectionOperationPanel,{connection:c,busy:remote.busy.value,onPrepare:(payload:unknown)=>remote.work(()=>call('PrepareConnectorAction',payload))}),
    ]))),
    h('section',{class:'product-card'},[
      h('h2',t(editing.value?'unified.editConnection':'unified.addConnection')),
      label('accountName',h('input',{value:form.name,'data-testid':'connection-name',disabled:remote.busy.value,onInput:(e:Event)=>{form.name=textValue(e);}})),
      label('service',h('select',{value:form.kind,'data-testid':'connection-kind',disabled:remote.busy.value||!!editing.value,onChange:(e:Event)=>{form.kind=textValue(e) as ConnectionKind;form.origin=catalog[form.kind].origin;form.operations=[catalog[form.kind].operations[0]];form.resource='';form.credential='';}},Object.keys(catalog).map(k=>h('option',{value:k},t('unified.serviceName.'+k))))),
      label('origin',h('input',{value:form.origin,disabled:remote.busy.value,'data-testid':'connection-origin',onInput:(e:Event)=>{form.origin=textValue(e);},placeholder:'https://'})),
      label('resource',h('input',{value:form.resource,disabled:remote.busy.value,'data-testid':'connection-resource',placeholder:catalog[form.kind].resource,onInput:(e:Event)=>{form.resource=textValue(e);}})),
      h('fieldset',{class:'unified-grants'},[h('legend',t('unified.allowedOperations')),...catalog[form.kind].operations.map(operation=>h('label',{class:'product-check'},[
        h('input',{type:'checkbox',checked:form.operations.includes(operation),disabled:remote.busy.value,onChange:(e:Event)=>{form.operations=(e.target as HTMLInputElement).checked?[...form.operations,operation]:form.operations.filter(value=>value!==operation);}}),h('code',operation),h('span',t('unified.operations.'+operation.replaceAll('.','_'))),
      ]))]),
      form.kind!=='mcp'?control('addIdentityGrant',()=>{const resource=form.kind==='graph'?'me':'$identity';if(!additionalScopes.value.some(grant=>grant.resource===resource&&grant.operations.includes('identity.read')))additionalScopes.value.push({resource,operations:['identity.read']});},remote.busy.value):null,
      additionalScopes.value.length?h('div',[h('h3',t('unified.additionalGrants')),...additionalScopes.value.map((grant,index)=>h('div',{class:'product-row'},[h('code',grant.resource+' · '+grant.operations.join(', ')),control('removeGrant',()=>{additionalScopes.value.splice(index,1);},remote.busy.value)]))]):null,
      label('credentialLabel',h('input',{type:'password',value:form.credential,autocomplete:'new-password','data-testid':'connection-credential',disabled:remote.busy.value,onInput:(e:Event)=>{form.credential=textValue(e);}})),h('p',{class:'micro muted'},t('unified.credentialHint')),
      h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:form.enabled,disabled:remote.busy.value,onChange:(e:Event)=>{form.enabled=(e.target as HTMLInputElement).checked;}}),t('unified.enableConnection')]),
      h('p',{class:'product-warning'},t('unified.grantConsent')),
      h('div',{class:'product-row'},[control('saveConnection',save,remote.busy.value||!form.name.trim()||!form.origin.trim()||!form.resource.trim()||!form.operations.length,'connection-save',true),editing.value?control('cancel',reset,remote.busy.value):null]),
    ]),
    h('h2',t('unified.externalActions')),...actionCards(remote.data.value?.actions??[],remote.data.value?.connections??[],remote.busy.value,remote.work,remote.interrupt),
    !remote.data.value?.actions.length?h('p',{class:'product-empty'},t('unified.noActions')):null,
  ]);
}});

export const EvolutionPanel=defineComponent({setup(){
  const workspace=ref(product.selectedWorkspace),maintenance=ref<MemoryMaintenance>();
  const selected=ref<string[]>([]),compositionConsent=ref(false),composed=ref<CognitionSkill>();
  const composition=reactive({slug:'',name:'',description:''});
  const remote=useLive(async()=>{const [insights,cognition]=await Promise.all([call<EvolutionInsights>('GetEvolutionInsights',{workspaceId:workspace.value}),call<{candidates:SkillCandidate[];skills:CognitionSkill[]}>('GetCognitionState',{workspaceId:workspace.value})]);return {insights,skills:cognition.skills??[],candidates:(cognition.candidates??[]).filter(c=>c.status==='pending')};});
  watch(workspace,()=>{maintenance.value=undefined;selected.value=[];composed.value=undefined;remote.data.value=undefined;void remote.load();});
  watch([()=>composition.slug,()=>composition.name,()=>composition.description,selected,workspace,()=>remote.data.value?.skills.map(skill=>[skill.id,skill.version,skill.checksum,skill.enabled,skill.status,skill.source].join(':')).join('|')],()=>{compositionConsent.value=false;});
  const eligible=()=>remote.data.value?.skills.filter(skill=>skill.enabled&&skill.source.startsWith('reviewed:')&&!['archived','deprecated'].includes(skill.status??'')&&(skill.scope==='user'||workspace.value&&skill.scope==='project'&&skill.workspaceId===workspace.value))??[];
  const dependencies=()=>eligible().filter(skill=>selected.value.includes(skill.id));
  const compose=()=>remote.work(async()=>{const deps=dependencies();if(!compositionConsent.value||!deps.length||deps.length!==selected.value.length)return;compositionConsent.value=false;composed.value=await call<CognitionSkill>('ComposeCognitionSkills',{workspaceId:workspace.value,scope:workspace.value?'project':'user',slug:composition.slug.trim(),name:composition.name.trim(),description:composition.description.trim(),triggers:[],dependencies:deps.map(skill=>({id:skill.id,version:skill.version,checksum:skill.checksum}))});selected.value=[];});
  function maintain(apply:boolean){return remote.work(async()=>{if(apply&&!maintenance.value)return;maintenance.value=await call<MemoryMaintenance>('RunMemoryMaintenance',{workspaceId:workspace.value,apply});});}
  return ()=>{const data=remote.data.value;return h('section',{class:'product-card unified-evolution','data-testid':'evolution-panel'},[
    h('div',{class:'split'},[h('div',[h('h2',t('unified.evolution')),h('p',{class:'muted micro'},t('unified.evolutionIntro'))]),icon('growth',24)]),
    h('div',{class:'product-row'},[workspacePicker(workspace.value,value=>{workspace.value=value;},remote.busy.value),control('refresh',remote.load,remote.busy.value,'evolution-refresh')]),...remoteState(remote.loading.value,remote.error.value),
    h('p',{class:'micro'},t('unified.profileBoundary')),
    data?h('div',[
      h('div',{class:'unified-memory-stats'},(['active','expired','conflicted','archived'] as const).map(key=>h('div',[h('strong',number(data.insights.memory[key])),h('span',t('unified.memory.'+key))]))),
      data.insights.skills.length?h('div',{class:'unified-table-wrap'},[h('table',{class:'unified-table'},[
        h('thead',[h('tr',['skill','version','contextUses','executions','successes','failures','duration','modelRequests'].map(key=>h('th',{scope:'col'},t('unified.metric.'+key))))]),
        h('tbody',data.insights.skills.map(skill=>h('tr',{key:skill.id},[h('th',{scope:'row'},skill.namespace),...[skill.version,skill.contextUses,skill.executions,skill.successes,skill.failures,skill.totalDurationMs,skill.modelRequests].map(value=>h('td',number(value)))]))),
      ])]):h('p',{class:'product-empty'},t('unified.noMetrics')),
      data.insights.failures.length?h('details',[h('summary',t('unified.failureEvidence')),...data.insights.failures.map(f=>h('div',[h('p',[h('code',f.taskId+' · '+f.code),h('span',' '+f.summary)]),control('proposeImprovement',()=>remote.work(()=>call('ProposeSkillImprovement',{workspaceId:workspace.value,skillId:f.skillId,taskId:f.taskId})),remote.busy.value||!data.skills.some(skill=>skill.id===f.skillId&&skill.version===f.version),'evolution-propose-'+f.taskId+'-'+f.skillId)]))]):null,
      h('h3',t('unified.maintenance')),h('p',{class:'micro'},t('unified.maintenanceHint')),
      control('previewMaintenance',()=>maintain(false),remote.busy.value,'evolution-maintenance-preview'),
      maintenance.value?h('div',{'data-testid':'evolution-maintenance-review'},[
        h('dl',{class:'product-details'},(['expiredIds','archiveIds','preservedIds'] as const).flatMap(key=>[h('dt',t('unified.'+key)),h('dd',(maintenance.value![key]??[]).join(', ')||'—')])),
        !maintenance.value.applied?control('applyMaintenance',()=>maintain(true),remote.busy.value,'evolution-maintenance-apply'):h('p',{role:'status'},t('unified.maintenanceApplied')),
      ]):null,
      h('details',{class:'unified-composition'},[
        h('summary',t('unified.composeSkills')),h('p',{class:'micro'},t('unified.compositionHint')),
        ...(['slug','name','description'] as const).map(key=>label('compositionField.'+key,h('input',{value:composition[key],disabled:remote.busy.value,'data-testid':'evolution-composition-'+key,onInput:(e:Event)=>{composition[key]=textValue(e);}}))),
        !eligible().length?h('p',{class:'product-empty'},t('unified.noDependencies')):null,
        ...eligible().map(skill=>h('div',{key:skill.id},[
          h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:selected.value.includes(skill.id),disabled:remote.busy.value,'data-testid':'evolution-dependency-'+skill.id,onChange:(e:Event)=>{selected.value=(e.target as HTMLInputElement).checked?[...selected.value,skill.id]:selected.value.filter(id=>id!==skill.id);}}),h('span',skill.name+' · v'+skill.version)]),
          selected.value.includes(skill.id)?h('details',{open:true},[h('summary',skill.namespace+' · v'+skill.version),h('code',{class:'product-hash'},skill.checksum),h('pre',pretty(skill))]):null,
        ])),
        selected.value.length?h('div',[h('p',t('unified.compositionTools')),h('code',[...new Set(dependencies().flatMap(skill=>skill.allowedTools??[]))].sort().join(', ')||'—'),h('p',{class:'micro'},t('unified.compositionLimit'))]):null,
        h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:compositionConsent.value,disabled:remote.busy.value||!selected.value.length,'data-testid':'evolution-composition-consent',onChange:(e:Event)=>{compositionConsent.value=(e.target as HTMLInputElement).checked;}}),t('unified.compositionConsent')]),
        control('createComposition',compose,remote.busy.value||!compositionConsent.value||!composition.slug.trim()||!composition.name.trim()||!composition.description.trim()||!selected.value.length,'evolution-compose'),
        composed.value?h('details',{open:true,'data-testid':'evolution-composition-result'},[h('summary',t('unified.compositionCreated')),h('pre',pretty(composed.value))]):null,
      ]),
      h('h3',t('unified.pendingImprovements')),h('p',{class:'micro'},t('unified.pendingHint')),
      !data.candidates.length?h('p',{class:'product-empty'},t('unified.noCandidates')):null,
      ...data.candidates.map(candidate=>h('article',{class:'product-card',key:candidate.id,'data-testid':'evolution-candidate-'+candidate.id},[
        h('h4',candidate.manifest.name),h('p',{class:'micro'},t('unified.candidateSource',{task:candidate.taskId,version:candidate.baseVersion})),
        candidate.previous?h('details',[h('summary',t('unified.previousVersion')),h('pre',pretty(candidate.previous))]):null,
        h('details',{open:true},[h('summary',t('unified.proposedVersion')),h('pre',pretty(candidate.manifest))]),
        h('div',{class:'product-row'},[true,false].map(approve=>control(approve?'approveImprovement':'rejectImprovement',()=>remote.work(()=>call('ReviewCognitionCandidate',{id:candidate.id,workspaceId:candidate.workspaceId,approve})),remote.busy.value,'evolution-candidate-'+(approve?'approve-':'reject-')+candidate.id))),
      ])),
    ]):null,
  ]);};
}});
