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

async function reviewVisual() {
  if (!params.has('visual')) return;
  globalThis.__asimiAcceptanceReady = { stage };
  await until('visual review', () => globalThis.__asimiAcceptanceContinue, 42);
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
  check('standardModeDefault', byId('preset-interface-mode')?.value === 'standard');
  if (!live) {
    await until('mode control enabled', () => !byId('preset-interface-mode').disabled);
    input('preset-interface-mode', 'developer', 'change');
    await until('developer choice saved', async () => (await call('GetSettings')).interfaceMode === 'developer' && !byId('preset-continue').disabled);
  }
  await click('preset-continue');
  const provider = await until('provider', () => byId('preset-provider'));
  check('allBuiltInProvidersOffered', [...provider.options].map(option => option.value).join(',') ===
    'deepseek,kimi,glm,qwen,hy,openai,claude,gemini' && provider.value === 'deepseek');
  check('passwordField', byId('preset-api-key').type === 'password' && byId('preset-connect').disabled);
  check('setupUsesOpaqueSurface', getComputedStyle(byId('preset-onboarding')).visibility === 'visible' &&
    getComputedStyle(byId('preset-onboarding')).backgroundColor !== 'rgba(0, 0, 0, 0)' &&
    getComputedStyle(document.querySelector('.desktop-app')).visibility === 'hidden');
  await reviewVisual();
  if (!live) {
    input('preset-api-key', 'unused-key-fixture');
    input('preset-provider', 'qwen', 'change');
    await until('Qwen preset loaded', () => byId('preset-api-key') && byId('preset-onboarding').textContent.includes('qwen-plus'));
    check('switchingPresetClearsKey', !byId('preset-api-key').value && byId('preset-connect').disabled);
    check('setupUsesSelectedProviderName', byId('preset-onboarding').textContent.includes('Qwen'));
    input('preset-provider', 'deepseek', 'change');
    await until('DeepSeek preset restored', () => byId('preset-api-key') && byId('preset-onboarding').textContent.includes('deepseek-flash'));
  }
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
  if (!live) {
    await click('preset-pick-project');
    await until('selected project', () => byId('preset-selected-project'));
  } else {
    check('standardOnboardingSkipsProjects', !byId('preset-pick-project'));
  }
  await click('preset-finish');
  await until('onboarding complete', () => (live ? byId('product-home') : byId('product-developer')) && !byId('preset-onboarding'));
  const state = await call('GetMVPState');
  if (!live) {
  const workspace = state.workspaces[0];
  check('onboardingAndWorkspacePersisted', state.onboarding.completed && state.workspaces.length === 1 && workspace.trustStatus === 'USER_SELECTED');
  const inspection = await call('InspectDeveloperProject', { workspaceId: workspace.id, exclusions: [] });
  check('autoDetectedGoProject', JSON.stringify(inspection.summary).includes('Go'));
  check('gitStatusAbsentIsExplicit', inspection.git.status === 'NOT_REPOSITORY');
    await click('nav-settings');
    const advanced = await until('advanced settings', () => byId('preset-advanced'));
    if (asimi) {
      checkBrand('settingsAboutBrand', document.querySelector('.about-brand h2')?.textContent);
      checkBrand('settingsDescriptionBrand', document.querySelector('.about-brand p')?.textContent);
    }
    check('releaseSettingsHideDemoCopy', !document.querySelector('.settings-page').textContent.includes('Mock'));
    check('advancedCollapsed', !advanced.open);
    const modelControl = await until('default model select', () => byId('settings-default-model'));
    const bounds = modelControl.getBoundingClientRect();
    const modeBounds = byId('interface-mode').getBoundingClientRect();
    check('settingsControlsAligned', modelControl.tagName === 'SELECT' &&
      Math.abs(bounds.left - modeBounds.left) < 1 && Math.abs(bounds.height - modeBounds.height) < 1);
    advanced.querySelector('summary').click();
    await until('limit form', () => byId('preset-limit-requests'));
    const sources = document.querySelector('.preset-provenance');
    sources.open = true;
    check('sourcesUsePairedRows', !!sources.querySelector('.detail-row > dt + dd'));
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
  check('workspaceSurvivesRestart', state.workspaces.length === (live ? 0 : 1));
  const settings = await call('GetSettings');
  check('languageSurvivesRestart', settings.appLanguage === (live ? 'en-US' : 'zh-CN'));
  check('modeSurvivesRestart', settings.interfaceMode === (live ? 'standard' : 'developer'));
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
    await click('nav-settings');
    input('interface-mode', 'standard', 'change');
    await until('standard menus', () => !byId('nav-developer') && !byId('nav-playground'));
    check('standardHidesAdvancedSettings', !byId('preset-advanced') && !!byId('settings-default-model') && !!byId('nav-computer'));
    await click('nav-skills');
    await until('standard skill', () => byId('skill-screen-analysis'));
    check('standardHidesProjectSkills', !byId('skill-project-overview'));
    await click('nav-settings');
    await until('mode setting ready', () => byId('interface-mode') && !byId('interface-mode').disabled);
    input('interface-mode', 'developer', 'change');
    await until('developer menus restored', () => byId('nav-developer') && byId('nav-playground'));
    await click('nav-home');
    await click('product-chat');
    await until('model setup entry', () => byId('chat-model-required'));
    check('releaseHasNoDemoControls', !byId('ai-assistantMode') && !byId('composer') && !document.querySelector('.runtime-info,.local-indicator'));
    check('releaseRejectsDemoRequests', (await rpc('SendMessage')).error?.code === 'METHOD_NOT_ALLOWED');
    check('noUsageWithoutModel', !(await call('GetUsage'))?.length);
    await click('nav-skills');
    const skill = await until('skill tile', () => byId('skill-project-overview'));
    check('skillsGridWithoutRawSchemas', !!skill.closest('.skill-grid') && !byId('product-skills').querySelector('pre, code'));
    await click('skill-project-overview');
    const skillDetail = await until('skill details', () => byId('skill-detail'));
    check('skillDetailsAreLocalized', skillDetail.textContent.includes('查看所选项目的目录结构和依赖。'));
    const enabled = skillDetail.querySelector('input[type=checkbox]');
    enabled.click();
    await until('skill disabled', () => !enabled.checked && !enabled.disabled);
    await click('product-skillBack');
    await until('updated skill card', () => byId('skill-project-overview')?.textContent.includes('已停用'));
    if (params.has('visual')) {
      await call('SaveAIProfile', { profile: {
        id: 'visual-fixture', name: 'Local fixture', protocol: 'chat-completions',
        baseUrl: 'http://127.0.0.1:1/v1', enabled: true, trustedHttp: false,
        models: [{ id: 'fixture-model', displayName: 'Fixture model',
          capabilities: {
            text: 'SUPPORTED', vision: 'SUPPORTED', streaming: 'SUPPORTED', toolCalling: 'UNSUPPORTED',
            structuredOutput: 'UNSUPPORTED', computerUse: 'UNSUPPORTED', temperature: 'UNSUPPORTED',
            contextLimit: 32768, imageDetails: ['auto'],
          } }],
      } });
      await click('nav-models');
      await until('provider page', () => byId('preset-provider'));
      await click('nav-settings');
      await until('default model fixture', () => [...byId('settings-default-model').options].some(option => option.textContent.includes('Fixture model')));
      await until('model control ready', () => !byId('settings-default-model').disabled);
      const fixtureValue = [...byId('settings-default-model').options].find(option => option.textContent.includes('Fixture model')).value;
      input('settings-default-model', fixtureValue, 'change');
      await until('default model saved from settings', async () => (await call('GetAISettings')).defaultModelId === 'fixture-model');
      await click('nav-models');
      await until('model refresh', () => byId('preset-default-model')?.textContent.includes('fixture-model'));
      await click('nav-assistant');
      await until('configured composer', () => byId('ai-composer'));
      check('sendLabel', byId('real-send').textContent.trim() === '发送');
      input('ai-composer', 'Keyboard fixture');
      const composer = byId('ai-composer');
      const previousConfirm = window.confirm;
      let prompts = 0;
      window.confirm = () => { prompts++; return false; };
      try {
        for (const modifiers of [{ shiftKey: true }, { isComposing: true }]) {
          const key = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true, ...modifiers });
          composer.dispatchEvent(key);
          check(modifiers.shiftKey ? 'shiftEnterKeepsDefault' : 'imeEnterKeepsDefault', !key.defaultPrevented);
        }
        check('draftRemainsBeforeSend', composer.value === 'Keyboard fixture');
        const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
        composer.dispatchEvent(enter);
        check('enterSubmits', enter.defaultPrevented);
        const usage = await until('keyboard request reaches backend', async () => (await call('GetUsage'))?.[0]);
        check('textSentWithoutPrompt', prompts === 0 && usage.modelId === 'fixture-model');
        await until('keyboard conversation loaded', () => document.querySelector('.message-user')?.textContent.includes('Keyboard fixture'));
        check('chatHidesRequestMetadata', !document.querySelector('.message-user')?.textContent.includes(usage.requestId));
      } finally {
        window.confirm = previousConfirm;
      }
      await click('nav-usage');
      await until('usage summary', () => byId('usage-totals'));
      check('usageDefaultsToCurrentWeek', byId('usage-period').value === 'thisWeek');
      const usagePage = byId('ai-usage');
      check('usageShowsProviderName', byId('usage-breakdown').textContent.includes('Local fixture') &&
        !usagePage.textContent.includes('visual-fixture'));
      check('usageSummaryKeepsUnknowns', byId('usage-requests').textContent === '1' &&
        byId('usage-totalTokens').textContent.includes('未知'));
      check('usageDiagnosticsLoadOnlyOnExpansion', !!byId('usage-diagnostics') && !byId('usage-request'));
      input('usage-period', 'lastMonth', 'change');
      await until('last month usage', () => byId('usage-requests')?.textContent === '0');
      input('usage-period', 'threeDays', 'change');
      await until('three days usage', () => byId('usage-requests')?.textContent === '1');
      byId('usage-diagnostics').open = true;
      await until('today request diagnostics', () => byId('usage-request'));
      await click('nav-settings');
      await until('standard mode control', () => byId('interface-mode') && !byId('interface-mode').disabled);
      input('interface-mode', 'standard', 'change');
      await until('standard mode set', () => !byId('nav-playground'));
      await click('nav-usage');
      await until('standard usage summary', () => byId('usage-totals'));
      check('standardUsageHasNoRequestDiagnostics', !byId('usage-diagnostics') && !byId('usage-request'));
      await click('nav-settings');
      await until('developer mode control', () => byId('interface-mode') && !byId('interface-mode').disabled);
      input('interface-mode', 'developer', 'change');
      await until('developer mode set', () => byId('nav-playground'));
      await click('nav-skills');
      await until('local skills editor',()=>byId('cognition-skill-editor'));
      byId('cognition-skill-editor').open=true;
      for(const [field,value] of Object.entries({
        slug:'local-review-fixture',name:'Local review fixture',description:'Review a local Go project',
        instructions:'Summarize the existing tests before proposing a change.',triggers:'Go review',
      })) input('cognition-skill-'+field,value);
      const saveSkill=await until('skill save enabled',()=>{
        const button=byId('cognition-skill-editor').querySelector('button');
        return button && !button.disabled && button;
      });
      saveSkill.click();
      const saved=await until('skill saved by UI',()=>document.querySelector('[data-skill-slug="local-review-fixture"]'));
      check('manualSkillSavedFromUI',saved.textContent.includes('Local review fixture'));
      await click('nav-memory');
      check('memoryPageAvailable',!!await until('memory page',()=>byId('cognition-memory')));
      await click('nav-assistant');
      await reviewVisual();
    }
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
