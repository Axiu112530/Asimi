import { defineComponent, h, onBeforeUnmount, onMounted, ref } from 'vue';
import { t } from './i18n';
import { state, stopComputerControl } from './store';
import { icon } from './ui';
import './computer-copy';

export const ComputerActivity = defineComponent({
  setup() {
    const stopping = ref(false);
    async function stop() {
      if (!state.activeControlActionId || stopping.value) return;
      stopping.value = true;
      try { await stopComputerControl(); } finally { stopping.value = false; }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key !== 'Escape' || event.isComposing || !state.activeControlActionId) return;
      event.preventDefault();
      void stop();
    }
    onMounted(() => window.addEventListener('keydown', onKey, true));
    onBeforeUnmount(() => window.removeEventListener('keydown', onKey, true));
    return () => state.activeControlActionId ? h('div', {
      class: 'computer-activity', role: 'status', 'data-testid': 'computer-activity',
    }, [
      h('span', { class: 'computer-activity-title' }, [icon('computer', 18), t('computerCenter.active')]),
      h('span', t(state.controlEscapeAvailable ? 'computerCenter.escape' : 'computerCenter.escapeLocal')),
      h('button', {
        type: 'button', class: 'button danger-subtle', disabled: stopping.value,
        onClick: stop, 'data-testid': 'computer-activity-stop',
      }, t(stopping.value ? 'computerCenter.stopping' : 'computerCenter.stop')),
    ]) : null;
  },
});
