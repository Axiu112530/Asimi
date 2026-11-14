import { defineComponent, h, ref, watch, type PropType } from 'vue';
import { call } from './bridge';
import { product, productWork, refreshProduct } from './product-store';
import { t, date } from './i18n';
import { badge, button, errorText } from './ui';
import type { AgentTask } from './product-types';

interface Preview {
  localSkill?: {namespace: string; version: number; tool: string; args: string[]};
  taskId: string;
  provider: string;
  endpoint: string;
  model: string;
  files: string[];
  content: string;
  hash: string;
  maxRequestUsd: number;
  remainingUsd: number;
  remainingRequests: number;
  knowledgeMatches?: {id:string;namespace:string;version:number;reason:string;bytes:number;included:boolean}[];
  knowledgeBytes: number;
  inputTokenUpperBound: number;
  systemInstruction: string;
}

export const Agent02Panel = defineComponent({
  props: { task: { type: Object as PropType<AgentTask>, required: true } },
  setup(props) {
    const preview = ref<Preview | null>(null);
    const correction = ref('');
    watch(() => props.task.updatedAt, () => { preview.value = null; });

    function loadPreview() {
      return productWork(async () => {
        preview.value = await call<Preview>('PreviewAgentRun', { id: props.task.id });
      });
    }
    function changeSkill(review: Preview, skillId: string, event: Event) {
      const mode = (event.target as HTMLSelectElement).value;
      return productWork(async () => {
        preview.value = null;
        await call('SetAgentCognitionChoice', {id:review.taskId,skillId,mode});
        await refreshProduct();
      });
    }
    function correctTask() {
      return productWork(async () => {
        await call('SetAgentCognitionCorrection',{id:props.task.id,correction:correction.value});
        correction.value = '';
        preview.value = null;
        await refreshProduct();
      });
    }
    function sendPlan(review: Preview) {
      return productWork(async () => {
        preview.value = null;
        await call('PlanAgentRun', { id: review.taskId, contextHash: review.hash });
        await refreshProduct();
      });
    }
    const action = (key: string, id: string, run: () => unknown) => h('button', {
      class: 'button primary', type: 'button', disabled: product.busy, 'data-testid': id, onClick: run,
    }, t('agent02.' + key));

    return () => {
      const run = props.task.agent;
      if (!run) return null;
      const review = preview.value;
      return h('section', { class: 'product-card', 'data-testid': 'agent02-run', 'data-task-id': props.task.id }, [
        run.acceptance ? h('div', { 'data-testid': 'agent02-acceptance' }, [
          h('h3', t('agent02.acceptance')),
          h('p', t('agent02.requirement.' + run.acceptance.kind)),
          run.acceptance.paths?.length ? h('code', run.acceptance.paths.join(', ')) : null,
          run.acceptance.commandId ? h('p', h('code', [run.acceptance.commandId, ...(run.acceptance.args ?? [])].join(' '))) : null,
          run.verification?.conditions.length ? h('ul', run.verification.conditions.map(condition =>
            h('li', { key: condition.kind }, [
              t('agent02.condition.' + condition.kind), ' · ', t('agent02.evidenceStatus.' + condition.status),
              condition.evidence?.length ? h('details', [
                h('summary', t('agent02.evidence')),
                h('ul', condition.evidence.map(ref => h('li', { key: ref }, h('code', ref)))),
              ]) : null,
            ]))) : null,
        ]) : h('p', { class: 'micro muted' }, t('agent02.legacyAcceptance')),
        h('h3', t('agent02.planHistory')),
        h('p', { class: 'micro muted' }, run.limits
          ? t('preset.runLimits', { rounds: run.rounds.length, requests: run.limits.requests, at: date(run.deadline), reserve: run.reservedUsd.toFixed(4), budget: run.limits.budgetUsd })
          : t('agent02.limits', { rounds: run.rounds.length, at: date(run.deadline), reserve: run.reservedUsd.toFixed(4) })),
        ...run.rounds.map(round => h('details', { key: round.version }, [
          h('summary', [t('agent02.round', { number: round.version }), ' ', badge(round.status)]),
          round.skill ? h('p', t('agent02.localSkill', { name: round.skill.namespace, version: round.skill.version, command: [round.skill.tool, ...round.skill.args].join(' ') })) : null,
          round.error ? h('p', { role: 'alert', class: 'ai-error' }, errorText(round.error)) : null,
          round.plan ? h('ul', round.plan.steps.map(step => h('li', [h('code', step.type), ' ', step.summary || step.path || step.commandId || '']))) : null,
          !round.skill ? h('p', { class: 'micro muted' }, t('agent02.roundUsage', { ms: round.durationMs, cost: round.estimatedUsd == null ? t('common.unknown') : round.estimatedUsd.toFixed(6) + ' USD' })) : null,
          h('code', { class: 'product-hash' }, round.contextHash),
        ])),
        props.task.status === 'NEEDS_REVIEW' && h('details',[
          h('summary',t('cognition.correctTask')),
          h('textarea',{'aria-label':t('cognition.correctTask'),value:correction.value,maxlength:1200,
            onInput:(event:Event)=>{correction.value=(event.target as HTMLTextAreaElement).value;}}),
          button('cognition.save',correctTask,{class:'button secondary',disabled:product.busy||!correction.value.trim()}),
        ]),
        props.task.status === 'NEEDS_REVIEW' && !review ? action('review', 'agent02-review', loadPreview) : null,
        review ? h('div', { 'data-testid': 'agent02-preview' }, [
          h('h3', t('agent02.review')),
          review.localSkill ? h('p', t('agent02.localSkill', {
            name: review.localSkill.namespace, version: review.localSkill.version,
            command: [review.localSkill.tool, ...review.localSkill.args].join(' '),
          })) : [
            h('p', t('agent02.disclosure', { provider: review.provider, model: review.model, count: review.files.length })),
            h('code', review.endpoint),
            h('p', t('agent02.estimate', { cost: review.maxRequestUsd.toFixed(6), remaining: review.remainingUsd.toFixed(4), count: review.remainingRequests })),
          ],
          h('pre', {'data-testid':'agent02-request-body'}, review.content),
          h('details',[
            h('summary',t('cognition.localKnowledge')),
            h('p',t('cognition.knowledgeBudget',{bytes:review.knowledgeBytes,tokens:review.inputTokenUpperBound})),
            h('ul',(review.knowledgeMatches??[]).map(skill=>h('li',{key:skill.id},[
              h('label',[
                t('cognition.skillMatch',{name:skill.namespace,version:skill.version,bytes:skill.bytes})+' · '+t('cognition.matchReason.'+skill.reason)+' · '+t(skill.included?'cognition.injected':'cognition.omitted')+' ',
                h('select',{value:skill.reason==='manual'?'include':skill.reason==='excluded'?'exclude':'auto',disabled:product.busy,
                  onChange:(event:Event)=>changeSkill(review,skill.id,event)},['auto','include','exclude'].map(mode=>h('option',{value:mode},t('cognition.skillChoice.'+mode)))),
              ]),
            ]))),
          ]),
          h('details',[h('summary',t('cognition.systemInstruction')),h('pre',review.systemInstruction)]),
          action(review.localSkill ? 'prepareLocalSkill' : 'send', 'agent02-send', () => sendPlan(review)),
        ]) : null,
      ]);
    };
  },
});
