import { computed, defineComponent, h, onBeforeUnmount, ref, watch, type VNodeChild } from 'vue';
import { call, ServiceError } from './bridge';
import type { ComputerSuggestion, ComputerSuggestionTarget, ControlAction, ControlApp, ControlElement, ControlObservation, ControlProposal, ControlRecord, ControlStatus, ControlWindow } from './control-types';
import { date, known, t } from './i18n';
import type { ScreenRect } from './screen-types';
import { state } from './store';
import { product, refreshProduct } from './product-store';
import { button, detail, errorText, icon } from './ui';
import type { AgentTask } from './product-types';
import { computerTaskReview, computerTaskSelector } from './agent02-computer-fields';

const targetApps: ControlApp[] = ['test_host', 'notepad'];
const actionNames: Record<ControlAction, string> = {
  'app.launch_test_host': 'launch',
  'window.focus': 'focus',
  'uia.invoke': 'invoke',
  'uia.set_value': 'setValue',
  'keyboard.type_text': 'typeText',
};
const label = (key: string) => t('control.' + key);
const actionName = (action: ControlAction) => label('actions.' + actionNames[action]);
const rect = (bounds: ScreenRect) => t('screen.rect', { ...bounds });
const missing = () => t('common.dash');
const codeOf = (error: unknown) => error instanceof ServiceError ? error.code : 'app.unexpected';
const bool = (value: boolean) => t(value ? 'common.yes' : 'common.no');
const field = (key: string, value: VNodeChild, mono = false) => detail('control.' + key, value, mono);
const actionButton = (key: string, testId: string, work: () => unknown, disabled = false, style = 'secondary', iconName?: string) =>
  button('control.' + key, work, { class: ['button', style], disabled, 'data-testid': testId },
    iconName ? [icon(iconName, 15), label(key)] : undefined);
const isoTime = (value: string) => h('time', { datetime: value, title: value }, date(value));
const loadText = (key: string) => h('p', { class: 'muted', role: 'status' }, label(key));

function windowFields(target: ControlWindow) {
  return [
    field('windowTitle', target.title || missing()),
    field('imagePath', target.imagePath || missing(), true),
    field('pid', String(target.pid), true),
    field('hwnd', target.hwnd, true),
    field('creationTime', target.creationTime, true),
    field('windowClass', target.className || missing(), true),
    field('session', String(target.sessionId), true),
    field('integrity', `0x${target.integrity.toString(16)}`, true),
    target.imageHash ? field('imageHash', target.imageHash, true) : null,
    target.packageFamily ? field('package', target.packageFamily, true) : null,
    target.appUserModelId ? field('appUserModelId', target.appUserModelId, true) : null,
    field('windowBounds', rect(target.bounds), true),
  ];
}

function elementFields(element: ControlElement) {
  return [
    field('controlName', element.name || missing()),
    field('automationId', element.automationId || missing(), true),
    field('controlType', element.controlType || missing(), true),
    field('elementBounds', rect(element.bounds), true),
    field('patterns', element.patterns?.join(', ') || missing(), true),
    field('enabled', bool(element.enabled)),
    field('offscreen', bool(element.offscreen)),
    field('keyboardFocus', bool(element.focused)),
    field('password', bool(element.password)),
    field('pid', String(element.processId), true),
    field('elementHandle', element.nativeHandle || missing(), true),
  ];
}

function supports(element: ControlElement, controlType: string, pattern: string) {
  return (element.controlType === controlType || element.controlType === 'ControlType.' + controlType) &&
    !!element.patterns?.some(value => value === pattern || value === pattern + 'Pattern');
}

type TreeNode = { element: ControlElement; children: TreeNode[] };
function treeNodes(elements: ControlElement[]) {
  const roots: TreeNode[] = [];
  const parents: TreeNode[] = [];
  for (const element of elements) {
    const node: TreeNode = { element, children: [] };
    while (parents.length && parents[parents.length - 1].element.depth >= element.depth) parents.pop();
    const siblings = parents[parents.length - 1]?.children || roots;
    siblings.push(node);
    parents.push(node);
  }
  return roots;
}

