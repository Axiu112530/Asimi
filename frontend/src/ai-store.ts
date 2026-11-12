import { reactive, watch, type WatchStopHandle } from 'vue';
import { call, subscribe, ServiceError } from './bridge';
import { state, fail } from './store';
import { t } from './i18n';
import type { Conversation } from './types';
import type { AIAttachment, AIModel, AIProfile, AIReply, AISelection, AIStreamEvent, AIUsageRecord } from './ai-types';
import type { ScreenAnalysisInput } from './screen-types';

export interface ChatSession {
  providerId:string; modelId:string; draft:string; attachments:AIAttachment[]; importing:boolean;
  busy:boolean; starting:boolean; requestId:string; conversation:Conversation|null; scope:number;
  stream:boolean; maxOutputTokens:number; temperature:number|null; imageDetail:string; error:string;
  response?:AIStreamEvent['response'];
  screenRequestId?:string;
}
const session=():ChatSession=>({providerId:'',modelId:'',draft:'',attachments:[],importing:false,busy:false,starting:false,requestId:'',conversation:null,scope:0,stream:true,maxOutputTokens:1024,temperature:null,imageDetail:'auto',error:''});
export const ai=reactive({profiles:[] as AIProfile[],settings:{defaultProviderId:'',defaultModelId:'',visionProviderId:'',visionModelId:'',maxOutputTokens:1024,maxContextBytes:65536,warningThreshold:null} as AISelection,usage:[] as AIUsageRecord[],mode:'mock',assistant:session(),playground:session(),loaded:false,saving:false});
export function profile(s:ChatSession){return ai.profiles.find(p=>p.id===s.providerId);}
export function model(s:ChatSession){return profile(s)?.models.find(m=>m.id===s.modelId);}
export function usableDefaultAI() {
  const provider = ai.profiles.find(p => p.id === ai.settings.defaultProviderId);
  if (!provider?.enabled || !provider.models.some(m => m.id === ai.settings.defaultModelId && m.capabilities.text === 'SUPPORTED')) return;
  if (!provider.hasCredential && /^https:\/\/api\.deepseek\.com(?:\/|$)/i.test(provider.baseUrl)) return;
  return provider;
}
export function selectProvider(s:ChatSession,id:string){s.providerId=id;s.modelId=ai.profiles.find(p=>p.id===id)?.models[0]?.id||'';selectModel(s,s.modelId);}
export function selectModel(s:ChatSession,id:string){s.modelId=id;s.temperature=null;s.imageDetail=model(s)?.capabilities.imageDetails?.[0]||'auto';s.stream=model(s)?.capabilities.streaming==='SUPPORTED';}
export async function refreshAI(){
  const firstLoad=!ai.loaded;
  const previousDefault = ai.settings;
  const [profiles,settings,usage]=await Promise.all([call<AIProfile[]>('ListAIProfiles'),call<AISelection>('GetAISettings'),call<AIUsageRecord[]>('GetUsage')]);
  ai.profiles=profiles||[];ai.settings=settings;ai.usage=usage||[];ai.loaded=true;
  for(const s of [ai.assistant,ai.playground]){
    const followsDefault = s.providerId === previousDefault.defaultProviderId &&
      s.modelId === previousDefault.defaultModelId;
    const defaultChanged = settings.defaultProviderId !== previousDefault.defaultProviderId ||
      settings.defaultModelId !== previousDefault.defaultModelId;
    if (settings.defaultProviderId && (!profile(s)?.enabled || followsDefault && defaultChanged && !s.conversation?.messages.length)) {
      selectProvider(s, settings.defaultProviderId);
      if (profile(s)?.models.some(m => m.id === settings.defaultModelId)) selectModel(s, settings.defaultModelId);
    }
    if(firstLoad)s.maxOutputTokens=settings.maxOutputTokens||1024;
  }
}

