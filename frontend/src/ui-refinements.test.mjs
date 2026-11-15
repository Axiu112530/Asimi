import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { createRenderer, h, nextTick, reactive } from 'vue';

const state = reactive({ connected: true });
const listeners = new Set();
let snapshot = { agentTasks: [], actions: [] }, heldRead;
const fixture = globalThis.__polish = {
  state, t: (key, params = {}) => key + (Object.keys(params).length ? ':' + JSON.stringify(params) : ''),
  async call(method) { assert.equal(method, 'GetMVPState'); if (heldRead) { const read = heldRead; heldRead = undefined; return read; } return snapshot; },
  subscribe(name, listener) { assert.equal(name, 'asimi:changed'); listeners.add(listener); return () => listeners.delete(listener); },
};
registerHooks({
  resolve(specifier, context, next) {
    if (context.parentURL?.includes('/src/')) {
      if (['./i18n', './bridge', './store'].includes(specifier)) return { url: 'polish:' + specifier.slice(2), shortCircuit: true };
      if (specifier.endsWith('.css')) return { url: 'polish:css', shortCircuit: true };
      if (specifier.startsWith('./') && !/\.(mjs|ts)$/.test(specifier)) return next(specifier + '.ts', context);
    }
    return next(specifier, context);
  },
  load(url, context, next) {
    const source = {
      'polish:i18n': 'const f=globalThis.__polish;export const t=f.t,known=t;export const i18n={global:{te:()=>true}};',
      'polish:bridge': 'export const call=(...a)=>globalThis.__polish.call(...a),subscribe=(...a)=>globalThis.__polish.subscribe(...a);',
      'polish:store': 'export const state=globalThis.__polish.state;',
      'polish:css': '',
    }[url];
    return source === undefined ? next(url, context) : { format: 'module', shortCircuit: true, source };
  },
});
const create = (tag, text = '') => ({ tag, text, children: [], props: {} });
const remove = node => { if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1); };
const renderer = createRenderer({
  createElement: create, createText: value => create('#text', value), createComment: () => create('#comment'),
  insert(node, parent, anchor) { remove(node); node.parent = parent; parent.children.splice(anchor ? parent.children.indexOf(anchor) : parent.children.length, 0, node); },
  remove, patchProp(node, key, old, value) { node.props[key] = value; }, setText(node, value) { node.text = value; },
  setElementText(node, value) { node.text = value; node.children = []; }, parentNode: node => node.parent,
  nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1],
});
const tick = async () => { for (let i = 0; i < 8; i++) await nextTick(); };
const settle = async () => { await new Promise(resolve => setTimeout(resolve, 180)); await tick(); };
const find = (node, id) => node.props['data-testid'] === id ? node : node.children.map(child => find(child, id)).find(Boolean);
const text = node => node.text + node.children.map(text).join('');
const { TaskActivity, taskActivityRows, replyActivity } = await import('./task-activity.ts');
const { ConversationActivity } = await import('./conversation-activity.ts');
const { SkillImport } = await import('./skill-import.ts');
const task = { id: 'task-a', workspaceId: 'project', conversationId: 'chat-a', goal: 'Fix calc', status: 'WAITING_APPROVAL', steps: [], currentStep: 0 };
const file = { id: 'file-a', taskId: task.id, kind: 'file', status: 'PENDING', proposal: { file: { relativePath: 'calc.go' } } };
const command = { id: 'cmd-a', taskId: task.id, kind: 'command', status: 'EXECUTING', proposal: { command: { executable: 'go', args: ['test', './...'] } } };
assert.equal(taskActivityRows(task, [file])[0].state, 'approval');
assert.equal(taskActivityRows(task, [file])[0].icon, 'pencil');
assert.equal(taskActivityRows(task, [{ ...file, status: 'EXECUTING' }])[0].state, 'running');
assert.equal(taskActivityRows(task, [{ ...file, status: 'SUCCEEDED' }])[0].state, 'completed');
assert.equal(taskActivityRows(task, [{ ...file, status: 'UNKNOWN_RESULT' }])[0].state, 'unknown');
assert.equal(taskActivityRows({ ...task, status: 'PAUSED' }, [command])[0].state, 'paused');
assert.equal(taskActivityRows({ ...task, status: 'CANCELLED' }, [command])[0].state, 'cancelled');
assert.equal(taskActivityRows(task, [{ ...file, taskId: 'another' }]).length, 0, 'No foreign action leaks into a task');
assert.equal(taskActivityRows({ ...task, steps: [{ id: 'step', kind: 'agent.command', actionId: file.id, status: 'WAITING_APPROVAL' }] }, [file]).length, 1, 'A linked step and action appear only once');
assert.equal(taskActivityRows({ ...task, steps: [{ id: 'read', kind: 'project', status: 'RUNNING' }] }, [])[0].icon, 'wrench');
assert.equal(taskActivityRows({ ...task, status: 'RUNNING', steps: [{ id: 'queued', kind: 'project', status: 'PENDING' }] }, [])[0].state, 'pending', 'Queued read-only work does not claim approval is required');
assert.equal(taskActivityRows({ ...task, status: 'FAILED' }, [file])[0].state, 'failed');
assert.equal(replyActivity('PENDING', false).icon, 'spark');
assert.equal(replyActivity('STREAMING', true).icon, 'pencil');
assert.equal(replyActivity('COMPLETED', true), null, 'Completed replies do not retain a running animation');
let props = reactive({ task, actions: [file] }), root = create('root');
let app = renderer.createApp({ render: () => h(TaskActivity, props) }); app.mount(root); await tick();
assert.ok(find(root, 'activity-file-a')); assert.ok(text(root).includes('activity.approval'));
props.actions = [{ ...file, status: 'EXECUTING' }]; await tick();
assert.equal(find(root, 'activity-file-a').props['data-state'], 'running');
props.actions = [{ ...file, status: 'FAILED', error: 'WRITE_FAILED' }]; await tick();
assert.equal(find(root, 'activity-file-a').props['data-state'], 'failed'); app.unmount();

