// Own-WebView + loopback fixture acceptance. No paid or external model is called.
import { Call, Events } from '/wails/runtime.js';
const params=new URLSearchParams(location.search), stage=Number(params.get('stage'));
const checks={},events=[];let error='';
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
function check(name,value){checks[name]=!!value;if(!value)throw new Error(name);}
async function call(method,payload={}){const reply=await Call.ByName('main.DesktopService.Call',method,JSON.stringify(payload));if(reply.error)throw new Error(reply.error.code);return reply.data;}
async function until(fn){for(let i=0;i<300;i++){const value=await fn();if(value)return value;await sleep(50);}throw new Error('Acceptance condition timed out');}
const unsub=['started','delta','completed','failed','cancelled'].map(kind=>Events.On('assistant.stream.'+kind,event=>{let data=event.data;if(Array.isArray(data))data=data[0];events.push({...data,kind});}));
const caps={text:'SUPPORTED',vision:'SUPPORTED',streaming:'SUPPORTED',toolCalling:'UNSUPPORTED',structuredOutput:'UNKNOWN',computerUse:'UNSUPPORTED',temperature:'SUPPORTED',contextLimit:32768,imageDetails:['auto','low','high']};
const model={id:'fixture-model',displayName:'Local fixture',capabilities:caps,pricing:{inputPerMillion:1,outputPerMillion:2,cachedInputPerMillion:0.5,currency:'USD',effectiveAt:'2026-11-11'}};
const input=(providerId,content,extra={})=>({providerId,modelId:model.id,content,attachmentIds:[],stream:true,maxOutputTokens:128,imageDetail:'auto',confirmSend:true,confirmProviderSwitch:false,playground:false,...extra});
async function terminal(requestId){await until(()=>events.find(e=>e.requestId===requestId&&['completed','failed','cancelled'].includes(e.kind)));return (await call('GetUsage')).find(x=>x.requestId===requestId);}
try {
 await until(()=>document.querySelector('[data-testid="composer"]'));
 if(stage===6){
  const profiles=await call('ListAIProfiles');check('profilesRestored',profiles.some(p=>p.id==='fixture-chat'&&p.hasCredential));
  const records=await call('GetUsage');check('usageRestored',records.length>=4);check('noRunningRequests',records.every(r=>r.status!=='RUNNING'));
  const conversations=await call('ListConversations');let restored=false;for(const c of conversations){const full=await call('GetConversation',{id:c.id});if(full.messages.some(m=>m.providerId==='fixture-responses'&&m.content.includes('Fixture')))restored=true;}check('chatRestored',restored);
  document.querySelector('[data-testid="nav-playground"]').click();await until(()=>document.querySelector('[data-testid="ai-playground"]'));
  const provider=await until(()=>document.querySelector('[data-testid="ai-provider"]'));provider.value='fixture-chat';provider.dispatchEvent(new Event('change',{bubbles:true}));
  const prompt=await until(()=>document.querySelector('[data-testid="ai-composer"]'));prompt.value='UI Playground loopback test';prompt.dispatchEvent(new Event('input',{bubbles:true}));
  const confirm=window.confirm;window.confirm=()=>true;
  try {const send=await until(()=>{const b=document.querySelector('[data-testid="playground-send"]');return b&&!b.disabled?b:null;});send.click();await until(async()=>{const usage=await call('GetUsage');return usage.length>records.length&&usage[0].status==='COMPLETED';});} finally {window.confirm=confirm;}
  await until(()=>document.querySelector('[data-testid="ai-playground"]').textContent.includes('Fixture response'));check('playgroundUIRequest',true);
  document.querySelector('[data-testid="nav-history"]').click();const historic=await until(()=>[...document.querySelectorAll('.history-conversation')].find(e=>e.textContent.includes('UI Playground loopback test')));historic.click();await until(()=>document.querySelector('[data-testid="ai-playground"]')?.textContent.includes('Fixture response'));check('playgroundHistoryMode',true);
  document.querySelector('[data-testid="nav-usage"]').click();await until(()=>document.querySelector('[data-testid="ai-usage"]'));check('usageUI',document.querySelector('[data-testid="ai-usage"]').textContent.includes('fixture-chat'));
  document.querySelector('[data-testid="nav-models"]').click();const profile=await until(()=>document.querySelector('[data-testid="ai-profile-fixture-chat"]'));profile.click();const secret=await until(()=>document.querySelector('[data-testid="api-key"]'));secret.value='fixture-secret';secret.dispatchEvent(new Event('change',{bubbles:true}));await until(()=>secret.value==='');check('keyInputCleared',true);
  document.querySelector('[data-testid="nav-playground"]').click();await until(()=>document.querySelector('[data-testid="ai-playground"]'));
 }else{
  const baseUrl=params.get('fixture');check('loopbackFixtureOnly',baseUrl?.startsWith('http://127.0.0.1:'));
  for(const [id,protocol] of [['fixture-chat','chat-completions'],['fixture-responses','responses']]){
   const profile=await call('SaveAIProfile',{profile:{id,name:id,protocol,baseUrl,enabled:true,trustedHttp:false,models:[model]}});check(id+'Saved',profile.id===id);
   const saved=await call('StoreCredential',{providerId:id,secret:'fixture-secret'});check(id+'Vault',saved.hasCredential);
  }
  check('noSecretInProfiles',!JSON.stringify(await call('ListAIProfiles')).includes('fixture-secret'));
  const first=await call('StartChat',input('fixture-chat','hello'));
  let record=await terminal(first.requestId);check('chatStreamComplete',record.status==='COMPLETED');check('providerUsage',record.usage.inputTokens===12&&record.usage.totalTokens===18&&record.usage.source==='PROVIDER_REPORTED');check('snapshotCost',record.estimatedCost>0&&record.pricing.currency==='USD');
  const message=(await call('GetConversation',{id:first.conversationId})).messages.find(m=>m.id===first.messageId);check('finalMessageDurable',message?.content.includes('世界')&&message.requestId===first.requestId);
  check('streamDeltas',events.some(e=>e.requestId===first.requestId&&e.kind==='delta'));
  const next=await call('StartChat',input('fixture-chat','second turn',{conversationId:first.conversationId}));check('multiTurn',(await terminal(next.requestId)).status==='COMPLETED');
  const other=await call('StartChat',input('fixture-responses','responses protocol'));check('responsesComplete',(await terminal(other.requestId)).status==='COMPLETED');
  const attachment=await call('ImportAttachment',{name:'selected-image.gif',dataBase64:'R0lGODlhAQABAIAAAAD/AP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'});check('imageImported',attachment.width===1&&attachment.height===1&&attachment.previewDataUrl.startsWith('data:image/'));
  const imageRequest=await call('StartChat',input('fixture-responses','Describe the selected image',{attachmentIds:[attachment.id]}));check('imageProtocolComplete',(await terminal(imageRequest.requestId)).status==='COMPLETED');
  const imageMessage=(await call('GetConversation',{id:imageRequest.conversationId})).messages.find(m=>m.id===imageRequest.messageId);check('imageDataSent',imageMessage?.content.includes('Fixture image received'));
  const broken=await call('StartChat',input('fixture-chat','INTERRUPT_REQUEST'));record=await terminal(broken.requestId);check('interruptedNotCompleted',record.status!=='COMPLETED'&&['STREAM_INTERRUPTED','REQUEST_OUTCOME_UNKNOWN'].includes(record.errorCode));
  const slow=await call('StartChat',input('fixture-chat','SLOW_REQUEST'));await until(()=>events.find(e=>e.requestId===slow.requestId&&e.kind==='delta'));await call('CancelChat',{requestId:slow.requestId});record=await terminal(slow.requestId);check('cancelled',record.status==='CANCELLED');
  const partial=(await call('GetConversation',{id:slow.conversationId})).messages.find(m=>m.id===slow.messageId);check('cancelRetainsPartial',partial?.content.length>0);
  check('noRealTools',(await call('GetToolExecutions')).length===0);
  const settings=await call('GetSettings');await call('UpdateSettings',{...settings,appLanguage:'zh-CN'});await until(()=>document.documentElement.lang==='zh-CN');check('chineseSwitch',true);
 }
 if(params.has('visual')){globalThis.__asimiAcceptanceReady={stage};await until(()=>globalThis.__asimiAcceptanceContinue);}
} catch(e){error=String(e);}
for(const off of unsub)off();
await Call.ByName('main.AcceptanceService.Report',JSON.stringify({stage,passed:!error,checks,error,eventCount:events.length}));