interface TrackedRequest { session:ChatSession; scope:number; sequence:number; terminal:boolean }
const requests=new Map<string,TrackedRequest>();
const terminalRequests=new Set<string>();
const pendingEvents=new Map<string,{event:AIStreamEvent;kind:string}[]>();
let disposers:(()=>void)[]=[];let stopWatch:WatchStopHandle|undefined;let epoch=0;let refreshVersion=0;
function terminal(id:string){requests.delete(id);terminalRequests.add(id);if(terminalRequests.size>256)terminalRequests.delete(terminalRequests.values().next().value!);}
async function discardUnsent(attachments:AIAttachment[]){for(const attachment of attachments){try{await call('DeleteAttachment',{id:attachment.id});}catch{/* The backend refuses deletion if a request already references this image. */}}}
export function resetChatDraft(s:ChatSession){
  const unsent=s.attachments;s.scope++;s.attachments=[];s.draft='';s.importing=false;s.error='';s.response=undefined;
  s.busy=false;s.starting=false;s.requestId='';s.screenRequestId=undefined;void discardUnsent(unsent);
}
export function stopAI(){
  epoch++;for(const off of disposers)off();disposers=[];stopWatch?.();stopWatch=undefined;
  requests.clear();terminalRequests.clear();pendingEvents.clear();
  for(const s of [ai.assistant,ai.playground]){s.scope++;s.busy=false;s.starting=false;s.importing=false;}
}
export async function startAI(releaseMode = false){
  stopAI();const generation=epoch;
  if (releaseMode) ai.mode = 'real';
  for(const name of ['started','delta','completed','failed','cancelled'])disposers.push(subscribe('assistant.stream.'+name,data=>{if(data&&typeof data==='object')void receive(data as AIStreamEvent,name,generation);}));
  let selectedSession:ChatSession|undefined;
  stopWatch=watch(()=>state.conversation?.id,()=>{
    const c=state.conversation;const target=c?.playground?ai.playground:ai.assistant;
    if(selectedSession&&selectedSession!==target)resetChatDraft(selectedSession);
    if(target.conversation?.id!==c?.id)resetChatDraft(target);
    target.conversation=c;selectedSession=target;
    const previous=[...(c?.messages||[])].reverse().find(m=>m.providerId&&m.providerId!=='mock');
    if(previous){if(!c?.playground)ai.mode='real';target.providerId=previous.providerId!;target.modelId=previous.modelId||'';state.inspectorOpen=false;}
    else if(!releaseMode && c?.tasks?.length)ai.mode='mock';
    if(c?.playground)state.route='playground';
    if(c)void hydrateAttachments(c);
  },{immediate:true,flush:'sync'});
  await refreshAI();
}
async function hydrateAttachments(c:Conversation){
  await Promise.all((c.messages||[]).flatMap(m=>(m.attachments||[]).filter(a=>!a.previewDataUrl).map(async a=>{
    try{const stored=await call<AIAttachment>('GetAttachment',{id:a.id});a.previewDataUrl=stored.previewDataUrl;}catch{/* Retain metadata if the local image is unavailable. */}
  })));
}
async function loadConversation(s:ChatSession,id:string,generation:number,requestId:string){
  const c=await call<Conversation>('GetConversation',{id});await hydrateAttachments(c);
  if(generation!==epoch||s.requestId!==requestId||s.conversation?.id!==id)return;
  if(s.busy){for(const live of s.conversation.messages.filter(m=>m.requestId===requestId&&m.role==='assistant')){
    const index=c.messages.findIndex(m=>m.id===live.id);if(index<0)c.messages.push(live);else if(live.content.length>c.messages[index].content.length)c.messages[index]=live;
  }}
  s.conversation=c;if(state.conversation?.id===id)state.conversation=c;
}
function bufferEvent(e:AIStreamEvent,kind:string){
  if(![ai.assistant,ai.playground].some(s=>s.starting)||(!pendingEvents.has(e.requestId)&&pendingEvents.size>=8))return;
  const queue=pendingEvents.get(e.requestId)||[];const last=queue.at(-1);
  if(last&&e.sequence<=last.event.sequence)return;
  // Coalesce early deltas instead of dropping a terminal event after a long burst.
  if(kind==='delta'&&last?.kind==='delta')last.event={...e,contentDelta:last.event.contentDelta+(e.contentDelta||'')};
  else queue.push({event:{...e},kind});
  pendingEvents.set(e.requestId,queue);
}
async function refreshHistory(generation:number){
  const version=++refreshVersion;const [usage,conversations]=await Promise.all([call<AIUsageRecord[]>('GetUsage'),call<Conversation[]>('ListConversations')]);
  if(generation===epoch&&version===refreshVersion){ai.usage=usage||[];state.conversations=conversations||[];}
}
async function receive(e:AIStreamEvent,kind:string,generation:number){
  if(generation!==epoch||!e.requestId||!e.messageId||!e.conversationId||!Number.isFinite(e.sequence)||terminalRequests.has(e.requestId))return;
  const request=requests.get(e.requestId);if(!request){bufferEvent(e,kind);return;}
  if(request.terminal||e.sequence<=request.sequence)return;request.sequence=e.sequence;
  const s=request.session;const current=request.scope===s.scope&&s.requestId===e.requestId&&s.conversation?.id===e.conversationId;
  if(current){
    let message=s.conversation!.messages.find(m=>m.id===e.messageId);
    if(!message){message={id:e.messageId,conversationId:e.conversationId,role:'assistant',content:'',createdAt:new Date().toISOString(),providerId:s.providerId,modelId:s.modelId,requestId:e.requestId,status:e.status};s.conversation!.messages.push(message);}
    message.content+=e.contentDelta||'';message.status=e.status;message.errorCode=e.errorCode;
    if(e.response){message.content=e.response.content??message.content;message.usage=e.response.usage;s.response=e.response;}
  }
  if(['completed','failed','cancelled'].includes(kind)){
    request.terminal=true;terminal(e.requestId);
    if(current){s.busy=false;s.error=e.errorCode||'';}
    try{if(current)await loadConversation(s,e.conversationId,generation,e.requestId);await refreshHistory(generation);}catch(error){if(generation===epoch)fail(error);}
  }
}
async function admitChat(
  s: ChatSession,
  start: () => Promise<AIReply>,
  adopt: (reply: AIReply) => void,
  currentRoute = () => true,
) {
  s.starting=true;s.busy=true;s.error='';s.response=undefined;s.requestId='';
  const generation=epoch,scope=s.scope;
  const current=()=>generation===epoch&&s.scope===scope;
  try {
    const reply=await start();
    if(generation!==epoch)return false;
    const visible=current()&&currentRoute();
    requests.set(reply.requestId,{session:s,scope:visible?scope:-1,sequence:-1,terminal:false});
    if(visible){s.requestId=reply.requestId;adopt(reply);}
    else if(current())s.busy=false;
    const queue=pendingEvents.get(reply.requestId)||[];pendingEvents.delete(reply.requestId);
    for(const pending of queue)await receive(pending.event,pending.kind,generation);
    if(visible){
      await loadConversation(s,reply.conversationId,generation,reply.requestId);
      if(s===ai.assistant&&current()&&s.requestId===reply.requestId)state.conversation=s.conversation;
    }
    return true;
  } catch(error) {
    if(current()){s.error=error instanceof ServiceError?error.code:'app.unexpected';s.busy=false;}
    return false;
  } finally {
    if(current())s.starting=false;
    if(![ai.assistant,ai.playground].some(x=>x.starting))pendingEvents.clear();
  }
}

