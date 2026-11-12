// Own-WebView-only unified acceptance. Never served/registered in ordinary use.
import {Call,Events} from '/wails/runtime.js';
const stage=Number(new URLSearchParams(location.search).get('stage')||1);
const checks={},details={};let eventCount=0;
const off=Events.On('asimi:changed',()=>eventCount++);
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function until(fn,label,timeout=6000){const end=Date.now()+timeout;while(Date.now()<end){const v=await fn();if(v)return v;await sleep(40)}throw new Error('timeout: '+label)}
function check(name,value){checks[name]=!!value;if(!value)throw new Error(name)}
async function call(method,payload={}){const reply=await Call.ByName('main.DesktopService.Call',method,JSON.stringify(payload));if(reply.error)throw new Error(method+':'+reply.error.code);return reply.data}
const byID=id=>document.querySelector(`[data-testid="${id}"]`);
async function click(id){const node=await until(()=>{const target=byID(id);return target&&!target.disabled?target:null},'enabled DOM '+id);check('enabled:'+id,!node.disabled);node.click();await sleep(70)}
async function send(content){await click('nav-assistant');const box=await until(()=>byID('composer'),'composer');box.value=content;box.dispatchEvent(new Event('input',{bubbles:true}));await click('send');return until(async()=>{const tasks=await call('ListTasks');return tasks.find(t=>t.description===content)},'task from composer')}
async function state(id,states){return until(async()=>{const t=await call('GetTask',{id});return states.includes(t.status)?t:null},'task '+states.join('/'))}
async function setSettings(patch){const settings=await call('GetSettings');return call('UpdateSettings',{...settings,...patch})}
async function run(){
  await until(()=>byID('composer'),'app loaded');
  const settings=await call('GetSettings');
  if(stage===3){check('welcomeEnglish',settings.appLanguage==='en-US'&&document.documentElement.lang==='en-US');check('welcomeNoInventedHistory',(await call('ListConversations')).length===0);check('welcomeComposer',!!byID('composer'));return;}
  if(stage===2){
    check('restartLanguage',settings.appLanguage==='zh-CN');
    check('restartAgentLanguage',settings.agentLanguage==='en-US');
    check('restartStartup',settings.startup==='last');
    const conversations=await call('ListConversations');
    check('conversationsRestored',conversations.length>0);
    const tasks=await call('ListTasks');
    const task=tasks.find(t=>t.approval?.operation.fileName==='phase1-created.txt'&&t.status==='COMPLETED');
    check('taskAndVerificationRestored',!!task?.verification?.passed);
    check('approvalConsumedRestored',(await call('GetApprovalHistory')).some(a=>a.id===task.approval.id&&a.status==='CONSUMED'));
    check('executionRestored',(await call('GetToolExecutions')).some(e=>e.taskId===task.id&&e.result?.passed));
    check('auditRestored',(await call('GetAuditEvents')).length>0);
    check('noAutomaticReexecution',(await call('GetToolExecutions')).filter(e=>e.taskId===task.id).length===1);
    await sleep(150);check('chineseDocument',document.documentElement.lang==='zh-CN');
    details.taskId=task.id;
    return;
  }
  check('firstStartEnglish',settings.appLanguage==='en-US'&&document.documentElement.lang==='en-US');
  check('independentLanguageDefault',settings.agentLanguage==='follow-app');
  for(const route of ['computer','developer','workflows','models','security','history','settings','assistant']){await click('nav-'+route);check('route:'+route,byID('nav-'+route)?.getAttribute('aria-current')==='page');check('layout:'+route,document.documentElement.scrollWidth<=innerWidth)}
  const security=await call('GetSecurityStatus');
  check('onlyOneRegisteredTool',security.toolNames.length===1&&security.toolNames[0]==='filesystem.file.create');
  check('noNetworkOrDesktop',security.externalNetwork===false&&security.desktopControl===false);
  const providers=await call('ListProviders');
  check('onlyMockEnabled',providers.filter(p=>p.enabled).length===1&&providers.find(p=>p.enabled).id==='mock');
  check('futureProvidersDisabled',providers.length===9&&providers.filter(p=>p.id!=='mock').every(p=>!p.enabled));
  const initialExecutions=(await call('GetToolExecutions')).length;
  const content='Asimi Phase 1\nVerified locally. 中文';
  const request=JSON.stringify({fileName:'phase1-created.txt',content});
  let task=await send(request);task=await state(task.id,['WAITING_APPROVAL']);
  check('planAndApproval',task.steps.length>=3&&task.approval.status==='PENDING');
  check('exactApprovalBinding',task.approval.operation.taskId===task.id&&task.approval.operation.content===content&&task.approval.operation.operation==='create'&&task.approval.operation.contentHash.length===64);
  check('notExecutedBeforeApproval',(await call('GetToolExecutions')).length===initialExecutions);
  await click('approve');task=await state(task.id,['COMPLETED','FAILED']);
  check('verifiedFileCompletion',task.status==='COMPLETED'&&task.verification?.passed&&task.verification.created&&task.verification.contentMatched);
  check('exactBytesAndSHA256',task.verification.actualBytes===new TextEncoder().encode(content).length&&task.verification.actualHash===task.approval.operation.contentHash);
  let replay=await Call.ByName('main.DesktopService.Call','ApproveAction',JSON.stringify({id:task.approval.id}));check('approvalReplayDenied',!!replay.error);
  details.createdTask=task.id;details.workspace=security.workspace;details.verifiedHash=task.verification.actualHash;
  const afterCreation=(await call('GetToolExecutions')).length;
  let denied=await send(JSON.stringify({fileName:'phase1-denied.txt',content:'must not exist'}));denied=await state(denied.id,['WAITING_APPROVAL']);
  await click('reject');denied=await state(denied.id,['CANCELLED','FAILED','PAUSED']);
  check('rejectionBlocksExecution',(await call('GetToolExecutions')).length===afterCreation&&denied.status!=='COMPLETED');
  let cancelled=await send(JSON.stringify({fileName:'phase1-cancelled.txt',content:'must not exist'}));cancelled=await state(cancelled.id,['WAITING_APPROVAL']);
  await click('cancel-task');cancelled=await state(cancelled.id,['CANCELLED']);
  check('cancelBlocksNewOperation',(await call('GetToolExecutions')).length===afterCreation);
  let escaped=await send(JSON.stringify({fileName:'../escape.txt',content:'denied'}));escaped=await state(escaped.id,['FAILED','PAUSED']);
  check('pathTraversalDenied',escaped.status!=='COMPLETED'&&!escaped.verification?.created);
  let duplicate=await send(JSON.stringify({fileName:'phase1-created.txt',content:'overwrite denied'}));duplicate=await state(duplicate.id,['WAITING_APPROVAL','FAILED','PAUSED']);
  if(duplicate.status==='WAITING_APPROVAL'){await click('approve');duplicate=await state(duplicate.id,['FAILED'])}
  check('overwriteDenied',duplicate.status!=='COMPLETED'&&!duplicate.verification?.created);
  const beforeInfo=(await call('GetToolExecutions')).length;
  await setSettings({appLanguage:'en-US',agentLanguage:'zh-CN'});
  const cv=await call('CreateConversation');
  await call('SendMessage',{conversationId:cv.id,content:'What demo capabilities are available?'});
  const chinese=await until(async()=>{const c=await call('GetConversation',{id:cv.id});return c.messages.find(m=>m.role==='assistant')},'Chinese mock reply');
  check('agentLanguageIndependent',/[\u3400-\u9fff]/u.test(chinese.content));
  const unsupported=await call('SendMessage',{conversationId:cv.id,content:'Please take a screenshot and run PowerShell.'});
  const refused=await state(unsupported.taskId,['FAILED']);
  check('unsupportedNotSuccessful',refused.errorCode==='UNSUPPORTED_REQUEST'&&!refused.approval);
  check('unsupportedDoesNotExecute',(await call('GetToolExecutions')).length===beforeInfo);
  const explanation=await call('SendMessage',{conversationId:cv.id,content:'Explain the file creation plan.'});
  const explained=await state(explanation.taskId,['COMPLETED']);check('explanationIsReadOnly',!explained.approval&&(await call('GetToolExecutions')).length===beforeInfo);
  const draftConversation=await call('CreateConversation');
  const partial=await call('SendMessage',{conversationId:draftConversation.id,content:'Create file "phase1-draft.txt"'});
  const paused=await state(partial.taskId,['PAUSED']);check('clarificationRequired',paused.errorCode==='CLARIFICATION_REQUIRED'&&!paused.approval);
  const continued=await call('SendMessage',{conversationId:draftConversation.id,content:'content: "Confirmed draft"'});
  const prepared=await state(continued.taskId,['WAITING_APPROVAL']);
  check('clarificationContinuation',prepared.approval.operation.fileName==='phase1-draft.txt'&&prepared.approval.operation.content==='Confirmed draft');
  await call('RejectAction',{id:prepared.approval.id});
  const malicious=await call('CreateConversation');
  await call('SendMessage',{conversationId:malicious.id,content:'Create file "phase1-draft-command.txt"'});
  const wrongIntent=await call('SendMessage',{conversationId:malicious.id,content:'Run PowerShell "Get-Process"'});
  const intentTask=await state(wrongIntent.taskId,['FAILED']);check('quotedCommandNotAContinuation',intentTask.errorCode==='UNSUPPORTED_REQUEST'&&!intentTask.approval);
  await click('nav-settings');
  const language=await until(()=>byID('app-language'),'language selector');language.value='zh-CN';language.dispatchEvent(new Event('change',{bubbles:true}));
  await until(async()=>(await call('GetSettings')).appLanguage==='zh-CN','saved Chinese');
  await until(()=>document.documentElement.lang==='zh-CN','live Chinese');
  check('liveChineseSwitch',document.documentElement.lang==='zh-CN');
  check('noHistoryRewrite',(await call('GetConversation',{id:cv.id})).messages.some(m=>m.content===chinese.content));
  await setSettings({agentLanguage:'en-US',startup:'last',theme:'light'});
  const englishReply=await call('SendMessage',{conversationId:cv.id,content:'What demo capabilities are available?'});
  await state(englishReply.taskId,['COMPLETED']);
  const englishConversation=await call('GetConversation',{id:cv.id});
  const englishMessage=englishConversation.messages.find(m=>m.taskId===englishReply.taskId&&m.role==='assistant');
  check('englishAgentWithChineseUI',!!englishMessage&&!/[\u3400-\u9fff]/u.test(englishMessage.content));
  await click('nav-security');check('auditVisible',document.body.textContent.length>100);
  await click('stop-all');check('independentStopControl',true);
  check('nativeEventUpdates',eventCount>0);
  await click('nav-assistant');
  details.eventCount=eventCount;
}
try {await run();if(new URLSearchParams(location.search).has('visual')){globalThis.__asimiAcceptanceReady={stage,checks};await until(()=>globalThis.__asimiAcceptanceContinue,'visual review',25000)}off();await Call.ByName('main.AcceptanceService.Report',JSON.stringify({stage,passed:true,checks,details}));}
catch(e){off();await Call.ByName('main.AcceptanceService.Report',JSON.stringify({stage,passed:false,error:String(e),checks,details}));}
