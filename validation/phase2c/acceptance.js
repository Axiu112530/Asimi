// Runs inside the app's own WebView for the explicitly selected Stage 7.
// Reports contain assertions and metadata only. Synthetic pixels stay in memory.
import { Call, Events, Window } from '/wails/runtime.js';

const checks = {};
const details = {};
const events = [];
const providerId = 'fixture-screen';
const modelId = 'fixture-screen-model';
const question = 'Describe the synthetic fixture after redaction.';
const followup = 'Summarize that observation without sending another image.';
const unsub = ['started', 'delta', 'completed', 'failed'].map(kind => Events.On('assistant.stream.' + kind, event => {
  const value = Array.isArray(event.data) ? event.data[0] : event.data;
  events.push({kind, requestId: value?.requestId});
}));
const byID = id => document.querySelector(`[data-testid="${id}"]`);
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

function check(name, condition) {
  checks[name] = Boolean(condition);
  if (!condition) throw new Error(name);
}

async function until(name, read, timeout = 5500) {
  const deadline = Date.now() + timeout;
  do {
    const value = await read();
    if (value) return value;
    await sleep(40);
  } while (Date.now() < deadline);
  throw new Error(`timeout: ${name}`);
}

const rpc = (method, payload = {}) => Call.ByName('main.DesktopService.Call', method, JSON.stringify(payload));
async function call(method, payload) {
  const reply = await rpc(method, payload);
  if (reply.error) throw new Error(`${method}: ${reply.error.code}`);
  return reply.data;
}

async function click(id) {
  const node = await until(id, () => {
    const element = byID(id);
    return element && !element.disabled && element.getClientRects().length ? element : null;
  });
  node.click();
}

async function set(id, value, event = 'input') {
  const node = await until(id, () => byID(id));
  node.value = value;
  node.dispatchEvent(new Event(event, {bubbles: true}));
}

async function capture() {
  await until('capture dialog', () => byID('screen-dialog'));
  check('confirmationGatesCapture', byID('screen-capture').disabled);
  await click('screen-capture-confirm');
  await click('screen-capture');
  return preview(120, 80);
}

function preview(width, height, oldSrc) {
  return until(`preview ${width}x${height}`, () => {
    const image = byID('screen-preview');
    return image?.complete && image.naturalWidth === width && image.naturalHeight === height && image.src !== oldSrc ? image : null;
  });
}

function sample(image, x, y) {
  const canvas = document.createElement('canvas');
  canvas.width = 1;
  canvas.height = 1;
  const ctx = canvas.getContext('2d');
  ctx.drawImage(image, x, y, 1, 1, 0, 0, 1, 1);
  return [...ctx.getImageData(0, 0, 1, 1).data].join(',');
}

async function setRect(tool, x, y, width, height) {
  const input = await until(tool + ' controls', () => byID('screen-' + tool + '-x'));
  input.closest('details').open = true;
  for (const [name, value] of Object.entries({x, y, width, height})) await set(`screen-${tool}-${name}`, value);
  await click(tool === 'crop' ? 'screen-apply-crop' : 'screen-apply-redaction');
}

function layout(name) {
  const dialog = byID('screen-dialog');
  const bounds = dialog.getBoundingClientRect();
  check(name, bounds.width > 0 && bounds.height > 0 && bounds.left >= -1 && bounds.top >= -1 &&
    bounds.right <= innerWidth + 1 && bounds.bottom <= innerHeight + 1 &&
    dialog.scrollWidth <= dialog.clientWidth + 1 && document.documentElement.scrollWidth <= innerWidth + 1);
  details[name] = {viewportWidth: innerWidth, viewportHeight: innerHeight, dialogWidth: Math.round(bounds.width), dialogHeight: Math.round(bounds.height)};
}

