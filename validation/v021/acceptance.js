import { Call } from '/wails/runtime.js';

const params = new URLSearchParams(location.search);
const stage = Number(params.get('stage'));
const fixture = params.get('fixture') || '';
const asimi = fixture === 'asimi' || fixture.startsWith('asimi-');
const checks = {};
const byId = id => document.querySelector(`[data-testid="${id}"]`);
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const rpc = (method, payload = {}) => Call.ByName('main.DesktopService.Call', method, JSON.stringify(payload));
async function call(method, payload) {
  const reply = await rpc(method, payload);
  if (reply.error) throw Error(method + ' ' + reply.error.code);
  return reply.data;
}
function check(name, value) {
  checks[name] = !!value;
  if (!value) throw Error(name);
}
async function until(name, read, seconds = 12) {
  const deadline = performance.now() + seconds * 1000;
  while (performance.now() < deadline) {
    const value = await read();
    if (value) return value;
    await pause(40);
  }
  throw Error('timeout ' + name);
}
async function click(id) {
  const element = await until(id, () => byId(id) && !byId(id).disabled && byId(id));
  element.click();
}
function input(id, value, event = 'input') {
  const element = byId(id);
  if (!element) throw Error('missing ' + id);
  element.value = value;
  element.dispatchEvent(new Event(event, { bubbles: true }));
}

function checkBrand(surface, text) {
  check(surface, /\bAsimi\b/.test(text || '') && !/\bDeskPilot\b/.test(text || ''));
}

function checkWelcomeBrand(language) {
  const heading = byId('preset-onboarding').querySelector('h1');
  checkBrand(`welcomeBrand${language}`, heading?.textContent);
  checkBrand(`sidebarBrand${language}`, document.querySelector('.brand strong')?.textContent);
  checkBrand(`webViewTitle${language}`, document.title);
}

async function setup(live) {
  await until('first run', () => byId('preset-onboarding'));
  check('accessibleWizard', byId('preset-onboarding').getAttribute('aria-modal') === 'true');
  if (asimi) {
    check('englishByDefault', document.documentElement.lang === 'en-US');
    checkWelcomeBrand('EN');
  }
  if (!live) {
    input('preset-language', 'zh-CN', 'change');
    await until('onboarding locale', () => document.documentElement.lang === 'zh-CN');
    check('switchedInterfaceLanguage', true);
    if (asimi) checkWelcomeBrand('ZH');
  }
  await click('preset-continue');
  const provider = await until('provider', () => byId('preset-provider'));
  check('onlyVerifiedPresetOffered', provider.options.length === 1 && provider.value === 'deepseek');
  check('passwordField', byId('preset-api-key').type === 'password' && byId('preset-connect').disabled);
  if (live) {
    input('preset-api-key', 'preset-native-acceptance-placeholder');
    await click('preset-connect');
    await until('verified or error', () => byId('preset-connected') || byId('preset-connection-error'), 30);
    check('actualConnectionVerified', !!byId('preset-connected'));
    check('keyInputCleared', !byId('preset-api-key').value);
    check('presetSource', (await call('GetConfiguration')).sources['provider.selection'] === 'PRESET');
    await click('preset-next-ready');
  } else {
    check('offlineHasNoUsage', !(await call('GetUsage'))?.length);
    await click('preset-skip-ai');
  }
  await click('preset-pick-project');
  await until('selected project', () => byId('preset-selected-project'));
  await click('preset-finish');
  await until('developer page', () => byId('product-developer') && !byId('preset-onboarding'));
  const state = await call('GetMVPState');
  const workspace = state.workspaces[0];
  check('onboardingAndWorkspacePersisted', state.onboarding.completed && state.workspaces.length === 1 && workspace.trustStatus === 'USER_SELECTED');
  const inspection = await call('InspectDeveloperProject', { workspaceId: workspace.id, exclusions: [] });
  check('autoDetectedGoProject', JSON.stringify(inspection.summary).includes('Go'));
  check('gitStatusAbsentIsExplicit', inspection.git.status === 'NOT_REPOSITORY');
  if (!live) {
    await click('nav-settings');
    const advanced = await until('advanced settings', () => byId('preset-advanced'));
    if (asimi) {
      checkBrand('settingsAboutBrand', document.querySelector('.about-brand h2')?.textContent);
      checkBrand('settingsDescriptionBrand', document.querySelector('.about-brand p')?.textContent);
    }
    check('advancedCollapsed', !advanced.open);
    advanced.querySelector('summary').click();
    await until('limit form', () => byId('preset-limit-requests'));
    input('preset-limit-requests', '3');
    input('preset-limit-attachments', '2');
    await click('preset-save-limits');
    await until('saved override', async () => (await call('GetConfiguration')).agent.requests === 3);
    check('userOverrideHasProvenance', (await call('GetConfiguration')).sources['agent.requests'] === 'USER_DEFINED');
    check('mediaOverrideHasProvenance', (await call('GetConfiguration')).sources['media.attachments'] === 'USER_DEFINED');
    check('upperLimitGuard', !!(await rpc('SaveAgentPreferences', { limits: { requests: 6 } })).error);
    check('resetRequiresConfirmation', !!(await rpc('RestoreConfigurationDefaults', { confirm: false })).error);
  }
}

