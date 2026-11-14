// Explicit stage 8 only. Positive effects target the dedicated host launched
// by the broker. Other applications are never selected or mutated.
import { Call } from '/wails/runtime.js';
const checks = {}, details = {};
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const byID = id => document.querySelector(`[data-testid="${id}"]`);
const rpc = (method, payload = {}) => Call.ByName('main.DesktopService.Call', method, JSON.stringify(payload));
async function call(method, payload) {
  const r = await rpc(method, payload);
  if (r.error) throw new Error(method + ' ' + r.error.code);
  return r.data;
}
function check(name, value) { checks[name] = Boolean(value); if (!value) throw new Error(name); }
async function until(name, read) {
  const deadline = Date.now() + 7000;
  do { const value = await read(); if (value) return value; await sleep(50); } while (Date.now() < deadline);
  throw new Error('timeout ' + name);
}
async function observe() {
  const windows = await call('ListControlWindows');
  const host = windows.find(w => w.app === 'test_host');
  check('hostIdentity', host && host.pid > 0 && host.creationTime && host.imagePath && host.imageHash?.length === 64 && host.integrity === 8192);
  const o = await call('InspectControlWindow', {windowId: host.id});
  check('boundedTree', o.elements.length > 0 && o.elements.length <= 500 && !o.truncated && !o.modal && o.elements.every(e => e.depth <= 12));
  return o;
}
async function click(id) {
  const node = await until(id, ()=> { const n=byID(id); return n && !n.disabled ? n:null; });
  node.click();
  await sleep(80);
}
async function uiInspect() {
  await click('control-refresh-targets');
  const button = await until('host inspect', ()=>document.querySelector('[data-control-app="test_host"] [data-testid="control-inspect-window"]'));
  button.click();
  await until('tree ready', ()=>byID('control-tree'));
}
async function act(type, selector, text) {
  const o = await observe();
  const element = selector ? o.elements.find(e => e.automationId === selector) : undefined;
  if (selector) check('selector_' + selector, !!element);
  const p = await call('ProposeControlAction', {type, observationId:o.id, ...(element ? {elementId:element.id}:{}), ...(text ? {text}:{})});
  check('hash_' + type, /^[a-f0-9]{64}$/.test(p.hash) && p.status === 'AWAITING_APPROVAL');
  const r = await call('ApproveControlAction', {actionId:p.id, actionHash:p.hash});
  if (type === 'window.focus') {
    details.focus = {status:r.status, verified:r.verified, code:r.code || ''};
    check('focusVerifiedOrHonestUnknown', (r.status === 'COMPLETED' && r.verified) ||
      (r.status === 'UNKNOWN_RESULT' && !r.verified && r.code === 'CONTROL_UNKNOWN_RESULT'));
  } else check('verified_' + selector, r.status === 'COMPLETED' && r.verified);
  return p;
}
async function run() {
  const status = await call('GetControlStatus');
  check('narrowPermission', status.available && status.permissionMode === 'PER_ACTION_APPROVAL' && !status.rawInput && !status.notepadText);
  const denied = await rpc('ApproveControlAction', {actionId:'unapproved',actionHash:'wrong'});
  check('unapprovedDenied', denied.error?.code === 'CONTROL_APPROVAL_USED');
  const launch = await call('ProposeControlAction', {type:'app.launch_test_host'});
  const rejected = await call('RejectControlAction', {actionId:launch.id,actionHash:launch.hash});
  check('launchRejected', rejected.status === 'REJECTED');
  const p = await call('ProposeControlAction', {type:'app.launch_test_host'});
  const modified = await rpc('ApproveControlAction', {actionId:p.id,actionHash:'modified'});
  check('modifiedHashDenied', modified.error?.code === 'CONTROL_APPROVAL_MISMATCH');
  const launched = await call('ApproveControlAction', {actionId:p.id,actionHash:p.hash});
  check('nativeLaunchVerified', launched.status === 'COMPLETED' && launched.verified);
  const reused = await rpc('ApproveControlAction', {actionId:p.id,actionHash:p.hash});
  check('usedApprovalDenied', reused.error?.code === 'CONTROL_APPROVAL_USED');
  const o = await observe();
  details.nativeTarget = {pid:o.window.pid,creationTime:o.window.creationTime,imageHash:o.window.imageHash,sessionId:o.window.sessionId,integrity:o.window.integrity,nodes:o.elements.length};
  check('fixtureControls', ['TestInput','SubmitButton','ResetButton','ResultLabel','TestCheckbox'].every(id => o.elements.some(e => e.automationId === id)));
  await act('window.focus');
  await act('keyboard.type_text', 'TestInput', 'Hello Asimi');
  await act('uia.invoke', 'SubmitButton');
  await act('uia.invoke', 'ResetButton');
  const journal = await call('ListControlJournal');
  check('journalShowsLifecycle', ['PROPOSED','VALIDATING','AWAITING_APPROVAL','APPROVED','REVALIDATING','EXECUTING','VERIFYING','COMPLETED','REJECTED'].every(status => journal.some(r=>r.status===status)));
  check('journalNoLiteralText', !JSON.stringify(journal).includes('Hello Asimi'));
  await call('StopComputerControl');
  await call('StopComputerControl');
  const stopped = await rpc('ProposeControlAction', {type:'app.launch_test_host'});
  check('stopPreventsDispatch', stopped.error?.code === 'CONTROL_STOPPED');
  await observe();
  check('stopAllowsReadOnly', true);
  await call('ResumeComputerControl');
  check('explicitResume', !(await call('GetControlStatus')).stopped);
  await click('nav-computer');
  const tools = await until('developer computer tools', ()=>byID('computer-tools'));
  tools.open = true;
  tools.dispatchEvent(new Event('toggle'));
  await until('control panel', ()=>byID('computer-control'));
  check('controlPanelRendered', true);
  await uiInspect();
  document.querySelector('[data-testid="control-select-element"][data-automation-id="TestInput"]').click();
  const input=await until('text editor', ()=>byID('control-text-input'));
  input.value='UI approved text';input.dispatchEvent(new Event('input',{bubbles:true}));
  await click('control-propose-set-value');
  const review=await until('exact text review', ()=>byID('control-approval-text'));
  check('exactTextVisibleInApproval',review.textContent==='UI approved text');
  const proposed=(await call('ListControlJournal')).find(r=>r.status==='AWAITING_APPROVAL'&&r.type==='uia.set_value');
  await click('control-approve');
  await until('UI text verified',async()=> (await call('ListControlJournal')).find(r=>r.id===proposed.id&&r.status==='COMPLETED'));
  check('realVueApprovedValueVerified',true);
  await uiInspect();
  document.querySelector('[data-testid="control-select-element"][data-automation-id="SubmitButton"]').click();
  await click('control-propose-invoke');
  await until('invoke review', ()=>byID('control-approve'));
  const invoke=(await call('ListControlJournal')).find(r=>r.status==='AWAITING_APPROVAL'&&r.type==='uia.invoke');
  await click('control-approve');
  await until('UI invoke verified',async()=> (await call('ListControlJournal')).find(r=>r.id===invoke.id&&r.status==='COMPLETED'));
  check('realVueApprovedInvokeVerified',true);
  check('legacyToolsSeparate', (await call('GetToolExecutions')).length === 0);
}
let error = '';
try { await run(); } catch (e) {error = /^[A-Za-z0-9 _.-]{1,160}$/.test(e?.message || '') ? e.message : 'Unexpected acceptance failure';}
await Call.ByName('main.AcceptanceService.Report',JSON.stringify({stage:8,passed:!error,error,checks,details}));
