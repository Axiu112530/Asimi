import { defineComponent, h, onMounted, ref } from 'vue';
import { call } from './bridge';
import { refreshAI } from './ai-store';
import { t } from './i18n';
import { errorText } from './ui';

const agentFields = {
  requests: 1,
  stepsPerPlan: 1,
  contextBytes: 1024,
  outputTokens: 128,
  timeoutSeconds: 5,
  runSeconds: 60,
  budgetUsd: 0.0001,
};
const mediaFields = { attachments: 1, imageMiB: 1 };
type AgentField = keyof typeof agentFields;
type MediaField = keyof typeof mediaFields;
type Field = AgentField | MediaField;
interface Configuration {
  agent: Record<AgentField, number>;
  agentMaximum: Record<AgentField, number>;
  media: Record<MediaField, number>;
  mediaMaximum: Record<MediaField, number>;
  sources: Record<string, string>;
}
const agentKeys = Object.keys(agentFields) as AgentField[];
const mediaKeys = Object.keys(mediaFields) as MediaField[];

export const ConfigurationPanel = defineComponent({
  setup() {
    const resolved = ref<Configuration>();
    const inputs = ref<Partial<Record<Field, string>>>({});
    const error = ref('');
    const busy = ref(false);

    async function update(method: string, payload = {}, refresh = false) {
      busy.value = true;
      error.value = '';
      try {
        const value = await call<Configuration>(method, payload);
        resolved.value = value;
        inputs.value = Object.fromEntries([
          ...agentKeys.filter(key => value.sources['agent.' + key] === 'USER_DEFINED').map(key => [key, String(value.agent[key])]),
          ...mediaKeys.filter(key => value.sources['media.' + key] === 'USER_DEFINED').map(key => [key, String(value.media[key])]),
        ]);
        if (refresh) await refreshAI();
      } catch (e) {
        error.value = (e as { code?: string }).code || 'CONFIGURATION_INVALID';
      } finally {
        busy.value = false;
      }
    }
    onMounted(() => void update('GetConfiguration'));

    function field(key: Field, value: number, min: number, max: number, source: string) {
      return h('label', { class: 'settings-row', key }, [
        h('span', [
          h('strong', t('preset.fields.' + key)),
          h('p', { class: 'micro muted' }, t('preset.effective', { value, source: t('preset.sources.' + source) })),
        ]),
        h('input', {
          type: 'number', min, max, step: key === 'budgetUsd' ? 'any' : 1,
          value: inputs.value[key] ?? '', placeholder: String(value), disabled: busy.value,
          'data-testid': 'preset-limit-' + key,
          onInput: (event: Event) => { inputs.value[key] = (event.target as HTMLInputElement).value; },
        }),
      ]);
    }

    return () => h('section', { class: 'settings-section', 'data-testid': 'preset-configuration' }, [
      h('h2', t('preset.limits')),
      h('p', { class: 'muted' }, t('preset.limitsHint')),
      error.value ? h('p', { class: 'ai-error', role: 'alert' }, errorText(error.value)) : null,
      resolved.value ? h('form', {
        onSubmit: (event: SubmitEvent) => {
          event.preventDefault();
          const limits = Object.fromEntries(agentKeys.map(key => [key, Number(inputs.value[key] || 0)]));
          const media = Object.fromEntries(mediaKeys.map(key => [key, Number(inputs.value[key] || 0)]));
          void update('SaveAgentPreferences', { limits, media });
        },
      }, [
        ...agentKeys.map(key => field(key, resolved.value!.agent[key], agentFields[key], resolved.value!.agentMaximum[key], resolved.value!.sources['agent.' + key])),
        h('h3', t('preset.mediaLimits')),
        ...mediaKeys.map(key => field(key, resolved.value!.media[key], mediaFields[key], resolved.value!.mediaMaximum[key], resolved.value!.sources['media.' + key])),
        h('button', { type: 'submit', class: 'button primary', disabled: busy.value, 'data-testid': 'preset-save-limits' }, t('preset.saveLimits')),
        h('details', { class: 'preset-provenance' }, [
          h('summary', t('preset.provenance')),
          h('dl', { class: 'about-details' }, Object.entries(resolved.value.sources).filter(([key]) => !key.startsWith('agent.') && !key.startsWith('media.')).flatMap(([key, source]) => [h('dt', t('preset.provenanceFields.' + key.replace('.', '-'))), h('dd', t('preset.sources.' + source))])),
        ]),
      ]) : null,
      h('button', { class: 'button secondary', disabled: busy.value, 'data-testid': 'preset-reset-limits', onClick: () => {
        if (window.confirm(t('preset.resetConfirm'))) void update('RestoreConfigurationDefaults', { confirm: true }, true).then(() => { if (!error.value) return update('GetConfiguration'); });
      } }, t('preset.resetLimits')),
    ]);
  },
});
