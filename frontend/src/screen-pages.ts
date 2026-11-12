import { computed, defineComponent, h, nextTick, onBeforeUnmount, onMounted, ref, Teleport, watch, type PropType, type VNodeChild } from 'vue';
import { ai, screenAnalysisBlocked, screenConversation, startScreenAnalysis } from './ai-store';
import { call, ServiceError } from './bridge';
import { ComputerControlPanel } from './control-panel';
import type { ComputerSuggestionTarget } from './control-types';
import { date, number, t } from './i18n';
import { openConversation, state } from './store';
import { button, detail, errorText, icon, iconButton } from './ui';
import type { CaptureCapabilities, ScreenDisplay, ScreenMetadata, ScreenPreview, ScreenRect, ScreenUploadReview, VisionResult } from './screen-types';
import { product } from './product-store';
import { computerTaskReview, computerTaskSelector } from './agent02-computer-fields';

type Entry = 'capture' | 'analyze';
type LocalPreview = Omit<ScreenPreview, 'previewDataUrl'> & { url: string };
const rectKeys = ['x', 'y', 'width', 'height'] as const;
const rectText = (rect: ScreenRect) => t('screen.rect', { ...rect });
const dimensions = (width: number, height: number) => t('screen.dimensions', { width, height });
const known = (value: string | undefined) => value || t('common.unknown');
const errorCode = (error: unknown) => error instanceof ServiceError ? error.code : 'app.unexpected';
const status = (text: string) => h('p', { role: 'status', class: 'muted' }, t(text));
const screenError = (code: string) => code ? h('p', { role: 'alert', class: 'ai-error' }, errorText(code)) : null;
const action = (key: string, testId: string, work: () => unknown, disabled = false, primary = false) =>
  button(key, work, { class: ['button', primary ? 'primary' : 'secondary'], disabled, 'data-testid': testId });

function selectField(key: string, id: string, value: string, options: [string, string][], onChange: (value: string) => void, disabled = false) {
  return h('label', { class: 'ai-field' }, [
    t(key), h('select', {
      value, disabled, 'data-testid': id,
      onChange: (event: Event) => onChange((event.target as HTMLSelectElement).value),
    }, options.map(([id, label]) => h('option', { value: id }, label))),
  ]);
}

function rectInputs(prefix: string, value: ScreenRect, limit: ScreenRect, changed: (rect: ScreenRect) => void, disabled: boolean) {
  return h('div', { class: 'screen-rect-fields' }, rectKeys.map(key => h('label', { class: 'ai-field' }, [
    t('screen.' + key), h('input', {
      type: 'number', value: Number.isNaN(value[key]) ? '' : value[key], step: 1,
      min: key === 'x' || key === 'y' ? 0 : 1, max: limit[key], disabled,
      'data-testid': `screen-${prefix}-${key}`,
      onInput: (event: Event) => changed({ ...value, [key]: (event.target as HTMLInputElement).valueAsNumber }),
    }),
  ])));
}

function within(rect: ScreenRect, image: { width: number; height: number }) {
  return rectKeys.every(key => Number.isSafeInteger(rect[key])) && rect.x >= 0 && rect.y >= 0 &&
    rect.width > 0 && rect.height > 0 && rect.x + rect.width <= image.width && rect.y + rect.height <= image.height;
}

function discardCapture(captureId: string) {
  // A consumed or expired capture may already be gone from the native cache.
  void call('DiscardScreenCapture', { captureId }).catch(() => {});
}

export function ScreenCaptureLabel(capture: ScreenMetadata) {
  return h('p', { class: 'screen-capture-label', 'data-testid': 'screen-message-metadata' }, [
    icon('computer', 15),
    t('screen.metadataLabel', { display: capture.displayName, size: dimensions(capture.width, capture.height) }),
  ]);
}

