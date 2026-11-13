import { defineComponent, h, reactive, ref, watch, type VNodeChild } from 'vue';
import { call } from './bridge';
import { date, t } from './i18n';
import { product } from './product-store';
import { fail, state } from './store';
import { button } from './ui';
import './cognition.css';

interface Settings {
  memory: boolean;
  automaticLearning: boolean;
  automaticSkillCreation: boolean;
  projectLearning: boolean;
}
interface Memory {
  id: string;
  workspaceId?: string;
  kind: 'user' | 'project' | 'episode' | 'procedure';
  key: string;
  text: string;
  source: string;
  verified: boolean;
  archived?: boolean;
  priority?: number;
  updatedAt: string;
}
interface SkillInput {
  scope: 'official' | 'user' | 'project';
  workspaceId?: string;
  slug: string;
  name: string;
  description: string;
  instructions: string;
  triggers: string[] | null;
  allowedTools: string[] | null;
  applicability?: string;
  prerequisites?: string;
  workflow?: string[];
  expectedOutputs?: string;
  validation?: string;
  limitations?: string;
  sourceExperience?: string;
}
interface Skill extends SkillInput {
  id: string;
  namespace: string;
  source: string;
  enabled: boolean;
  version: number;
  usage: number;
  status?: string;
  completedUses?: number;
  failedUses?: number;
  checksum: string;
}
interface Version {
  version: number;
  manifest: SkillInput;
  checksum: string;
  createdAt: string;
}
interface Candidate {
  id: string;
  taskId: string;
  workspaceId: string;
  manifest: SkillInput;
  previous?: SkillInput;
  baseVersion: number;
  status: 'pending' | 'approved' | 'rejected';
}
interface LearningRecord {
  taskId: string;
  status: 'pending' | 'paused' | 'done' | 'failed' | 'cancelled' | 'expired';
  attempts: number;
  durationMs: number;
  error?: string;
  createdAt: string;
}
interface CognitionState {
  memoryChanges: {id:number;previous:string;current:string;source:string;at:string}[];
  settings: Settings;
  memories: Memory[];
  skills: Skill[];
  pending: number;
  paused: number;
  completed: number;
  candidates: Candidate[];
  learning: LearningRecord[];
}

const settingsKeys: (keyof Settings)[] = ['memory', 'automaticLearning', 'automaticSkillCreation', 'projectLearning'];
const textValue = (event: Event) => (event.target as HTMLInputElement).value;
const label = (key: string, field: VNodeChild) => h('label', { class: 'product-field' }, [h('span', t('cognition.' + key)), field]);
const localButton = (key: string, action: () => unknown, disabled = false) =>
  button('cognition.' + key, action, { class: 'button secondary', disabled });

function useCognition() {
  const workspaceId = ref('');
  const data = ref<CognitionState>();
  const busy = ref(false);
  let request = 0;
  async function load() {
    const current = ++request;
    const result = await call<CognitionState>('GetCognitionState', { workspaceId: workspaceId.value });
    if (request === current) data.value = result;
  }
  watch(() => state.connected, connected => {
    if (!connected) {
      request++;
      data.value = undefined;
    }
  });
  watch([workspaceId, () => state.connected], ([, connected]) => {
    data.value = undefined;
    if (connected) void load().catch(fail);
  }, { immediate: true });
  async function work(action: () => Promise<unknown>) {
    if (!state.connected || busy.value) return;
    busy.value = true;
    try {
      await action();
      await load();
    } catch (error) {
      fail(error);
    } finally {
      busy.value = false;
    }
  }
  function projectPicker() {
    return label('workspace', h('select', {
      value: workspaceId.value, disabled: busy.value,
      onChange: (event: Event) => { workspaceId.value = textValue(event); },
    }, [
      h('option', { value: '' }, t('cognition.noWorkspace')),
      ...product.data.workspaces.map(workspace => h('option', { key: workspace.id, value: workspace.id }, workspace.name)),
    ]));
  }
  return { data, busy, workspaceId, projectPicker, work };
}

