import { Call } from '/wails/runtime.js';

const params=new URLSearchParams(location.search);
const stage=Number(params.get('stage'));
const checks={},timings={},rounds=[];
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const rpc=(method,payload={})=>Call.ByName('main.DesktopService.Call',method,JSON.stringify(payload));
async function call(method,payload){const reply=await rpc(method,payload);if(reply.error)throw Error(method+' '+reply.error.code);return reply.data;}
function check(name,value){checks[name]=Boolean(value);if(!value)throw Error(name);}
async function until(name,read,ms=15000){const deadline=performance.now()+ms;do{const result=await read();if(result)return result;await sleep(50);}while(performance.now()<deadline);throw Error('timeout '+name);}
const byId=id=>document.querySelector(`[data-testid="${id}"]`);
async function click(name,selector){const node=await until(name,()=>{const element=document.querySelector(selector);return element&&!element.disabled?element:null;});node.click();}
async function measured(name,fn){const start=performance.now();const value=await fn();timings[name]=Math.round(performance.now()-start);return value;}
let taskId='';
const task=()=>call('GetAgentTask',{id:taskId});
async function trackRound(want,actionKind=''){
  const prefix=`[data-testid="agent02-run"][data-task-id="${taskId}"]`;
  const before=await task();
  await measured(`context${want}Ms`,()=>click('review context',`${prefix} [data-testid="agent02-review"]`).then(()=>until('context',()=>document.querySelector(`${prefix} [data-testid="agent02-preview"] pre`))));
  const preview=document.querySelector(`${prefix} [data-testid="agent02-preview"] pre`).textContent;
  const payload=JSON.parse(preview);
  check(`reviewIncludesFixture${want}`,payload.projectContext.files.some(f=>f.path==='calc.go'&&f.editable)&&!preview.includes('DEEPSEEK_API_KEY'));
  if(want===1&&params.has('gitFixture'))check('reviewShowsGitAndSelectedDirtyFile',payload.projectContext.git.status==='AVAILABLE'&&payload.projectContext.git.unstaged===1&&payload.projectContext.files.some(f=>f.path==='calc.go'&&f.gitDirty));
  if(want===3)check('actualTestOutputInPreview',payload.observations.some(o=>o.kind==='command'&&JSON.parse(o.evidence).exitCode===0&&o.evidence.includes('fixture')));
  await click('confirm send',`${prefix} [data-testid="agent02-send"]`);
  const t=await until('model result',async()=>{const v=await task();return v.agent.rounds.length===before.agent.rounds.length+1&&v.status!=='RUNNING'?v:null;},62000);
  const round=t.agent.rounds.at(-1);
  rounds.push({version:round.version,status:round.status,error:round.error||'',durationMs:round.durationMs,usage:round.usage,estimatedUsd:round.estimatedUsd,stepTypes:round.plan?.steps.map(x=>x.type)||[]});
  if(round.error){
    if(params.has('gitFixture')&&['AGENT_PLAN_INVALID','AGENT_EMPTY_PATCH'].includes(round.error)&&t.status==='NEEDS_REVIEW'){
      check(`invalidPlanDidNotCreateEffect${round.version}`,!round.actionId);
      return false;
    }
    throw Error(`Round ${want} ${round.error}`);
  }
  check(`modelPlan${want}`,round.status==='COMPLETED'&&round.version===before.agent.rounds.length+1&&round.contextHash.length===64&&!!round.plan);
  if(!actionKind){check('finalTaskReport',t.status==='COMPLETED'&&t.summary.length>3);return true;}
  check(`waiting${want}`,t.status==='WAITING_APPROVAL'&&!!round.actionId);
  const readAction=async()=> (await call('GetMVPState')).actions.find(a=>a.id===round.actionId);
  const proposal=await readAction();
  check(`boundProposal${want}`,proposal.kind===actionKind&&proposal.taskId===taskId&&proposal.proposal.planVersion===round.version&&proposal.proposal.policyVersion==='agent-02-v1'&&proposal.proposal.stepId===t.steps.at(-1).id);
  if(actionKind==='file'){
    if(params.has('gitFixture'))check('dirtyFileWarningAndExactBase',proposal.proposal.gitDirtyFile&&proposal.proposal.file.beforeContent.includes('Workspace author note')&&!!await until('Git warning',()=>byId('community-git-warning')));
    check('exactDiffVisible',!!await until('diff preview',()=>document.querySelector(`[data-action-id="${proposal.id}"] [data-testid="product-diff"]`)?.textContent.includes('a + b')));
    const bad=await rpc('ApproveProductAction',{id:proposal.id,hash:'invalid'});check('invalidApprovalDenied',!!bad.error);
  }else{
    check('registeredGoTest',proposal.proposal.command.id==='go.test'&&proposal.proposal.command.args[0]==='test');
    check('networkConsentRequired',(await rpc('ApproveProductAction',{id:proposal.id,hash:proposal.hash})).error?.code==='COMMAND_NETWORK_CONSENT');
    await click('command network consent',`[data-action-id="${proposal.id}"] [data-testid="agent02-network-consent"]`);
  }
  const selector=`[data-action-id="${proposal.id}"] [data-testid="product-approve"]`;
  const done=await measured(`${actionKind}ExecutionMs`,async()=>{
    await click('per-action approval',selector);
    return until('approved action result',async()=>{const row=await readAction();return row&& !['PENDING','EXECUTING'].includes(row.status)?row:null;},50000);
  });
  check(`approvedActionVerified${want}`,done.status==='SUCCEEDED'&&(actionKind==='file'?done.result.verified:done.result.exitCode===0));
  check(`singleConsumption${want}`,!!(await rpc('ApproveProductAction',{id:proposal.id,hash:proposal.hash})).error);
  return true;
}

