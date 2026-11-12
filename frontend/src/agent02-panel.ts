import { defineComponent, h, ref, watch, type PropType } from 'vue';
import { call } from './bridge';
import { product, productWork, refreshProduct } from './product-store';
import { t, date } from './i18n';
import { badge, errorText } from './ui';
import type { AgentTask } from './product-types';

interface Preview {
  taskId: string;
  provider: string;
  endpoint: string;
  model: string;
  files: string[];
  content: string;
  hash: string;
  maxRequestUsd: number;
  remainingUsd: number;
  remainingRequests: number;
}

export const Agent02Panel = defineComponent({
  props: { task: { type: Object as PropType<AgentTask>, required: true } },
  setup(props) {
    const preview = ref<Preview | null>(null);
    watch(() => props.task.updatedAt, () => { preview.value = null; });

    function loadPreview() {
      return productWork(async () => {
        preview.value = await call<Preview>('PreviewAgentRun', { id: props.task.id });
      });
    }
    function sendPlan(review: Preview) {
      return productWork(async () => {
        preview.value = null;
        await call('PlanAgentRun', { id: review.taskId, contextHash: review.hash });
        await refreshProduct();
      });
    }
    const action = (key: string, id: string, run: () => unknown) => h('button', {
      class: 'button primary', type: 'button', disabled: product.busy, 'data-testid': id, onClick: run,
    }, t('agent02.' + key));

    return () => {
      const run = props.task.agent;
      if (!run) return null;
      const review = preview.value;
      return h('section', { class: 'product-card', 'data-testid': 'agent02-run', 'data-task-id': props.task.id }, [
        h('h3', t('agent02.planHistory')),
        h('p', { class: 'micro muted' }, run.limits
          ? t('preset.runLimits', { rounds: run.rounds.length, requests: run.limits.requests, at: date(run.deadline), reserve: run.reservedUsd.toFixed(4), budget: run.limits.budgetUsd })
          : t('agent02.limits', { rounds: run.rounds.length, at: date(run.deadline), reserve: run.reservedUsd.toFixed(4) })),
        ...run.rounds.map(round => h('details', { key: round.version }, [
          h('summary', [t('agent02.round', { number: round.version }), ' ', badge(round.status)]),
          round.error ? h('p', { role: 'alert', class: 'ai-error' }, errorText(round.error)) : null,
          round.plan ? h('ul', round.plan.steps.map(step => h('li', [h('code', step.type), ' ', step.summary || step.path || step.commandId || '']))) : null,
          h('p', { class: 'micro muted' }, t('agent02.roundUsage', { ms: round.durationMs, cost: round.estimatedUsd == null ? t('common.unknown') : round.estimatedUsd.toFixed(6) + ' USD' })),
          h('code', { class: 'product-hash' }, round.contextHash),
        ])),
        props.task.status === 'NEEDS_REVIEW' && !review ? action('review', 'agent02-review', loadPreview) : null,
        review ? h('div', { 'data-testid': 'agent02-preview' }, [
          h('h3', t('agent02.review')),
          h('p', t('agent02.disclosure', { provider: review.provider, model: review.model, count: review.files.length })),
          h('code', review.endpoint),
          h('p', t('agent02.estimate', { cost: review.maxRequestUsd.toFixed(6), remaining: review.remainingUsd.toFixed(4), count: review.remainingRequests })),
          h('pre', review.content),
          action('send', 'agent02-send', () => sendPlan(review)),
        ]) : null,
      ]);
    };
  },
});