snapshot = { agentTasks: [task, { ...task, id: 'foreign', conversationId: 'chat-other' }], actions: [file] };
const conversation = reactive({ id: 'chat-a', messages: [] });
root = create('root'); app = renderer.createApp({ render: () => h(ConversationActivity, { conversation }) }); app.mount(root); await tick();
assert.ok(find(root, 'task-activity-task-a')); assert.ok(!find(root, 'task-activity-foreign'));
snapshot = { agentTasks: [task], actions: [{ ...file, status: 'SUCCEEDED' }] };
for (const notify of listeners) notify({ taskId: task.id }); await settle();
assert.equal(find(root, 'activity-file-a').props['data-state'], 'completed');
let resolveRead; heldRead = new Promise(resolve => { resolveRead = resolve; });
for (const notify of listeners) notify({}); await settle(); conversation.id = 'chat-other'; await tick();
assert.ok(!find(root, 'task-activity-task-a'), 'A conversation switch clears previous activity immediately');
resolveRead({ agentTasks: [task], actions: [file] }); await tick();
assert.ok(!find(root, 'task-activity-task-a'), 'A late read cannot show the previous conversation');
app.unmount(); assert.equal(listeners.size, 0);

let imported = [], disabled = false;
const importFile = { name: 'workflow.asimi-skill.json', size: 123, text: async () => '{}' };
root = create('root'); props = reactive({ disabled });
app = renderer.createApp({ render: () => h(SkillImport, { ...props, onImport: file => { imported.push(file); } }) }); app.mount(root); await tick();
const input = () => find(root, 'skill-import-input');
assert.equal(input().props.accept, '.json,application/json');
input().props.onChange({ target: { files: [importFile], value: 'path' } }); await tick();
assert.equal(imported[0], importFile); assert.ok(text(root).includes(importFile.name));
props.disabled = true; await tick(); input().props.onChange({ target: { files: [importFile], value: 'path' } });
assert.equal(imported.length, 1, 'Disabled imports cannot dispatch through a forged change event');
app.unmount();
console.log('PASS truthful task activities, live conversation isolation, terminal states and accessible skill file selection');
