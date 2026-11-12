import { defineComponent,h,onMounted,onUnmounted,ref,reactive,watch,type VNodeChild } from 'vue';
import { t,date,i18n } from './i18n';
import { button,badge,errorText } from './ui';
import { state,saveSettings,openConversation,newConversation } from './store';
import { call,subscribe } from './bridge';
import { ai,sendAI,startAgentAnalysis,selectProvider,selectModel,profile,model,usableDefaultAI } from './ai-store';
import { AIProfilesPage } from './ai-pages';
import { Markdown } from './markdown';
import { product,productWork,refreshProduct,selectWorkspace,pickWorkspace,analyzeWorkspace,readSource,approveProductAction,rejectProductAction,diagnoseEnvironment } from './product-store';
import type { AgentTask,ProductAction,ProjectFile } from './product-types';
import { Agent02Panel } from './agent02-panel';
import { ComputerTaskEvidence } from './agent02-computer-evidence';
import { DeveloperProjectPanel } from './project021-panel';

const text=(value:string,change:(s:string)=>void,extra:Record<string,unknown>={})=>h('input',{value,onInput:(e:Event)=>change((e.target as HTMLInputElement).value),...extra});
const area=(value:string,change:(s:string)=>void,extra:Record<string,unknown>={})=>h('textarea',{value,rows:5,onInput:(e:Event)=>change((e.target as HTMLTextAreaElement).value),...extra});
const field=(key:string,child:VNodeChild)=>h('label',{class:'product-field'},[h('span',t('product.'+key)),child]);
const action=(key:string,fn:()=>unknown,disabled=false,primary=false)=>button('product.'+key,fn,{class:['button',primary?'primary':'secondary'],disabled:disabled||product.busy||!state.connected,'data-testid':'product-'+key});
const panel=(key:string,children:VNodeChild)=>h('section',{class:'product-card'},[h('h2',t('product.'+key)),children]);
function refresh(){if(state.connected)void refreshProduct().catch(e=>{product.error=e.code||e.message;});}
function useProduct(){onMounted(refresh);}
function workspaceSelect(){return h('select',{'aria-label':t('product.workspace'),value:product.selectedWorkspace,onChange:(e:Event)=>selectWorkspace((e.target as HTMLSelectElement).value)},[h('option',{value:''},t('product.selectWorkspace')),...product.data.workspaces.map(w=>h('option',{value:w.id},w.name))]);}
function providerSelect(){
 const session=ai.assistant;
 return h('div',{class:'product-row'},[
  field('currentProvider',h('select',{value:session.providerId,onChange:(e:Event)=>selectProvider(session,(e.target as HTMLSelectElement).value)},[h('option',{value:''},t('product.offline')),...ai.profiles.filter(p=>p.enabled).map(p=>h('option',{value:p.id},p.name))])),
  field('model',h('select',{value:session.modelId,onChange:(e:Event)=>selectModel(session,(e.target as HTMLSelectElement).value)},[h('option',{value:''},t('product.selectModel')),...(profile(session)?.models||[]).filter(m=>m.capabilities.text==='SUPPORTED').map(m=>h('option',{value:m.id},m.displayName||m.id))])),
 ]);
}
interface Folder { folders:Map<string,Folder>; files:ProjectFile[] }
function fileTree(entries:ProjectFile[],selected:string,open:(f:ProjectFile)=>void){
 const root:Folder={folders:new Map(),files:[]};
 for(const entry of entries){
  const names=entry.path.split('/');if(entry.type!=='directory')names.pop();let branch=root;
  for(const name of names){let next=branch.folders.get(name);if(!next){next={folders:new Map(),files:[]};branch.folders.set(name,next);}branch=next;}
  if(entry.type!=='directory')branch.files.push(entry);
 }
 const render=(branch:Folder):VNodeChild[]=>[
  ...Array.from(branch.folders,([name,child])=>h('details',{key:name,class:'product-folder'},[h('summary',name),...render(child)])),
  ...branch.files.map(f=>h('button',{key:f.path,title:f.path,class:['product-list-button',{'is-active':selected===f.path}],onClick:()=>open(f)},[h('code',f.path.split('/').at(-1)),h('span',{class:'micro'},f.language||f.type||'')])),
 ];
 return render(root);
}
const networkConsents=reactive(new Set<string>());
function actionCard(a:ProductAction){const file=a.proposal.file;const cmd=a.proposal.command;const networkReview=a.proposal.policyVersion==='agent-02-v1'&&!!cmd?.network;return h('article',{class:'product-card product-approval','data-testid':'product-action','data-action-id':a.id},[
 h('div',{class:'split'},[h('h3',file?file.relativePath:cmd?.id||a.kind),badge(a.status)]),h('code',a.id),
 file?h('div',[h('p',t('product.fileRisk')),a.proposal.gitDirtyFile?h('p',{class:'product-warning','data-testid':'preset-git-warning'},t('preset.dirtyFile')):a.proposal.gitStatus&&['UNSUPPORTED','ERROR','PARTIAL'].includes(a.proposal.gitStatus)?h('p',{class:'product-warning'},t('preset.gitUnknown')):null,h('pre',{class:'product-diff','data-testid':'product-diff'},file.diff),h('details',[h('summary',t('product.beforeAfter')),h('div',{class:'product-columns'},[h('pre',file.beforeContent||t('product.newFile')),h('pre',file.afterContent)])]),h('p',{class:'micro'},`${file.beforeHash} → ${file.afterHash}`)]):null,
 cmd?h('div',[h('dl',{class:'product-details'},[h('dt',t('product.executable')),h('dd',[h('code',cmd.executable)]),h('dt',t('product.arguments')),h('dd',[h('code',JSON.stringify(cmd.args))]),h('dt',t('product.directory')),h('dd',cmd.directory),h('dt',t('product.network')),h('dd',t(cmd.network?'product.networkAllowed':'product.networkNo')),h('dt',t('product.codeRisk')),h('dd',t(cmd.executesCode?'product.codeYes':'product.codeNo')),h('dt',t('product.limits')),h('dd',`${cmd.timeoutSeconds}s / ${cmd.maxOutputBytes} bytes`)]),cmd.executesCode?h('p',{class:'product-warning'},t('product.projectCodeWarning')):null]):null,
 h('p',{class:'micro'},t('product.expires',{at:date(a.expiresAt)})),h('code',{class:'product-hash'},a.hash),
 a.status==='PENDING'&&networkReview?h('label',{class:'product-check'},[h('input',{type:'checkbox','data-testid':'agent02-network-consent',checked:networkConsents.has(a.hash),onChange:(e:Event)=>{if((e.target as HTMLInputElement).checked)networkConsents.add(a.hash);else networkConsents.delete(a.hash);}}),t('product.networkConsent')]):null,
 a.status==='PENDING'?h('div',{class:'product-row'},[action('approve',()=>{const confirmed=networkConsents.delete(a.hash);return approveProductAction(a,confirmed);},Date.parse(a.expiresAt)<=Date.now()||(networkReview&&!networkConsents.has(a.hash)),true),action('reject',()=>{networkConsents.delete(a.hash);return rejectProductAction(a);})]):null,
 a.error?h('p',{class:'ai-error',role:'alert'},errorText(a.error)):null,
 a.result?h('details',{open:true},[h('summary',t('product.result')),h('pre',typeof a.result==='string'?a.result:JSON.stringify(a.result,null,2))]):null,
 ['RECOVERY_REQUIRED','UNKNOWN_RESULT'].includes(a.status)?h('p',{class:'product-warning'},t('product.noReplay')):null,
 ]);}

