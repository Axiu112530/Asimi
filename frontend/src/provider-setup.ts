import { defineComponent, h, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { AIProfilesPage, AISettingsPanel } from './ai-pages';
import { ai, refreshAI, selectModel, selectProvider } from './ai-store';
import type { AIModel, AIProfile } from './ai-types';
import { call, ServiceError } from './bridge';
import { i18n, t } from './i18n';
import en from './locales/provider-setup-en.json';
import zh from './locales/provider-setup-zh.json';
import { product, refreshProduct, selectWorkspace } from './product-store';
import type { Workspace } from './product-types';
import { fail, saveSettings, state } from './store';
import { button, errorText, icon } from './ui';
import './provider-setup.css';

i18n.global.mergeLocaleMessage('en-US', { presetSetup: en });
i18n.global.mergeLocaleMessage('zh-CN', { presetSetup: zh });

interface ProviderStatus {
  preset: { id: string; profile: AIProfile };
  options: { id: string; name: string }[];
  configuredModel?: AIModel;
  hasCredential: boolean;
  verifiedAt?: string;
  defaultProviderId: string;
  defaultProviderName: string;
  defaultModelId: string;
  defaultUsable: boolean;
}

type Message = keyof typeof en;
const label = (key: Message, params?: Record<string, string>) => t('presetSetup.' + key, params);

function action(key: Message, id: string, work: () => unknown, disabled = false, primary = false) {
  return button('presetSetup.' + key, work, {
    class: ['button', primary ? 'primary' : 'secondary'],
    disabled, 'data-testid': 'preset-' + id,
  });
}

const connectionErrors: Record<string, Message> = {
  REQUEST_TIMEOUT: 'errorTimeout',
  INVALID_API_KEY: 'errorKey',
  CREDENTIAL_UNAVAILABLE: 'errorCredential',
  CREDENTIAL_DECRYPT_FAILED: 'errorCredential',
  NETWORK_ERROR: 'errorNetwork',
  TLS_ERROR: 'errorNetwork',
  RATE_LIMITED: 'errorRate',
  PRESET_PROVIDER_BALANCE: 'errorBalance',
  MODEL_NOT_FOUND: 'errorModel',
  PROVIDER_UNAVAILABLE: 'errorUnavailable',
  PRESET_PROVIDER_CHANGED: 'errorChanged',
  RESPONSE_INCOMPLETE: 'errorIncomplete',
  INVALID_RESPONSE: 'errorIncomplete',
  REQUEST_OUTCOME_UNKNOWN: 'errorOutcome',
};

function codeOf(error: unknown) {
  return error instanceof ServiceError ? error.code : 'app.unexpected';
}

const ProviderConnection = defineComponent({
  props: { wizard: Boolean },
  emits: ['status', 'busy'],
  setup(props, { emit }) {
    const status = ref<ProviderStatus>();
    const pending = ref(false);
    const loading = ref(false);
    const error = ref('');
    const input = ref<HTMLInputElement>();
    const entered = ref(false);
    const selected = ref('');
    let alive = true;

    onBeforeUnmount(() => {
      alive = false;
      if (input.value) input.value.value = '';
    });

    function setStatus(result: ProviderStatus) {
      status.value = result;
      selected.value = result.preset.id;
      emit('status', result);
    }

    async function load(preserveError = false) {
      if (!state.connected || loading.value || pending.value) return;
      loading.value = true;
      if (!preserveError) error.value = '';
      try {
        const result = await call<ProviderStatus>('GetPresetProvider', { presetId: selected.value });
        if (alive) setStatus(result);
      } catch (cause) {
        if (alive) error.value = codeOf(cause);
      } finally {
        loading.value = false;
      }
    }

    function changeProvider(event: Event) {
      selected.value = (event.target as HTMLSelectElement).value;
      if (input.value) input.value.value = '';
      entered.value = false;
      void load();
    }

    async function connect(event: Event) {
      event.preventDefault();
      const password = input.value;
      if (!password || loading.value || pending.value || selected.value !== status.value?.preset.id ||
        (!entered.value && !status.value.hasCredential)) return;
      pending.value = true;
      emit('busy', true);
      error.value = '';
      try {
        const request = call<ProviderStatus>('ConnectPresetProvider', {
          presetId: selected.value, secret: password.value,
        });
        password.value = '';
        entered.value = false;
        const result = await request;
        if (alive) setStatus(result);
        await refreshAI().catch(fail);
        if (!state.conversation) {
          ai.mode = 'real';
          selectProvider(ai.assistant, result.defaultProviderId);
          selectModel(ai.assistant, result.defaultModelId);
          state.inspectorOpen = false;
        }
      } catch (cause) {
        if (alive) error.value = codeOf(cause);
      } finally {
        pending.value = false;
        if (alive) {
          emit('busy', false);
          if (error.value === 'PRESET_PROVIDER_CHANGED') void load(true);
        }
      }
    }

    watch(() => [state.connected, ai.profiles, ai.settings], () => void load(), { immediate: true });

    return () => {
      const current = status.value;
      const preset = current?.preset.profile;
      const model = current?.configuredModel ?? preset?.models[0];
      const selectionReady = !loading.value && selected.value === current?.preset.id;
      const providerName = current?.options.find(item => item.id === selected.value)?.name ?? '';
      const capabilityKeys = ['text', 'streaming', 'vision', 'structuredOutput', 'toolCalling', 'computerUse'] as const;
      return h('div', { class: 'preset-connection', 'aria-busy': pending.value || loading.value }, [
        loading.value && !current ? h('p', { role: 'status' }, label('loading')) : null,
        !props.wizard && current ? h('dl', { class: 'preset-default' }, [
          h('dt', label('defaultModel')),
          h('dd', { 'data-testid': 'preset-default-model' }, current.defaultModelId
            ? [current.defaultProviderName ? current.defaultProviderName + ' · ' : '', h('code', current.defaultModelId)]
            : label('offline')),
        ]) : null,
        current && preset && model ? h('form', { class: 'preset-form', onSubmit: connect }, [
          h('div', { class: 'ai-form-grid' }, [
            h('label', { class: 'ai-field' }, [
              h('span', label('provider')),
              h('select', {
                value: selected.value, disabled: loading.value || pending.value || !state.connected,
                onChange: changeProvider,
                'data-testid': 'preset-provider',
              }, current.options.map(item => h('option', { value: item.id, key: item.id }, item.name))),
            ]),
            selectionReady ? h('dl', { class: 'preset-default' }, [h('dt', label('recommended')), h('dd', h('code', model.id))]) : null,
          ]),
          !selectionReady ? (error.value ? h('div', [
            h('p', { role: 'alert', class: 'ai-error' }, errorText(error.value)),
            action('retry', 'reload-provider', () => load(), loading.value || !state.connected),
          ]) : h('p', { role: 'status' }, label('loading'))) : [h('label', { class: 'ai-field' }, [
            h('span', label('key')),
            h('input', {
              ref: input, type: 'password', autocomplete: 'off', spellcheck: false,
              disabled: pending.value || !state.connected, maxlength: 8192,
              'aria-describedby': 'preset-key-description', 'data-testid': 'preset-api-key',
              onInput: (event: Event) => { entered.value = !!(event.target as HTMLInputElement).value; },
            }),
          ]),
          h('p', { class: 'muted micro', id: 'preset-key-description' }, label(current.hasCredential ? 'keyStored' : 'keyPrivacy')),
          h('p', { class: 'muted micro' }, label('testNote', { provider: providerName })),
          h('div', { class: 'preset-actions' }, [
            h('button', {
              type: 'submit', class: 'button primary',
              disabled: pending.value || !state.connected || !selected.value || (!entered.value && !current.hasCredential),
              'data-testid': 'preset-connect',
            }, label(error.value ? 'retry' : 'connect')),
            pending.value ? h('span', { role: 'status' }, label('testing')) : null,
          ]),
          error.value ? null : current.verifiedAt
            ? h('p', { class: 'preset-verified', role: 'status', 'data-testid': 'preset-connected' }, [
                icon('check', 16), label('verified', { model: model.id }),
              ])
            : !props.wizard && !pending.value && current.hasCredential ? h('p', { class: 'muted' }, label('unverified')) : null,
          error.value ? h('p', {
            role: 'alert', class: 'ai-error', 'data-testid': 'preset-connection-error',
          }, connectionErrors[error.value] ? label(connectionErrors[error.value]) : errorText(error.value)) : null,
          props.wizard ? null : h('details', { class: 'preset-capabilities' }, [
            h('summary', label(current.configuredModel ? 'configuredCapabilities' : 'capabilities')),
            h('dl', { class: 'ai-capabilities' }, capabilityKeys.map(key => h('div', [
              h('dt', t('ai.' + key)), h('dd', t('ai.' + model.capabilities[key])),
            ]))),
            h('p', { class: 'muted micro' }, label(current.configuredModel ? 'configuredTestScope' : 'testScope')),
          ]),
          ],
        ]) : error.value ? h('div', [
          h('p', { role: 'alert', class: 'ai-error', 'data-testid': 'preset-connection-error' }, errorText(error.value)),
          action('retry', 'reload-provider', () => load(), loading.value || !state.connected),
        ]) : null,
      ]);
    };
  },
});

export const PresetProviders = defineComponent({
  setup() {
    const loadedAdvanced = ref(false);
    return () => h('div', { class: 'page-scroll' }, h('div', { class: 'page preset-provider-page' }, [
      h('header', { class: 'page-heading' }, h('h1', t('ai.providers'))),
      h('section', { class: 'settings-section' }, h(ProviderConnection)),
      h('details', {
        class: 'settings-section preset-advanced', 'data-testid': 'preset-provider-advanced',
        onToggle: (event: Event) => { if ((event.currentTarget as HTMLDetailsElement).open) loadedAdvanced.value = true; },
      }, [
        h('summary', label('advanced')),
        loadedAdvanced.value ? h(AIProfilesPage, { embedded: true }) : null,
        loadedAdvanced.value ? AISettingsPanel() : null,
      ]),
    ]));
  },
});

const SetupWizard = defineComponent({
  props: { openWorkspace: Function },
  setup(props) {
    const step = ref<'welcome' | 'connection' | 'ready'>('welcome');
    const provider = ref<ProviderStatus>();
    const connecting = ref(false);
    const busy = ref(false);
    const error = ref('');
    const title = ref<HTMLHeadingElement>();
    const workspace = () => product.data.workspaces.find(w => w.id === product.selectedWorkspace);

    watch(step, async () => {
      error.value = '';
      await nextTick();
      title.value?.focus();
    }, { immediate: true });

    async function pick() {
      busy.value = true;
      error.value = '';
      try {
        const chosen = await call<Workspace>('PickWorkspace');
        await refreshProduct();
        if (chosen) selectWorkspace(chosen.id);
      } catch (cause) {
        if (codeOf(cause) !== 'WORKSPACE_SELECTION_CANCELLED') error.value = codeOf(cause);
      } finally {
        busy.value = false;
      }
    }

    async function finish() {
      busy.value = true;
      error.value = '';
      try {
        await call('CompleteOnboarding', { completed: true });
        await refreshProduct();
        product.showOnboarding = false;
        if (workspace()) {
          if (props.openWorkspace) props.openWorkspace();
          else state.route = 'developer';
        } else {
          state.route = 'home';
        }
      } catch (cause) {
        error.value = codeOf(cause);
      } finally {
        busy.value = false;
      }
    }

    function keepFocus(event: KeyboardEvent) {
      if (event.key !== 'Tab' || !(event.currentTarget instanceof HTMLElement)) return;
      const targets = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), select:not(:disabled), input:not(:disabled), summary'))
        .filter(element => element.getClientRects().length);
      const active = document.activeElement;
      if (!targets.length) {
        event.preventDefault();
      } else if (event.shiftKey && (active === targets[0] || active === title.value)) {
        event.preventDefault();
        targets[targets.length - 1].focus();
      } else if (!event.shiftKey && active === targets[targets.length - 1]) {
        event.preventDefault();
        targets[0].focus();
      }
    }

    return () => h('div', {
      class: 'setup-screen', role: 'dialog', 'aria-modal': 'true',
      'aria-labelledby': 'provider-setup-title', 'data-testid': 'preset-onboarding', onKeydown: keepFocus,
    }, h('section', { class: 'preset-wizard' }, [
      h('div', { class: 'setup-brand', 'aria-hidden': 'true' }, [icon('logo', 30)]),
      h('h1', { id: 'provider-setup-title', ref: title, tabindex: -1 }, label(step.value)),
      step.value === 'welcome' ? [
        h('p', label('intro')),
        h('label', { class: 'ai-field' }, [
          h('span', t('settings.language')),
          h('select', {
            value: state.settings.appLanguage, disabled: state.saving || !state.connected,
            'data-testid': 'preset-language',
            onChange: (event: Event) => saveSettings({ appLanguage: (event.target as HTMLSelectElement).value as 'en-US' | 'zh-CN' }),
          }, [
            h('option', { value: 'en-US' }, t('settings.english')),
            h('option', { value: 'zh-CN' }, t('settings.chinese')),
          ]),
        ]),
        h('div', { class: 'preset-actions' }, action('continue', 'continue', () => { step.value = 'connection'; }, false, true)),
      ] : step.value === 'connection' ? [
        h(ProviderConnection, {
          wizard: true,
          onStatus: (result: ProviderStatus) => { provider.value = result; },
          onBusy: (value: boolean) => { connecting.value = value; },
        }),
        h('div', { class: 'preset-actions' }, [
          action('back', 'back', () => { step.value = 'welcome'; }, connecting.value),
          provider.value?.defaultUsable
            ? action('nextReady', 'next-ready', () => { step.value = 'ready'; }, connecting.value, true)
            : action('skipAI', 'skip-ai', () => { step.value = 'ready'; }, connecting.value),
        ]),
      ] : [
        h('p', { class: 'muted' }, label('project')),
        workspace() ? h('dl', { class: 'preset-default' }, [
          h('dt', label('selectedProject')), h('dd', { 'data-testid': 'preset-selected-project' }, workspace()?.name),
        ]) : null,
        error.value ? h('p', { role: 'alert', class: 'ai-error' }, errorText(error.value)) : null,
        h('div', { class: 'preset-actions' }, [
          action('back', 'back', () => { step.value = 'connection'; }, busy.value),
          action('pickProject', 'pick-project', pick, busy.value || !state.connected),
          action(workspace() ? 'openProject' : 'finish', 'finish', finish, busy.value || !state.connected, true),
        ]),
      ],
    ]));
  },
});

export const PresetOnboarding = defineComponent({
  props: { openWorkspace: Function },
  setup(props) {
    return () => product.showOnboarding ? h(SetupWizard, { openWorkspace: props.openWorkspace }) : null;
  },
});
