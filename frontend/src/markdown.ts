import { defineComponent, h, type VNodeChild } from 'vue';
import { t } from './i18n';
// Text nodes only: no HTML, embedded media, external links or network requests.
function inline(text: string): VNodeChild[] {
  return text.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*)/g).map((part,i)=>part.startsWith('`')&&part.endsWith('`') ? h('code',{key:i},part.slice(1,-1)) : part.startsWith('**')&&part.endsWith('**') ? h('strong',{key:i},part.slice(2,-2)) : part);
}
export const Markdown = defineComponent({
  props: { content:{type:String,required:true} },
  setup(props){return ()=>{
    const out: VNodeChild[]=[];const lines=props.content.replace(/\r\n/g,'\n').split('\n');let code:string[]|null=null;let language='';let paragraph:string[]=[];
    const flush=()=>{if(paragraph.length){out.push(h('p',inline(paragraph.join('\n'))));paragraph=[];}};
    for(const line of lines){
      if(/^\s*```/.test(line)){flush();if(code){out.push(h('div',{class:'code-block'},[h('div',{class:'code-label'},language||t('assistant.codeBlock')),h('pre',[h('code',code.join('\n'))])]));code=null;}else{code=[];language=line.trim().slice(3).trim();}continue;}
      if(code){code.push(line);continue;}
      const heading=line.match(/^(#{1,6})\s+(.+)$/),list=line.match(/^\s*(?:[-*]|\d+\.)\s+(.+)$/);
      if(!line.trim()){flush();continue;}
      if(heading){flush();out.push(h('h'+Math.min(heading[1].length+1,6),inline(heading[2])));}
      else if(list){flush();out.push(h('div',{class:'markdown-item'},[h('span',{class:'list-dot','aria-hidden':'true'}),h('span',inline(list[1]))]));}
      else if(line.startsWith('> ')){flush();out.push(h('blockquote',inline(line.slice(2))));}
      else paragraph.push(line);
    }
    flush();if(code)out.push(h('pre',[h('code',code.join('\n'))]));
    return h('div',{class:'markdown'},out);
  };},
});