async function run(){
 await until('launch ready',()=>byId('nav-workflows'));
 timings.navigationReadyMs=Math.round(performance.now());
 if(stage===13){
   const current=await call('GetMVPState');
   const restored=current.agentTasks.find(t=>t.agent?.rounds.filter(round=>round.status==='COMPLETED').length===3&&t.status==='COMPLETED');
   check('threeRoundsRestored',!!restored);
   check('effectsRestored',current.actions.filter(a=>a.taskId===restored.id&&a.status==='SUCCEEDED').length===2);
   check('noReplay',current.actions.filter(a=>a.taskId===restored.id).length===2);
   return;
 }
 const modelId=stage===12?'deepseek-flash':'agent-fixture';
 let providerId='deepseek-live-acceptance';
 if(params.has('communityProfileAgent')){
   const provider=await call('GetCommunityProvider');
   check('usesVerifiedDPAPIProfile',provider.hasCredential&&provider.verifiedAt&&provider.defaultUsable&&provider.defaultModelId===modelId);
   providerId=provider.defaultProviderId;
 }
 if(stage===11){
   const profile=await call('SaveAIProfile',{profile:{id:'',name:'Agent loopback fixture',baseUrl:params.get('fixture'),protocol:'chat-completions',enabled:true,trustedHttp:true,models:[{id:modelId,displayName:modelId,capabilities:{text:'SUPPORTED',vision:'UNSUPPORTED',streaming:'UNSUPPORTED',toolCalling:'UNSUPPORTED',structuredOutput:'SUPPORTED',computerUse:'UNSUPPORTED',temperature:'UNSUPPORTED',contextLimit:65536,imageDetails:[]},pricing:{inputPerMillion:0.30,outputPerMillion:1.20,cachedInputPerMillion:0.006,currency:'USD',effectiveAt:'2026-11-11'}}]}});
   providerId=profile.id;
 }
 const workspace=await call('PickWorkspace');
 if(params.has('gitFixture')){
   const inspection=await call('InspectDeveloperProject',{workspaceId:workspace.id,exclusions:[]});
   check('detectedGoAndReadOnlyGit',inspection.summary.types.includes('Go')&&inspection.git.branch==='community-fixture'&&inspection.git.staged===3&&inspection.git.unstaged===1&&inspection.git.untracked===1);
 }
 const before=await call('ReadWorkspaceFile',{workspaceId:workspace.id,path:'calc.go'});
 check('knownBrokenFixture',before.content.includes('a - b'));
 const input={workspaceId:workspace.id,goal:'Fix only the Go Add function in calc.go: it subtracts instead of adding. First propose the precise patch. After approved write, propose go.test with ./... to test this small project, then summarize the observed result. Do not edit tests.'+(params.has('gitFixture')?' Preserve the existing comment and all other working-copy changes.':''),providerId,modelId,paths:['calc.go','calc_test.go']};
 if(params.has('gitFixture')){input.paths=[];input.autoSelect=true;}
 const created=await call('CreateAgentRun',input);
 taskId=created.id;
 if(params.has('gitFixture'))check('boundedRelevantSourcesSuggested',created.agent.paths.includes('calc.go')&&created.agent.paths.includes('calc_test.go')&&created.agent.paths.length<=3);
 check('unreviewedRequestDenied',!!(await rpc('PlanAgentRun',{id:taskId,contextHash:'0'.repeat(64)})).error);
 await measured('pageSwitchMs',()=>click('Tasks route','[data-testid="nav-workflows"]').then(()=>until('Tasks rendered',()=>byId('product-tasks'))));
 for(const [want,kind] of [[1,'file'],[2,'command'],[3,'']]){
   let completed=false;
   for(let attempt=0;attempt<3&&!completed;attempt++)completed=await trackRound(want,kind);
   check(`reviewRoundCompleted${want}`,completed);
 }
 const after=await call('ReadWorkspaceFile',{workspaceId:workspace.id,path:'calc.go'});
 check('independentFileRead',after.content.includes('a + b')&&before.hash!==after.hash);
 const settings=await call('GetSettings');await call('UpdateSettings',{...settings,appLanguage:'zh-CN'});
 check('languageSwitch',!!await until('Chinese locale',()=>document.documentElement.lang==='zh-CN'));
}

let error='';
try{await run();}catch(e){error=/^[A-Za-z0-9 _.]+$/.test(e?.message||'')?String(e.message).slice(0,140):'Acceptance failed';}
await Call.ByName('main.AcceptanceService.Report',JSON.stringify({stage,passed:!error,error,checks,timings,rounds,source:params.has('communityProfileAgent')?'REAL_DEEPSEEK_DPAPI_PROFILE':stage===12?'REAL_DEEPSEEK':'LOOPBACK_OR_RESTART'}));