export const MemoryPage = defineComponent({
  setup() {
    const { data, busy, workspaceId, projectPicker, work } = useCognition();
    const key = ref('');
    const search = ref('');
    const text = ref('');
    const scope = ref<'user' | 'project'>('user');
    function save() {
      return work(async () => {
        await call('SaveCognitionMemory', {
          kind: scope.value, key: key.value, text: text.value,
          workspaceId: scope.value === 'project' ? workspaceId.value : '',
        });
        key.value = '';
        text.value = '';
      });
    }
    return () => h('section', { class: 'product-page', 'data-testid': 'cognition-memory' }, [
      h('header', { class: 'product-heading' }, [h('h1', t('cognition.memoryPage'))]),
      h('div', { class: 'product-row cognition-toolbar' }, [projectPicker(), localButton('refresh', () => work(async () => {}), busy.value)]),
      data.value && h('article', { class: 'product-card' }, [
        h('h2', t('cognition.learning')),
        h('p', t('cognition.providerNotice')),
        ...settingsKeys.map(setting => h('label', { class: 'product-check cognition-check', key: setting }, [
          h('input', {
            type: 'checkbox', checked: data.value?.settings[setting], disabled: busy.value,
            onChange: (event: Event) => {
              const checked = (event.target as HTMLInputElement).checked;
              void work(() => call('SetCognitionSettings', { ...data.value?.settings, [setting]: checked }));
            },
          }),
          h('span', t('cognition.' + setting)),
        ])),
        workspaceId.value && h('p', t('cognition.queue', {
          pending: data.value.pending, paused: data.value.paused, completed: data.value.completed,
        })),
        h('div', { class: 'product-row' }, [
          localButton('pause', () => work(() => call('PauseCognitionLearning')), busy.value),
          localButton('resume', () => work(() => call('ResumeCognitionLearning')), busy.value),
        ]),
      ]),
      h('article', { class: 'product-card' }, [
        h('h2', t('cognition.addMemory')),
        label('scope', h('select', { value: scope.value, disabled: busy.value, onChange: (event: Event) => {
          scope.value = textValue(event) === 'project' ? 'project' : 'user';
        } }, [h('option', { value: 'user' }, t('cognition.user')), h('option', { value: 'project' }, t('cognition.project'))])),
        label('memoryKey', h('input', { value: key.value, maxlength: 100, onInput: (event: Event) => { key.value = textValue(event); } })),
        label('memoryText', h('textarea', { value: text.value, rows: 3, maxlength: 2400, onInput: (event: Event) => { text.value = textValue(event); } })),
        localButton('save', save, busy.value || !key.value.trim() || !text.value.trim() || scope.value === 'project' && !workspaceId.value),
      ]),
      data.value && data.value.memoryChanges.length>0 && h('article',{class:'product-card'},[
        h('h2',t('cognition.conflictReview')),
        ...data.value.memoryChanges.map(change=>h('div',{class:'product-card',key:change.id},[
          h('p',t('cognition.previousMemory')),h('pre',change.previous),
          h('p',t('cognition.currentMemory')),h('pre',change.current),
          h('p',change.source+' · '+date(change.at)),
          localButton('dismissConflict',()=>work(()=>call('DismissCognitionMemoryChange',{id:change.id,workspaceId:workspaceId.value})),busy.value),
        ])),
      ]),
      label('memorySearch',h('input',{type:'search',value:search.value,onInput:(event:Event)=>{search.value=textValue(event);}})),
      data.value && !data.value.memories.length && h('p', t('cognition.emptyMemory')),
      ...(data.value?.memories ?? []).filter(memory => (memory.key+" "+memory.text).toLowerCase().includes(search.value.trim().toLowerCase())).map(memory => h('article', { key: memory.id, class: 'product-card' }, [
        h('h3', memory.kind === 'episode' ? t('cognition.episode') : memory.key),
        h('p', { class: 'micro' }, [t('cognition.' + memory.kind), ' · ', date(memory.updatedAt), ' · ',
          t(memory.verified ? 'cognition.verified' : 'cognition.unverified')]),
        h('p', memory.text),
        h('code', memory.source),
        h('div',{class:'product-row'},[
          memory.archived && h('span',t('cognition.archived')),
          localButton(memory.archived ? 'unarchive' : 'archive', () => work(() => call('ArchiveCognitionMemory',{
            id:memory.id,workspaceId:memory.workspaceId,archived:!memory.archived,priority:memory.priority,
          })),busy.value),
          (memory.kind==='user'||memory.kind==='project') && localButton('edit',()=>{
            scope.value=memory.kind==='project'?'project':'user';
            key.value=memory.key;
            text.value=memory.text;
          },busy.value),
        ]),
        localButton('delete', () => {
          if (window.confirm(t('cognition.confirmDelete'))) void work(() => call('DeleteCognitionMemory', {
            id: memory.id, workspaceId: memory.workspaceId,
          }));
        }, busy.value),
      ])),
    ]);
  },
});

