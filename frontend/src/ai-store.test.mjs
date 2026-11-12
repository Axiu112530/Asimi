// Run with Node 24: node frontend/src/ai-store.test.mjs
// Uses the actual Vue store with a local bridge mock. No network or credentials.
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { reactive } from 'vue';

const listeners=new Map(),calls=[],conversations=new Map();
let override;
const clone=value=>structuredClone(value);
const caps={text:'SUPPORTED',vision:'SUPPORTED',streaming:'SUPPORTED',toolCalling:'UNSUPPORTED',structuredOutput:'UNKNOWN',computerUse:'UNSUPPORTED',temperature:'SUPPORTED',contextLimit:null,imageDetails:['auto']};
const profiles=[{id:'p',name:'Local fixture',protocol:'responses',baseUrl:'http://127.0.0.1:1/v1',enabled:true,hasCredential:false,trustedHttp:false,models:[{id:'m',displayName:'Fixture',capabilities:caps,pricing:null}]}];
const settings={defaultProviderId:'p',defaultModelId:'m',visionProviderId:'p',visionModelId:'m',maxOutputTokens:1024,maxContextBytes:65536,warningThreshold:null};
const state=reactive({connected:true,conversation:null,conversations:[],route:'assistant',inspectorOpen:true});
globalThis.__aiTest={state,fail:error=>{throw error;},call:async(method,payload={})=>{
  calls.push({method,payload});if(override){const result=override(method,payload);if(result!==undefined)return await result;}
  if(method==='ListAIProfiles')return clone(profiles);
  if(method==='GetAISettings')return clone(settings);
  if(method==='GetUsage')return [];
  if(method==='ListConversations')return [...conversations.values()].map(clone);
  if(method==='GetConversation')return clone(conversations.get(payload.id));
  if(method==='DeleteAttachment'||method==='CancelChat')return true;
  if(method==='GetAttachment')return {id:payload.id,previewDataUrl:'data:image/png;base64,AA=='};
  throw new Error('Unexpected bridge call: '+method);
},subscribe:(name,fn)=>{listeners.set(name,fn);return ()=>listeners.delete(name);}};
globalThis.window={confirm:()=>true};
registerHooks({
  resolve(specifier,context,next){if(context.parentURL?.includes('/ai-store.ts')&&['./bridge','./store','./i18n'].includes(specifier))return {url:'ai-test:'+specifier.slice(2),shortCircuit:true};return next(specifier,context);},
  load(url,context,next){
    if(url==='ai-test:bridge')return {format:'module',shortCircuit:true,source:`export const call=(...args)=>globalThis.__aiTest.call(...args);export const subscribe=(...args)=>globalThis.__aiTest.subscribe(...args);export class ServiceError extends Error {constructor(code){super(code);this.code=code;}}`};
    if(url==='ai-test:store')return {format:'module',shortCircuit:true,source:`export const state=globalThis.__aiTest.state;export const fail=globalThis.__aiTest.fail;`};
    if(url==='ai-test:i18n')return {format:'module',shortCircuit:true,source:`export const t=key=>key;`};
    return next(url,context);
  },
});
const {ai,startAI,stopAI,sendAI,startScreenAnalysis,importImages,resetChatDraft,cancelAI}=await import('./ai-store.ts');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};};
const conversation=(id,playground=false,content='')=>({id,title:id,createdAt:'2026-11-11T00:00:00Z',updatedAt:'2026-11-11T00:00:00Z',playground,tasks:[],messages:content?[{id:'answer-'+id,conversationId:id,role:'assistant',content,createdAt:'2026-11-11T00:00:00Z',providerId:'p',modelId:'m',requestId:'r-'+id,status:'COMPLETED'}]:[]});
function emit(id,kind,sequence,contentDelta='',extra={}){listeners.get('assistant.stream.'+kind)?.({requestId:'r-'+id,messageId:'answer-'+id,conversationId:id,sequence,contentDelta,status:kind==='completed'?'COMPLETED':'RUNNING',...extra});}
function fresh(s){resetChatDraft(s);s.conversation=null;s.providerId='p';s.modelId='m';s.draft='Test';}
await startAI();assert.equal(listeners.size,5);

// A gateway may finish thousands of deltas before StartChat returns its IDs.
fresh(ai.playground);
override=(method)=>{if(method!=='StartChat')return;const content='x'.repeat(5000);conversations.set('early',conversation('early',true,content));emit('early','started',0);for(let i=1;i<=5000;i++)emit('early','delta',i,'x');emit('early','completed',5001);return {requestId:'r-early',messageId:'answer-early',conversationId:'early'};};
await sendAI(ai.playground,true);
assert.equal(ai.playground.busy,false);assert.equal(ai.playground.conversation.messages[0].content.length,5000);
emit('early','delta',6000,'late duplicate');assert.equal(ai.playground.busy,false);assert.equal(ai.playground.conversation.messages[0].content.length,5000);
console.log('PASS: 5000 early deltas retain terminal completion; late terminal duplicates ignored');

