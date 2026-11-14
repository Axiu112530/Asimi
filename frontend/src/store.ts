import { reactive } from 'vue';
import { call, connect, disconnect, ServiceError, type ChangeEvent } from './bridge';
import { i18n } from './i18n';
import type { Settings, AppInfo, Conversation, Task, Approval, AuditEvent, ToolExecution, ProviderProfile, SecurityStatus, Route } from './types';
const defaults: Settings = { appLanguage:'en-US',agentLanguage:'follow-app',theme:'light',startup:'welcome',interfaceMode:'standard' };
export const state = reactive({
  route:'assistant' as Route, ready:false, connected:false, loading:false, busy:false, saving:false, error:'', errorParams:{} as Record<string,string>, notice:'',
  settings:{...defaults}, info:null as AppInfo|null, conversations:[] as Conversation[], conversation:null as Conversation|null,
  tasks:[] as Task[], task:null as Task|null, approvals:[] as Approval[], approvalHistory:[] as Approval[], audits:[] as AuditEvent[], executions:[] as ToolExecution[], providers:[] as ProviderProfile[], security:null as SecurityStatus|null,
  inspectorOpen:true, selectedProvider:'mock', securityTab:'overview', draft:'', controlStopVersion:0, controlStoppedLocally:false, activeControlActionId:'', controlEscapeAvailable:false,
});
export function fail(error: unknown) { state.error = error instanceof ServiceError ? error.code : 'app.unexpected'; state.errorParams = error instanceof ServiceError ? error.params : {}; }
export async function run(work: () => Promise<void>) { state.error=''; try { await work(); } catch(e) { fail(e); } }
export function applySettings(value: Settings) {
  state.settings=value; i18n.global.locale.value=value.appLanguage==='zh-CN'?'zh-CN':'en-US';
  const dark = value.theme==='dark' || (value.theme==='system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme=dark?'dark':'light';
}
export async function refreshSecurity() {
  const [security,approvals,history,audits,executions]=await Promise.all([
    call<SecurityStatus>('GetSecurityStatus'),call<Approval[]>('GetPendingApprovals'),call<Approval[]>('GetApprovalHistory'),call<AuditEvent[]>('GetAuditEvents'),call<ToolExecution[]>('GetToolExecutions'),
  ]);
  state.security=security;state.approvals=approvals||[];state.approvalHistory=history||[];state.audits=audits||[];state.executions=executions||[];
}
async function refreshIndex() { const [conversations,tasks]=await Promise.all([call<Conversation[]>('ListConversations'),call<Task[]>('ListTasks')]);state.conversations=conversations||[];state.tasks=tasks||[]; }
let refreshChain = Promise.resolve();
function changed(data: ChangeEvent) {
  if (data.errorCode) fail(new ServiceError(data.errorCode));
  refreshChain=refreshChain.then(async()=>{
    if (!state.connected) return;
    await Promise.all([refreshIndex(),refreshSecurity()]);
    if(!data.conversationId&&!data.taskId) applySettings(await call<Settings>('GetSettings'));
    if (state.conversation?.id===data.conversationId) {
      const c=await call<Conversation>('GetConversation',{id:data.conversationId});
      if (state.conversation?.id===c.id) state.conversation=c;
    }
    if (state.task?.id===data.taskId) {
      const task=await call<Task>('GetTask',{id:data.taskId});
      if (state.task?.id===task.id) state.task=task;
    }
  }).catch(error=>{if(state.connected)fail(error);});
}
export function disconnectStore() {
  state.connected = false;
  disconnect();
}
let connecting: Promise<void>|null=null;
export async function boot() {
  if(connecting) return connecting;
  connecting=(async()=>{state.loading=true;state.error='';try {
    await connect(changed);state.connected=true;
    applySettings(await call<Settings>('GetSettings'));
    const [info,providers]=await Promise.all([call<AppInfo>('GetAppInfo'),call<ProviderProfile[]>('ListProviders'),refreshIndex(),refreshSecurity()]);
    state.info=info;state.providers=providers||[];
    const recent = state.conversations.find(item => !item.playground || state.settings.interfaceMode === 'developer');
    if (state.settings.startup === 'last' && recent) await openConversation(recent.id);
  }catch(e){disconnectStore();fail(e);}finally{state.ready=true;state.loading=false;connecting=null;}})();return connecting;
}
export async function saveSettings(patch: Partial<Settings>) {
  const changed = (Object.keys(patch) as (keyof Settings)[]).filter(key => patch[key] !== state.settings[key]);
  if (state.saving || !changed.length) return;
  state.saving = true;
  state.notice = '';
  const route = state.route;
  const notices: Partial<Record<keyof Settings, string>> = {
    appLanguage: 'settings.savedLanguage', agentLanguage: 'settings.savedReplyLanguage',
    theme: 'settings.savedTheme', interfaceMode: 'settings.savedMode', startup: 'settings.savedStartup',
  };
  await run(async () => {
    applySettings(await call<Settings>('UpdateSettings', { ...state.settings, ...patch }));
    if (state.route === route) state.notice = changed.length === 1 ? notices[changed[0]] || 'settings.saved' : 'settings.saved';
  });
  state.saving = false;
}
export async function newConversation() {
  await run(async()=>{state.conversation=await call<Conversation>('CreateConversation');state.task=null;state.draft='';state.route='assistant';await refreshIndex();});
}
export async function openConversation(id: string, taskId?: string) {
  await run(async()=>{state.loading=true;try {
    const c=await call<Conversation>('GetConversation',{id});state.conversation=c;state.route=c.playground?'playground':'assistant';
    const tasks=c.tasks||[];const task=taskId?tasks.find(x=>x.id===taskId):tasks[0];
    state.task=task?await call<Task>('GetTask',{id:task.id}):null;state.inspectorOpen=true;
  }finally{state.loading=false;}});
}
export async function selectTask(id: string) { await run(async()=>{state.task=await call<Task>('GetTask',{id});state.inspectorOpen=true;}); }
export async function send() {
  const content=state.draft.trim();if(!content||state.busy||!state.connected)return;
  state.busy=true;state.notice='';
  await run(async()=>{
    if(!state.conversation) state.conversation=await call<Conversation>('CreateConversation');
    const reply=await call<{conversationId:string;taskId:string}>('SendMessage',{conversationId:state.conversation.id,content});
    state.draft='';state.conversation=await call<Conversation>('GetConversation',{id:reply.conversationId});
    state.task=await call<Task>('GetTask',{id:reply.taskId});state.inspectorOpen=true;
    await Promise.all([refreshIndex(),refreshSecurity()]);
  });state.busy=false;
}
export function active(task: Task) { return ['CREATED','PLANNING','WAITING_APPROVAL','RUNNING','VERIFYING','PAUSED'].includes(task.status); }
export async function decide(id: string, approve: boolean) {
  if(state.busy)return;state.busy=true;
  await run(async()=>{state.task=await call<Task>(approve?'ApproveAction':'RejectAction',{id});await Promise.all([refreshIndex(),refreshSecurity()]);if(state.conversation)state.conversation=await call<Conversation>('GetConversation',{id:state.conversation.id});});state.busy=false;
}
export async function cancel() { if(!state.task)return;await run(async()=>{state.task=await call<Task>('CancelTask',{id:state.task!.id,reason:'user'});await Promise.all([refreshIndex(),refreshSecurity()]);if(state.conversation)state.conversation=await call<Conversation>('GetConversation',{id:state.conversation.id});}); }
export async function stopComputerControl() {
  state.controlStoppedLocally = true;
  state.controlStopVersion++;
  await run(async () => { await call('StopComputerControl'); });
}
export async function stopAll() { state.controlStoppedLocally=true;state.controlStopVersion++;await run(async()=>{await call('StopAll');await Promise.all([refreshIndex(),refreshSecurity()]);if(state.task)state.task=await call<Task>('GetTask',{id:state.task.id});if(state.conversation)state.conversation=await call<Conversation>('GetConversation',{id:state.conversation.id});state.notice='tasks.stopped';}); }
