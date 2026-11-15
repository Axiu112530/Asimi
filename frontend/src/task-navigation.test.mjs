import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { createRenderer, h, nextTick, reactive } from 'vue';

const tasks = ['first', 'second'].map((id, index) => ({
  id, workspaceId: 'project-' + index, goal: 'Task ' + id, status: 'COMPLETED',
  currentStep: 0, steps: [], summary: '', updatedAt: '2026-11-14T00:00:00Z',
}));
let jobs = [], candidates = [], skills = [];
let heldRead;
const listeners = new Set(), reads = [], failures = [];
const snapshot = () => structuredClone({ skills, memories: [], settings: {}, pending: 0, paused: 0, completed: jobs.length, candidates, learning: jobs });
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const changed = () => { for (const listener of [...listeners]) listener({}); };
const settleEvent = async () => { await new Promise(resolve => setTimeout(resolve, 180)); await tick(); };
const product = reactive({
  selectedWorkspace: 'project-0', selectedTask: '', busy: false, exclusions: [],
  data: { agentTasks: tasks, workspaces: tasks.map((task, index) => ({ id: task.workspaceId, name: 'Project ' + index })), actions: [] },
});
const state = reactive({ connected: true, conversations: [], route: 'home', settings: { interfaceMode: 'standard' } });
const fixture = globalThis.__taskNavigation = {
  product, state, h, failures,
  subscribe(name, listener) { assert.equal(name, 'asimi:changed'); listeners.add(listener); return () => listeners.delete(listener); },
  selectWorkspace(id) { if (product.selectedWorkspace !== id) { product.selectedWorkspace = id; product.selectedTask = ''; } },
  async call(method, payload) {
    assert.equal(method, 'GetCognitionState');
    reads.push(payload);
    if (heldRead) { const current = heldRead; heldRead = undefined; return current.promise; }
    return snapshot();
  },
};
registerHooks({
  resolve(specifier, context, next) {
    if (context.parentURL?.includes('/src/')) {
      if (['./bridge', './store', './i18n', './ai-store', './product-store'].includes(specifier)) return { url: 'task-nav:' + specifier.slice(2), shortCircuit: true };
      if (['./ai-pages', './markdown', './agent02-panel', './agent02-computer-evidence', './project021-panel'].includes(specifier)) return { url: 'task-nav:components', shortCircuit: true };
      if (specifier.endsWith('.css')) return { url: 'task-nav:style', shortCircuit: true };
      if (specifier.endsWith('.json')) return { url: 'task-nav:locale', shortCircuit: true };
      if (specifier.startsWith('./') && !/\.(mjs|ts)$/.test(specifier)) return next(specifier + '.ts', context);
    }
    return next(specifier, context);
  },
  load(url, context, next) {
    const source = {
      'task-nav:bridge': 'export const call=(...args)=>globalThis.__taskNavigation.call(...args);export const subscribe=(...args)=>globalThis.__taskNavigation.subscribe(...args);',
      'task-nav:store': 'export const state=globalThis.__taskNavigation.state;export const fail=error=>globalThis.__taskNavigation.failures.push(error);export const saveSettings=()=>{};export const openConversation=()=>{};export const newConversation=()=>{};',
      'task-nav:i18n': 'export const t=(key)=>key,known=t,date=value=>value,number=String;export const i18n={global:{mergeLocaleMessage:()=>{},te:()=>false}};',
      'task-nav:ai-store': 'export const ai={assistant:{providerId:"",modelId:""},profiles:[]};export const sendAI=()=>{};export const startAgentAnalysis=()=>{};export const selectProvider=()=>{};export const selectModel=()=>{};export const profile=()=>undefined;export const model=()=>undefined;export const usableDefaultAI=()=>undefined;',
      'task-nav:product-store': 'export const product=globalThis.__taskNavigation.product;export const selectWorkspace=globalThis.__taskNavigation.selectWorkspace;export const refreshProduct=async()=>{};export const productWork=async fn=>fn();export const pickWorkspace=()=>{};export const analyzeWorkspace=()=>{};export const readSource=()=>{};export const approveProductAction=()=>{};export const rejectProductAction=()=>{};export const diagnoseEnvironment=()=>{};',
      'task-nav:components': 'const h=globalThis.__taskNavigation.h;export const ComputerTaskEvidence={props:["taskId"],render(){return h("div",{"data-testid":"evidence-"+this.taskId});}};export const AIProfilesPage={},Markdown={},Agent02Panel={},DeveloperProjectPanel={};',
      'task-nav:style': '',
      'task-nav:locale': 'export default {};',
    }[url];
    return source !== undefined ? { format: 'module', shortCircuit: true, source } : next(url, context);
  },
});
const create = (tag, text = '') => ({ tag, text, children: [], props: {} });
const remove = node => { if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1); node.parent = undefined; };
const renderer = createRenderer({
  createElement: create, createText: value => create('#text', value), createComment: () => create('#comment'),
  insert(node, parent, anchor) { remove(node); node.parent = parent; parent.children.splice(anchor ? parent.children.indexOf(anchor) : parent.children.length, 0, node); },
  remove, patchProp(node, key, _, value) { node.props[key] = value; }, setText(node, value) { node.text = value; },
  setElementText(node, value) { node.text = value; node.children = []; }, parentNode: node => node.parent,
  nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1],
});
const tick = async () => { await new Promise(resolve => setImmediate(resolve)); await nextTick(); };
function find(root, predicate) { if (predicate(root)) return root; for (const child of root.children) { const found = find(child, predicate); if (found) return found; } }
const byId = (root, id) => find(root, node => node.props['data-testid'] === id);
const { HomePage, TasksPage } = await import('./product-pages.ts');
const { LearningPage } = await import('./cognition-pages.ts');
let root = create('root'), app = renderer.createApp({ render: () => h(HomePage, { developer: true }) });
app.mount(root); await tick();
try {
  const taskLink = find(root, node => node.tag === 'button' && node.children.some(child => child.tag === 'strong' && child.text === 'Task second'));
  assert.ok(taskLink); taskLink.props.onClick(); await tick();
  assert.equal(state.route, 'workflows');
  assert.equal(product.selectedTask, 'second', 'A recent-task link selects the exact task');
  assert.equal(product.selectedWorkspace, 'project-1', 'A recent-task link selects its own project');
} finally { app.unmount(); }
product.selectedWorkspace = 'project-0';
root = create('root'); app = renderer.createApp({ render: () => h(TasksPage) }); app.mount(root); await tick();
try {
  assert.equal(byId(root, 'product-task-second')?.props['aria-current'], 'true', 'The review page marks the task selected by its incoming link');
  assert.ok(byId(root, 'evidence-second'), 'Incoming selection exposes that task’s evidence');
  assert.equal(product.selectedWorkspace, 'project-1', 'Review selects the incoming task’s project before actions');
  product.selectedTask = 'first'; await tick();
  assert.equal(byId(root, 'product-task-first')?.props['aria-current'], 'true');
  assert.ok(byId(root, 'evidence-first')); assert.equal(byId(root, 'evidence-second'), undefined);
} finally { app.unmount(); }
root = create('root'); app = renderer.createApp({ render: () => h(LearningPage) }); app.mount(root); await tick();
try {
  assert.ok(byId(root, 'cognition-learning-open-goals'), 'Ordinary mode provides a way to begin a source task');
  byId(root, 'cognition-learning-open-goals').props.onClick(); assert.equal(state.route, 'goals');
} finally { app.unmount(); }
jobs = [{ taskId: 'second', status: 'done', attempts: 1, durationMs: 1, createdAt: '2026-11-14T00:00:00Z' }];
root = create('root'); app = renderer.createApp({ render: () => h(LearningPage) }); app.mount(root); await tick();
try {
  const workspace = find(root, node => node.tag === 'select');
  workspace.props.onChange({ target: { value: 'project-1' } }); await tick();
  const outcome = () => byId(root, 'cognition-learning-outcome-second');
  assert.equal(outcome()?.text, 'cognitionLayout.jobExperienceOnly', 'Processed experience does not imply a reusable skill was created');
  candidates = [{ taskId: 'unrelated', status: 'pending' }];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  assert.equal(outcome()?.text, 'cognitionLayout.jobExperienceOnly', 'An unrelated draft does not count for this task');
  candidates = [{ taskId: 'second', status: 'pending' }];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  assert.equal(outcome()?.text, 'cognitionLayout.jobDraftPending');
  candidates = [{ taskId: 'second', status: 'approved' }];
  skills = [{ sourceExperience: 'second', enabled: true, source: 'reviewed:candidate' }];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  assert.equal(outcome()?.text, 'cognitionLayout.jobReviewedSkill');
  skills = [{ sourceExperience: 'second', enabled: false, source: 'reviewed:candidate' }];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  assert.equal(outcome()?.text, 'cognitionLayout.jobExperienceOnly', 'A disabled skill is not offered as available');
  skills = [{ sourceExperience: 'second', enabled: true, source: 'manual' }];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  assert.equal(outcome()?.text, 'cognitionLayout.jobExperienceOnly', 'Unreviewed skill metadata is not reported as a reviewed result');
  const completedJob = jobs[0]; jobs = [{ ...completedJob, status: 'pending' }];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  assert.equal(outcome(), undefined, 'An unfinished job does not claim its experience was processed');
  jobs = [completedJob];
  byId(root, 'cognition-learning-refresh').props.onClick(); await tick();
  const source = byId(root, 'cognition-learning-source-second'); assert.ok(source, 'Learning evidence links to its source task in ordinary mode');
  source.props.onClick(); await tick();
  assert.equal(state.route, 'workflows'); assert.equal(product.selectedTask, 'second'); assert.equal(product.selectedWorkspace, 'project-1');
} finally { app.unmount(); }
jobs = [{ ...jobs[0], status: 'pending' }]; candidates = []; skills = [];
root = create('root'); app = renderer.createApp({ render: () => h(LearningPage) }); app.mount(root); await tick();
try {
  byId(root, 'cognition-workspace').props.onChange({ target: { value: 'project-1' } }); await tick();
  assert.equal(byId(root, 'cognition-learning-outcome-second'), undefined);
  const beforeEvent = reads.length;
  jobs = [{ ...jobs[0], status: 'done' }]; changed(); changed(); changed();
  await settleEvent();
  assert.equal(byId(root, 'cognition-learning-outcome-second')?.text, 'cognitionLayout.jobExperienceOnly', 'Learning completion updates from the backend event without manual refresh');
  assert.equal(reads.length, beforeEvent + 1, 'A burst of change events is debounced into one state read');
  assert.equal(reads.at(-1).workspaceId, 'project-1');
  assert.equal(byId(root, 'cognition-workspace').props.value, 'project-1', 'Live refresh retains the selected project');

  const oldScope = deferred(); heldRead = oldScope;
  byId(root, 'cognition-workspace').props.onChange({ target: { value: 'project-0' } }); await tick();
  byId(root, 'cognition-workspace').props.onChange({ target: { value: 'project-1' } }); await tick();
  oldScope.resolve({ ...snapshot(), candidates: [{ taskId: 'second', status: 'pending' }] }); await tick();
  assert.equal(byId(root, 'cognition-learning-outcome-second')?.text, 'cognitionLayout.jobExperienceOnly', 'An older scope response cannot overwrite the current scope');

  const disconnected = deferred(); heldRead = disconnected; changed(); await settleEvent();
  state.connected = false; await tick();
  assert.equal(listeners.size, 0, 'Disconnect removes the backend change subscription');
  disconnected.resolve(snapshot()); await tick();
  assert.equal(byId(root, 'cognition-learning-outcome-second'), undefined, 'A disconnected read cannot repopulate stale learning results');
  state.connected = true; await tick();
  assert.equal(byId(root, 'cognition-workspace').props.value, 'project-1');

  const unmounted = deferred(); heldRead = unmounted; changed(); await settleEvent();
  app.unmount(); unmounted.reject(new Error('late unmounted read')); await tick();
  assert.equal(listeners.size, 0, 'Unmount removes the backend change subscription');
  assert.equal(failures.length, 0, 'A stale read error after unmount does not affect another page');
} finally { app.unmount(); }
root = create('root'); app = renderer.createApp({ render: () => h(LearningPage) }); app.mount(root); await tick();
const beforeUnmount = reads.length; changed(); app.unmount(); await settleEvent();
assert.equal(reads.length, beforeUnmount, 'Unmount cancels a queued refresh');
assert.equal(listeners.size, 0);
console.log('PASS exact task selection, learning source navigation and state-derived pending/reviewed/experience-only outcomes');
