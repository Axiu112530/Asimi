import { defineComponent, h, onUnmounted, ref, watch, type PropType } from 'vue';
import { call, subscribe } from './bridge';
import { state } from './store';
import { t } from './i18n';
import { TaskActivity } from './task-activity';
import type { ProductState } from './product-types';
import type { Conversation } from './types';

export const ConversationActivity = defineComponent({
  props: { conversation: { type: Object as PropType<Conversation | null>, default: null } },
  setup(props) {
    const snapshot = ref<ProductState>(), error = ref(false);
    let version = 0, active = true, off: (() => void) | undefined, timer: ReturnType<typeof setTimeout> | undefined;
    async function load() {
      if (!active || !state.connected || !props.conversation?.id) return;
      const current = ++version;
      try {
        const result = await call<ProductState>('GetMVPState');
        if (active && state.connected && current === version) { snapshot.value = result; error.value = false; }
      } catch {
        if (active && state.connected && current === version) error.value = true;
      }
    }
    function dispose() { version++; off?.(); off = undefined; clearTimeout(timer); }
    watch([() => props.conversation?.id, () => state.connected], ([id, connected]) => {
      dispose(); snapshot.value = undefined; error.value = false;
      if (!id || !connected) return;
      void load();
      off = subscribe('asimi:changed', event => {
        const change = event as { conversationId?: string; taskId?: string } | undefined;
        if (change?.conversationId && change.conversationId !== props.conversation?.id && !change.taskId) return;
        clearTimeout(timer); timer = setTimeout(() => void load(), 120);
      });
    }, { immediate: true, flush: 'sync' });
    onUnmounted(() => { active = false; dispose(); });
    return () => {
      const conversation = props.conversation;
      if (!conversation || !state.connected) return null;
      const taskIds = new Set(conversation.messages.map(message => message.taskId).filter(Boolean));
      const tasks = snapshot.value?.agentTasks.filter(task => task.conversationId === conversation.id || taskIds.has(task.id)) || [];
      if (!tasks.length && !error.value) return null;
      return h('section', { class: 'conversation-activity', 'aria-label': t('activity.history'), 'data-testid': 'conversation-activity' }, [
        error.value ? h('p', { class: 'micro muted', role: 'status' }, [t('activity.unavailable'), ' ', h('button', { type: 'button', class: 'text-button', onClick: load }, t('activity.retry'))]) : null,
        ...tasks.map(task => h(TaskActivity, { key: task.id, task, actions: snapshot.value?.actions || [] })),
      ]);
    };
  },
});