function chatConversation(id: string, playground = false): Conversation {
  return {id,title:'',createdAt:'',updatedAt:'',messages:[],tasks:[],playground};
}

export function screenConversation(c = state.conversation) {
  return c&&!c.playground&&!c.tasks?.length&&!c.messages?.some(m=>m.taskId||m.providerId==='mock')?c:undefined;
}

export function screenAnalysisBlocked() {
  const s=ai.assistant;
  return s.busy||s.starting||s.importing||s.attachments.length>0;
}

export async function startScreenAnalysis(input: ScreenAnalysisInput) {
  const s=ai.assistant;
  if(!state.connected||screenAnalysisBlocked()||screenConversation()?.id!==input.conversationId)return false;
  const route=state.route;
  return admitChat(s,()=>call<AIReply>('StartScreenAnalysis',{
    consentId:input.consentId,content:input.content,confirmSend:true,confirmProviderSwitch:input.confirmProviderSwitch,
    stream:input.stream,maxOutputTokens:s.maxOutputTokens,
  }),reply=>{
    if(s.conversation?.id!==reply.conversationId)s.conversation=chatConversation(reply.conversationId);
    if(s.draft.trim()===input.content||!input.conversationId)s.draft='';
    selectProvider(s,input.providerId);selectModel(s,input.modelId);
    s.imageDetail=input.imageDetail;s.stream=input.stream;s.screenRequestId=reply.requestId;
    ai.mode='real';state.inspectorOpen=false;
    if(!input.keepComputerOpen)state.route='assistant';
  },()=>state.route===route);
}