export function ScreenResult(result: VisionResult | undefined, text: string, pending: boolean) {
  if (pending) return status('screen.analyzing');
  if (!result?.parsed) return h('div', { class: 'screen-result', 'data-testid': 'screen-result' }, [
    h('p', { class: 'micro muted' }, t('screen.unstructured')),
    h('p', { class: 'screen-plain-text' }, text),
  ]);
  function resultList(key: string, values?: string[]) {
    return values?.length ? h('section', [h('h4', t('screen.' + key)), h('ul', values.map(value => h('li', value)))]) : null;
  }
  return h('div', { class: 'screen-result', 'data-testid': 'screen-result' }, [
    result.summary ? h('p', { class: 'screen-plain-text' }, result.summary) : null,
    resultList('ocr', result.detectedText),
    resultList('applications', result.applications),
    resultList('windows', result.windows),
    result.uiElements?.length ? h('section', [
      h('h4', t('screen.uiElements')),
      h('p', { class: 'micro muted' }, t('screen.normalized')),
      h('ul', result.uiElements.map(element => h('li', [
        h('strong', element.label || element.type),
        element.label && element.type ? h('span', { class: 'micro muted' }, ` · ${element.type}`) : null,
        element.description ? h('p', element.description) : null,
        element.bounds || element.confidence !== undefined ? h('dl', [
          element.bounds ? detail('screen.bounds', rectText(element.bounds)) : null,
          element.confidence !== undefined ? detail('screen.confidence', t('screen.percent', { value: number(Math.round(element.confidence * 100)) })) : null,
        ]) : null,
      ]))),
    ]) : null,
    resultList('issues', result.issues),
    resultList('suggestions', result.suggestions),
    resultList('uncertainties', result.uncertainties),
  ]);
}

