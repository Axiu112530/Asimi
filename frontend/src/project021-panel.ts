import { defineComponent, h, ref, shallowRef, watch } from 'vue';
import type { PropType } from 'vue';
import { call } from './bridge';
import { i18n, number, t } from './i18n';
import { errorText } from './ui';
import en from './locales/project021.en-US.json';
import zh from './locales/project021.zh-CN.json';

i18n.global.mergeLocaleMessage('en-US', { project021: en });
i18n.global.mergeLocaleMessage('zh-CN', { project021: zh });

type ProjectField = 'types' | 'languages' | 'frameworks' | 'managers' | 'manifests' | 'entryPoints' | 'testPaths' | 'testGuidance';
type Summary = Partial<Record<ProjectField, string[]>> & { truncated: boolean };
type GitChange = { path: string; staged: boolean; unstaged: boolean; untracked: boolean };
type GitContext = {
  status: 'AVAILABLE' | 'NOT_REPOSITORY' | 'UNSUPPORTED' | 'ERROR';
  branch?: string;
  head?: string;
  staged: number;
  unstaged: number;
  untracked: number;
  changedFiles: number;
  changes?: GitChange[];
  truncated: boolean;
};
type RuntimeDiagnostic = {
  component: string;
  status: string;
  executable?: string;
  version?: string;
  minimumVersion?: string;
  source?: string;
  problem?: string;
  downloadUrl?: string;
  installPreview?: string[];
};
type ProjectInspection = { summary: Summary; git: GitContext; runtimes: RuntimeDiagnostic[] };

const label = (key: string) => t(`project021.${key}`);
const summaryFields: ProjectField[] = ['types', 'languages', 'frameworks', 'managers', 'manifests', 'entryPoints', 'testPaths', 'testGuidance'];
const field = (key: string, value: string) => [h('dt', label(key)), h('dd', value)];

function projectOverview(summary: Summary) {
  return h('article', { class: 'product-card', 'data-testid': 'developer-project-overview' }, [
    h('h2', label('project')),
    !summary.types?.length ? h('p', label('noProject')) : null,
    h('dl', summaryFields.flatMap(key => {
      const values = summary[key];
      return values?.length ? field(key, values.join(', ')) : [];
    })),
    summary.truncated ? h('p', { class: 'micro muted' }, label('projectTruncated')) : null,
  ]);
}

function gitOverview(git: GitContext) {
  const unavailable = { NOT_REPOSITORY: 'noRepo', UNSUPPORTED: 'unsupportedGit', ERROR: 'gitError' };
  return h('article', { class: 'product-card', 'data-testid': 'developer-git-status', 'data-status': git.status }, [
    h('h2', label('git')),
    git.status === 'AVAILABLE' ? h('dl', [
      ...field('branch', git.branch || label('detached')),
      ...field('head', git.head || label('unborn')),
      ...field('changedFiles', number(git.changedFiles)),
      ...field('staged', number(git.staged)),
      ...field('unstaged', number(git.unstaged)),
      ...field('untracked', number(git.untracked)),
    ]) : h('p', label(unavailable[git.status])),
    git.truncated ? h('p', { class: 'micro muted' }, label('gitLimited')) : null,
    git.changes?.length ? h('details', [
      h('summary', label('changes')),
      h('ul', git.changes.map(change => h('li', { key: change.path }, [
        h('code', change.path),
        ' · ',
        (['staged', 'unstaged', 'untracked'] as const).filter(key => change[key]).map(label).join(', '),
      ]))),
    ]) : null,
  ]);
}

function runtimeCard(runtime: RuntimeDiagnostic) {
  const installable = runtime.status === 'MISSING' || runtime.status === 'INCOMPATIBLE';
  return h('article', { class: 'product-card', 'data-testid': `developer-runtime-${runtime.component}`, 'data-status': runtime.status }, [
    h('div', { class: 'split' }, [h('h3', label(`component.${runtime.component}`)), h('span', label(`status.${runtime.status}`))]),
    runtime.executable ? h('code', runtime.executable) : null,
    h('dl', [
      ...(runtime.version ? field('version', runtime.version) : []),
      ...(runtime.minimumVersion ? field('minimum', runtime.minimumVersion) : []),
      ...(runtime.source ? field('source', label(`origin.${runtime.source}`)) : []),
    ]),
    runtime.problem ? h('p', label(`problem.${runtime.problem}`)) : null,
    installable && runtime.downloadUrl ? h('a', { href: runtime.downloadUrl, target: '_blank', rel: 'noopener noreferrer' }, label('officialSource')) : null,
    installable && runtime.installPreview?.length ? h('pre', runtime.installPreview.join(' ')) : null,
    installable ? h('p', { class: 'micro muted' }, label('installManually')) : null,
  ]);
}

function environmentOverview(runtimes: RuntimeDiagnostic[]) {
  const required = runtimes.filter(runtime => runtime.status !== 'NOT_REQUIRED');
  const optional = runtimes.filter(runtime => runtime.status === 'NOT_REQUIRED');
  return h('section', { 'data-testid': 'developer-project-environment' }, [
    h('h2', label('environment')),
    h('p', { class: 'micro muted' }, label('runtimeIntro')),
    required.length ? required.map(runtimeCard) : h('p', label('noRuntime')),
    optional.length ? h('details', [h('summary', label('notRequired')), ...optional.map(runtimeCard)]) : null,
  ]);
}

export const DeveloperProjectPanel = defineComponent({
  props: {
    workspaceId: { type: String, required: true },
    exclusions: { type: Array as PropType<string[]>, required: true },
    mode: { type: String as PropType<'project' | 'environment'>, required: true },
    refreshKey: { type: Number, default: 0 },
  },
  setup(props) {
    const inspection = shallowRef<ProjectInspection>();
    const pending = ref(false);
    const error = ref('');
    watch([
      () => props.workspaceId,
      () => JSON.stringify(props.exclusions.map(value => value.trim()).filter(Boolean)),
      () => props.refreshKey,
    ], async ([workspaceId, exclusions], _, onCleanup) => {
      let active = true;
      onCleanup(() => { active = false; });
      inspection.value = undefined;
      error.value = '';
      pending.value = Boolean(workspaceId);
      if (!workspaceId) return;
      try {
        const result = await call<ProjectInspection>('InspectDeveloperProject', { workspaceId, exclusions: JSON.parse(exclusions) });
        if (active) inspection.value = result;
      } catch (failure) {
        if (active) error.value = failure instanceof Error ? failure.message : 'bridge.call_failed';
      } finally {
        if (active) pending.value = false;
      }
    }, { immediate: true });
    return () => {
      if (pending.value) return h('p', { role: 'status', class: 'micro muted' }, label('loading'));
      if (error.value) return h('p', { role: 'alert', class: 'ai-error' }, errorText(error.value));
      if (!inspection.value) return props.mode === 'environment' ? h('p', label('chooseWorkspace')) : null;
      return props.mode === 'environment' ? environmentOverview(inspection.value.runtimes) :
        h('section', [projectOverview(inspection.value.summary), gitOverview(inspection.value.git)]);
    };
  },
});