async function run() {
  const baseUrl = new URL(new URLSearchParams(location.search).get('fixture'));
  check('loopbackProviderOnly', baseUrl.protocol === 'http:' && baseUrl.hostname === '127.0.0.1' && baseUrl.pathname === '/v1');
  await until('app ready', () => byID('composer'));
  const displays = await call('ListDisplays');
  check('syntheticMonitorOnly', displays.length === 1 && displays[0].id === 'phase2c-fixture' && displays[0].physicalBounds.x === -120);
  const displayId = displays[0].id;
  const capabilities = await call('GetCaptureCapabilities');
  check('captureWithoutInputControl', capabilities.backend === 'SYNTHETIC_FIXTURE' && capabilities.display &&
    capabilities.region && !capabilities.window && !capabilities.control);
  const noCapture = await rpc('CaptureScreen', {displayId});
  check('captureWithoutConfirmationRejected', noCapture.error?.code === 'SCREEN_CAPTURE_CONSENT_REQUIRED');
  const direct = await rpc('StartScreenAnalysis', {confirmSend: true, content: question});
  check('directSendWithoutReviewRejected', direct.error?.code === 'SCREEN_UPLOAD_REVIEW_REQUIRED');

  const caps = {text: 'SUPPORTED', vision: 'SUPPORTED', streaming: 'SUPPORTED', toolCalling: 'UNSUPPORTED',
    structuredOutput: 'UNKNOWN', computerUse: 'UNSUPPORTED', temperature: 'SUPPORTED', contextLimit: 32768, imageDetails: ['auto', 'low', 'high']};
  await call('SaveAIProfile', {profile: {id: providerId, name: 'Screen loopback fixture', protocol: 'chat-completions',
    baseUrl: baseUrl.href, enabled: true, trustedHttp: false, models: [{id: modelId, displayName: 'Fixture Vision', capabilities: caps}]}});
  await call('StoreCredential', {providerId, secret: 'fixture-secret'});
  const settings = await call('GetAISettings');
  await call('SaveAISettings', {...settings, defaultProviderId: providerId, defaultModelId: modelId, visionProviderId: providerId, visionModelId: modelId});
  // The fixture was configured over RPC after boot. Models refreshes the app's profile store.
  await click('nav-models');
  await until('fixture visible in Models', () => byID('ai-profile-' + providerId));

  const local = await call('CaptureScreen', {displayId, confirmCapture: true});
  const target = {providerId, modelId, imageDetail: 'auto'};
  const consent = await call('PrepareScreenUpload', {captureId: local.captureId, ...target});
  const declined = await rpc('StartScreenAnalysis', {consentId: consent.consentId, confirmSend: false, content: question});
  check('reviewAloneCannotSend', Boolean(declined.error?.code));
  const changed = await call('TransformScreenCapture', {captureId: local.captureId, region: {x: 1, y: 1, width: 80, height: 60}});
  const transformedConsent = await rpc('StartScreenAnalysis', {consentId: consent.consentId, confirmSend: true, content: question});
  check('cropInvalidatesReview', changed.captureId !== local.captureId && Boolean(transformedConsent.error?.code));
  await call('DiscardScreenCapture', {captureId: changed.captureId});
  const stale = await rpc('PrepareScreenUpload', {captureId: changed.captureId, ...target});
  check('discardedCaptureCannotBeSent', stale.error?.code === 'SCREEN_CAPTURE_EXPIRED');

  await click('nav-settings');
  await set('app-language', 'en-US', 'change');
  await until('English app', () => document.documentElement.lang === 'en-US');
  await set('theme', 'light', 'change');
  await until('light theme', () => document.documentElement.dataset.theme === 'light');
  await click('nav-computer');
  const privacy = await until('computer privacy notice', () => byID('computer-privacy'));
  check('computerPrivacyNotice', privacy.textContent.trim().length > 20);
  await click('computer-capture');
  let image = await capture();
  check('dialogPrivacyNotice', byID('screen-privacy')?.textContent.trim().length > 20);
  check('englishCaptureLabels', /screen|display/i.test(byID('screen-dialog').textContent));
  check('computerShowsTransientPixels', image.src.startsWith('blob:') && sample(image, 3, 3) === '28,112,184,255');
  layout('englishLightLayout');
  let previous = image.src;
  await set('screen-format', 'jpeg', 'change');
  await set('screen-quality', '83');
  await click('screen-apply-format');
  image = await preview(120, 80, previous);
  check('jpegRendersLocally', image.src.startsWith('blob:'));
  await click('screen-close');
  await until('preview dismissed', () => !byID('screen-dialog') && !byID('screen-preview'));
  check('previewOnlyDoesNotSend', (await call('GetUsage')).length === 0 && (await call('ListScreenHistory')).length === 0);

  await click('nav-settings');
  await set('app-language', 'zh-CN', 'change');
  await until('Chinese app', () => document.documentElement.lang === 'zh-CN');
  await set('theme', 'dark', 'change');
  await until('dark theme', () => document.documentElement.dataset.theme === 'dark');
  await Window.SetSize(900, 620);
  await until('compact native viewport', () => innerWidth <= 980 && innerHeight <= 700);
  await click('nav-assistant');
  check('mockHasExplicitScreenEntry', Boolean(byID('assistant-analyze')) && !byID('screen-preview'));
  await set('ai-assistantMode', 'real', 'change');
  await click('assistant-analyze');
  image = await capture();
  check('chineseCaptureLabels', /[\u3400-\u9fff]/.test(byID('screen-dialog').textContent));
  layout('chineseDarkCompactLayout');
  previous = image.src;
  await setRect('crop', 12, 10, 80, 60);
  image = await preview(80, 60, previous);
  check('cropPixelsTranslated', sample(image, 2, 2) === '28,112,184,255' && sample(image, 58, 42) === '221,61,75,255');
  previous = image.src;
  await setRect('redact', 5, 5, 18, 15);
  image = await preview(80, 60, previous);
  check('redactionPixelsAreOpaque', sample(image, 8, 8) === '0,0,0,255' && sample(image, 58, 42) === '221,61,75,255');
  await set('screen-question', question);
  await set('screen-provider', providerId, 'change');
  await set('screen-model', modelId, 'change');
  check('sendHiddenBeforeReview', !byID('screen-upload-review') && !byID('screen-send'));
  await click('screen-prepare');
  const review = await until('upload review', () => byID('screen-upload-review'));
  const fields = Array.from(review.querySelectorAll('dd'), node => node.textContent.trim());
  const approvedHash = fields.find(value => /^[a-f0-9]{64}$/.test(value));
  const approvedByteSize = Number(fields[3]?.replace(/[^\d]/g, ''));
  check('reviewShowsTargetAndBytes', fields.includes('Screen loopback fixture') &&
    fields.includes(modelId) && fields.includes(baseUrl.host) && !!approvedHash && approvedByteSize > 0);
  layout('uploadReviewCompactLayout');
  check('reviewDoesNotSend', (await call('GetUsage')).length === 0);
  await click('screen-send');

  const usage = await until('screen usage completed', async () => (await call('GetUsage')).find(row => row.status === 'COMPLETED'));
  check('providerUsageRetained', usage.usage?.totalTokens === 18 && usage.usage?.source === 'PROVIDER_REPORTED');
  await until('screen terminal event', () => events.some(event => event.kind === 'completed' && event.requestId === usage.requestId));
  const history = await call('ListScreenHistory');
  const captureMetadata = history.find(item => item.requestId === usage.requestId);
  check('historyStoresOnlyMetadata', history.length === 1 && captureMetadata?.width === 80 && captureMetadata?.height === 60 &&
    captureMetadata.providerId === providerId && captureMetadata.modelId === modelId &&
    captureMetadata.sha256 === approvedHash && captureMetadata.byteSize === approvedByteSize &&
    !/previewDataUrl|data:image\/|base64/i.test(JSON.stringify(history)));
  const conversation = await call('GetConversation', {id: captureMetadata.conversationId});
  const screened = conversation.messages.find(message => message.role === 'user' && message.content === question);
  const analysis = conversation.messages.find(message => message.role === 'assistant' && message.vision);
  check('messageMetadataPersisted', screened?.screenCapture?.sha256 === captureMetadata.sha256 && !screened?.attachments?.length);
  check('structuredVisionPersisted', analysis?.vision?.parsed && analysis.vision.summary === 'Synthetic fixture inspection' &&
    analysis.vision.uiElements?.[0]?.bounds?.width === 300);
  details.sentImage = {sha256: captureMetadata.sha256, byteSize: captureMetadata.byteSize, width: captureMetadata.width, height: captureMetadata.height};
  if (byID('screen-dialog')) await click('screen-close');
  const result = await until('structured result visible', () => byID('screen-result'));
  check('structuredResultUsesText', result.textContent.includes('Synthetic fixture inspection') &&
    result.textContent.includes('<b>literal fixture markup</b>') && !result.querySelector('b'));
  check('sentPreviewRemoved', !byID('screen-preview') && !result.querySelector('img'));

  await set('ai-composer', followup);
  const originalConfirm = window.confirm;
  window.confirm = () => true; // Native text-only confirmation, outside the screen dialog.
  try {
    await click('real-send');
    await until('follow-up completed', async () => (await call('GetUsage')).filter(row => row.status === 'COMPLETED').length === 2);
  } finally { window.confirm = originalConfirm; }
  const continued = await call('GetConversation', {id: captureMetadata.conversationId});
  check('sameConversationTextFollowUp', continued.messages.some(message => message.role === 'user' && message.content === followup && !message.screenCapture) &&
    continued.messages.some(message => message.role === 'assistant' && message.content.includes('Fixture text-only follow-up')));
  check('noPreviewInPersistedConversation', !/previewDataUrl|data:image\/|base64/i.test(JSON.stringify(continued)));

  await click('nav-computer');
  const historyUI = await until('capture history UI', () => {
    const section = byID('screen-capture-history');
    return section?.textContent.includes(modelId) ? section : null;
  });
  check('historyShowsMetadataWithoutImage', historyUI.textContent.includes('80') && historyUI.textContent.includes('60') && !historyUI.querySelector('img'));
  check('screenFlowNoDesktopOperations', (await call('GetToolExecutions')).length === 0 && (await call('ListTasks')).length === 0);
  check('noWebViewProviderTraffic', !performance.getEntriesByType('resource').some(entry => entry.name.startsWith(baseUrl.origin + '/')));
}

let error = '';
try { await run(); }
catch (failure) {
  // Error labels come from test steps or allowlisted service codes. Never include request bodies.
  error = /^[A-Za-z0-9 :_+x-]{1,140}$/.test(failure?.message || '') ? failure.message : 'Unexpected acceptance failure';
}
finally { for (const off of unsub) off(); }
await Call.ByName('main.AcceptanceService.Report', JSON.stringify({stage: 7, passed: !error, error, checks, details}));