const ScreenCaptureDialog = defineComponent({
  props: {
    entry: { type: String as PropType<Entry>, required: true },
    source: { type: String as PropType<'assistant' | 'computer'>, required: true },
    suggestion: Object as PropType<ComputerSuggestionTarget>,
  },
  emits: ['dismiss', 'suggestionSubmitted'],
  setup(props, { emit }) {
    const dialog = ref<HTMLDialogElement | null>(null);
    const reviewHeading = ref<HTMLElement | null>(null);
    const displays = ref<ScreenDisplay[]>([]);
    const displayId = ref('');
    const captureRegion = ref(false);
    const selectedRegion = ref<ScreenRect>({ x: 0, y: 0, width: 1, height: 1 });
    const selectedDisplay = computed(() => displays.value.find(display => display.id === displayId.value));
    const validCaptureRegion = computed(() => !captureRegion.value ||
      !!selectedDisplay.value && within(selectedRegion.value, selectedDisplay.value.physicalBounds));
    const confirmCapture = ref(false);
    const preview = ref<LocalPreview>();
    const imageReady = ref(false);
    const review = ref<ScreenUploadReview>();
    const busy = ref('');
    const error = ref('');
    const crop = ref<ScreenRect>({ x: 0, y: 0, width: 1, height: 1 });
    const redact = ref<ScreenRect>({ x: 0, y: 0, width: 1, height: 1 });
    const cropDirty = ref(false);
    const redactDirty = ref(false);
    const format = ref('png');
    const quality = ref(90);
    let currentQuality = 0;
    const session = ai.assistant;
    const initial = props.source === 'assistant' && props.entry === 'analyze' ?
      (ai.mode === 'real' ? session.draft : state.draft).trim() : '';
    const question = ref(initial);
    const taskId = ref(props.suggestion?.taskId || '');
    const providers = computed(() => ai.profiles.filter(p => p.enabled && p.models.some(m =>
      m.capabilities.text === 'SUPPORTED' && m.capabilities.vision === 'SUPPORTED')));
    const providerId = ref(providers.value.some(p => p.id === session.providerId) ? session.providerId :
      providers.value.find(p => p.id === ai.settings.visionProviderId)?.id || providers.value[0]?.id || '');
    const provider = computed(() => providers.value.find(p => p.id === providerId.value));
    const missingCredential = computed(() => {
      const p = provider.value;
      return !!p && !p.hasCredential && new URL(p.baseUrl).hostname === 'api.deepseek.com';
    });
    const models = computed(() => (provider.value?.models || []).filter(m =>
      m.capabilities.text === 'SUPPORTED' && m.capabilities.vision === 'SUPPORTED'));
    const modelId = ref(models.value.find(m => m.id === session.modelId && providerId.value === session.providerId)?.id ||
      models.value.find(m => m.id === ai.settings.visionModelId && providerId.value === ai.settings.visionProviderId)?.id ||
      models.value[0]?.id || '');
    const model = computed(() => models.value.find(m => m.id === modelId.value));
    const details = computed(() => model.value?.capabilities.imageDetails?.length ? model.value.capabilities.imageDetails : ['auto']);
    const imageDetail = ref(details.value.includes(session.imageDetail) ? session.imageDetail : details.value[0]);
    const desiredQuality = computed(() => format.value === 'jpeg' ? quality.value : 0);
    const validQuality = computed(() => format.value === 'png' || (Number.isInteger(quality.value) && quality.value >= 60 && quality.value <= 100));
    const previousProvider = computed(() => screenConversation()?.messages.filter(m => m.providerId).at(-1)?.providerId);
    const providerSwitch = computed(() => !!previousProvider.value && previousProvider.value !== providerId.value);
    let active = true;
    let version = 0;
    let captureExpiry: ReturnType<typeof setTimeout> | undefined;
    let reviewExpiry: ReturnType<typeof setTimeout> | undefined;

    function invalidateReview() {
      version++;
      clearTimeout(reviewExpiry);
      review.value = undefined;
    }

    function clearPreview(discard = true) {
      invalidateReview();
      clearTimeout(captureExpiry);
      imageReady.value = false;
      if (!preview.value) return;
      URL.revokeObjectURL(preview.value.url);
      if (discard) discardCapture(preview.value.captureId);
      preview.value = undefined;
    }

    function dismiss() {
      active = false;
      clearPreview();
      emit('dismiss');
    }

    function acceptPreview(next: ScreenPreview, encodingQuality: number) {
      if (!active) {
        discardCapture(next.captureId);
        return;
      }
      clearPreview(false);
      try {
        const { previewDataUrl, ...metadata } = next;
        const image = /^data:(image\/(?:png|jpeg));base64,([A-Za-z0-9+/=]+)$/.exec(previewDataUrl);
        if (!image || !Number.isFinite(Date.parse(next.expiresAt))) throw new Error();
        const bytes = Uint8Array.from(atob(image[2]), char => char.charCodeAt(0));
        if (bytes.byteLength !== metadata.byteSize) throw new Error();
        preview.value = { ...metadata, url: URL.createObjectURL(new Blob([bytes], { type: image[1] })) };
        crop.value = { x: 0, y: 0, width: next.width, height: next.height };
        redact.value = { x: 0, y: 0, width: 1, height: 1 };
        cropDirty.value = false;
        redactDirty.value = false;
        currentQuality = encodingQuality;
        format.value = encodingQuality ? 'jpeg' : 'png';
        captureExpiry = setTimeout(() => {
          clearPreview();
          error.value = 'SCREEN_CAPTURE_EXPIRED';
        }, Math.max(0, Date.parse(next.expiresAt) - Date.now()));
      } catch {
        discardCapture(next.captureId);
        error.value = 'SCREEN_CAPTURE_FAILED';
      }
    }

    async function work(task: string, operation: () => Promise<void>) {
      if (!active || busy.value) return;
      busy.value = task;
      error.value = '';
      try { await operation(); }
      catch (problem) { if (active) error.value = errorCode(problem); }
      finally { if (active) busy.value = ''; }
    }

    function loadDisplays() {
      return work('loadingDisplays', async () => {
        const listed = await call<ScreenDisplay[]>('ListDisplays');
        if (!active) return;
        displays.value = listed || [];
        displayId.value = displays.value.find(display => display.primary)?.id || displays.value[0]?.id || '';
        resetSelection();
      });
    }

    function resetSelection() {
      const display = selectedDisplay.value;
      captureRegion.value = false;
      if (display) selectedRegion.value = { x: 0, y: 0, width: display.physicalBounds.width, height: display.physicalBounds.height };
      confirmCapture.value = false;
    }

    function capture() {
      if (!confirmCapture.value || !displayId.value || !validCaptureRegion.value) return;
      const id = displayId.value;
      const region = captureRegion.value ? selectedRegion.value : undefined;
      confirmCapture.value = false;
      return work('capturing', async () => {
        clearPreview();
        acceptPreview(await call<ScreenPreview>('CaptureScreen', { displayId: id, region, confirmCapture: true }), 0);
      });
    }

    function transform(edit: 'crop' | 'redact' | 'encode') {
      const current = preview.value;
      if (!current || !validQuality.value) return;
      const region = edit === 'crop' ? crop.value : undefined;
      const redactions = edit === 'redact' ? [redact.value] : undefined;
      if (region && !within(region, current) || redactions?.some(rect => !within(rect, current))) return;
      const encodingQuality = desiredQuality.value;
      return work('processing', async () => {
        invalidateReview();
        const edited = await call<ScreenPreview>('TransformScreenCapture', {
          captureId: current.captureId, region, redactions, quality: encodingQuality,
        });
        if (preview.value?.captureId !== current.captureId) {
          discardCapture(edited.captureId);
          return;
        }
        acceptPreview(edited, encodingQuality);
      });
    }

    const canPrepare = computed(() => preview.value && imageReady.value && !busy.value && !cropDirty.value &&
      !redactDirty.value && validQuality.value && desiredQuality.value === currentQuality && !screenAnalysisBlocked() &&
      !missingCredential.value && !!model.value && (!props.suggestion || !!question.value.trim()));

    function prepare() {
      const shown = preview.value;
      if (!shown || !canPrepare.value) return;
      return work('preparing', async () => {
        const snapshot = version;
        const result = await call<ScreenUploadReview>('PrepareScreenUpload', {
          captureId: shown.captureId, providerId: providerId.value, modelId: modelId.value,
          imageDetail: imageDetail.value, conversationId: screenConversation()?.id, taskId: taskId.value || undefined,
          controlObservationId: props.suggestion?.observationId, controlElementId: props.suggestion?.elementId,
        });
        if (!active || snapshot !== version) return;
        if (result.sha256 !== shown.sha256 || result.bytes !== shown.byteSize || result.modelId !== modelId.value || result.taskId !== (taskId.value || undefined) ||
            result.suggestionSelector !== props.suggestion?.selector || !result.apiHost || !Number.isFinite(Date.parse(result.expiresAt))) {
          error.value = 'SCREEN_UPLOAD_TARGET_CHANGED';
          return;
        }
        review.value = result;
        reviewExpiry = setTimeout(invalidateReview, Math.max(0, Date.parse(result.expiresAt) - Date.now()));
        await nextTick();
        reviewHeading.value?.focus();
      });
    }

    function send() {
      const approved = review.value;
      if (!approved || !canPrepare.value || Date.parse(approved.expiresAt) <= Date.now()) return;
      return work('sending', async () => {
        const admission = startScreenAnalysis({
          consentId: approved.consentId, content: question.value.trim(), providerId: providerId.value,
          modelId: modelId.value, imageDetail: imageDetail.value, conversationId: screenConversation()?.id,
          confirmProviderSwitch: providerSwitch.value, stream: model.value?.capabilities.streaming === 'SUPPORTED',
          keepComputerOpen: !!props.suggestion,
        });
        invalidateReview();
        const admitted = await admission;
        if (admitted && props.suggestion) emit('suggestionSubmitted');
        clearPreview();
        if (!active) return;
        if (admitted) dismiss();
        else error.value = session.error || 'SCREEN_UPLOAD_REVIEW_REQUIRED';
      });
    }

    watch(() => state.conversation?.id, dismiss, { flush: 'sync' });
    watch(() => state.connected, connected => { if (!connected) dismiss(); }, { flush: 'sync' });
    watch([providerId, modelId, imageDetail, question, format, quality, provider, model, taskId], invalidateReview, { flush: 'sync' });
    watch(() => product.data.agentTasks, tasks => {
      if (!tasks.some(task => task.id === taskId.value)) taskId.value = '';
    });
    onMounted(() => {
      dialog.value?.showModal();
      void loadDisplays();
    });
    onBeforeUnmount(() => {
      active = false;
      clearPreview();
    });

    function edits(shown: LocalPreview, disabled: boolean) {
      const limits = { x: shown.width - 1, y: shown.height - 1, width: shown.width, height: shown.height };
      return h('div', { class: 'screen-edit' }, [
        h('details', [
          h('summary', t('screen.crop')),
          h('p', { class: 'micro muted' }, t('screen.regionHelp')),
          rectInputs('crop', crop.value, limits, value => { crop.value = value; cropDirty.value = true; invalidateReview(); }, disabled),
          action('screen.applyCrop', 'screen-apply-crop', () => transform('crop'), disabled || redactDirty.value || !within(crop.value, shown) || !validQuality.value),
        ]),
        h('details', [
          h('summary', t('screen.redaction')),
          h('p', { class: 'micro muted' }, t('screen.redactionHelp')),
          rectInputs('redact', redact.value, limits, value => { redact.value = value; redactDirty.value = true; invalidateReview(); }, disabled),
          action('screen.applyRedaction', 'screen-apply-redaction', () => transform('redact'), disabled || cropDirty.value || !within(redact.value, shown) || !validQuality.value),
        ]),
        h('div', { class: 'ai-row screen-encoding' }, [
          selectField('screen.format', 'screen-format', format.value, [['png', t('screen.png')], ['jpeg', t('screen.jpeg')]], value => format.value = value, disabled),
          format.value === 'jpeg' ? h('label', { class: 'ai-field' }, [
            t('screen.quality'), h('input', {
              type: 'number', min: 60, max: 100, step: 1, value: Number.isNaN(quality.value) ? '' : quality.value, disabled,
              'data-testid': 'screen-quality', onInput: (event: Event) => quality.value = (event.target as HTMLInputElement).valueAsNumber,
            }),
          ]) : null,
          action('screen.applyFormat', 'screen-apply-format', () => transform('encode'), disabled || cropDirty.value || redactDirty.value || !validQuality.value || desiredQuality.value === currentQuality),
          cropDirty.value || redactDirty.value ? action('screen.resetRegions', 'screen-reset-regions', () => {
            crop.value = { x: 0, y: 0, width: shown.width, height: shown.height };
            redact.value = { x: 0, y: 0, width: 1, height: 1 };
            cropDirty.value = false;
            redactDirty.value = false;
          }, disabled) : null,
        ]),
      ]);
    }

    function upload(disabled: boolean) {
      const reviewed = review.value;
      const pricing = model.value?.pricing;
      const outputCost = pricing ? session.maxOutputTokens * pricing.outputPerMillion / 1_000_000 : null;
      return h('section', { class: 'screen-upload' }, [
        h('h3', t('screen.analysis')),
        props.suggestion ? h('p', { class: 'muted' }, t('computerSuggestion.reviewTarget', { selector: props.suggestion.selector })) : null,
        h('label', { class: 'ai-field' }, [
          t('screen.question'), h('textarea', {
            rows: 2, maxlength: 4096, value: question.value, placeholder: t('screen.questionExample'),
            'data-testid': 'screen-question', disabled,
            onInput: (event: Event) => question.value = (event.target as HTMLTextAreaElement).value,
          }),
        ]),
        h('div', { class: 'ai-form-grid' }, [
          props.suggestion ? null : computerTaskSelector(product.data.agentTasks, taskId.value, value => { taskId.value = value; }, disabled, 'screen-task-link'),
          selectField('ai.provider', 'screen-provider', providerId.value, [['', t('ai.selectProvider')], ...providers.value.map(p => [p.id, p.name] as [string, string])], value => {
            providerId.value = value; modelId.value = models.value[0]?.id || ''; imageDetail.value = details.value[0];
          }, disabled),
          selectField('ai.model', 'screen-model', modelId.value, models.value.map(m => [m.id, m.displayName || m.id]), value => {
            modelId.value = value; imageDetail.value = details.value[0];
          }, disabled),
          selectField('ai.imageDetail', 'screen-detail', imageDetail.value, details.value.map(value => [value, t('ai.detail' + value)]), value => imageDetail.value = value, disabled),
        ]),
        !providers.value.length ? status('screen.noVisionProvider') : missingCredential.value ? status('screen.credentialRequired') : null,
        screenAnalysisBlocked() && busy.value !== 'sending' ? status('screen.assistantBusy') : null,
        !screenConversation() ? h('p', { class: 'micro muted' }, t('screen.newConversation')) : null,
        h('p', { class: 'micro muted' }, t('screen.imageCost')),
        outputCost !== null && pricing ? h('p', { class: 'micro muted' }, t('ai.outputEstimate', { cost: outputCost.toFixed(6), currency: pricing.currency })) : null,
        outputCost !== null && ai.settings.warningThreshold !== null && outputCost > ai.settings.warningThreshold ?
          h('p', { class: 'micro' }, t('ai.thresholdExceeded')) : null,
        h('p', { class: 'micro muted' }, t('ai.thresholdNotice', { threshold: ai.settings.warningThreshold ?? t('common.unknown') })),
        reviewed ? h('section', { class: 'screen-review', 'data-testid': 'screen-upload-review' }, [
          h('h4', { ref: reviewHeading, tabindex: -1 }, t('screen.uploadReview')),
          h('p', { class: 'micro muted' }, t('screen.sendNotice')),
          h('dl', [
            computerTaskReview(product.data.agentTasks, reviewed.taskId),
            reviewed.suggestionSelector ? detail('computerSuggestion.selectedControl', reviewed.suggestionSelector, true) : null,
            detail('ai.provider', reviewed.providerName),
            detail('ai.model', reviewed.modelId, true),
            detail('screen.apiHost', reviewed.apiHost, true),
            detail('screen.byteSize', t('screen.bytes', { size: number(reviewed.bytes) })),
            detail('screen.sha256', reviewed.sha256, true),
            detail('screen.expires', date(reviewed.expiresAt)),
          ]),
          providerSwitch.value ? h('p', { class: 'micro' }, t('screen.providerSwitch')) : null,
          action('screen.send', 'screen-send', send, !canPrepare.value, true),
        ]) : action('screen.prepare', 'screen-prepare', prepare, !canPrepare.value, true),
      ]);
    }

    return () => {
      const shown = preview.value;
      const disabled = !!busy.value || !state.connected;
      const selected = selectedDisplay.value;
      const bounds = selected?.physicalBounds;
      return h('dialog', {
        ref: dialog, class: 'screen-dialog', 'data-testid': 'screen-dialog', 'aria-labelledby': 'screen-dialog-title',
        onCancel: (event: Event) => { event.preventDefault(); dismiss(); },
      }, [
        h('header', { class: 'screen-dialog-header' }, [
          h('h2', { id: 'screen-dialog-title' }, t(props.suggestion ? 'computerSuggestion.capture' : props.entry === 'analyze' ? 'screen.analyze' : 'screen.capture')),
          iconButton('close', 'screen.close', dismiss, { 'data-testid': 'screen-close' }),
        ]),
        h('div', { class: 'screen-dialog-body' }, [
          h('p', { class: 'screen-privacy', 'data-testid': 'screen-privacy' }, [icon('lock', 16), t('screen.privacyWarning')]),
          h('div', { class: 'screen-selection' }, [
            selectField('screen.display', 'screen-display', displayId.value, displays.value.map(display =>
              [display.id, displayLabel(display)]), value => {
              displayId.value = value; resetSelection(); clearPreview();
            }, disabled || !displays.value.length),
            selected ? h('p', { class: 'micro muted' }, t('screen.displayGeometry', {
              physical: rectText(selected.physicalBounds), logical: rectText(selected.logicalBounds), dpi: selected.dpi, scale: selected.scale,
            })) : null,
            !displays.value.length && !busy.value ? h('p', { class: 'muted' }, t('screen.noDisplays')) : null,
            bounds ? h('label', { class: 'ai-checkbox' }, [h('input', {
              type: 'checkbox', checked: captureRegion.value, disabled,
              'data-testid': 'screen-capture-region',
              onChange: (event: Event) => {
                captureRegion.value = (event.target as HTMLInputElement).checked;
                confirmCapture.value = false;
              },
            }), t('screen.captureRegion')]) : null,
            captureRegion.value && bounds ? h('div', [
              h('p', { class: 'micro muted' }, t('screen.captureRegionHelp')),
              rectInputs('region', selectedRegion.value, {
                x: bounds.width - 1, y: bounds.height - 1, width: bounds.width, height: bounds.height,
              }, value => { selectedRegion.value = value; confirmCapture.value = false; }, disabled),
            ]) : null,
            h('label', { class: 'ai-checkbox' }, [h('input', {
              type: 'checkbox', checked: confirmCapture.value, disabled: disabled || !selected || !validCaptureRegion.value,
              'data-testid': 'screen-capture-confirm',
              onChange: (event: Event) => confirmCapture.value = (event.target as HTMLInputElement).checked,
            }), t('screen.confirmCapture')]),
            h('div', { class: 'ai-row' }, [
              action('screen.capture', 'screen-capture', capture, disabled || !confirmCapture.value || !validCaptureRegion.value, !shown),
              !displays.value.length ? action('screen.refresh', 'screen-refresh-displays', loadDisplays, disabled) : null,
              shown ? action('screen.discard', 'screen-discard', () => clearPreview(), disabled) : null,
            ]),
          ]),
          busy.value ? status('screen.' + busy.value) : null,
          screenError(error.value),
          shown ? h('section', { class: 'screen-preview' }, [
            h('h3', t('screen.preview')),
            h('figure', [
              h('img', {
                key: shown.captureId, src: shown.url, alt: t('screen.previewAlt', { display: shown.displayName }),
                'data-testid': 'screen-preview', draggable: false,
                onLoad: () => { if (preview.value?.captureId === shown.captureId) imageReady.value = true; },
                onError: () => {
                  if (preview.value?.captureId === shown.captureId) { clearPreview(); error.value = 'SCREEN_CAPTURE_FAILED'; }
                },
              }),
              h('figcaption', t('screen.previewMeta', {
                size: dimensions(shown.width, shown.height), format: shown.format.toUpperCase(), bytes: number(shown.byteSize),
                backend: shown.backend, dpi: shown.dpi,
              })),
            ]),
            h('details', [
              h('summary', t('screen.metadata')),
              h('dl', [
                detail('screen.captureId', shown.captureId, true),
                detail('screen.display', shown.displayName),
                detail('screen.physical', rectText(shown.physicalBounds)),
                detail('screen.logical', rectText(shown.logicalBounds)),
                detail('screen.capturedAt', date(shown.capturedAt)),
                detail('screen.expires', date(shown.expiresAt)),
                detail('screen.sha256', shown.sha256, true),
              ]),
            ]),
            edits(shown, disabled),
            upload(disabled),
          ]) : null,
        ]),
      ]);
    };
  },
});

