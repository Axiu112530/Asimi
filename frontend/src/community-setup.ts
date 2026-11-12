import { defineComponent, h, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { AIProfilesPage, AISettingsPanel } from './ai-pages';
import { ai, refreshAI, selectModel, selectProvider } from './ai-store';
import type { AIModel, AIProfile } from './ai-types';
import { call, ServiceError } from './bridge';
import { i18n, t } from './i18n';
import en from './locales/community-setup-en.json';
import zh from './locales/community-setup-zh.json';
import { product, refreshProduct, selectWorkspace } from './product-store';
import type { Workspace } from './product-types';
import { fail, saveSettings, state } from './store';
import { button, errorText, icon } from './ui';
import './community-setup.css';

i18n.global.mergeLocaleMessage('en-US', { communitySetup: en });
i18n.global.mergeLocaleMessage('zh-CN', { communitySetup: zh });

interface ProviderStatus {
  preset: { id: string; profile: AIProfile };
  configuredModel?: AIModel;
  hasCredential: boolean;
  verifiedAt?: string;
  defaultProviderId: string;
  defaultProviderName: string;
  defaultModelId: string;
  defaultUsable: boolean;
}

type Message = keyof typeof en;
const label = (key: Message, params?: Record<string, string>) => t('communitySetup.' + key, params);

function action(key: Message, id: string, work: () => unknown, disabled = false, primary = false) {
  return button('communitySetup.' + key, work, {
    class: ['button', primary ? 'primary' : 'secondary'],
    disabled, 'data-testid': 'community-' + id,
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
  COMMUNITY_PROVIDER_BALANCE: 'errorBalance',
  MODEL_NOT_FOUND: 'errorModel',
  PROVIDER_UNAVAILABLE: 'errorUnavailable',
  COMMUNITY_PROVIDER_CHANGED: 'errorChanged',
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
        const result = await call<ProviderStatus>('GetCommunityProvider');
        if (alive) setStatus(result);
      } catch (cause) {
        if (alive) error.value = codeOf(cause);
      } finally {
        loading.value = false;
      }
    }

    async function connect(event: Event) {
      event.preventDefault();
      const password = input.value;
      if (!password || pending.value || !selected.value || (!entered.value && !status.value?.hasCredential)) return;
      pending.value = true;
      emit('busy', true);
      error.value = '';
      try {
        const request = call<ProviderStatus>('ConnectCommunityProvider', {
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
          if (error.value === 'COMMUNITY_PROVIDER_CHANGED') void load(true);
        }
      }
    }

    watch(() => [state.connected, ai.profiles, ai.settings], () => void load(), { immediate: true });

    return () => {
      const current = status.value;
      const preset = current?.preset.profile;
      const model = current?.configuredModel ?? preset?.models[0];
      const capabilityKeys = ['text', 'streaming', 'vision', 'structuredOutput', 'toolCalling', 'computerUse'] as const;
      return h('div', { class: 'community-connection', 'aria-busy': pending.value || loading.value }, [
        loading.value && !current ? h('p', { role: 'status' }, label('loading')) : null,
        !props.wizard && current ? h('dl', { class: 'community-default' }, [
          h('dt', label('defaultModel')),
          h('dd', { 'data-testid': 'community-default-model' }, current.defaultModelId
            ? [current.defaultProviderName ? current.defaultProviderName + ' · ' : '', h('code', current.defaultModelId)]
            : label('offline')),
        ]) : null,
        current && preset && model ? h('form', { class: 'community-form', onSubmit: connect }, [
          h('div', { class: 'ai-form-grid' }, [
            h('label', { class: 'ai-field' }, [
              h('span', label('provider')),
              h('select', {
                value: selected.value, disabled: pending.value || !state.connected,
                onChange: (event: Event) => { selected.value = (event.target as HTMLSelectElement).value; },
                'data-testid': 'community-provider',
              }, [h('option', { value: current.preset.id }, preset.name)]),
            ]),
            h('dl', { class: 'community-default' }, [h('dt', label('recommended')), h('dd', h('code', model.id))]),
          ]),
          h('label', { class: 'ai-field' }, [
            h('span', label('key')),
            h('input', {
              ref: input, type: 'password', autocomplete: 'off', spellcheck: false,
              disabled: pending.value || !state.connected, maxlength: 8192,
              'aria-describedby': 'community-key-description', 'data-testid': 'community-api-key',
              onInput: (event: Event) => { entered.value = !!(event.target as HTMLInputElement).value; },
            }),
          ]),
          h('p', { class: 'muted micro', id: 'community-key-description' }, label(current.hasCredential ? 'keyStored' : 'keyPrivacy')),
          h('p', { class: 'muted micro' }, label('testNote')),
          h('div', { class: 'community-actions' }, [
            h('button', {
              type: 'submit', class: 'button primary',
              disabled: pending.value || !state.connected || !selected.value || (!entered.value && !current.hasCredential),
              'data-testid': 'community-connect',
            }, label(error.value ? 'retry' : 'connect')),
            pending.value ? h('span', { role: 'status' }, label('testing')) : null,
          ]),
          error.value ? null : current.verifiedAt
            ? h('p', { class: 'community-verified', role: 'status', 'data-testid': 'community-connected' }, [
                icon('check', 16), label('verified', { model: model.id }),
              ])
            : !pending.value ? h('p', { class: 'muted' }, label(current.hasCredential ? 'unverified' : 'noKey')) : null,
          error.value ? h('p', {
            role: 'alert', class: 'ai-error', 'data-testid': 'community-connection-error',
          }, connectionErrors[error.value] ? label(connectionErrors[error.value]) : errorText(error.value)) : null,
          h('details', { class: 'community-capabilities', open: !!current.verifiedAt }, [
            h('summary', label(current.configuredModel ? 'configuredCapabilities' : 'capabilities')),
            h('dl', { class: 'ai-capabilities' }, capabilityKeys.map(key => h('div', [
              h('dt', t('ai.' + key)), h('dd', t('ai.' + model.capabilities[key])),
            ]))),
            h('p', { class: 'muted micro' }, label(current.configuredModel ? 'configuredTestScope' : 'testScope')),
          ]),
        ]) : error.value ? h('div', [
          h('p', { role: 'alert', class: 'ai-error', 'data-testid': 'community-connection-error' }, errorText(error.value)),
          action('retry', 'reload-provider', () => load(), loading.value || !state.connected),
        ]) : null,
      ]);
    };
  },
});