export const HomePage=defineComponent({setup(){useProduct();return()=>{
 const current=usableDefaultAI();
 return h('section',{class:'product-page','data-testid':'product-home'},[
 h('header',{class:'product-heading'},[h('h1',t('product.welcome')),h('p',t('product.homeIntro'))]),
 h('div',{class:'product-row'},[
   button('product.chat',()=>newConversation(),{class:'button primary',disabled:!state.connected,'data-testid':'product-chat'}),
   action('newTask',()=>state.route='workflows'),
   action('chooseWorkspace',pickWorkspace),
 ]),
 h('div',{class:'product-columns'},[
 panel('currentProvider',h('div',[h('strong',current?.name||t('product.offline')),h('p',current?t('preset.activeModel',{model:ai.settings.defaultModelId}):t('preset.offlineSummary')),action('configure',()=>state.route='models')])),
 panel('workspaces',product.data.workspaces.length?product.data.workspaces.map(w=>h('button',{class:'product-list-button',onClick:()=>{selectWorkspace(w.id);state.route='developer';}},[h('strong',w.name),h('code',w.root)])):h('p',t('product.noWorkspace'))),
 panel('recentConversations',state.conversations.length?state.conversations.slice(0,5).map(c=>h('button',{class:'product-list-button',onClick:()=>openConversation(c.id)},[h('strong',c.title||t('assistant.newConversation')),h('time',date(c.updatedAt))])):h('p',t('preset.noConversations'))),
 panel('recentTasks',product.data.agentTasks.length?product.data.agentTasks.slice(0,5).map(task=>h('button',{class:'product-list-button',onClick:()=>state.route='workflows'},[h('strong',task.goal),badge(task.status)])):h('p',t('preset.noTasks'))),
 ]),
 ]);};}});

