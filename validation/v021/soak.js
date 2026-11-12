import { Call } from '/wails/runtime.js';

const params = new URLSearchParams(location.search);
const duration = Number(params.get('seconds')) * 1000;
const providerId = 'preset-loopback';
const modelId = 'soak-fixture';
const counts = { completedTurns: 0, cancelledTurns: 0, completedTasks: 0, pauseResumeCycles: 0 };
const samples = [];
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function call(method, payload = {}) {
  const reply = await Call.ByName('main.DesktopService.Call', method, JSON.stringify(payload));
  if (reply.error) throw Error(method + ' ' + reply.error.code);
  return reply.data;
}
async function until(label, read) {
  const end = performance.now() + 8000;
  while (performance.now() < end) {
    const value = await read();
    if (value) return value;
    await pause(60);
  }
  throw Error('timeout ' + label);
}
function expect(ok, label) {
  if (!ok) throw Error(label);
}

let start = performance.now();
async function sample() {
  samples.push({ elapsedMs: Math.round(performance.now() - start), ...await Call.ByName('main.AcceptanceService.Metrics') });
}

async function chat(index, conversationId = '', cancel = false) {
  const request = await call('StartChat', {
    providerId, modelId, conversationId, attachmentIds: [], stream: true, maxOutputTokens: 128,
    content: cancel ? 'SLOW_REQUEST' : 'Isolated stability turn ' + (index + 1),
    confirmSend: true, confirmProviderSwitch: false, imageDetail: 'auto', playground: false,
  });
  if (cancel) {
    await pause(100);
    await call('CancelChat', { requestId: request.requestId });
  }
  const record = await until('chat', async () => (await call('GetUsage')).find(row => row.requestId === request.requestId && row.status !== 'RUNNING'));
  expect(record.status === (cancel ? 'CANCELLED' : 'COMPLETED'), 'unexpected chat terminal state');
  counts[cancel ? 'cancelledTurns' : 'completedTurns']++;
  return request.conversationId;
}

async function localTask(workspaceId, index) {
  const task = await call('CreateAgentTask', { workspaceId, goal: 'Inspect fixture ' + index });
  const done = await until('local task', async () => {
    const state = await call('GetAgentTask', { id: task.id });
    return state.status === 'COMPLETED' ? state : null;
  });
  expect(done.steps.every(step => step.status === 'COMPLETED'), 'local task step incomplete');
  counts.completedTasks++;
}

async function pauseResume(workspaceId, index) {
  const task = await call('CreateAgentRun', { workspaceId, goal: 'Review fixture ' + index, providerId, modelId, paths: [] });
  await call('PauseAgentTask', { id: task.id });
  expect((await call('GetAgentTask', { id: task.id })).status === 'PAUSED', 'pause did not persist');
  expect((await call('ResumeAgentRun', { id: task.id })).status === 'NEEDS_REVIEW', 'resume bypassed review');
  await call('CancelAgentTask', { id: task.id });
  counts.pauseResumeCycles++;
}

let error = '';
try {
  expect(/^http:\/\/127\.0\.0\.1:\d+\/v1$/.test(params.get('fixture')), 'fixture must be loopback');
  await until('desktop', () => document.querySelector('[data-testid="nav-home"]'));
  await call('SaveAIProfile', {
    profile: {
      id: providerId, name: 'Preset loopback fixture', baseUrl: params.get('fixture'), protocol: 'chat-completions', enabled: true,
      models: [{
        id: modelId, displayName: modelId,
        capabilities: { text: 'SUPPORTED', streaming: 'SUPPORTED', structuredOutput: 'SUPPORTED', vision: 'UNSUPPORTED', contextLimit: 65536 },
        pricing: { inputPerMillion: 0.30, outputPerMillion: 1.20, currency: 'USD', effectiveAt: '2026-11-12' },
      }],
    },
  });
  await call('StoreCredential', { providerId, secret: 'fixture-secret' });
  const workspace = await call('PickWorkspace');
  let conversationId = '';
  start = performance.now();
  await sample();
  for (let turn = 0; turn < 105; turn++) {
    await pause(Math.max(0, start + duration * turn / 105 - performance.now()));
    if (turn % 21 === 0) conversationId = '';
    conversationId = await chat(turn, conversationId);
    if (turn < 100 && turn % 5 === 0) await localTask(workspace.id, turn);
    if (turn % 21 === 0) {
      await chat(turn, '', true);
      await pauseResume(workspace.id, turn);
      await sample();
    }
  }
  await pause(Math.max(0, start + duration - performance.now()));
  const usage = await call('GetUsage');
  expect(usage.length === 110 && usage.every(row => ['COMPLETED', 'CANCELLED'].includes(row.status)), 'terminal usage ledger');
  await sample();
} catch (cause) {
  error = String(cause.message || cause);
}
await Call.ByName('main.AcceptanceService.Report', JSON.stringify({
  stage: 19, passed: !error, error, counts, elapsedMs: Math.round(performance.now() - start), samples, externalModelCalls: 0,
}));
