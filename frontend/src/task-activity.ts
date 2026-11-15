import { defineComponent, h, type PropType } from 'vue';
import { t } from './i18n';
import { icon } from './ui';
import type { AgentStep, AgentTask, ProductAction } from './product-types';
import './task-activity.css';

type ActivityState = 'running' | 'approval' | 'completed' | 'failed' | 'cancelled' | 'paused' | 'unknown' | 'pending';
export interface ActivityRow { id: string; icon: string; label: string; target: string; state: ActivityState }

function activityState(status: string, taskStatus?: string, isAction = false): ActivityState {
  if (['SUCCEEDED', 'COMPLETED'].includes(status)) return 'completed';
  if (['FAILED', 'REJECTED', 'EXPIRED'].includes(status)) return 'failed';
  if (taskStatus === 'FAILED') return 'failed';
  if (taskStatus === 'COMPLETED') return 'unknown';
  if (status === 'CANCELLED' || taskStatus === 'CANCELLED') return 'cancelled';
  if (['UNKNOWN', 'UNKNOWN_RESULT', 'RECOVERY_REQUIRED', 'INTERRUPTED'].includes(status) || taskStatus === 'RECOVERY_REQUIRED') return 'unknown';
  if (status === 'PAUSED' || taskStatus === 'PAUSED') return 'paused';
  if (['RUNNING', 'EXECUTING', 'PLANNING', 'VERIFYING'].includes(status)) {
    return taskStatus === 'FAILED' ? 'failed' : taskStatus === 'COMPLETED' ? 'unknown' : 'running';
  }
  return ['WAITING_APPROVAL', 'NEEDS_APPROVAL', 'NEEDS_REVIEW'].includes(status) || isAction && status === 'PENDING' ? 'approval' : 'pending';
}

function stepRow(step: AgentStep, task: AgentTask): ActivityRow {
  const kind = step.kind;
  const category = kind === 'agent.plan' ? 'planning' : kind === 'agent.verify' || kind === 'agent.finalize' ? 'verification' :
    kind.includes('file') ? 'file' : kind.includes('command') || kind === 'environment' ? 'command' :
    task.desktop ? 'computer' : 'tool';
  return { id: step.id, icon: category === 'file' ? 'pencil' : category === 'command' ? 'terminal' :
    category === 'verification' ? 'check' : category === 'planning' ? 'spark' : category === 'computer' ? 'computer' : 'wrench',
    label: category, target: '', state: activityState(step.status, task.status) };
}

export function taskActivityRows(task: AgentTask, actions: ProductAction[]): ActivityRow[] {
  const owned = actions.filter(action => action.taskId === task.id);
  const byId = new Map(owned.map(action => [action.id, action]));
  const seen = new Set<string>();
  function actionRow(action: ProductAction): ActivityRow {
    const file = action.proposal.file;
    const command = action.proposal.command;
    return { id: action.id, icon: file ? 'pencil' : command ? 'terminal' : 'wrench',
      label: file ? 'file' : command ? 'command' : 'tool',
      target: file?.relativePath || (command ? [command.executable, ...command.args].join(' ') : ''),
      state: activityState(action.status, task.status, true) };
  }
  const rows = (task.steps || []).map(step => {
    const action = step.actionId ? byId.get(step.actionId) : undefined;
    if (action) { seen.add(action.id); return actionRow(action); }
    return stepRow(step, task);
  });
  for (const action of owned) if (!seen.has(action.id)) rows.push(actionRow(action));
  return rows;
}

export function replyActivity(status: string, hasContent: boolean): { icon: string; label: string } | null {
  if (!['PENDING', 'RUNNING', 'STREAMING', 'STARTING'].includes(status)) return null;
  return { icon: hasContent ? 'pencil' : 'spark', label: hasContent ? 'replying' : 'thinking' };
}

export const TaskActivity = defineComponent({
  props: { task: { type: Object as PropType<AgentTask>, required: true }, actions: { type: Array as PropType<ProductAction[]>, default: () => [] } },
  setup(props) {
    return () => {
      const rows = taskActivityRows(props.task, props.actions);
      if (!rows.length) return null;
      const effects = rows.filter(row => row.label === 'file' || row.label === 'command');
      const effectLabels = [...new Set(effects.map(row => row.label))].map(label =>
        t('activity.' + label + 'Count', { count: effects.filter(row => row.label === label).length }));
      const current = rows.find(row => row.state === 'running') || rows.find(row => row.state === 'approval') || rows.at(-1)!;
      return h('details', { class: 'task-activity', 'data-testid': 'task-activity-' + props.task.id }, [
        h('summary', [
          h('span', { class: ['activity-symbol', 'activity-' + current.icon], 'data-state': current.state }, icon(current.state === 'failed' || current.state === 'unknown' ? 'warning' : current.icon, 17)),
          h('span', { class: 'activity-summary-copy' }, [effects.length && !['running','approval'].includes(current.state) ? effectLabels.join(' · ') : t('activity.' + current.label), h('span', { class: 'activity-state' }, t('activity.' + current.state))]),
          h('span', { class: 'activity-count' }, String(rows.length)), icon('chevron', 14),
        ]),
        h('ol', { class: 'activity-steps' }, rows.map(row => h('li', { key: row.id, 'data-testid': 'activity-' + row.id, 'data-state': row.state }, [
          h('span', { class: ['activity-symbol', 'activity-' + row.icon], 'data-state': row.state }, icon(row.state === 'failed' || row.state === 'unknown' ? 'warning' : row.state === 'cancelled' ? 'stop' : row.icon, 16)),
          h('div', { class: 'activity-step-copy' }, [h('span', t('activity.' + row.label)), row.target ? h('code', row.target) : null]),
          h('span', { class: 'activity-state' }, t('activity.' + row.state)),
        ]))),
      ]);
    };
  },
});