export const WorkspacesPage=defineComponent({setup(){useProduct();return()=>h('section',{class:'product-page','data-testid':'product-workspaces'},[
 h('header',{class:'product-heading'},[h('h1',t('product.workspaces')),h('p',t('product.workspacePrivacy'))]),action('chooseWorkspace',pickWorkspace,false,true),
 ...product.data.workspaces.map(w=>h('article',{class:'product-card'},[h('h2',w.name),h('code',w.root),h('p',t('product.workspaceScope')),h('div',{class:'product-row'},[action('openWorkspace',()=>{selectWorkspace(w.id);state.route='developer';}),action('forgetWorkspace',()=>productWork(async()=>{await call('RemoveWorkspace',{id:w.id});await refreshProduct();}))])])),
 !product.data.workspaces.length?h('p',{class:'product-empty'},t('product.noWorkspace')):null,
 ]);}});

export const DeveloperPage=defineComponent({setup(){useProduct();const query=ref(''),path=ref(''),content=ref(''),commandId=ref('git.status'),args=ref(''),network=ref(false),tab=ref<'project'|'environment'>('project'),inspectionVersion=ref(0);
 watch(()=>product.file,f=>{if(f){path.value=f.relativePath||f.path||path.value;content.value=f.content;}else{path.value='';content.value='';}});
 const prepareFile=()=>productWork(async()=>{product.selectedAction=await call<ProductAction>('PrepareFileChange',{workspaceId:product.selectedWorkspace,path:path.value,content:content.value,taskId:product.selectedTask||undefined,exclusions:[...product.exclusions]});await refreshProduct();});
 const prepareCommand=()=>productWork(async()=>{product.selectedAction=await call<ProductAction>('PrepareCommand',{workspaceId:product.selectedWorkspace,commandId:commandId.value,args:args.value.trim()?args.value.trim().split(/\s+/):[],network:network.value,taskId:product.selectedTask||undefined});await refreshProduct();});
 const explain=()=>{if(!product.file)return;ai.mode='real';ai.assistant.draft=t('product.explainPrompt')+'\n\n'+(product.file.relativePath||product.file.path||'')+'\n```\n'+product.file.content+'\n```';state.route='assistant';};
 return()=>h('section',{class:'product-page','data-testid':'product-developer'},[
 h('header',{class:'product-heading'},[h('h1',t('product.developer')),h('p',t('product.developerIntro'))]),
 h('div',{class:'product-row'},[workspaceSelect(),action('chooseWorkspace',pickWorkspace),action('refresh',()=>{refresh();inspectionVersion.value++;}),action('project',()=>tab.value='project'),action('environment',()=>tab.value='environment')]),
 h(DeveloperProjectPanel,{workspaceId:product.selectedWorkspace,exclusions:product.exclusions,mode:tab.value,refreshKey:inspectionVersion.value}),
 field('taskActionTarget',h('select',{value:product.selectedTask,onChange:(e:Event)=>product.selectedTask=(e.target as HTMLSelectElement).value},[h('option',{value:''},t('product.noTaskLink')),...product.data.agentTasks.filter(task=>task.workspaceId===product.selectedWorkspace&&!['CANCELLED','FAILED'].includes(task.status)).map(task=>h('option',{value:task.id},task.goal))])),
 product.error?h('p',{role:'alert',class:'ai-error'},errorText(product.error)):null,
 tab.value==='environment'?h('div',[action('diagnose',diagnoseEnvironment,false,true),h('p',{class:'micro muted'},t('product.environmentPrivacy')),...product.diagnostics.map(d=>h('article',{class:'product-card'},[h('div',{class:'split'},[h('h2',d.component),badge(d.status)]),h('code',d.executable),h('pre',d.version),...d.problems.map(p=>h('p',t('product.diagnostic.'+p))),...d.suggestions.map(p=>h('p',t('product.diagnostic.'+p))),h('a',{href:d.downloadUrl,target:'_blank',rel:'noopener noreferrer'},t('product.officialDownload')),d.installPreview?.length?h('pre',JSON.stringify(d.installPreview)):null,h('p',{class:'micro'},t('product.manualInstall'))]))]):
 !product.selectedWorkspace?h('p',{class:'product-empty'},t('product.noWorkspace')):h('div',[
 h('div',{class:'product-row'},[text(query.value,v=>query.value=v,{placeholder:t('product.search'),'aria-label':t('product.search')}),action('analyze',()=>analyzeWorkspace(query.value),false,true)]),
 field('exclusions',text(product.exclusions.join(','),v=>{product.exclusions=v.split(',');product.file=null;product.report=null;},{placeholder:'vendor,.cache',onBlur:()=>product.exclusions=product.exclusions.map(s=>s.trim()).filter(Boolean)})),
 product.report?h('div',{class:'product-explorer'},[
 h('aside',{class:'product-tree'},[h('h2',t('product.explorer')),h('p',{class:'micro'},t('product.boundedScan')),h('pre',{class:'micro'},JSON.stringify(product.report.languages)),product.report.truncated?h('p',{class:'product-warning'},t('product.truncated')):null,...fileTree(product.report.entries||[],path.value,f=>{path.value=f.path;void readSource(f.path);})]),
 h('div',{class:'product-source'},[product.file?h('div',[h('div',{class:'product-row'},[h('h2',path.value),action('explain',explain)]),h('pre',product.file.content),product.file.symbols?.length?h('details',[h('summary',t('product.symbols')),h('pre',JSON.stringify(product.file.symbols,null,2))]):null]):h('p',t('product.selectFile'))]),
 ]):null,
 product.report?h('details',{class:'product-card'},[h('summary',t('product.symbols')),h('pre',JSON.stringify({dependencies:product.report.dependencies,symbols:product.report.symbols,calls:product.report.calls,matches:product.report.matches},null,2))]):null,
 panel('edit',h('div',[h('p',t('product.fileRisk')),field('relativePath',text(path.value,v=>path.value=v,{'aria-label':t('product.relativePath')})),field('replacement',area(content.value,v=>content.value=v,{rows:12,spellcheck:false,'aria-label':t('product.replacement')})),action('previewDiff',prepareFile,!path.value,true)])),
 panel('command',h('div',[field('command',h('select',{value:commandId.value,onChange:(e:Event)=>{commandId.value=(e.target as HTMLSelectElement).value;args.value='';network.value=false;}},product.data.commandCatalog.map(c=>h('option',{value:c.id},c.id)))),field('testTargets',text(args.value,v=>args.value=v,{placeholder:'./...'})),h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:network.value,onChange:(e:Event)=>network.value=(e.target as HTMLInputElement).checked}),t('product.networkConsent')]),h('p',{class:'product-warning'},t('product.projectCodeWarning')),action('previewCommand',prepareCommand,false,true)])),
 product.selectedAction?actionCard(product.selectedAction):null,
 panel('executionHistory',product.data.actions.filter(a=>!a.taskId).slice(0,10).map(actionCard)),
 ]),
 ]);}});