export const CommunityProviders = defineComponent({
  setup() {
    const loadedAdvanced = ref(false);
    return () => h('div', { class: 'page-scroll' }, h('div', { class: 'page community-provider-page' }, [
      h('header', { class: 'page-heading' }, h('h1', t('ai.providers'))),
      h('section', { class: 'settings-section' }, h(ProviderConnection)),
      h('details', {
        class: 'settings-section community-advanced', 'data-testid': 'community-provider-advanced',
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
      class: 'product-overlay', role: 'dialog', 'aria-modal': 'true',
      'aria-labelledby': 'community-setup-title', 'data-testid': 'community-onboarding', onKeydown: keepFocus,
    }, h('section', { class: 'product-onboarding community-wizard' }, [
      h('h1', { id: 'community-setup-title', ref: title, tabindex: -1 }, label(step.value)),
      step.value === 'welcome' ? [
        h('p', label('intro')),
        h('p', { class: 'muted' }, label('privacy')),
        h('label', { class: 'ai-field' }, [
          h('span', t('settings.language')),
          h('select', {
            value: state.settings.appLanguage, disabled: state.saving || !state.connected,
            'data-testid': 'community-language',
            onChange: (event: Event) => saveSettings({ appLanguage: (event.target as HTMLSelectElement).value as 'en-US' | 'zh-CN' }),
          }, [
            h('option', { value: 'en-US' }, t('settings.english')),
            h('option', { value: 'zh-CN' }, t('settings.chinese')),
          ]),
        ]),
        h('div', { class: 'community-actions' }, action('continue', 'continue', () => { step.value = 'connection'; }, false, true)),
      ] : step.value === 'connection' ? [
        h(ProviderConnection, {
          wizard: true,
          onStatus: (result: ProviderStatus) => { provider.value = result; },
          onBusy: (value: boolean) => { connecting.value = value; },
        }),
        h('div', { class: 'community-actions' }, [
          action('back', 'back', () => { step.value = 'welcome'; }, connecting.value),
          provider.value?.defaultUsable
            ? action('nextReady', 'next-ready', () => { step.value = 'ready'; }, connecting.value, true)
            : action('skipAI', 'skip-ai', () => { step.value = 'ready'; }, connecting.value),
        ]),
      ] : [
        h('p', { 'data-testid': 'community-ready-status' }, provider.value?.defaultUsable
          ? label('readyAI', { provider: provider.value.defaultProviderName, model: provider.value.defaultModelId })
          : label('readyOffline')),
        h('p', { class: 'muted' }, label('project')),
        workspace() ? h('dl', { class: 'community-default' }, [
          h('dt', label('selectedProject')), h('dd', { 'data-testid': 'community-selected-project' }, workspace()?.name),
        ]) : null,
        error.value ? h('p', { role: 'alert', class: 'ai-error' }, errorText(error.value)) : null,
        h('div', { class: 'community-actions' }, [
          action('back', 'back', () => { step.value = 'connection'; }, busy.value),
          action('pickProject', 'pick-project', pick, busy.value || !state.connected),
          action(workspace() ? 'openProject' : 'finish', 'finish', finish, busy.value || !state.connected, true),
        ]),
      ],
    ]));
  },
});

export const CommunityOnboarding = defineComponent({
  props: { openWorkspace: Function },
  setup(props) {
    return () => product.showOnboarding ? h(SetupWizard, { openWorkspace: props.openWorkspace }) : null;
  },
});
