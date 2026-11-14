import { defineComponent, h, onBeforeUnmount, ref } from 'vue';
import { ai, emptyModel, refreshAI } from './ai-store';
import type { AIProfile } from './ai-types';
import { call, ServiceError } from './bridge';
import { t } from './i18n';
import { state } from './store';
import { errorText } from './ui';

export const CustomProviders = defineComponent({
  setup() {
    const draft = ref<AIProfile>();
    const name = ref('');
    const endpoint = ref('');
    const modelId = ref('');
    const key = ref<HTMLInputElement>();
    const pending = ref(false);
    const error = ref('');
    const done = ref(false);
    const open = ref(false);

    function begin(profile?: AIProfile) {
      draft.value = profile ? JSON.parse(JSON.stringify(profile)) : undefined;
      name.value = profile?.name || '';
      endpoint.value = profile?.baseUrl || '';
      modelId.value = profile?.id === ai.settings.defaultProviderId
        ? ai.settings.defaultModelId : profile?.models[0]?.id || '';
      if (key.value) key.value.value = '';
      done.value = false;
      error.value = '';
      open.value = true;
    }

    onBeforeUnmount(() => { if (key.value) key.value.value = ''; });

    async function save(event: Event) {
      event.preventDefault();
      if (pending.value || !state.connected) return;
      pending.value = true;
      error.value = '';
      done.value = false;
      try {
        const id = modelId.value.trim();
        const baseUrl = endpoint.value.trim();
        const stored = draft.value;
        const existing = stored?.models.find(model => model.id === id);
        const model = existing ?? { ...emptyModel(), id, displayName: id };
        if (!existing) model.capabilities.streaming = 'SUPPORTED';
        const profile: AIProfile = {
          ...stored,
          id: stored?.id ?? '', name: name.value.trim(), baseUrl,
          protocol: stored?.baseUrl === baseUrl ? stored.protocol
            : baseUrl.replace(/\/+$/, '').endsWith('/responses') ? 'responses' : 'chat-completions',
          enabled: true, hasCredential: stored?.hasCredential ?? false,
          trustedHttp: stored?.baseUrl === baseUrl ? stored.trustedHttp : false,
          models: [...(stored?.models.filter(item => item.id !== id) ?? []), model],
        };
        draft.value = await call<AIProfile>('SaveAIProfile', { profile });
        if (key.value?.value) {
          const request = call<{ hasCredential: boolean }>('StoreCredential', {
            providerId: draft.value.id, secret: key.value.value,
          });
          key.value.value = '';
          const credential = await request;
          draft.value.hasCredential = credential.hasCredential;
        }
        await call('SaveAISettings', {
          ...ai.settings, defaultProviderId: draft.value.id, defaultModelId: id,
        });
        await refreshAI();
        done.value = true;
        open.value = false;
      } catch (cause) {
        error.value = cause instanceof ServiceError ? cause.code : 'app.unexpected';
      } finally {
        if (key.value) key.value.value = '';
        pending.value = false;
      }
    }

    function field(label: string, value: string, change: (value: string) => void, extra: Record<string, unknown> = {}) {
      return h('label', { class: 'ai-field' }, [
        h('span', t('providerCustom.' + label)),
        h('input', {
          value, required: true, disabled: pending.value, maxlength: label === 'endpoint' ? 2048 : 256,
          onInput: (event: Event) => change((event.target as HTMLInputElement).value), ...extra,
        }),
      ]);
    }

    return () => h('section', { class: 'settings-section custom-providers', 'data-testid': 'custom-providers' }, [
      h('header', { class: 'ai-row' }, [
        h('h2', t('providerCustom.title')),
        h('button', {
          type: 'button', class: 'button secondary', disabled: pending.value || !state.connected,
          'data-testid': 'custom-provider-add', onClick: () => begin(),
        }, t('providerCustom.add')),
      ]),
      done.value ? h('p', { role: 'status' }, t('providerCustom.saved')) : null,
      h('details', { class: 'custom-provider-list' }, [
        h('summary', t('providerCustom.connected')),
        ...ai.profiles.map(profile => h('div', { class: 'custom-provider-row', key: profile.id }, [
          h('span', profile.name),
          h('button', {
            type: 'button', class: 'button secondary', disabled: pending.value,
            onClick: () => begin(profile), 'data-testid': 'provider-edit-' + profile.id,
          }, t('providerCustom.edit')),
        ])),
      ]),
      open.value ? h('form', { class: 'preset-form', onSubmit: save, 'data-testid': 'custom-provider-form' }, [
        field('name', name.value, value => { name.value = value; }, { 'data-testid': 'custom-provider-name' }),
        field('endpoint', endpoint.value, value => { endpoint.value = value; }, {
          type: 'url', placeholder: 'https://api.example.com/v1', 'data-testid': 'custom-provider-endpoint',
        }),
        field('model', modelId.value, value => { modelId.value = value; }, { 'data-testid': 'custom-provider-model' }),
        h('label', { class: 'ai-field' }, [
          h('span', t('presetSetup.key')),
          h('input', {
            type: 'password', ref: key, autocomplete: 'off', maxlength: 8192,
            disabled: pending.value, 'data-testid': 'custom-provider-key',
          }),
          h('small', { class: 'muted' }, t(draft.value?.hasCredential ? 'presetSetup.keyStored' : 'presetSetup.keyPrivacy')),
        ]),
        h('p', { class: 'micro muted' }, t('providerCustom.hint')),
        h('div', { class: 'preset-actions' }, [
          h('button', { type: 'submit', class: 'button primary', disabled: pending.value, 'data-testid': 'custom-provider-save' }, t('providerCustom.save')),
          h('button', {
            type: 'button', class: 'button secondary', disabled: pending.value,
            onClick: () => { if (key.value) key.value.value = ''; open.value = false; },
          }, t('common.cancel')),
        ]),
      ]) : null,
      error.value ? h('p', { role: 'alert', class: 'ai-error' }, errorText(error.value)) : null,
    ]);
  },
});
