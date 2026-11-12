import { defineComponent, h } from 'vue';
import { state, active, cancel, decide } from './store';
import { date, known, number, t } from './i18n';
import { badge, button, detail, errorText, icon, iconButton, section } from './ui';
import { Markdown } from './markdown';
import type { Approval, Verification } from './types';
export function approvalPreview(approval: Approval, controls=false) {
  const op=approval.operation;const expired=Date.parse(approval.expiresAt)<=Date.now();
  return h('div',{class:'approval-preview','data-approval-id':approval.id},[
    h('div',{class:'approval-heading'},[icon('lock',18),h('strong',t('approvals.title'))]),h('p',{class:'muted'},t('approvals.intro')),
    h('dl',{class:'approval-fields'},[
      detail('approvals.operation',op.operation==='create'?t('approvals.create'):op.operation),detail('approvals.task',op.taskId,true),detail('approvals.id',approval.id,true),detail('common.tool',op.toolName,true),detail('common.path',op.normalizedPath,true),detail('approvals.size',t('common.bytes',{count:number(op.bytes)})),detail('common.hash',op.contentHash,true),
      detail('approvals.expires',h('time',{datetime:approval.expiresAt,title:approval.expiresAt},date(approval.expiresAt))),
    ]),
    h('h4',t('approvals.content')),h('pre',{class:'content-preview','data-testid':'approval-content'},h('code',op.content||t('approvals.contentEmpty'))),
    h('p',{class:'micro muted'},t('approvals.bound')),h('p',{class:'micro muted'},t('approvals.expiresHint')),
    expired && approval.status==='PENDING'?h('p',{class:'inline-error'},t('approvals.expired')):null,
    controls?h('div',{class:'approval-actions'},[
      button('approvals.reject',()=>decide(approval.id,false),{class:'button secondary',disabled:state.busy,'data-testid':'reject'}),
      button('approvals.approve',()=>decide(approval.id,true),{class:'button primary',disabled:state.busy||expired,'data-testid':'approve'},[icon('check',16),t('approvals.approve')]),
    ]):null,
  ]);
}
export function verificationDetails(v: Verification, recoveryRequired = false) {
  const pending = recoveryRequired && !v.passed;
  return h('div',{class:['verification',v.passed?'passed':'failed']},[
    h('div',{class:'verification-heading'},[icon(v.passed?'check':'warning',18),h('strong',t(pending?'tasks.verificationPending':v.passed?'tasks.verified':'tasks.verifyFailed'))]),
    h('dl',[
      detail('common.path',v.path,true),detail('tasks.fileCreated',t(v.created?'common.yes':pending?'common.unknown':'common.no')),detail('tasks.contentMatched',t(v.contentMatched?'common.yes':pending&&!v.actualHash?'common.unknown':'common.no')),
      detail('tasks.expectedBytes',number(v.expectedBytes)),detail('tasks.actualBytes',number(v.actualBytes)),detail('tasks.expectedHash',v.expectedHash,true),detail('tasks.actualHash',v.actualHash,true),
    ]),v.code?h('p',{class:'inline-error'},errorText(v.code)):null,
  ]);
}
export const Inspector = defineComponent({setup(){return ()=>{
  const task=state.task;const executions=state.executions.filter(x=>x.taskId===task?.id);const current=task?.steps?.find(s=>s.id===task.currentStep || s.nameKey===task.currentStep);
  return h('aside',{class:'inspector','aria-label':t('accessibility.taskInspector')},[
    h('header',{class:'inspector-header'},[h('div',[h('h2',t('tasks.title')),h('p',t('tasks.subtitle'))]),iconButton('panel','tasks.collapse',()=>state.inspectorOpen=false,{'data-testid':'inspector-toggle'})]),
    task?h('div',{class:'inspector-scroll'},[
      h('section',{class:'inspector-section task-overview'},[
        badge(task.status),h('h3',{class:'task-title'},task.title),h('p',{class:'muted'},task.description),
        h('dl',[detail('tasks.taskId',task.id,true),detail('tasks.currentStep',current?known(current.nameKey):t('common.none'))]),
        task.errorCode?h('p',{class:'inline-error'},errorText(task.errorCode)):null,
      ]),
      section('tasks.plan',h('ol',{class:'plan-list'},(task.steps||[]).map((step,index)=>h('li',{key:step.id,class:['plan-step','step-'+step.status.toLowerCase()]},[
        h('span',{class:'step-marker','aria-hidden':'true'},['COMPLETED','DONE','SUCCEEDED'].includes(step.status)?icon('check',13):String(index+1)),
        h('div',{class:'step-content'},[h('div',{class:'step-heading'},[h('h4',known(step.nameKey)),badge(step.status)]),h('p',{class:'micro muted'},known(step.descriptionKey)),step.toolName?h('code',{class:'tool-name'},step.toolName):null,step.result?h('p',{class:'micro'},step.result):null,step.errorCode?h('p',{class:'inline-error micro'},errorText(step.errorCode)):null]),
      ])))),
      task.approval?section(['PENDING','APPROVED'].includes(task.approval.status)?'tasks.approval':'tasks.approvalRecord',task.approval.status==='PENDING' && task.status==='WAITING_APPROVAL'?approvalPreview(task.approval,true):h('div',[badge(task.approval.status),h('p',{class:'micro muted'},t('approvals.id')+': '+task.approval.id)]),'approval-section'):null,
      section('tasks.execution',executions.length?executions.map(ex=>h('div',{class:'execution-record',key:ex.id},[h('div',{class:'split'},[h('code',ex.toolName),badge(ex.status)]),h('p',{class:'mono micro'},ex.path),h('time',{class:'micro muted',datetime:ex.updatedAt},date(ex.updatedAt)),ex.errorCode?h('p',{class:'inline-error'},errorText(ex.errorCode)):null])):h('p',{class:'micro muted'},t('tasks.noExecution'))),
      section('tasks.verification',task.verification?verificationDetails(task.verification,task.status==='RECOVERY_REQUIRED'):h('p',{class:'micro muted'},t('tasks.noVerification'))),
      section('tasks.summary',task.summary?h(Markdown,{content:task.summary}):h('p',{class:'micro muted'},t('tasks.noSummary'))),
      section('tasks.updatedAt',h('time',{class:'micro muted',datetime:task.updatedAt},date(task.updatedAt))),
    ]):h('div',{class:'inspector-empty'},[h('div',{class:'empty-symbol'},icon('workflows',28)),h('h3',t('tasks.empty')),h('p',t('tasks.emptyHint'))]),
    task&&active(task)?h('footer',{class:'inspector-footer'},button('tasks.cancel',cancel,{class:'button danger-subtle full','data-testid':'cancel-task'},[icon('stop',15),t('tasks.cancel')])):null,
  ]);
};}});