export async function sendAI(s:ChatSession,playground=false,test=false){
  if(s.busy||s.starting||s.importing||!state.connected)return;
  const p=profile(s),m=model(s);if(!p?.enabled||!m||m.capabilities.text!=='SUPPORTED')return;
  if(!test&&s.attachments.length&&m.capabilities.vision!=='SUPPORTED'){s.error='MODEL_IMAGE_UNSUPPORTED';return;}
  if(!test&&!s.draft.trim()&&!s.attachments.length)return;
  const conversation = playground ? s.conversation : screenConversation(s.conversation);
  const previous=conversation?.messages.filter(message=>message.providerId).at(-1)?.providerId;const switched=!!previous&&previous!==s.providerId;
  if(switched&&!window.confirm(t('ai.switchConfirm')))return;
  const outputCost=m.pricing?s.maxOutputTokens*m.pricing.outputPerMillion/1000000:null;
  const estimate=outputCost==null?t('ai.priceUnknown'):t('ai.outputEstimate',{cost:outputCost.toFixed(6),currency:m.pricing!.currency});
  const threshold=ai.settings.warningThreshold;const exceeded=threshold!==null&&outputCost!==null&&outputCost>threshold;
  const warning=t(!test&&s.attachments.length?'ai.imageConfirm':'ai.sendConfirm',{provider:p.name,model:m.displayName||m.id})+'\n'+estimate+(exceeded?'\n'+t('ai.thresholdExceeded'):'')+'\n'+t('ai.thresholdNotice',{threshold:threshold??t('common.unknown')});
  if(!window.confirm(warning))return;
  await admitChat(s,()=>call<AIReply>(test?'TestAIConnection':'StartChat',{
      conversationId:conversation?.id,providerId:s.providerId,modelId:s.modelId,content:test?t('ai.testPrompt'):s.draft.trim(),
      attachmentIds:test?[]:s.attachments.map(a=>a.id),stream:!test&&s.stream&&m.capabilities.streaming==='SUPPORTED',maxOutputTokens:s.maxOutputTokens,
      temperature:m.capabilities.temperature==='SUPPORTED'?s.temperature??undefined:undefined,imageDetail:s.imageDetail,confirmSend:true,confirmProviderSwitch:switched,playground,
  }),reply=>{
    if(s.conversation?.id!==reply.conversationId)s.conversation=chatConversation(reply.conversationId,playground);
    if(!test){s.draft='';s.attachments=[];}
  });
}
export async function cancelAI(s:ChatSession){if(!s.requestId)return;try{await call('CancelChat',{requestId:s.requestId});}catch(error){fail(error);}}
export async function startAgentAnalysis(id:string,contextHash:string){
  const s=ai.assistant,p=profile(s),m=model(s);
  if(!state.connected||s.busy||s.starting||s.importing||!p?.enabled||!m||m.capabilities.text!=='SUPPORTED')return false;
  if(!window.confirm(t('ai.sendConfirm',{provider:p.name,model:m.displayName||m.id})+'\n'+t('ai.privacyNotice')))return false;
  return admitChat(s,()=>call<AIReply>('AnalyzeAgentTask',{id,contextHash,providerId:p.id,modelId:m.id,confirmSend:true,stream:s.stream&&m.capabilities.streaming==='SUPPORTED'}),reply=>{
    s.conversation=chatConversation(reply.conversationId);ai.mode='real';state.inspectorOpen=false;state.route='assistant';
  });
}
export async function importImages(s:ChatSession,input:HTMLInputElement){
  if(s.importing||s.busy)return;const files=Array.from(input.files||[]);input.value='';const scope=s.scope,generation=epoch;
  if (!files.length) return;
  if (files.some(file => file.type.startsWith('video/') || /\.(mp4|m4v|mov|avi|mkv|webm|mpeg|mpg|wmv|flv|3gp)$/i.test(file.name))) {
    s.error = 'VIDEO_INPUT_UNAVAILABLE';
    return;
  }
  if (model(s)?.capabilities.vision !== 'SUPPORTED') {
    s.error = 'MODEL_IMAGE_UNSUPPORTED';
    return;
  }
  const current=()=>scope===s.scope&&generation===epoch;s.importing=true;s.error='';
  try{for(const file of files){
    if(!current())break;if(file.size>20*1024*1024)throw new ServiceError('ATTACHMENT_TOO_LARGE');
    if(!['image/png','image/jpeg','image/webp','image/gif'].includes(file.type))throw new ServiceError('INVALID_ATTACHMENT');
    const data=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]||'');reader.onerror=()=>reject(new ServiceError('INVALID_ATTACHMENT'));reader.readAsDataURL(file);});
    if(!current())break;const attachment=await call<AIAttachment>('ImportAttachment',{name:file.name,dataBase64:data});
    if(!current()){void discardUnsent([attachment]);break;}s.attachments.push(attachment);
  }}catch(error){if(current())s.error=error instanceof ServiceError?error.code:'INVALID_ATTACHMENT';}finally{if(current())s.importing=false;}
}
export async function removeAttachment(s:ChatSession,id:string){try{await call('DeleteAttachment',{id});s.attachments=s.attachments.filter(a=>a.id!==id);}catch(error){fail(error);}}
export async function clearAI(s:ChatSession,remove=false){
  if(s.busy||!s.conversation||!window.confirm(t(remove?'ai.deleteConversationConfirm':'ai.clearConfirm')))return;const id=s.conversation.id;
  try{await call(remove?'DeleteChatConversation':'ClearChatConversation',{id,deleteAttachments:true,confirm:true});resetChatDraft(s);s.conversation=null;if(state.conversation?.id===id)state.conversation=null;state.conversations=await call<Conversation[]>('ListConversations');}catch(error){fail(error);}
}
export function emptyModel():AIModel{return {id:'',displayName:'',capabilities:{text:'SUPPORTED',vision:'UNKNOWN',streaming:'UNKNOWN',toolCalling:'UNSUPPORTED',structuredOutput:'UNKNOWN',computerUse:'UNSUPPORTED',temperature:'UNKNOWN',contextLimit:null,imageDetails:['auto']},pricing:null};}