export const TasksPage=defineComponent({setup(){useProduct();const goal=ref(''),paths=ref(''),selected=ref(''),context=ref<{id:string;content:string;hash:string}|null>(null);
 let disconnect:(()=>void)|undefined;let refreshTimer:ReturnType<typeof setTimeout>|undefined;
 onMounted(()=>{if(state.connected)disconnect=subscribe('asimi:changed',()=>{clearTimeout(refreshTimer);refreshTimer=setTimeout(refresh,120);});});
 onUnmounted(()=>{disconnect?.();clearTimeout(refreshTimer);});
 const previewAI=(id:string)=>productWork(async()=>{const p=await call<{content:string;hash:string}>('PreviewAgentTaskContext',{id});context.value={id,...p};});
 const create=()=>productWork(async()=>{const task=await call<AgentTask>('CreateAgentTask',{workspaceId:product.selectedWorkspace,goal:goal.value});selected.value=task.id;goal.value='';await refreshProduct();});
 const createModelRun=()=>productWork(async()=>{const task=await call<AgentTask>('CreateAgentRun',{workspaceId:product.selectedWorkspace,goal:goal.value,providerId:ai.assistant.providerId,modelId:ai.assistant.modelId,paths:paths.value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),autoSelect:true,exclusions:product.exclusions.map(path=>path.trim()).filter(Boolean)});selected.value=task.id;goal.value='';await refreshProduct();});
 const control=(method:string,id:string)=>productWork(async()=>{await call(method,{id});await refreshProduct();});
 const resume=(task:AgentTask)=>{if(!task.agent)return control('ResumeAgentTask',task.id);const recovery=task.steps.some(s=>s.status==='RECOVERY_REQUIRED');if(recovery&&!window.confirm(t('agent02.acknowledge')))return;return productWork(async()=>{await call('ResumeAgentRun',{id:task.id,acknowledgeRecovery:recovery});await refreshProduct();});};
 return()=>h('section',{class:'product-page','data-testid':'product-tasks'},[
 h('header',{class:'product-heading'},[h('h1',t('product.tasks')),h('p',t('product.taskIntro'))]),
 h('div',{class:'product-row'},[workspaceSelect(),action('refresh',refresh)]),
 field('goal',area(goal.value,v=>goal.value=v,{'aria-label':t('product.goal')})),
 providerSelect(),h('details',{class:'preset-task-sources'},[h('summary',t('preset.taskSources')),h('p',{class:'micro muted'},t('preset.taskSourcesHint')),h('label',{class:'product-field'},[h('span',t('agent02.paths')),area(paths.value,v=>paths.value=v,{rows:3,placeholder:'src/calc.go'})])]),
 h('div',{class:'product-row'},[button('agent02.create',createModelRun,{class:'button primary',disabled:product.busy||!goal.value.trim()||!product.selectedWorkspace||model(ai.assistant)?.capabilities.text!=='SUPPORTED','data-testid':'agent02-create'}),action('createTask',create,!goal.value.trim()||!product.selectedWorkspace)]),
 ...product.data.agentTasks.map(task=>h('article',{class:'product-card','data-task-id':task.id},[
 h('div',{class:'split'},[h('button',{class:'text-button',onClick:()=>selected.value=task.id},task.goal),badge(task.status)]),h('p',{class:'micro'},date(task.updatedAt)),
 h('div',{class:'product-row'},[action('pause',()=>control('PauseAgentTask',task.id),!['RUNNING','PLANNING','WAITING_APPROVAL','NEEDS_REVIEW'].includes(task.status)),action('resume',()=>resume(task),!['PAUSED','RECOVERY_REQUIRED'].includes(task.status)),action('cancel',()=>control('CancelAgentTask',task.id),['COMPLETED','CANCELLED','FAILED'].includes(task.status)),task.agent?null:action('analyzeWithAI',()=>previewAI(task.id)),task.agent?null:action('taskActions',()=>{selectWorkspace(task.workspaceId);product.selectedTask=task.id;state.route='developer';}),task.conversationId?action('taskConversation',()=>openConversation(task.conversationId!)):null]),
 task.agent?h(Agent02Panel,{task}):null,
 selected.value===task.id?h(ComputerTaskEvidence,{taskId:task.id}):null,
 h('ol',{class:'product-steps'},task.steps.map((s,index)=>h('li',{class:{current:String(task.currentStep)===String(index)||task.currentStep===s.id}},[h('strong',t('product.step.'+s.kind)===('product.step.'+s.kind)?s.title:t('product.step.'+s.kind)),badge(s.status),s.result?h('details',[h('summary',t('product.result')),h('pre',typeof s.result==='string'?s.result:JSON.stringify(s.result,null,2))]):null,s.error?h('p',{role:'alert'},errorText(s.error)):null]))),
 task.summary?h(Markdown,{content:i18n.global.te('product.summary.'+task.summary,'en-US')?t('product.summary.'+task.summary):/^[A-Z_]+$/.test(task.summary)?errorText(task.summary):task.summary}):null,
 ...product.data.actions.filter(a=>a.taskId===task.id).map(actionCard),
 task.status==='RECOVERY_REQUIRED'?h('p',{class:'product-warning'},t('product.noReplay')):null,
 ])),
 context.value?panel('reviewContext',h('div',[h('pre',context.value.content),h('code',context.value.hash),providerSelect(),action('sendContext',()=>startAgentAnalysis(context.value!.id,context.value!.hash),model(ai.assistant)?.capabilities.text!=='SUPPORTED'||ai.assistant.busy),action('configure',()=>state.route='models'),ai.assistant.error?h('p',{role:'alert'},errorText(ai.assistant.error)):null])):null,
 ]);}});

