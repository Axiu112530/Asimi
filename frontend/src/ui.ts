import { h, type VNode, type VNodeChild } from 'vue';
import { i18n, known, t } from './i18n';
const paths: Record<string,string[]> = {
  logo:['M5 5h9a5 5 0 0 1 0 10H8','M5 5v14','M8 11h6','M15 15l4 4'],
  assistant:['M8 3h8l4 5v9H4V8l4-5Z','M8 17v4','M16 17v4','M8 10h.01','M16 10h.01','M9 14h6'],
  computer:['M3 4h18v13H3z','M8 21h8','M12 17v4'], developer:['m8 7-5 5 5 5','m16 7 5 5-5 5','m14 4-4 16'],
  workflows:['M3 3h6v6H3z','M15 15h6v6h-6z','M15 3h6v6h-6z','M9 6h6','M6 9v9h9'],
  models:['m12 3 9 5-9 5-9-5 9-5Z','m3 12 9 5 9-5','m3 16 9 5 9-5'], security:['m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z','m8 12 3 3 5-6'],
  history:['M3 11a9 9 0 1 1 2 7','M3 4v7h7','M12 7v5l3 2'], settings:['M9 3h6l1 4 4 1v8l-4 1-1 4H9l-1-4-4-1V8l4-1 1-4Z','M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z'],
  memory:['M5 4h14v16H5z','M8 8h8','M8 12h8','M8 16h5'], skills:['M5 19v-2l10-10 2 2L7 19H5Z','m14 8 2 2','M17 4v3','M19 6h-3'],
  learning:['M3 6h7l2 2 2-2h7v13h-7l-2 2-2-2H3z','M12 8v13'], growth:['M4 19h16','M6 16l4-5 4 2 5-7','M15 6h4v4'],
  plus:['M12 5v14','M5 12h14'], arrow:['M5 12h14','m13 6 6 6-6 6'], stop:['M6 6h12v12H6z'], chevron:['m9 5 7 7-7 7'], close:['m6 6 12 12','M18 6 6 18'],
  check:['m5 12 4 4L19 6'], clock:['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z','M12 7v5l3 2'], file:['M5 3h9l5 5v13H5z','M14 3v5h5','M8 13h8','M8 17h5'],
  lock:['M5 10h14v11H5z','M8 10V7a4 4 0 0 1 8 0v3'], globe:['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z','M3 12h18','M12 3c5 4 5 14 0 18-5-4-5-14 0-18Z'],
  panel:['M3 4h18v16H3z','M14 4v16'], spark:['m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3Z'], info:['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z','M12 11v6','M12 7h.01'],
  warning:['m12 3 10 18H2L12 3Z','M12 9v5','M12 17h.01'], menu:['M4 6h16','M4 12h16','M4 18h16'], refresh:['M4 10a8 8 0 1 1 2 9','M4 4v6h6'],
};
export function icon(name: string, size = 20): VNode { return h('svg',{width:size,height:size,viewBox:'0 0 24 24',fill:'none',stroke:'currentColor','stroke-width':1.65,'stroke-linecap':'round','stroke-linejoin':'round','aria-hidden':'true',class:'icon'},(paths[name]||paths.info).map(d=>h('path',{d}))); }
export function badge(status: string): VNode { return h('span',{class:['badge','state-'+status.toLowerCase()],'data-testid':'task-status'},[h('span',{class:'status-dot'}),known('tasks.status.'+status)]); }
export function button(key: string, action: () => unknown, opts: Record<string,unknown> = {}, children?: VNodeChild): VNode {
  return h('button',{type:'button',...opts,onClick:action},[children||t(key)]);
}
export function iconButton(name: string,key: string,action: () => unknown,opts: Record<string,unknown> = {}): VNode { return button(key,action,{class:'icon-button',title:t(key),'aria-label':t(key),...opts},icon(name)); }
export function errorText(code: string, params: Record<string,string> = {}): string { return i18n.global.te('errors.'+code,'en-US') ? t('errors.'+code,params) : t('errors.generic',{code}); }
export function detail(key: string,value: VNodeChild,mono=false): VNode { return h('div',{class:'detail-row'},[h('dt',t(key)),h('dd',{class:{mono}},[value])]); }
export function section(key: string,children: VNodeChild, cls=''): VNode { return h('section',{class:['inspector-section',cls]},[h('h3',t(key)),children]); }