// Two simultaneous pending calls must associate early events by returned ID.
fresh(ai.assistant);fresh(ai.playground);const assistantReply=deferred(),playgroundReply=deferred();
override=(method,payload)=>method==='StartChat'?(payload.playground?playgroundReply.promise:assistantReply.promise):undefined;
const aSend=sendAI(ai.assistant),pSend=sendAI(ai.playground,true);
conversations.set('a',conversation('a',false,'Assistant result'));conversations.set('p',conversation('p',true,'Playground result'));
emit('a','delta',1,'Assistant result');emit('p','delta',1,'Playground result');emit('a','completed',2);emit('p','completed',2);
playgroundReply.resolve({requestId:'r-p',messageId:'answer-p',conversationId:'p'});await pSend;
assistantReply.resolve({requestId:'r-a',messageId:'answer-a',conversationId:'a'});await aSend;
assert.equal(ai.assistant.conversation.id,'a');assert.equal(ai.playground.conversation.id,'p');
assert.equal(ai.assistant.conversation.messages[0].content,'Assistant result');assert.equal(ai.playground.conversation.messages[0].content,'Playground result');
console.log('PASS: concurrent Assistant and Playground early events stay in their sessions');

// Duplicate or out-of-order deltas cannot append twice; cancellation calls Go.
fresh(ai.playground);conversations.set('sequence',conversation('sequence',true));
override=method=>method==='StartChat'?{requestId:'r-sequence',messageId:'answer-sequence',conversationId:'sequence'}:undefined;
await sendAI(ai.playground,true);emit('sequence','delta',1,'A');emit('sequence','delta',2,'B');emit('sequence','delta',1,'duplicate');
assert.equal(ai.playground.conversation.messages[0].content,'AB');await cancelAI(ai.playground);assert.equal(calls.at(-1).method,'CancelChat');assert.equal(calls.at(-1).payload.requestId,'r-sequence');
conversations.set('sequence',conversation('sequence',true,'AB'));emit('sequence','cancelled',3,'',{status:'CANCELLED'});await tick();assert.equal(ai.playground.busy,false);
console.log('PASS: per-request sequence deduplication and Go cancellation');

// Changing conversations clears unsent drafts, but never deletes history images.
override=undefined;const original=conversation('original');original.messages=[{id:'user',role:'user',content:'',createdAt:'',attachments:[{id:'history-image',previewDataUrl:'data:image/png;base64,AA=='}]}];
state.conversation=original;ai.assistant.attachments=[{id:'unsent-image'}];ai.assistant.draft='Private old draft';
const readers=[];globalThis.FileReader=class {readAsDataURL(){readers.push(this);}};
const input=()=>({value:'selected',files:[{name:'picked.png',type:'image/png',size:12}]});
const oldImport=importImages(ai.assistant,input());state.conversation=conversation('other');
assert.equal(ai.assistant.draft,'');assert.equal(ai.assistant.attachments.length,0);
function finishRead(){const reader=readers.shift();reader.result='data:image/png;base64,AA==';reader.onload();}
finishRead();await oldImport;
assert.ok(!calls.some(call=>call.method==='ImportAttachment'));
assert.ok(calls.some(call=>call.method==='DeleteAttachment'&&call.payload.id==='unsent-image'));
assert.ok(!calls.some(call=>call.method==='DeleteAttachment'&&call.payload.id==='history-image'));

const importReply=deferred();
override=method=>method==='ImportAttachment'?importReply.promise:undefined;
const pendingImport=importImages(ai.assistant,input());finishRead();await tick();
assert.ok(calls.some(call=>call.method==='ImportAttachment'));
state.conversation=conversation('third');
importReply.resolve({id:'late-image'});await pendingImport;
assert.equal(ai.assistant.attachments.length,0);
assert.ok(calls.some(call=>call.method==='DeleteAttachment'&&call.payload.id==='late-image'));
console.log('PASS: switching conversations discards unsent drafts and late image imports');

state.conversation=conversation('saved-playground',true,'Saved reply');
assert.equal(state.route,'playground');assert.equal(ai.playground.conversation.id,'saved-playground');
assert.notEqual(ai.assistant.conversation?.id,'saved-playground');
console.log('PASS: restored Playground history binds to Playground');

// Screen analysis uses the Assistant stream, opens a fresh conversation when the
// selected one is a Playground conversation, and keeps the local image out of chat.
resetChatDraft(ai.assistant);
state.route='computer';
override=(method,payload)=>{
  if(method!=='StartScreenAnalysis')return;
  assert.deepEqual(payload,{consentId:'synthetic-consent',content:'Read the screenshot',confirmSend:true,confirmProviderSwitch:false,stream:true,maxOutputTokens:1024});
  const result=conversation('screen',false,'Screen result');
  result.messages[0].vision={parsed:true,summary:'Screen result'};
  conversations.set('screen',result);
  emit('screen','completed',1);
  return {requestId:'r-screen',messageId:'answer-screen',conversationId:'screen'};
};
const target={consentId:'synthetic-consent',providerId:'p',modelId:'m',imageDetail:'auto',content:'Read the screenshot',confirmProviderSwitch:false,stream:true,conversationId:undefined};
await startScreenAnalysis(target);
assert.equal(state.route,'assistant');
assert.equal(state.conversation.id,'screen');
assert.equal(ai.assistant.conversation.messages[0].vision.summary,'Screen result');
assert.equal(ai.assistant.busy,false);
assert.equal(ai.assistant.attachments.length,0);
console.log('PASS: screen admission preserves early terminal events in the Assistant conversation');
stopAI();
assert.equal(listeners.size,0);
