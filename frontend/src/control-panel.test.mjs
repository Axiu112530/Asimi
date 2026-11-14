// Node 24. Renders the real Vue panel with an in-memory Wails boundary.
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { createRenderer, h, nextTick, reactive } from 'vue';

const calls = [];
const deferred = () => {
  let resolve;
  const promise = new Promise(accept => { resolve = accept; });
  return { promise, resolve };
};
const firstStatus = deferred();
let delayedProposal;
let delayedInspection;
let nextExpiry;
let approvalError;
let proposedTaskOverride;
let suggestions = [];
let capturedTarget;
let stopped = false;
let targetListed = false;
const bounds = { x: 5, y: 5, width: 300, height: 200 };
const observedAt = new Date().toISOString();
const window = {
  id: 'window', app: 'test_host', hwnd: '112', pid: 123, creationTime: 'time',
  imagePath: 'fixture.exe', sessionId: 1, integrity: 8192, className: 'Fixture',
  title: 'Fixture title', bounds, foreground: false, observedAt,
};
const element = {
  id: 'editor', name: 'Test input', automationId: 'TestInput', controlType: 'Edit', bounds,
  enabled: true, offscreen: false, focused: true, password: false, processId: 123,
  nativeHandle: '', patterns: ['Value'], parentPath: 'root', runtimeId: 'element', depth: 1, blank: true,
};
const observation = { id: 'observation', window, elements: [element], truncated: false, modal: false, observedAt };
const status = () => ({ stopped, available: true, permissionMode: 'PER_ACTION_APPROVAL', rawInput: false, notepadText: false });
const proposal = (type, text, taskId) => ({
  id: 'action', type, window, element, text, taskId, observedAt, expiresAt: nextExpiry || new Date(Date.now() + 20_000).toISOString(),
  expected: 'TEXT_MATCH', hash: 'approved-hash', targetHash: 'target-hash', policyVersion: '2d-v1', status: 'AWAITING_APPROVAL',
});
globalThis.__controlTest = {
  state: reactive({ connected: true, controlStopVersion: 0 }),
  async call(method, payload = {}) {
    calls.push({ method, payload });
    switch (method) {
      case 'GetControlStatus': return calls.length === 1 ? firstStatus.promise : status();
      case 'ListControlJournal': return [];
      case 'ListControlWindows': targetListed = true; return [window];
      case 'InspectControlWindow': assert.ok(targetListed); return delayedInspection?.promise || observation;
      case 'ProposeControlAction': return delayedProposal?.promise || proposal(payload.type, payload.text, proposedTaskOverride || payload.taskId);
      case 'CreateDesktopTask':
        return {id:'desktop-created', desktop:true, proposal:proposal(payload.type,payload.text,'desktop-created')};
      case 'GetMVPState': return {workspaces:[],agentTasks:[],skills:[],actions:[],onboarding:{completed:false},commandCatalog:[]};
      case 'ListComputerSuggestions': return suggestions;
      case 'ProposeComputerSuggestion': {
        const item = suggestions.find(item => item.requestId === payload.requestId);
        assert.equal(payload.taskId, task.id);
        return { ...proposal(item.actionType, 'Model fixture text', payload.taskId), sourceRequestId: item.requestId };
      }
      case 'ApproveControlAction':
        if (approvalError) throw new globalThis.__controlTest.ServiceError(approvalError);
        return { id: 'action', type: 'keyboard.type_text', status: 'UNKNOWN_RESULT', at: observedAt, verified: false };
      case 'RejectControlAction': return { id: 'action', type: 'app.launch_test_host', status: 'REJECTED', at: observedAt, verified: false };
      case 'StopComputerControl': stopped = true; return true;
      case 'ResumeComputerControl': stopped = false; return true;
      default: throw new Error('Unexpected call ' + method);
    }
  },
};

registerHooks({
  resolve(specifier, context, next) {
    if (context.parentURL?.startsWith('file:') && context.parentURL.includes('/src/')) {
      if (['./bridge', './store', './i18n'].includes(specifier)) return { url: 'control-test:' + specifier.slice(2), shortCircuit: true };
      if (specifier.startsWith('./') && !/\.(mjs|ts)$/.test(specifier)) return next(specifier + '.ts', context);
    }
    return next(specifier, context);
  },
  load(url, context, next) {
    const source = {
      'control-test:bridge': 'export const call=(...args)=>globalThis.__controlTest.call(...args); export class ServiceError extends Error { constructor(code){super(code);this.code=code;} } globalThis.__controlTest.ServiceError=ServiceError;',
      'control-test:store': 'export const state=globalThis.__controlTest.state; export const fail=error=>{throw error};',
      'control-test:i18n': 'export const t=key=>key, known=t, date=value=>value, i18n={global:{te:()=>false,locale:{value:"en-US"}}};',
    }[url];
    return source ? { format: 'module', shortCircuit: true, source } : next(url, context);
  },
});

