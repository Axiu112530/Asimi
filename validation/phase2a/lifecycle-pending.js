import { Call, Window } from '/wails/runtime.js';

const stage = 4;
const fileName = 'phase2a-pending-close.txt';
async function call(method, input = {}) {
  const reply = await Call.ByName('main.DesktopService.Call', method, JSON.stringify(input));
  if (reply.error) throw new Error(reply.error.code);
  return reply.data;
}

let report;
try {
  const { taskId } = await call('SendMessage', { content: JSON.stringify({ fileName, content: 'This request must stay unexecuted.' }) });
  const task = await call('GetTask', { id: taskId });
  if (task.status !== 'WAITING_APPROVAL' || task.approval?.status !== 'PENDING') {
    throw new Error('Expected a real pending approval before shutdown');
  }
  report = { stage, passed: true, taskId, approvalId: task.approval.id, taskStatus: task.status, approvalStatus: task.approval.status, fileName };
} catch (error) {
  report = { stage, passed: false, error: String(error) };
}
await Call.ByName('main.AcceptanceService.Report', JSON.stringify(report));
// Exercise the same Wails WindowClosing hook as the native titlebar action.
await Window.Close();