export const GrowthPage = defineComponent({
  setup(){
    const {data,busy,projectPicker,work}=useCognition();
    return ()=>h('section',{class:'product-page','data-testid':'cognition-growth'},[
      h('header',{class:'product-heading'},[h('h1',t('cognition.growthPage'))]),
      h('div',{class:'product-row cognition-toolbar'},[projectPicker(),localButton('refresh',()=>work(async()=>{}),busy.value)]),
      data.value && h('article',{class:'product-card'},[
        h('p',t('cognition.queue',{pending:data.value.pending,paused:data.value.paused,completed:data.value.completed})),
        h('div',{class:'product-row'},[
          localButton('memoryPage',()=>{state.route='memory';}),
          localButton('localSkills',()=>{state.route='skills';}),
          localButton('learningPage',()=>{state.route='learning';}),
        ]),
        h('h2',t('cognition.recentSkills')),
        ...data.value.skills.filter(skill=>skill.scope!=='official').slice(0,5).map(skill=>h('p',{key:skill.id},
          t('cognition.skillMeta',{namespace:skill.namespace,version:skill.version,usage:skill.usage}))),
        h('h2',t('cognition.recentKnowledge')),
        ...data.value.memories.filter(item=>!item.archived).slice(0,5).map(item=>h('p',{key:item.id},
          item.text+' · '+t(item.verified?'cognition.verified':'cognition.unverified'))),
      ]),
    ]);
  },
});

export const LearningPage = defineComponent({
  setup() {
    const {data,busy,workspaceId,projectPicker,work}=useCognition();
    function change(job:LearningRecord,operation:string) {
      return work(()=>call('ChangeCognitionJob',{id:job.taskId,workspaceId:workspaceId.value,operation}));
    }
    return ()=>h('section',{class:'product-page','data-testid':'cognition-learning'},[
      h('header',{class:'product-heading'},[h('h1',t('cognition.learningPage'))]),
      h('div',{class:'product-row cognition-toolbar'},[projectPicker(),localButton('refresh',()=>work(async()=>{}),busy.value)]),
      data.value && h('article',{class:'product-card'},[
        h('p',t('cognition.queue',{pending:data.value.pending,paused:data.value.paused,completed:data.value.completed})),
        h('div',{class:'product-row'},[
          localButton('pause',()=>work(()=>call('PauseCognitionLearning')),busy.value),
          localButton('resume',()=>work(()=>call('ResumeCognitionLearning')),busy.value),
        ]),
        h('h2',t('cognition.history')),
        !data.value.learning.length && h('p',t('cognition.noHistory')),
        ...data.value.learning.map(job=>h('article',{key:job.taskId,class:'product-card'},[
          h('strong',t('cognition.job.'+job.status)),
          h('p',t('cognition.attempts',{count:job.attempts,ms:job.durationMs})+' · '+date(job.createdAt)),
          h('p',t('cognition.sourceTask',{id:job.taskId})),
          job.error && h('code',job.error),
          job.status==='pending'||job.status==='paused' ? localButton('cancelJob',()=>change(job,'cancel'),busy.value) : null,
          ['failed','cancelled','expired'].includes(job.status) && localButton('retry',()=>change(job,'retry'),busy.value),
        ])),
      ]),
    ]);
  },
});