const { ComputerControlPanel } = await import('./control-panel.ts');
const create = (tag, text = '') => ({ tag, text, children: [], props: {} });
function remove(node) {
  if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1);
  node.parent = undefined;
}
const renderer = createRenderer({
  createElement: create, createText: text => create('#text', text), createComment: () => create('#comment'),
  insert(node, parent, anchor) {
    remove(node);
    node.parent = parent;
    const index = anchor ? parent.children.indexOf(anchor) : parent.children.length;
    parent.children.splice(index, 0, node);
  },
  remove,
  patchProp(node, key, _, value) { node.props[key] = value; },
  setText(node, value) { node.text = value; },
  setElementText(node, value) { node.text = value; node.children = []; },
  parentNode: node => node.parent,
  nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1],
});
const root = create('#root');
function find(id, node = root) {
  if (node.props['data-testid'] === id) return node;
  for (const child of node.children) {
    const result = find(id, child);
    if (result) return result;
  }
}
const tick = async () => { await new Promise(resolve => setImmediate(resolve)); await nextTick(); };
function click(id) {
  const node = find(id);
  assert.ok(node && !node.props.disabled, `Expected enabled ${id}`);
  node.props.onClick();
}
const task = { id: 'isolated-task-id', goal: 'Inspect the fixture' };
const app = renderer.createApp({ render: () => h(ComputerControlPanel, {
  tasks: [task], onRequestSuggestion: target => { capturedTarget = target; },
}) });
app.mount(root);
try {
  assert.ok(!find('control-propose-launch') || find('control-propose-launch').props.disabled);
  assert.ok(!calls.some(({ method }) => ['ListControlWindows', 'InspectControlWindow'].includes(method)));
  firstStatus.resolve(status());
  await tick();

  click('control-refresh-targets');
  await tick();
  click('control-inspect-window');
  await tick();
  click('control-select-element');
  await tick();
  find('control-text-input').props.onInput({target:{value:'Harness task text'}});
  await tick();
  click('control-propose-task-value');
  await tick();
  const createdTask=calls.find(({method})=>method==='CreateDesktopTask');
  assert.equal(createdTask.payload.observationId,observation.id);
  assert.equal(createdTask.payload.elementId,element.id);
  assert.equal(createdTask.payload.workspaceId,'');
  assert.equal(createdTask.payload.text,'Harness task text');
  assert.equal(find('computer-reviewed-task').children.find(child=>child.tag==='dd').props.title,'desktop-created');
  assert.ok(!calls.some(({method})=>method==='ApproveControlAction'));
  click('control-reject');
  await tick();
  const exact = '  Hello Asimi  ';
  find('control-text-input').props.onInput({ target: { value: exact } });
  find('control-task-link').props.onChange({ target: { value: task.id } });
  await tick();
  proposedTaskOverride = 'unrelated-task';
  click('control-propose-type-text');
  await tick();
  assert.ok(!find('control-approve'), 'A proposal linked to a different task cannot be approved');
  proposedTaskOverride = undefined;
  click('control-propose-type-text');
  await tick();
  assert.equal(calls.filter(({ method }) => method === 'ProposeControlAction').at(-1).payload.taskId, task.id);
  assert.equal(find('computer-reviewed-task').children.find(child => child.tag === 'dd').props.title, task.id);
  assert.ok(find('control-task-link').props.disabled, 'Task choice is locked during action approval');
  assert.equal(find('control-approval-text').text, exact);
  assert.ok(!calls.some(({ method }) => method === 'ApproveControlAction'));
  click('control-approve');
  await tick();
  assert.deepEqual(calls.find(({ method }) => method === 'ApproveControlAction').payload, {
    actionId: 'action', actionHash: 'approved-hash',
  });
  assert.ok(find('control-uncertain-result'));
  assert.ok(!find('control-approve'));
  find('control-task-link').props.onChange({ target: { value: '' } });
  await tick();

  delayedProposal = deferred();
  click('control-propose-launch');
  await tick();
  click('control-emergency-stop');
  await tick();
  delayedProposal.resolve(proposal('app.launch_test_host'));
  await tick();
  assert.ok(!find('control-approve'));
  assert.ok(!find('control-propose-launch') || find('control-propose-launch').props.disabled);
  assert.equal(calls.filter(({ method }) => method === 'ApproveControlAction').length, 1);
  click('control-resume');
  await tick();
  assert.ok(find('control-propose-launch') && !find('control-propose-launch').props.disabled);
  assert.ok(!find('control-approve'));
  delayedProposal = undefined;

  click('control-propose-launch');
  await tick();
  click('control-reject');
  await tick();
  assert.deepEqual(calls.find(({ method }) => method === 'RejectControlAction').payload, {
    actionId: 'action', actionHash: 'approved-hash',
  });
  assert.equal(calls.filter(({ method }) => method === 'ApproveControlAction').length, 1);
  assert.ok(!find('control-approve'));

  nextExpiry = new Date(Date.now() - 100).toISOString();
  click('control-propose-launch');
  await tick();
  assert.ok(!find('control-approve'));
  click('control-dismiss-expired');
  await tick();
  nextExpiry = undefined;

  approvalError = 'STORAGE_CONSTRAINT';
  click('control-propose-launch');
  await tick();
  click('control-approve');
  await tick();
  assert.ok(find('control-uncertain-result'), 'A failed final persistence write must expose possible desktop effects');
  approvalError = undefined;

  // Real Notepad acceptance is a separate user action. These Vue tests prove
  // that only a newly inspected, proven blank Value editor offers approval.
  Object.assign(window, { app: 'notepad', className: 'Notepad',
    packageFamily: 'Microsoft.WindowsNotepad_8wekyb3d8bbwe',
    appUserModelId: 'Microsoft.WindowsNotepad_8wekyb3d8bbwe!App' });
  Object.assign(element, { name: '', automationId: 'TextEditor', controlType: 'Document',
    valueWritable: true, valueSha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' });
  click('control-refresh-targets'); await tick();
  click('control-inspect-window'); await tick();
  click('control-select-element'); await tick();
  assert.ok(find('control-notepad-conditional'));
  assert.ok(find('control-text-input'), 'Blank writable Notepad editor should offer a conditional manual action');
  assert.ok(!find('control-propose-focus') && !find('control-propose-invoke'));
  find('control-text-input').props.onInput({ target: { value: 'Notepad sample' } }); await tick();
  click('control-propose-type-text'); await tick();
  assert.equal(find('control-approval-text').text, 'Notepad sample');
  click('control-reject'); await tick();
  for (const change of [
    () => { element.blank = false; },
    () => { element.blank = true; element.patterns = ['Text']; },
    () => { element.patterns = ['Value']; element.valueWritable = false; },
    () => { element.valueWritable = true; observation.elements.push({ ...element, id: 'second' }); },
  ]) {
    change(); click('control-refresh-targets'); await tick();
    click('control-inspect-window'); await tick(); click('control-select-element'); await tick();
    assert.ok(!find('control-text-input'), 'Unsafe or ambiguous Notepad editor must stay read only');
    assert.ok(find('control-notepad-unavailable'));
  }
  observation.elements = [element];

  click('control-refresh-targets');
  await tick();
  delayedInspection = deferred();
  click('control-inspect-window');
  await tick();
  click('control-emergency-stop');
  await tick();
  delayedInspection.resolve(observation);
  await tick();
  assert.ok(!find('control-tree'));
  click('control-resume');
  await tick();
  click('control-propose-launch');
  await tick();
  globalThis.__controlTest.state.controlStopVersion++;
  await tick();
  assert.ok(!find('control-approve'));
  assert.ok(!find('control-propose-launch') || find('control-propose-launch').props.disabled);
  assert.equal(calls.filter(({ method }) => method === 'ApproveControlAction').length, 2);
  globalThis.__controlTest.state.controlStoppedLocally = false;
  Object.assign(window, { app: 'test_host' });
  Object.assign(element, { automationId: 'TestInput', controlType: 'Edit' });
  click('control-refresh-targets'); await tick();
  click('control-inspect-window'); await tick();
  click('control-select-element'); await tick();
  assert.ok(!find('control-request-suggestion'), 'A model suggestion requires a task');
  suggestions = [{ requestId: 'provider-result', status: 'READY', selector: 'TestInput',
    actionType: 'uia.set_value', observedAt }];
  find('control-task-link').props.onChange({ target: { value: task.id } }); await tick();
  click('control-request-suggestion');
  assert.deepEqual(capturedTarget, { observationId: observation.id, elementId: element.id, selector: 'TestInput', taskId: task.id });
  click('control-review-suggestion'); await tick();
  assert.deepEqual(calls.find(({ method }) => method === 'ProposeComputerSuggestion').payload, {
    requestId: 'provider-result', taskId: task.id,
  });
  assert.equal(find('control-approval-text').text, 'Model fixture text');
  assert.equal(calls.filter(({ method }) => method === 'ApproveControlAction').length, 2);
  click('control-reject'); await tick();
  console.log('PASS: exact approval, model suggestion review, explicit observation, conditional manual Notepad guards, unknown/storage outcomes, stop invalidation, explicit resume, rejection, expiry, late inspection, and global stop');
} finally {
  app.unmount();
}
