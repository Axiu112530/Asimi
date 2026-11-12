import { reactive } from 'vue';
import { call } from './bridge';
import { fail } from './store';
import type { ProductState,ProductAction,ProjectReport,SourceFile,Diagnostic } from './product-types';

export const product=reactive({ready:false,busy:false,error:'',data:{workspaces:[],agentTasks:[],skills:[],actions:[],onboarding:{completed:false},commandCatalog:[]} as ProductState,selectedWorkspace:'',selectedTask:'',exclusions:[] as string[],report:null as ProjectReport|null,file:null as SourceFile|null,diagnostics:[] as Diagnostic[],selectedAction:null as ProductAction|null,showOnboarding:false});
export async function productWork(work:()=>Promise<void>){if(product.busy)return;product.busy=true;product.error='';try{await work();}catch(e){product.error=e instanceof Error?e.message:String(e);fail(e);}finally{product.busy=false;}}
export async function refreshProduct(){const data=await call<ProductState>('GetMVPState');product.data=data;product.ready=true;if(!data.workspaces.some(w=>w.id===product.selectedWorkspace))selectWorkspace(data.workspaces[0]?.id||'');}
export function selectWorkspace(id:string){if(product.selectedWorkspace===id)return;product.selectedWorkspace=id;product.selectedTask='';product.exclusions=[];product.report=null;product.file=null;product.selectedAction=null;}
export async function pickWorkspace(){await productWork(async()=>{const w=await call<{id:string}>('PickWorkspace');await refreshProduct();if(w)selectWorkspace(w.id);});}
export async function analyzeWorkspace(query=''){const id=product.selectedWorkspace;await productWork(async()=>{const r=await call<ProjectReport>('AnalyzeWorkspace',{workspaceId:id,query,exclusions:[...product.exclusions]});if(product.selectedWorkspace===id)product.report=r;});}
export async function readSource(path:string){const id=product.selectedWorkspace;await productWork(async()=>{const f=await call<SourceFile>('ReadWorkspaceFile',{workspaceId:id,path,exclusions:[...product.exclusions]});if(product.selectedWorkspace===id)product.file=f;});}
export async function approveProductAction(action:ProductAction, acceptNetwork=false){if(action.status!=='PENDING'||Date.parse(action.expiresAt)<=Date.now())return;await productWork(async()=>{product.selectedAction=await call<ProductAction>('ApproveProductAction',{id:action.id,hash:action.hash,...(acceptNetwork?{acceptNetwork}: {})});await refreshProduct();});}
export async function rejectProductAction(action:ProductAction){await productWork(async()=>{product.selectedAction=await call<ProductAction>('RejectProductAction',{id:action.id});await refreshProduct();});}
export async function diagnoseEnvironment(){await productWork(async()=>{product.diagnostics=await call<Diagnostic[]>('DiagnoseEnvironment');});}
