import { Call } from '/wails/runtime.js';

const stage = Number(new URLSearchParams(location.search).get('stage'));
const checks = {}, timings = {};
const rpc = (name, input={}) => Call.ByName('main.DesktopService.Call', name, JSON.stringify(input));
async function call(name,input){const reply=await rpc(name,input);if(reply.error)throw Error(name+' '+reply.error.code);return reply.data;}
function check(name,value){checks[name]=Boolean(value);if(!value)throw Error(name);}
const byId=id=>document.querySelector(`[data-testid="${id}"]`);
async function until(name,read){for(let i=0;i<240;i++){const result=await read();if(result)return result;await new Promise(r=>setTimeout(r,50));}throw Error('timeout '+name);}
async function click(id){(await until(id,()=>{const element=byId(id);return element&&!element.disabled?element:null;})).click();}
async function timed(name,fn){const start=performance.now();const value=await fn();timings[name]=Math.round(performance.now()-start);return value;}

async function fresh(){
 await until('onboarding',()=>document.querySelector('.product-overlay'));
 check('onboardingVisible',document.querySelector('.product-overlay').getAttribute('aria-modal')==='true');
 for(const step of ['next','next','offlineNext','next','next','next'])await click('product-'+step);
 await click('product-chooseWorkspace');
 const state=await until('fixture workspace',async()=>{const current=await call('GetMVPState');return current.workspaces.length?current:null;});
 check('workspaceViaPickerBoundary',state.workspaces.length===1&&state.workspaces[0].trustStatus==='USER_SELECTED');
 const workspaceId=state.workspaces[0].id;
 await click('product-next');await click('product-finish');
 await until('setup finished',()=>!document.querySelector('.product-overlay'));
 check('homeRendered',!!byId('product-home'));
 check('initialEnglish',document.documentElement.lang==='en-US');
 const settings=await call('GetSettings');
 await call('UpdateSettings',{...settings,appLanguage:'zh-CN'});
 await until('Chinese active',()=>document.documentElement.lang==='zh-CN');check('chineseActive',true);
 await click('nav-developer');await until('developer',()=>byId('product-developer'));await click('product-analyze');
 await until('source displayed',()=>document.querySelector('.product-tree')?.textContent.includes('sample.txt'));check('explorerRendered',true);
 const report=await timed('workspaceAnalysisMs',()=>call('AnalyzeWorkspace',{workspaceId,query:'original'}));
 check('boundedSearch',report.matches.some(m=>m.path==='sample.txt')&&!report.entries.some(e=>e.path.includes('node_modules')));
 const escaped=await rpc('ReadWorkspaceFile',{workspaceId,path:'../deskpilot.db'});check('workspaceEscapeDenied',!!escaped.error);
 const file=await call('PrepareFileChange',{workspaceId,path:'sample.txt',content:'updated by approved fixture\n'});
 check('exactDiff',file.proposal.file.diff.includes('-original')&&file.proposal.file.diff.includes('+updated by approved fixture'));
 check('hashMismatchRejected',!!(await rpc('ApproveProductAction',{id:file.id,hash:'incorrect'})).error);
 const applied=await call('ApproveProductAction',{id:file.id,hash:file.hash});check('verifiedFileChange',applied.status==='SUCCEEDED'&&applied.result.verified&&applied.result.atomic);
 check('independentFileRead',(await call('ReadWorkspaceFile',{workspaceId,path:'sample.txt'})).content==='updated by approved fixture\n');
 check('singleConsumption',!!(await rpc('ApproveProductAction',{id:file.id,hash:file.hash})).error);
 const command=await call('PrepareCommand',{workspaceId,commandId:'go.test',args:['./...'],network:true});
 check('exactCommand',command.proposal.command.args.includes('test')&&command.proposal.command.executesCode&&command.proposal.command.network);
 const commandResult=await timed('targetedTestMs',()=>call('ApproveProductAction',{id:command.id,hash:command.hash}));
 check('approvedTargetedTest',commandResult.status==='SUCCEEDED'&&commandResult.result.exitCode===0);
 const diagnostics=await timed('environmentMs',()=>call('DiagnoseEnvironment'));check('environmentDiagnosis',diagnostics.length===7&&diagnostics.some(d=>d.component==='go'&&d.installed));
 await click('nav-workflows');await until('task page',()=>byId('product-tasks'));
 const task=await call('CreateAgentTask',{workspaceId,goal:'Analyze sample'});
 const terminal=await until('task completed',async()=>{const value=await call('GetAgentTask',{id:task.id});return value.status==='COMPLETED'?value:null;});
 check('taskPlanAndSteps',terminal.steps.length===3&&terminal.steps.every(s=>s.status==='COMPLETED'));
 const preview=await call('PreviewAgentTaskContext',{id:task.id});check('providerContextPreview',preview.content.includes('Analyze sample')&&preview.hash.length===64);
 check('analysisRequiresConsent',!!(await rpc('AnalyzeAgentTask',{id:task.id,contextHash:preview.hash})).error);
 const pending=await call('PrepareFileChange',{workspaceId,path:'must-not-create.txt',content:'denied by StopAll',taskId:task.id});
 await call('StopAll');check('stopRevokesPending',!!(await rpc('ApproveProductAction',{id:pending.id,hash:pending.hash})).error);
 const pendingRestart=await call('PrepareFileChange',{workspaceId,path:'must-not-replay.txt',content:'expired on restart'});
 check('pendingRestartWitness',pendingRestart.status==='PENDING');
 await click('nav-skills');await until('skills',()=>byId('product-skills'));check('builtinSkills',state.skills.length===8);
}

async function restart(){
 await until('home',()=>byId('product-home'));
 const state=await call('GetMVPState');
 check('onboardingRestored',state.onboarding.completed&&!document.querySelector('.product-overlay'));
 check('localeRestored',document.documentElement.lang==='zh-CN');
 check('workspaceTaskHistoryRestored',state.workspaces.length===1&&state.agentTasks.some(t=>t.steps.length>=3));
 check('actionHistoryRestored',state.actions.some(a=>a.kind==='file'&&a.status==='SUCCEEDED')&&state.actions.some(a=>a.kind==='command'&&a.status==='SUCCEEDED'));
 const pending=state.actions.find(a=>a.proposal.file?.relativePath==='must-not-replay.txt');
 check('shutdownOrStartupRevokesApproval',['CANCELLED','EXPIRED'].includes(pending?.status));
 check('noReplay',!!(await rpc('ReadWorkspaceFile',{workspaceId:state.workspaces[0].id,path:'must-not-replay.txt'})).error);
}

let error='';
try { await (stage===9?fresh():restart()); } catch(e) { error=String(e.message||e); }
await Call.ByName('main.AcceptanceService.Report',JSON.stringify({stage,passed:!error,error,checks,timings,externalModelCalls:0,workspacePicker:'ISOLATED_LAUNCHER_FIXTURE'}));
