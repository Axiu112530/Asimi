import fs from 'node:fs';

const agent = {
  create: ['Start model-driven task', '启动模型驱动任务'],
  paths: ['Relevant workspace paths (optional, up to four, one per line)', '相关工作区路径（可选，最多四个，每行一个）'],
  review: ['Review the next model request', '审阅下一次模型请求'],
  send: ['Confirm and send this context', '确认并发送此上下文'],
  disclosure: ['Send the context below, including {count} selected source files, to {provider} using {model}. Review any source or test output before sending.', '将下方上下文（含 {count} 个所选源文件）发送给 {provider}，使用 {model}。请先检查源代码和测试输出。'],
  estimate: ['This request reserves up to {cost} USD. Remaining budget {remaining} USD, up to {count} requests.', '此请求预留最多 {cost} 美元。剩余预算 {remaining} 美元，最多 {count} 次请求。'],
  planHistory: ['Plan history', '计划历史'],
  limits: ['{rounds} of 5 requests used. Deadline {at}. Reserved {reserve} USD of 0.10 USD.', '已使用 {rounds}/5 次请求。截止时间 {at}。已预留 {reserve}/0.10 美元。'],
  round: ['Plan {number}', '计划 {number}'],
  roundUsage: ['Model request {ms} ms · estimated cost {cost}', '模型请求 {ms} 毫秒 · 估算费用 {cost}'],
  acknowledge: ['Confirm you have checked the current target. Uncertain actions remain in history and will never be replayed.', '请确认已核对当前目标。结果不确定的操作将保留在历史中，且不会自动重放。'],
};
const errors = {
  AGENT_PLAN_INVALID: ['The model did not return a valid plan. Review the context before trying another round.', '模型未返回有效计划。请重新审阅上下文后再发起一轮。'],
  AGENT_FILE_NOT_REVIEWED: ['The suggested patch does not match a complete, editable file in the reviewed context.', '建议的补丁与已审阅上下文中的完整可编辑文件不匹配。'],
  AGENT_SENSITIVE_PATCH: ['The patch resembles credential material and cannot be proposed through Agent Tasks.', '补丁内容疑似包含凭据，无法通过 Agent 任务提出。'],
  AGENT_EMPTY_PATCH: ['The proposed content matches the current file.', '提案内容与当前文件一致。'],
  AGENT_BUDGET_EXHAUSTED: ['This task reached its time, request or estimated cost limit.', '此任务已达到时间、请求次数或估算费用上限。'],
  AGENT_PRICING_REQUIRED: ['Set USD input and output pricing for this model before starting a budgeted Agent Task.', '请先为该模型配置以美元计价的输入与输出费率。'],
  AGENT_FILE_SCOPE_LIMIT: ['The task reached its source file limit.', '任务已达到源文件数量上限。'],
  AGENT_ACKNOWLEDGE_RECOVERY: ['Check the target and acknowledge the uncertain outcome before resuming.', '请核查目标并确认已了解不确定结果，再恢复任务。'],
};
const summaries = {
  AGENT_CONTEXT_REVIEW: ['Review the context to continue with the model.', '审阅上下文后继续请求模型。'],
  AGENT_PLANNING: ['Waiting for a plan from the model.', '正在等待模型返回计划。'],
  AGENT_ACTION_REVIEW: ['The proposed action is ready for your approval.', '提案已生成，等待你的审批。'],
};
for (const [index, locale] of ['en-US', 'zh-CN'].entries()) {
  const path = new URL(`../../frontend/src/locales/${locale}.json`, import.meta.url);
  const data = JSON.parse(fs.readFileSync(path, 'utf8'));
  const translated = pairs => Object.fromEntries(Object.entries(pairs).map(([key, texts]) => [key, texts[index]]));
  data.agent02 = translated(agent);
  Object.assign(data.errors, translated(errors));
  Object.assign(data.product.summary, translated(summaries));
  Object.assign(data.tasks.status, index ? { NEEDS_REVIEW: '等待审阅上下文', INTERRUPTED: '已中断' } : { NEEDS_REVIEW: 'Context review needed', INTERRUPTED: 'Interrupted' });
  fs.writeFileSync(path, JSON.stringify(data, null, 2) + '\n');
}