function displayLabel(display: ScreenDisplay) {
  const size = dimensions(display.physicalBounds.width, display.physicalBounds.height);
  return t(display.primary ? 'screen.primaryDisplay' : 'screen.displayName', { name: display.name, size });
}

export const ScreenCaptureActions = defineComponent({
  props: { source: { type: String as PropType<'assistant' | 'computer'>, required: true }, disabled: Boolean },
  setup(props) {
    const entry = ref<Entry>();
    return () => h('div', { class: 'ai-row screen-actions' }, [
      action('screen.capture', props.source + '-capture', () => entry.value = 'capture', !state.connected || props.disabled),
      action('screen.analyze', props.source + '-analyze', () => entry.value = 'analyze', !state.connected || props.disabled, props.source === 'computer'),
      entry.value ? h(Teleport, { to: 'body' }, [
        h(ScreenCaptureDialog, { entry: entry.value, source: props.source, onDismiss: () => entry.value = undefined }),
      ]) : null,
    ]);
  },
});

export const ComputerPage = defineComponent({
  setup() {
    const displays = ref<ScreenDisplay[]>([]);
    const capabilities = ref<CaptureCapabilities>();
    const history = ref<ScreenMetadata[]>([]);
    const pending = ref(false);
    const error = ref('');
    const suggestion = ref<ComputerSuggestionTarget>();
    const suggestionRevision = ref(0);
    let active = true;
    async function refresh() {
      if (pending.value || !state.connected) return;
      pending.value = true;
      error.value = '';
      const [displayReply, capabilitiesReply, historyReply] = await Promise.allSettled([
        call<ScreenDisplay[]>('ListDisplays'),
        call<CaptureCapabilities>('GetCaptureCapabilities'),
        call<ScreenMetadata[]>('ListScreenHistory'),
      ]);
      if (!active) return;
      if (displayReply.status === 'fulfilled') displays.value = displayReply.value || [];
      else error.value = errorCode(displayReply.reason);
      if (capabilitiesReply.status === 'fulfilled') capabilities.value = capabilitiesReply.value;
      else error.value ||= errorCode(capabilitiesReply.reason);
      if (historyReply.status === 'fulfilled') history.value = historyReply.value || [];
      else error.value ||= errorCode(historyReply.reason);
      pending.value = false;
    }
    onMounted(refresh);
    onBeforeUnmount(() => { active = false; });
    const capability = (available: boolean) => t(available ? 'screen.available' : 'common.unavailable');
    const card = (key: string, body: VNodeChild, id?: string) =>
      h('section', { class: 'screen-card', 'data-testid': id }, [h('h2', t('screen.' + key)), body]);
    return () => {
      const c = capabilities.value;
      return h('div', { class: 'page-scroll' }, h('div', { class: 'page computer-page', 'data-testid': 'computer-page' }, [
        h('header', { class: 'page-heading' }, [
          h('div', [h('h1', t('navigation.computer')), h('p', t('screen.subtitle'))]),
          action('screen.refresh', 'computer-refresh', refresh, pending.value || !state.connected),
        ]),
        screenError(error.value),
        pending.value ? status('screen.loadingDisplays') : null,
        h(ComputerControlPanel, {
          tasks: product.data.agentTasks, suggestionRevision: suggestionRevision.value,
          onRequestSuggestion: target => { suggestion.value = target; },
        }),
        suggestion.value ? h(Teleport, { to: 'body' }, [h(ScreenCaptureDialog, {
          entry: 'analyze', source: 'computer', suggestion: suggestion.value,
          onSuggestionSubmitted: () => { suggestionRevision.value++; },
          onDismiss: () => { suggestion.value = undefined; },
        })]) : null,
        h('div', { class: 'screen-observation' }, [
          h('strong', [icon('computer', 20), t('screen.observationOnly')]),
          h('span', t('screen.captureDescription')),
        ]),
        h('div', { class: 'screen-page-grid' }, [
          card('capture', [h('p', t('screen.captureDescription')), h(ScreenCaptureActions, { source: 'computer', disabled: pending.value })]),
          card('analysis', h('p', t('screen.analysisDescription'))),
          card('displayList', displays.value.length ? h('ul', { class: 'screen-display-list' }, displays.value.map(display =>
            h('li', { key: display.id }, [
              h('strong', displayLabel(display)),
              h('p', { class: 'micro muted' }, t('screen.displayGeometry', {
                physical: rectText(display.physicalBounds), logical: rectText(display.logicalBounds), dpi: display.dpi, scale: display.scale,
              })),
            ]))) : pending.value ? null : h('p', { class: 'muted' }, t('screen.noDisplays')), 'computer-display-list'),
          card('privacy', h('p', t('screen.privacyStatus')), 'computer-privacy'),
          card('capabilities', c ? h('dl', [
            detail('screen.backend', known(c.backend)),
            detail('screen.display', capability(c.display)),
            detail('screen.crop', capability(c.region)),
            detail('screen.singleWindow', capability(c.window)),
            detail('screen.maxCapture', t('screen.limits', { pixels: number(c.maxPixels), dimension: number(c.maxDimension), bytes: number(c.maxBytes) })),
            detail('screen.ttl', t('screen.seconds', { seconds: number(c.ttlSeconds) })),
          ]) : null, 'computer-capabilities'),
          card('history', [
            h('p', { class: 'muted' }, t('screen.historyDescription')),
            history.value.length ? h('ul', { class: 'screen-history' }, history.value.map(record =>
              h('li', { key: record.captureId }, [
                h('strong', record.displayName),
                h('p', { class: 'micro muted' }, t('screen.historyRecord', {
                  size: dimensions(record.width, record.height), format: record.format.toUpperCase(), date: date(record.capturedAt),
                  provider: record.providerId, model: record.modelId,
                })),
                button('screen.openConversation', () => openConversation(record.conversationId), { class: 'text-button' }),
              ]))) : pending.value ? null : h('p', { class: 'muted' }, t('screen.emptyHistory')),
          ], 'screen-capture-history'),
        ]),
      ]));
    };
  },
});
