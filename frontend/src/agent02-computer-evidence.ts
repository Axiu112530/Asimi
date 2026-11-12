import { defineComponent, h, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { call, ServiceError } from './bridge';
import { computerLabel } from './agent02-computer-fields';
import { date, known, t } from './i18n';
import { openConversation, state } from './store';
import { button, errorText } from './ui';

interface Evidence {
  id: string;
  kind: 'computer.action' | 'computer.screen_analysis';
  actionId?: string;
  actionType?: string;
  requestId?: string;
  conversationId?: string;
  status: string;
  verified: boolean;
  errorCode?: string;
  createdAt: string;
  selector?: string;
  observedAt?: string;
}

export const ComputerTaskEvidence = defineComponent({
  props: { taskId: { type: String, required: true } },
  setup(props) {
    const records = ref<Evidence[]>();
    const error = ref('');
    let version = 0;
    let loading = false;
    let timer: ReturnType<typeof setInterval>;

    async function refresh() {
      if (!state.connected || !props.taskId || loading) return;
      loading = true;
      const generation = version;
      try {
        const next = await call<Evidence[]>('ListComputerTaskEvidence', { taskId: props.taskId });
        if (generation === version) {
          records.value = next;
          error.value = '';
        }
      } catch (failure) {
        if (generation === version) error.value = failure instanceof ServiceError ? failure.code : 'app.unexpected';
      } finally {
        if (generation === version) loading = false;
      }
    }

    watch([() => props.taskId, () => state.connected], () => {
      version++;
      loading = false;
      records.value = undefined;
      error.value = '';
      void refresh();
    }, { immediate: true });
    onMounted(() => { timer = setInterval(refresh, 3000); });
    onBeforeUnmount(() => { version++; clearInterval(timer); });

    return () => h('section', { class: 'product-panel', 'data-testid': 'computer-task-evidence' }, [
      h('h2', computerLabel('evidence')),
      error.value ? h('p', { class: 'ai-error', role: 'alert' }, errorText(error.value)) : null,
      records.value?.length ? h('ol', { class: 'control-journal' }, records.value.map(entry => {
        const screen = entry.kind === 'computer.screen_analysis';
        const link = entry.conversationId;
        return h('li', { key: entry.id, 'data-testid': 'computer-task-evidence-entry', 'data-status': entry.status }, [
          h('div', { class: 'control-heading' }, [
            h('strong', computerLabel(screen ? 'screen' : 'action')),
            h('time', { datetime: entry.createdAt }, date(entry.createdAt)),
          ]),
          h('p', [
            known((screen ? 'tasks.status.' : 'control.statuses.') + entry.status, 'common.unknown'),
            entry.verified ? ` · ${t('control.verified')}` : null,
          ]),
          h('code', `${entry.actionType ? `${entry.actionType} · ` : ''}${entry.actionId || entry.requestId}`),
          entry.selector && entry.observedAt ? h('p', { class: 'muted' }, t('computerSuggestion.observation', {
            selector: entry.selector, date: date(entry.observedAt),
          })) : null,
          entry.selector ? h('p', { class: 'muted' }, t('computerSuggestion.model')) : null,
          entry.errorCode ? h('p', { class: 'muted' }, errorText(entry.errorCode)) : null,
          link ? button('screen.openConversation', () => openConversation(link), { class: 'text-button' }) : null,
        ]);
      })) : records.value ? h('p', { class: 'muted' }, computerLabel('empty')) : null,
    ]);
  },
});