export const CognitionSkillsPanel = defineComponent({
  setup() {
    const { data, busy, workspaceId, projectPicker, work } = useCognition();
    const query = ref('');
    const form = reactive({
      scope: 'user' as 'user' | 'project', slug: '', name: '', description: '', instructions: '', triggers: '', tools: '',
    });
    const editing = ref(false);
    const editedDetails=ref<Partial<SkillInput>>({});
    const importScope = ref<'user' | 'project'>('user');
    const selectedVersions = ref('');
    const versions = ref<Version[]>([]);
    const skillKey = (skill: Skill) => ({ scope: skill.scope, workspaceId: skill.workspaceId, slug: skill.slug });
    const field = (key: 'slug' | 'name' | 'description' | 'instructions' | 'triggers' | 'tools', maxlength: number) =>
      label(key, h(key === 'instructions' ? 'textarea' : 'input', {
        value: form[key], maxlength, 'data-testid': 'cognition-skill-'+key, disabled: busy.value || editing.value && key === 'slug',
        onInput: (event: Event) => { form[key] = textValue(event); },
      }));
    const lines = (value: string) => value.split(',').map(item => item.trim()).filter(Boolean);
    function reset() {
      Object.assign(form, { scope: 'user', slug: '', name: '', description: '', instructions: '', triggers: '', tools: '' });
      editedDetails.value={};
      editing.value = false;
    }
    watch(workspaceId, () => {
      if (form.scope === 'project') reset();
      selectedVersions.value = '';
      versions.value = [];
    });
    function edit(skill: Skill) {
      if (skill.scope === 'official') return;
      Object.assign(form, {
        scope: skill.scope, slug: skill.slug, name: skill.name, description: skill.description, instructions: skill.instructions,
        triggers: (skill.triggers ?? []).join(', '), tools: (skill.allowedTools ?? []).join(', '),
      });
	  const {applicability,prerequisites,workflow,expectedOutputs,validation,limitations,sourceExperience}=skill;
      editedDetails.value={applicability,prerequisites,workflow,expectedOutputs,validation,limitations,sourceExperience};
      editing.value = true;
    }
    function downloadSkill(skill: Skill) {
      return work(async () => {
        const bundle = await call<{ manifest: SkillInput; skillMarkdown: string }>('ExportCognitionSkill', skillKey(skill));
        const url = URL.createObjectURL(new Blob([JSON.stringify(bundle, null, 2)], { type: 'application/json' }));
        const link = document.createElement('a');
        link.href = url;
        link.download = skill.slug + '.asimi-skill.json';
        link.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      });
    }
    function importSkill(event: Event) {
      const input = event.target as HTMLInputElement;
      const file = input.files?.[0];
      input.value = '';
      if (!file) return;
      const scope = importScope.value;
      const project = scope === 'project' ? workspaceId.value : '';
      return work(async () => {
        if (file.size > 32 * 1024) throw new Error('COGNITION_INVALID_SKILL');
        await call('ImportCognitionSkill', { scope, workspaceId: project, package: JSON.parse(await file.text()) });
      });
    }
    async function listVersions(skill: Skill) {
      versions.value = await call<Version[]>('ListCognitionSkillVersions', skillKey(skill));
      selectedVersions.value = skill.id;
    }
    function change(skill: Skill, operation: string, version?: number) {
      if (operation === 'delete' && !window.confirm(t('cognition.confirmDelete'))) return;
      return work(async () => {
        await call('ChangeCognitionSkill', { ...skillKey(skill), operation, version });
        if (operation === 'delete') {
          selectedVersions.value = '';
          versions.value = [];
          if (form.slug === skill.slug && form.scope === skill.scope) reset();
        }
      });
    }
    return () => {
      const search = query.value.trim().toLocaleLowerCase();
      const skills = (data.value?.skills ?? []).filter(skill => skill.scope !== 'official' &&
        (!search || (skill.name + ' ' + skill.namespace + ' ' + skill.description).toLocaleLowerCase().includes(search)));
      const candidates=(data.value?.candidates??[]).filter(item=>item.status==='pending');
      return h('section', { class: 'product-page', 'data-testid': 'cognition-skills' }, [
        workspaceId.value && h('article',{class:'product-card'},[
          h('h2',t('cognition.reviewQueue')),
          !candidates.length && h('p',t('cognition.noReviews')),
          ...candidates.map(candidate=>h('article',{class:'product-card',key:candidate.id},[
            h('h3',candidate.manifest.name),
            h('p',candidate.manifest.description),
            h('p',t('cognition.sourceTask',{id:candidate.taskId})),
            candidate.previous && h('details',[h('summary',t('cognition.previousVersion',{version:candidate.baseVersion})),h('pre',JSON.stringify(candidate.previous,null,2))]),
            h('details',{open:true},[h('summary',t('cognition.proposedVersion')),h('pre',JSON.stringify(candidate.manifest,null,2))]),
            h('div',{class:'product-row'},[true,false].map(approve=>localButton(approve?'approveCandidate':'rejectCandidate',
              ()=>work(()=>call('ReviewCognitionCandidate',{id:candidate.id,workspaceId:candidate.workspaceId,approve})),busy.value))),
          ])),
        ]),
        h('article', { class: 'product-card' }, [
          h('h2', t('cognition.localSkills')),
          h('div', { class: 'product-row cognition-toolbar' }, [
            projectPicker(), label('search', h('input', { type: 'search', value: query.value, onInput: (event: Event) => { query.value = textValue(event); } })),
            localButton('refresh', () => work(async () => {}), busy.value),
          ]),
          h('div', { class: 'product-row' }, [
            label('importScope', h('select', { value: importScope.value, disabled: busy.value, onChange: (event: Event) => {
              importScope.value = textValue(event) === 'project' ? 'project' : 'user';
            } }, [h('option', { value: 'user' }, t('cognition.user')), h('option', { value: 'project' }, t('cognition.project'))])),
            label('import', h('input', { type: 'file', accept: '.json', disabled: busy.value || importScope.value === 'project' && !workspaceId.value, onChange: importSkill })),
          ]),
        ]),
        h('details', { class: 'product-card', 'data-testid': 'cognition-skill-editor', open: editing.value || undefined }, [
          h('summary', t(editing.value ? 'cognition.editSkill' : 'cognition.createSkill')),
          label('scope', h('select', { value: form.scope, disabled: editing.value || busy.value, onChange: (event: Event) => {
            form.scope = textValue(event) === 'project' ? 'project' : 'user';
          } }, [h('option', { value: 'user' }, t('cognition.user')), h('option', { value: 'project' }, t('cognition.project'))])),
          field('slug', 64), field('name', 100), field('description', 320), field('instructions', 3600),
          field('triggers', 1200), field('tools', 1200),
          h('div', { class: 'product-row' }, [
            localButton('save', () => work(async () => {
              await call('SaveCognitionSkill', {
                ...editedDetails.value,
                scope: form.scope, slug: form.slug, name: form.name, description: form.description, instructions: form.instructions,
                workspaceId: form.scope === 'project' ? workspaceId.value : '', triggers: lines(form.triggers), allowedTools: lines(form.tools),
              });
              reset();
            }), busy.value || !form.slug || !form.name || !form.description || !form.instructions || form.scope === 'project' && !workspaceId.value),
            editing.value && localButton('cancel', reset, busy.value),
          ]),
        ]),
        data.value && !skills.length && h('p', t('cognition.emptySkills')),
        ...skills.map(skill => h('article', { class: 'product-card', key: skill.id, 'data-skill-slug':skill.slug }, [
          h('h3', skill.name),
          h('p', skill.description),
          h('p', { class: 'micro' }, t('cognition.skillMeta', { namespace: skill.namespace, version: skill.version, usage: skill.usage })),
          h('p', t('cognition.skillOutcomes',{complete:skill.completedUses??0,failed:skill.failedUses??0})),
          skill.status && h('p',t('cognition.skillStatus.'+skill.status)),
          h('code', skill.source),
          h('details', [h('summary', t('cognition.instructions')), h('pre', skill.instructions),
            skill.workflow?.length && h('ol',skill.workflow.map(step=>h('li',step))),
            skill.sourceExperience && h('p',t('cognition.sourceTask',{id:skill.sourceExperience})),
          ]),
          h('div', { class: 'product-row' }, [
            localButton('edit', () => edit(skill), busy.value),
            localButton(skill.enabled ? 'disable' : 'enable', () => change(skill, skill.enabled ? 'disable' : 'enable'), busy.value),
            localButton('versions', () => work(() => listVersions(skill)), busy.value),
            skill.status!=='deprecated' && localButton('deprecateSkill',()=>change(skill,'deprecate'),busy.value),
            skill.status!=='archived' && localButton('archive',()=>change(skill,'archive'),busy.value),
            localButton('export', () => downloadSkill(skill), busy.value),
            localButton('delete', () => change(skill, 'delete'), busy.value),
          ]),
          selectedVersions.value === skill.id && h('ul', versions.value.map(version => h('li', { key: version.version }, [
            h('details', [
              h('summary', t('cognition.version', { version: version.version, date: date(version.createdAt) })),
              h('pre', version.manifest.instructions),
              h('code', version.checksum),
              localButton('restore', () => change(skill, 'restore', version.version), busy.value || skill.version === version.version),
            ]),
          ]))),
        ])),
      ]);
    };
  },
});