async function restart(live) {
  await until('home', () => byId('product-home'));
  const state = await call('GetMVPState');
  check('doesNotRepeatWizard', state.onboarding.completed && !byId('preset-onboarding'));
  if (asimi) checkBrand('homeBrandAfterRestart', byId('product-home').textContent);
  check('workspaceSurvivesRestart', state.workspaces.length === 1);
  const settings = await call('GetSettings');
  check('languageSurvivesRestart', settings.appLanguage === (live ? 'en-US' : 'zh-CN'));
  if (live) {
    await click('nav-models');
    const current = await until('current provider', () => byId('preset-default-model'));
    check('defaultShownAfterRestart', current.textContent.includes('deepseek-flash') && !!byId('preset-connected'));
    const provider = await call('GetPresetProvider');
    check('credentialAndVerificationRestored', provider.hasCredential && provider.defaultUsable && !!provider.verifiedAt);
  } else {
    const config = await call('GetConfiguration');
    check('overrideSurvivesRestart', config.agent.requests === 3 && config.media.attachments === 2);
    const reset = await call('RestoreConfigurationDefaults', { confirm: true });
    check('resetRestoresSourceAndValue', reset.agent.requests === reset.agentMaximum.requests && reset.media.attachments === reset.mediaMaximum.attachments && reset.sources['agent.requests'] === 'DEFAULT' && reset.sources['media.attachments'] === 'DEFAULT');
    await click('product-chat');
    await until('offline composer', () => byId('composer'));
    check('offlineChatRemainsAvailable', !(await call('GetUsage'))?.length);
  }
}

async function upgrade() {
  await until('desktop booted', () => byId('nav-home'));
  check('runningNewVersion', (await call('GetAppInfo')).version === '0.2.1');
  const settings = await call('GetSettings');
  check('validPreferencesAfterUpgrade', !!settings.theme && !!settings.agentLanguage);
  const config = await call('GetConfiguration');
  check('newAgentLimitsDefaultOnLegacyProfile', config.sources['agent.requests'] === 'DEFAULT' && config.agent.requests === 5);
  if (fixture === 'provider' || fixture.startsWith('asimi-provider:')) {
    const profile = (await call('ListAIProfiles')).find(p => p.id === 'fixture-chat');
    check('providerCredentialPreserved', profile?.hasCredential);
    check('providerHistoryPreserved', (await call('GetUsage')).length >= 4);
    check('conversationsPreserved', (await call('ListConversations')).length >= 3);
    check('languagePreserved', settings.appLanguage === 'zh-CN');
    if (asimi) {
      const endpoint = fixture.slice('asimi-provider:'.length);
      check('reusedOriginalLoopbackEndpoint', /^http:\/\/127\.0\.0\.1:[1-9]\d{0,4}\/v1$/.test(endpoint) && profile.baseUrl === endpoint);
      const result = await call('TestAIConnection', {
        providerId: profile.id, modelId: 'fixture-model', content: 'Asimi upgrade connection check',
        confirmSend: true, maxOutputTokens: 128,
      });
      const usage = await until('fixture connection using upgraded DPAPI credential', async () =>
        (await call('GetUsage')).find(item => item.requestId === result.requestId && item.status !== 'RUNNING'),
      );
      check('restoredCredentialAuthenticatedLoopback', usage.status === 'COMPLETED');
    }
  } else {
    const state = await call('GetMVPState');
    check('oldWorkspacePreserved', asimi ? state.workspaces.length >= 1 : state.workspaces.length === 1);
    check('oldTaskAndRoundHistoryPreserved', state.agentTasks.some(task => task.status === 'COMPLETED' && task.agent?.rounds.length >= 3));
    check('oldFileAndCommandApprovalsPreserved', ['file', 'command'].every(kind => state.actions.some(action => action.kind === kind && action.status === 'SUCCEEDED')));
    if (fixture === 'asimi-agent') {
      const provider = await call('GetPresetProvider');
      check('presetCredentialAndModelPreserved', provider.hasCredential && provider.defaultUsable && !!provider.verifiedAt && provider.defaultModelId === 'deepseek-flash');
    }
  }
}

let error = '';
try {
  if (stage === 14 || stage === 16) await setup(stage === 16);
  else if (stage === 15 || stage === 17) await restart(stage === 17);
  else if (stage === 18) await upgrade();
  else throw Error('unexpected acceptance stage');
} catch (cause) {
  error = String(cause.message || cause);
}
await Call.ByName('main.AcceptanceService.Report', JSON.stringify({ stage, passed: !error, error, checks }));
