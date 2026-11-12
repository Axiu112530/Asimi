export class ServiceError extends Error { constructor(public code: string, public params: Record<string,string> = {}) { super(code); } }
let runtime: typeof import('/wails/runtime.js') | undefined;
let unsubscribe: (() => void) | undefined;
let connection = 0;
const eventSubscriptions = new Set<() => void>();
export function subscribe(name:string, listener:(data:unknown)=>void):()=>void {
  if(!runtime) throw new ServiceError('bridge.unavailable');
  const off=runtime.Events.On(name,event=>{const data=Array.isArray(event.data)?event.data[0]:event.data;listener(data);});
  let active=true;const dispose=()=>{if(!active)return;active=false;off();eventSubscriptions.delete(dispose);};eventSubscriptions.add(dispose);return dispose;
}
export function disconnect(): void {
  connection++;
  for(const dispose of [...eventSubscriptions])dispose();
  unsubscribe?.();
  unsubscribe = undefined;
  runtime = undefined;
}
export type ChangeEvent = { conversationId?: string; taskId?: string; errorCode?: string };
export async function connect(onChange: (data: ChangeEvent) => void): Promise<void> {
  disconnect();
  const pendingConnection = connection;
  try {
    const loaded = await import('/wails/runtime.js');
    if (pendingConnection !== connection) throw new ServiceError('bridge.unavailable');
    unsubscribe = loaded.Events.On('deskpilot:changed', event => {
      let data = event.data;
      if (Array.isArray(data)) data = data[0];
      if (data && typeof data === 'object') onChange(data as ChangeEvent);
    });
    runtime = loaded;
  } catch {
    if (pendingConnection === connection) disconnect();
    throw new ServiceError('bridge.unavailable');
  }
}
export async function call<T>(method: string, payload: unknown = {}): Promise<T> {
  if (!runtime) throw new ServiceError('bridge.unavailable');
  let reply: { data?: T; error?: { code: string; params?: Record<string,string> } };
  try { reply = await runtime.Call.ByName('main.DesktopService.Call', method, JSON.stringify(payload)) as typeof reply; }
  catch { throw new ServiceError('bridge.call_failed'); }
  if (reply?.error) throw new ServiceError(reply.error.code, reply.error.params);
  if (!reply || !('data' in reply)) throw new ServiceError('bridge.invalid_response');
  return reply.data as T;
}
