import { h } from 'vue';
import { i18n, t } from './i18n';
import type { AgentTask } from './product-types';

const labels = {
  task: ['Link to task', '关联到任务'],
  noTask: ['No linked task', '不关联任务'],
  evidence: ['Computer evidence', '电脑操作记录'],
  empty: ['No computer actions or screen analyses linked yet.', '尚未关联电脑操作或截图分析。'],
  screen: ['Screen analysis', '截图分析'],
  action: ['Computer action', '电脑操作'],
} as const;

export const computerLabel = (key: keyof typeof labels) => labels[key][i18n.global.locale.value === 'zh-CN' ? 1 : 0];

function taskOption(task: AgentTask) {
  return `${task.desktop ? t('product.desktopTask') : task.goal} · ${task.id.slice(0, 8)}`;
}

export function computerTaskSelector(tasks: readonly AgentTask[], taskId: string, select: (id: string) => void, disabled: boolean, testId: string) {
  if (!tasks.length) return null;
  return h('label', { class: 'ai-field' }, [
    computerLabel('task'),
    h('select', {
      value: taskId, disabled, 'data-testid': testId,
      onChange: (event: Event) => select((event.target as HTMLSelectElement).value),
    }, [h('option', { value: '' }, computerLabel('noTask')), ...tasks.map(task =>
      h('option', { key: task.id, value: task.id }, taskOption(task)))]),
  ]);
}

export function computerTaskReview(tasks: readonly AgentTask[], id: string | undefined) {
  if (!id) return null;
  const task = tasks.find(task => task.id === id);
  return h('div', { class: 'detail-row', 'data-testid': 'computer-reviewed-task' }, [
    h('dt', computerLabel('task')),
    h('dd', { title: id }, task ? taskOption(task) : id),
  ]);
}