export const SkillsPage=defineComponent({setup(){useProduct();return()=>h('section',{class:'product-page','data-testid':'product-skills'},[h('header',{class:'product-heading'},[h('h1',t('product.skills')),h('p',t('product.skillBoundary'))]),...product.data.skills.map(s=>h('article',{class:'product-card'},[h('div',{class:'split'},[h('h2',t('product.skill.'+s.id)===('product.skill.'+s.id)?s.name:t('product.skill.'+s.id)),h('label',{class:'product-check'},[h('input',{type:'checkbox',checked:s.enabled,disabled:product.busy,onChange:(e:Event)=>productWork(async()=>{await call('SetSkillEnabled',{id:s.id,enabled:(e.target as HTMLInputElement).checked});await refreshProduct();})}),t('product.enabled')])]),h('p',t('product.skillDescription.'+s.id)),h('p',{class:'micro'},`${s.version||'0.1.0'} · ${s.risk}`),h('code',s.tools?.join(', ')),h('details',[h('summary',t('product.schemas')),h('pre',JSON.stringify({input:s.inputSchema||s.schema,output:s.outputSchema},null,2))])]))]);}});

export const Onboarding=defineComponent({setup(){const step=ref(0);const labels=['welcome','language','providerSetup','connection','visionCheck','safety','workspace','finish'];const complete=()=>productWork(async()=>{await call('CompleteOnboarding',{completed:true});await refreshProduct();product.showOnboarding=false;});
 return()=>product.showOnboarding?h('div',{class:'product-overlay',role:'dialog','aria-modal':true,'aria-label':t('product.setup')},[h('section',{class:'product-onboarding'},[
 h('p',{class:'eyebrow'},`${step.value+1} / ${labels.length}`),h('h1',t('product.'+labels[step.value])),
 step.value===0?h('p',t('product.homeIntro')):step.value===1?field('language',h('select',{value:state.settings.appLanguage,onChange:(e:Event)=>saveSettings({appLanguage:(e.target as HTMLSelectElement).value as 'en-US'|'zh-CN'})},[h('option',{value:'en-US'},'English'),h('option',{value:'zh-CN'},'简体中文')])):
 step.value===2?h('div',[h('p',t('product.byokIntro')),h(AIProfilesPage)]):step.value===3?h('div',[h('p',t('product.connectionIntro')),providerSelect(),action('testConnection',()=>sendAI(ai.assistant,false,true),model(ai.assistant)?.capabilities.text!=='SUPPORTED'||ai.assistant.busy),ai.assistant.error?h('p',{role:'alert'},errorText(ai.assistant.error)):ai.assistant.response?.status?h('p',{role:'status'},badge(ai.assistant.response.status)):null]):
 step.value===4?h('div',[h('p',t('product.visionIntro')),...ai.profiles.map(p=>h('div',[h('strong',p.name),...p.models.map(m=>h('p',`${m.displayName||m.id}: ${t('ai.'+m.capabilities.vision)}`))]))]):step.value===5?h('div',[h('p',t('product.workspacePrivacy')),h('p',t('product.controlScope')),h('p',t('product.noReplay'))]):step.value===6?h('div',[h('p',t('product.workspacePrivacy')),action('chooseWorkspace',pickWorkspace),workspaceSelect()]):h('p',t('product.finishIntro')),
 h('div',{class:'product-row'},[step.value>0?action('back',()=>step.value--):null,step.value<labels.length-1?action(step.value===2?'offlineNext':'next',()=>step.value++,false,true):action('finish',complete,false,true)]),
 ])]):null;}});