function uncertain(record: ControlRecord) {
  return ['UNKNOWN_RESULT', 'RECOVERY_REQUIRED', 'INPUT_PARTIAL'].includes(record.status);
}

export const ComputerControlPanel = defineComponent({
  props: { tasks: { type: Array<AgentTask>, default: () => [] }, suggestionRevision: Number },
  setup(props, { emit }) {
    const status = ref<ControlStatus>();
    const checking = ref(false);
    const modeChanging = ref(false);
    const busy = ref<'list' | 'inspect' | 'propose' | ''>('');
    const deciding = ref(false);
    const journalLoading = ref(false);
    const statusError = ref('');
    const error = ref('');
    const journalError = ref('');
    const windows = ref<ControlWindow[]>();
    const observed = ref<ControlObservation>();
    const selectedId = ref('');
    const taskId = ref('');
    const draft = ref('');
    const pending = ref<ControlProposal>();
    const lastResult = ref<ControlRecord>();
    const uncertainReply = ref(false);
    const approvalExpired = ref(false);
    const journal = ref<ControlRecord[]>([]);
    const suggestions = ref<ComputerSuggestion[]>([]);
    const suggestionError = ref('');
    const suggestionLoading = ref(false);
    let viewVersion = 0;
    let journalVersion = 0;
    let active = true;
    let expiryTimer: ReturnType<typeof setTimeout> | undefined;
    let suggestionVersion = 0;
    let suggestionTimer: ReturnType<typeof setTimeout> | undefined;

    const selected = computed(() => observed.value?.elements?.find(element => element.id === selectedId.value));
    const tree = computed(() => treeNodes(observed.value?.elements || []));
    const available = computed(() => state.connected && status.value?.available === true);
    const stopped = computed(() => state.controlStoppedLocally || status.value?.stopped === true);
    const permitted = computed(() => available.value && !stopped.value && !modeChanging.value && !checking.value &&
      status.value?.permissionMode === 'PER_ACTION_APPROVAL' && status.value.rawInput === false);
    const blocked = computed(() => !permitted.value || !!pending.value || !!busy.value || deciding.value);
    const canInspect = computed(() => available.value && !modeChanging.value && !busy.value && !deciding.value && !pending.value);
    watch(() => props.tasks, tasks => {
      if (!tasks.some(task => task.id === taskId.value)) taskId.value = '';
    });
    const canFocus = computed(() => !!observed.value && !observed.value.modal && observed.value.window.app === 'test_host');
    const eligible = computed(() => {
      const current = observed.value;
      const element = selected.value;
      if (!current || !element || current.modal || current.truncated || !targetApps.includes(current.window.app) ||
          element.processId !== current.window.pid || !element.enabled || element.offscreen || element.password) return;
      return element;
    });
    const canInvoke = computed(() => observed.value?.window.app === 'test_host' && !!eligible.value && ['SubmitButton', 'ResetButton'].includes(eligible.value.automationId) &&
      supports(eligible.value, 'Button', 'Invoke'));
    const notepadEditor = computed(() => {
      const current = observed.value;
      if (!current || current.modal || current.truncated || current.window.app !== 'notepad' ||
          current.window.className !== 'Notepad' || current.window.integrity !== 8192 ||
          current.window.packageFamily !== 'Microsoft.WindowsNotepad_8wekyb3d8bbwe' ||
          current.window.appUserModelId !== 'Microsoft.WindowsNotepad_8wekyb3d8bbwe!App') return;
      const editors = current.elements?.filter(element => ['Edit', 'Document', 'ControlType.Edit', 'ControlType.Document'].includes(element.controlType)) || [];
      const element = editors[0];
      if (editors.length !== 1 || !element || !['Document', 'ControlType.Document'].includes(element.controlType) || element.processId !== current.window.pid || !element.enabled ||
          element.offscreen || element.password || !element.blank || element.valueWritable !== true ||
          !element.runtimeId || !element.parentPath ||
          element.valueSha256 !== 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' ||
          !element.patterns?.includes('Value')) return;
      const inner = element.bounds, outer = current.window.bounds;
      if (inner.width <= 0 || inner.height <= 0 || inner.x < outer.x || inner.y < outer.y ||
          inner.x + inner.width > outer.x + outer.width || inner.y + inner.height > outer.y + outer.height) return;
      return element;
    });
    const canText = computed(() => !!eligible.value && (observed.value?.window.app === 'test_host'
      ? eligible.value.automationId === 'TestInput' && supports(eligible.value, 'Edit', 'Value')
      : !!notepadEditor.value && notepadEditor.value.id === eligible.value.id));
    const suggestionTarget = computed<ComputerSuggestionTarget | undefined>(() => {
      const observation = observed.value;
      const element = eligible.value;
      if (!taskId.value || !observation || observation.window.app !== 'test_host' ||
          !element || !(canInvoke.value || canText.value)) return;
      return { observationId: observation.id, elementId: element.id, selector: element.automationId, taskId: taskId.value };
    });
    const validText = computed(() => draft.value.length > 0 && [...draft.value].length <= 512 && !/[\p{Cc}\p{Cs}\u2028\u2029]/u.test(draft.value));
    const isCurrent = (version: number) => active && state.connected && version === viewVersion;

    function clearProposal() {
      clearTimeout(expiryTimer);
      pending.value = undefined;
      approvalExpired.value = false;
    }

    function showProposal(proposal: ControlProposal) {
      const ms = Date.parse(proposal.expiresAt) - Date.now();
      pending.value = proposal;
      approvalExpired.value = !Number.isFinite(ms) || ms <= 0;
      if (!approvalExpired.value) {
        expiryTimer = setTimeout(() => {
          approvalExpired.value = true;
          void refreshJournal();
        }, ms);
      }
      draft.value = '';
    }

    async function refreshSuggestions() {
      if (!active || !state.connected || !taskId.value || suggestionLoading.value) return;
      clearTimeout(suggestionTimer);
      const version = suggestionVersion;
      suggestionLoading.value = true;
      try {
        const result = await call<ComputerSuggestion[]>('ListComputerSuggestions', { taskId: taskId.value });
        if (active && version === suggestionVersion) {
          suggestions.value = result;
          suggestionError.value = '';
          if (result.some(item => ['PENDING', 'RUNNING', 'READY'].includes(item.status))) {
            suggestionTimer = setTimeout(refreshSuggestions, 2000);
          }
        }
      } catch (failure) {
        if (active && version === suggestionVersion) suggestionError.value = codeOf(failure);
      } finally {
        if (active && version === suggestionVersion) suggestionLoading.value = false;
      }
    }

    async function reviewSuggestion(item: ComputerSuggestion, asTask = false) {
      if (blocked.value || item.status !== 'READY') return;
      const version = viewVersion;
      const task = taskId.value;
      busy.value = 'propose';
      error.value = '';
      try {
        const proposal = await call<ControlProposal>('ProposeComputerSuggestion', { requestId: item.requestId, taskId: task, ...(asTask ? { asTask: true } : {}) });
        if (!isCurrent(version)) return;
        if (proposal.status !== 'AWAITING_APPROVAL' || proposal.sourceRequestId !== item.requestId ||
            (asTask ? !proposal.taskId || proposal.taskId === task : proposal.taskId !== task) || proposal.type !== item.actionType || proposal.window?.app !== 'test_host' ||
            proposal.element?.automationId !== item.selector || !proposal.id || !proposal.hash) {
          error.value = 'bridge.invalid_response';
          return;
        }
        showProposal(proposal);
        if (asTask) void refreshProduct().catch(() => undefined);
      } catch (failure) {
        if (isCurrent(version)) error.value = codeOf(failure);
      } finally {
        if (isCurrent(version)) busy.value = '';
        void refreshSuggestions();
        if (active) void refreshJournal();
      }
    }

    function clearObservation() {
      observed.value = undefined;
      selectedId.value = '';
      draft.value = '';
    }

    function invalidateView() {
      viewVersion++;
      busy.value = '';
      checking.value = false;
      clearProposal();
      clearObservation();
      windows.value = undefined;
    }

    async function refreshStatus() {
      if (!state.connected || checking.value) return;
      const version = viewVersion;
      checking.value = true;
      statusError.value = '';
      try {
        const next = await call<ControlStatus>('GetControlStatus');
        if (!isCurrent(version)) return;
        status.value = next;
        if (next.stopped) state.controlStoppedLocally = true;
        state.controlEscapeAvailable = next.nativeEscape === true;
        if (next.stopped || !next.available || next.permissionMode !== 'PER_ACTION_APPROVAL') {
          clearProposal();
          clearObservation();
          windows.value = undefined;
        }
      } catch (failure) {
        if (!isCurrent(version)) return;
        status.value = undefined;
        statusError.value = codeOf(failure);
        clearProposal();
      } finally {
        if (isCurrent(version)) checking.value = false;
      }
    }

    async function refreshJournal() {
      if (!state.connected) return;
      const version = ++journalVersion;
      journalLoading.value = true;
      journalError.value = '';
      try {
        const next = await call<ControlRecord[]>('ListControlJournal');
        if (active && version === journalVersion) journal.value = next || [];
      } catch (failure) {
        if (active && version === journalVersion) journalError.value = codeOf(failure);
      } finally {
        if (active && version === journalVersion) journalLoading.value = false;
      }
    }

    async function listTargets() {
      if (!canInspect.value) return;
      const version = viewVersion;
      busy.value = 'list';
      error.value = '';
      windows.value = undefined;
      clearObservation();
      try {
        const list = await call<ControlWindow[]>('ListControlWindows');
        if (isCurrent(version)) windows.value = (list || []).filter(target => targetApps.includes(target.app));
      } catch (failure) {
        if (isCurrent(version)) error.value = codeOf(failure);
      } finally {
        if (isCurrent(version)) busy.value = '';
      }
    }

    async function inspect(target: ControlWindow) {
      if (!canInspect.value) return;
      const version = viewVersion;
      busy.value = 'inspect';
      error.value = '';
      clearObservation();
      try {
        const tree = await call<ControlObservation>('InspectControlWindow', { windowId: target.id });
        if (isCurrent(version) && tree.window.app === target.app) observed.value = tree;
      } catch (failure) {
        if (isCurrent(version)) error.value = codeOf(failure);
      } finally {
        if (isCurrent(version)) busy.value = '';
      }
    }

    function choose(element: ControlElement) {
      if (!canInspect.value) return;
      if (selectedId.value !== element.id) draft.value = '';
      selectedId.value = element.id;
    }

    async function propose(type: ControlAction, asTask = false) {
      if (blocked.value) return;
      const current = observed.value;
      const params: { type: ControlAction; observationId?: string; elementId?: string; text?: string; taskId?: string } = { type };
      if (taskId.value && !asTask) params.taskId = taskId.value;
      if (type !== 'app.launch_test_host') {
        const expires = current && Date.parse(current.expiresAt || current.observedAt) + (current.expiresAt ? 0 : 60_000);
        if (!current || !expires || !Number.isFinite(expires) || expires <= Date.now()) {
          clearObservation();
          error.value = 'CONTROL_STALE_TARGET';
          return;
        }
        params.observationId = current.id;
      }
      if (type === 'window.focus' && !canFocus.value) return;
      if (type === 'uia.invoke') {
        if (!canInvoke.value || !selected.value) return;
        params.elementId = selected.value.id;
      }
      if (type === 'uia.set_value' || type === 'keyboard.type_text') {
        if (!canText.value || !selected.value || !validText.value) return;
        params.elementId = selected.value.id;
        params.text = draft.value;
      }
      const version = viewVersion;
      busy.value = 'propose';
      error.value = '';
      try {
        const task = asTask
          ? await call<AgentTask & { proposal: ControlProposal }>('CreateDesktopTask', { ...params, workspaceId: product.selectedWorkspace })
          : undefined;
        const proposal = task?.proposal ?? await call<ControlProposal>('ProposeControlAction', params);
        if (task) void refreshProduct().catch(() => undefined);
        if (!isCurrent(version)) return;
        if (proposal.status !== 'AWAITING_APPROVAL' || proposal.type !== type || proposal.text !== params.text || proposal.taskId !== (task?.id ?? params.taskId) || !proposal.id || !proposal.hash) {
          error.value = 'bridge.invalid_response';
          return;
        }
        showProposal(proposal);
      } catch (failure) {
        if (isCurrent(version)) {
          error.value = codeOf(failure);
          if (error.value === 'CONTROL_STALE_TARGET') clearObservation();
          if (error.value === 'CONTROL_STOPPED') void refreshStatus();
        }
      } finally {
        if (isCurrent(version)) busy.value = '';
        if (active) void refreshJournal();
      }
    }

    async function decide(approve: boolean) {
      const proposal = pending.value;
      if (!proposal || deciding.value || !permitted.value) return;
      if (approvalExpired.value || Date.parse(proposal.expiresAt) <= Date.now()) {
        clearProposal();
        clearObservation();
        error.value = 'CONTROL_EXPIRED';
        void refreshJournal();
        return;
      }
      deciding.value = true;
      error.value = '';
      lastResult.value = undefined;
      uncertainReply.value = false;
      clearProposal();
      if (approve) {
        clearObservation();
        state.activeControlActionId = proposal.id;
      }
      try {
        const record = await call<ControlRecord>(approve ? 'ApproveControlAction' : 'RejectControlAction', {
          actionId: proposal.id, actionHash: proposal.hash,
        });
        if (active && state.connected) lastResult.value = record;
      } catch (failure) {
        if (active && state.connected) {
          error.value = codeOf(failure);
          const preDispatchDenials = ['CONTROL_STOPPED', 'CONTROL_BUSY', 'CONTROL_ACTION_DENIED', 'CONTROL_STALE_TARGET',
            'CONTROL_EXPIRED', 'CONTROL_APPROVAL_MISMATCH', 'CONTROL_APPROVAL_USED', 'CONTROL_INPUT_REJECTED'];
          if (approve && !preDispatchDenials.includes(error.value)) {
            uncertainReply.value = true;
          }
          if (error.value === 'CONTROL_STOPPED') void refreshStatus();
        }
      } finally {
        if (state.activeControlActionId === proposal.id) state.activeControlActionId = '';
        if (active) {
          deciding.value = false;
          void refreshStatus();
          void refreshJournal();
        }
      }
    }

    async function changeMode(stop: boolean) {
      if (!state.connected || (modeChanging.value && !stop) || (!stop && (!stopped.value || !available.value || deciding.value))) return;
      invalidateView();
      state.controlStoppedLocally = true;
      modeChanging.value = true;
      error.value = '';
      const version = viewVersion;
      try {
        await call<boolean>(stop ? 'StopComputerControl' : 'ResumeComputerControl');
        if (isCurrent(version) && !stop) state.controlStoppedLocally = false;
      } catch (failure) {
        if (isCurrent(version)) error.value = codeOf(failure);
      } finally {
        if (isCurrent(version)) {
          await refreshStatus();
          if (isCurrent(version)) modeChanging.value = false;
          void refreshJournal();
        }
      }
    }

    watch(() => state.connected, connected => {
      if (connected) {
        void refreshStatus();
        void refreshJournal();
      } else {
        invalidateView();
        journalVersion++;
        journalLoading.value = false;
        modeChanging.value = false;
        status.value = undefined;
      }
    }, { immediate: true });
    watch(() => state.controlStopVersion, () => {
      invalidateView();
      modeChanging.value = false;
      state.controlStoppedLocally = true;
      void refreshSuggestions();
    }, { flush: 'sync' });
    watch([taskId, () => state.connected, () => props.suggestionRevision], () => {
      suggestionVersion++;
      clearTimeout(suggestionTimer);
      suggestionLoading.value = false;
      suggestions.value = [];
      suggestionError.value = '';
      void refreshSuggestions();
    });
    onBeforeUnmount(() => {
      active = false;
      clearTimeout(suggestionTimer);
      clearProposal();
    });

    const alert = (code: string) => code ? h('p', { class: 'ai-error', role: 'alert' }, errorText(code)) : null;

    function renderTree(nodes: TreeNode[]): VNodeChild {
      return h('ul', { class: 'control-tree' }, nodes.map(({ element, children }) => h('li', { key: element.id }, [
        button('control.selectElement', () => choose(element), {
          class: 'control-node', 'aria-pressed': element.id === selectedId.value, disabled: !canInspect.value,
          'data-testid': 'control-select-element', 'data-automation-id': element.automationId,
        }, [h('strong', element.name || element.automationId || element.controlType || missing()), h('span', element.controlType)]),
        children.length ? renderTree(children) : null,
      ])));
    }

    function renderTargets() {
      return h('section', { class: 'screen-card', 'data-testid': 'control-target-applications' }, [
        h('header', { class: 'control-heading' }, [
          h('h2', label('targets')),
          actionButton('refreshTargets', 'control-refresh-targets', listTargets, !canInspect.value, 'secondary', 'refresh'),
        ]),
        busy.value === 'list' ? loadText('loadingTargets') : null,
        ...targetApps.map(app => {
          const listed = windows.value?.filter(target => target.app === app);
          const appMode = app === 'test_host' ? (permitted.value ? 'allowedWithApproval' : 'observationMode') : 'notepadConditional';
          return h('div', { class: 'control-app', key: app, 'data-control-app': app }, [
            h('strong', label('apps.' + app)), h('p', { class: 'muted' }, label(appMode)),
            listed?.length ? h('ul', { class: 'control-window-list' }, listed.map(target => h('li', { key: target.id }, [
              h('details', { class: 'control-window-details' }, [
                h('summary', target.title || label('untitledWindow')),
                h('dl', [field('windowState', label(target.foreground ? 'foreground' : 'background')), ...windowFields(target)]),
              ]),
              h('p', { class: 'muted micro' }, t('control.windowSummary', { pid: target.pid, state: label(target.foreground ? 'foreground' : 'background') })),
              button('control.inspect', () => inspect(target), {
                class: 'button secondary', disabled: !canInspect.value, 'data-testid': 'control-inspect-window', 'data-window-id': target.id,
              }),
            ]))) : h('p', { class: 'muted' }, label(listed ? 'noWindows' : 'targetsNotListed')),
          ]);
        }),
      ]);
    }

    function renderInspector() {
      const current = observed.value;
      return h('section', { class: 'screen-card', 'data-testid': 'control-ui-inspector' }, [
        h('h2', label('inspector')),
        busy.value === 'inspect' ? loadText('inspecting') : current ? [
          h('div', { class: 'control-observation-meta' }, [
            h('strong', current.window.title || label('apps.' + current.window.app)),
            h('p', { class: 'muted micro' }, t('control.observed', { at: date(current.observedAt) })),
          ]),
          current.truncated ? loadText('truncated') : null,
          current.modal ? loadText('modal') : null,
          current.window.app === 'notepad' ? h('p', {
            class: 'muted', 'data-testid': notepadEditor.value ? 'control-notepad-conditional' : 'control-notepad-unavailable',
          }, label(notepadEditor.value ? 'notepadManualAcceptance' : 'notepadUnsupported')) : null,
          tree.value.length ? h('div', { class: 'control-tree-scroll', 'data-testid': 'control-tree' }, [renderTree(tree.value)]) : loadText('emptyTree'),
          selected.value ? h('dl', { class: 'control-selected', 'data-testid': 'control-element-properties' }, elementFields(selected.value)) : null,
        ] : h('p', { class: 'muted' }, label('inspectPrompt')),
      ]);
    }

    function renderApproval() {
      const proposal = pending.value;
      const required = proposal && known('control.expected.' + proposal.expected);
      const pattern = proposal?.type === 'uia.invoke' ? 'Invoke' : proposal?.element ? 'Value' : missing();
      const expired = approvalExpired.value;
      return h('section', { class: ['screen-card', 'control-approval', { 'control-approval-pending': proposal }], 'data-testid': 'control-approval-center' }, [
        h('h2', label('approval')),
        proposal ? [
          h('p', { class: 'muted', role: 'status' }, label(expired ? 'approvalExpired' : 'approvalPrompt')),
          proposal.window?.app === 'notepad' ? h('p', { class: 'muted' }, label('notepadConditional')) : null,
          h('dl', { 'data-testid': 'control-approval-details' }, [
            field('operation', actionName(proposal.type)),
            proposal.sourceRequestId ? detail('computerSuggestion.source', t('computerSuggestion.model')) : null,
            computerTaskReview(props.tasks, proposal.taskId),
            field('application', label('apps.' + (proposal.window?.app || 'test_host'))),
            ...proposal.window ? windowFields(proposal.window) : [field('windowTitle', label('newFixtureWindow'))],
            ...proposal.element ? elementFields(proposal.element) : [],
            field('requiredPattern', pattern, true),
            field('expectedResult', required),
            field('risk', label('risks.' + actionNames[proposal.type])),
            field('policyVersion', proposal.policyVersion, true),
            field('expiresAt', h('time', { datetime: proposal.expiresAt, title: date(proposal.expiresAt) }, proposal.expiresAt)),
          ]),
          proposal.text !== undefined ? h('div', { class: 'control-text-review' }, [
            h('h3', label('exactText')),
            h('pre', { dir: 'auto', 'data-testid': 'control-approval-text' }, proposal.text),
          ]) : null,
          h('div', { class: 'ai-row' }, expired ? [
            actionButton('dismissExpired', 'control-dismiss-expired', () => { clearProposal(); clearObservation(); }),
          ] : [
            actionButton('approve', 'control-approve', () => decide(true), !permitted.value, 'primary'),
            actionButton('reject', 'control-reject', () => decide(false), !permitted.value),
          ]),
        ] : deciding.value ? loadText('waitingForResult') : h('p', { class: 'muted' }, label('noApproval')),
      ]);
    }

    function renderActions() {
      return h('section', { class: 'screen-card', 'data-testid': 'control-actions' }, [
        h('h2', label('actionsHeading')),
        computerTaskSelector(props.tasks, taskId.value, value => { taskId.value = value; }, blocked.value, 'control-task-link'),
        h('div', { class: 'ai-row' }, [
          actionButton('proposeLaunch', 'control-propose-launch', () => propose('app.launch_test_host'), blocked.value || !!taskId.value),
          canFocus.value ? actionButton('proposeFocus', 'control-propose-focus', () => propose('window.focus'), blocked.value) : null,
          canInvoke.value ? actionButton('proposeInvoke', 'control-propose-invoke', () => propose('uia.invoke'), blocked.value) : null,
          canInvoke.value ? actionButton('proposeTaskInvoke', 'control-propose-task-invoke', () => propose('uia.invoke', true), blocked.value) : null,
        ]),
        canText.value ? [
          h('label', { class: 'ai-field' }, [label('plainText'), h('input', {
            type: 'text', value: draft.value, autocomplete: 'off', spellcheck: false, disabled: blocked.value,
            'data-testid': 'control-text-input', 'aria-describedby': 'control-text-hint',
            onInput: (event: Event) => { draft.value = (event.target as HTMLInputElement).value; },
          })]),
          h('p', { class: 'muted', id: 'control-text-hint' }, label('plainTextHint')),
          h('div', { class: 'ai-row' }, [
            actionButton('proposeValue', 'control-propose-set-value', () => propose('uia.set_value'), blocked.value || !validText.value),
            actionButton('proposeTaskValue', 'control-propose-task-value', () => propose('uia.set_value', true), blocked.value || !validText.value),
            actionButton('proposeText', 'control-propose-type-text', () => propose('keyboard.type_text'), blocked.value || !validText.value),
          ]),
        ] : null,
      ]);
    }

    function renderTimeline() {
      const record = lastResult.value;
      return h('section', { class: 'screen-card', 'data-testid': 'control-timeline' }, [
        h('header', { class: 'control-heading' }, [
          h('h2', label('timeline')),
          actionButton('refreshTimeline', 'control-refresh-timeline', refreshJournal, !state.connected || journalLoading.value, 'secondary', 'refresh'),
        ]),
        alert(journalError.value),
        record || uncertainReply.value ? h('div', { class: 'control-last-result', 'data-testid': 'control-result', role: 'status' }, [
          record ? h('strong', t('control.latestResult', { status: known('control.statuses.' + record.status) })) : null,
          (uncertainReply.value || (record && uncertain(record))) ? h('p', { 'data-testid': 'control-uncertain-result' }, label('uncertainResult')) : null,
        ]) : null,
        journalLoading.value ? loadText('loadingTimeline') : null,
        journal.value.length ? h('ol', { class: 'control-journal' }, journal.value.map((row, index) =>
          h('li', { key: `${row.id}:${row.at}:${index}`, 'data-testid': 'control-journal-entry', 'data-status': row.status }, [
            h('div', { class: 'control-heading' }, [h('strong', actionName(row.type)), isoTime(row.at)]),
            h('p', [h('span', { class: ['control-record-status', { 'control-record-uncertain': uncertain(row) }] }, known('control.statuses.' + row.status)),
              row.verified ? h('span', { class: 'muted' }, ` · ${label('verified')}`) : null]),
            row.code && row.code.startsWith('CONTROL_') ? h('p', { class: 'muted' }, errorText(row.code)) : null,
            uncertain(row) ? h('p', { class: 'muted' }, label('uncertainResult')) : null,
          ]))) : !journalLoading.value && !journalError.value ? h('p', { class: 'muted' }, label('emptyTimeline')) : null,
      ]);
    }

    function renderSuggestions() {
      const target = suggestionTarget.value;
      if (!taskId.value) return null;
      return h('section', { class: 'screen-card', 'data-testid': 'control-suggestions' }, [
        h('h2', t('computerSuggestion.heading')),
        h('p', { class: 'muted' }, t('computerSuggestion.targetHelp')),
        h('div', { class: 'ai-row' }, [
          target ? button('computerSuggestion.capture', () => emit('requestSuggestion', target), {
            class: ['button', 'primary'], disabled: blocked.value, 'data-testid': 'control-request-suggestion',
          }) : null,
          button('computerSuggestion.refresh', refreshSuggestions, {
            class: ['button', 'secondary'], disabled: suggestionLoading.value || !state.connected, 'data-testid': 'control-refresh-suggestions',
          }),
        ]),
        alert(suggestionError.value),
        suggestions.value.length ? h('ol', { class: 'control-journal' }, suggestions.value.map(item =>
          h('li', { key: item.requestId, 'data-testid': 'control-suggestion', 'data-status': item.status }, [
            h('strong', `${actionName(item.actionType)} · ${item.selector}`),
            h('p', [t('computerSuggestion.model'), ' · ', known('computerSuggestion.status.' + item.status), ' · ', isoTime(item.observedAt)]),
            item.summary ? h('p', { class: 'screen-plain-text' }, item.summary) : null,
            item.status === 'READY' ? button('computerSuggestion.review', () => reviewSuggestion(item), {
              class: ['button', 'primary'], disabled: blocked.value, 'data-testid': 'control-review-suggestion',
            }) : null,
            item.status === 'READY' ? button('control.proposeTaskInvoke', () => reviewSuggestion(item, true), {
              class: ['button', 'secondary'], disabled: blocked.value, 'data-testid': 'control-review-suggestion-task',
            }) : null,
          ]))) : null,
      ]);
    }

    return () => h('section', { class: 'computer-control', 'aria-labelledby': 'computer-control-title', 'data-testid': 'computer-control' }, [
      h('header', { class: 'control-toolbar' }, [
        h('div', [
          h('h2', { id: 'computer-control-title' }, label('title')),
          h('p', { class: 'muted', role: 'status', 'data-testid': 'control-status' }, label(checking.value ? 'checkingStatus' : stopped.value ? 'stopped' : permitted.value ? 'permissionMode' : 'unavailable')),
          h('p', { class: 'muted micro', 'data-testid': 'control-raw-input' }, label('rawInputDisabled')),
        ]),
        h('div', { class: 'ai-row' }, [
          actionButton('refreshStatus', 'control-refresh-status', refreshStatus, !state.connected || checking.value || modeChanging.value, 'secondary', 'refresh'),
          stopped.value ? actionButton('resume', 'control-resume', () => changeMode(false), !available.value || deciding.value || modeChanging.value || checking.value) : null,
          actionButton('stop', 'control-emergency-stop', () => changeMode(true), !state.connected, 'danger-subtle', 'stop'),
        ]),
      ]),
      alert(statusError.value), alert(error.value),
      h('div', { class: 'screen-page-grid' }, [renderTargets(), renderInspector(), renderActions(), renderSuggestions(), renderApproval(), renderTimeline()]),
    ]);
  },
  emits: { requestSuggestion: (_target: ComputerSuggestionTarget) => true },
});
