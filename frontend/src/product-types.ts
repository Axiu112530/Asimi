export interface Workspace { id:string; name:string; root:string; createdAt:string; lastUsedAt?:string; trustStatus?:string }
export interface ProjectFile { path:string; size:number; language?:string; type?:string; directory?:boolean; symbols?:string[] }
export interface SourceFile { path?:string; relativePath?:string; content:string; language?:string; hash?:string; symbols?:unknown[] }
export interface ProjectReport { entries:ProjectFile[]; languages?:unknown; dependencies?:unknown[]; symbols?:unknown[]; calls?:unknown[]; matches?:unknown[]; truncated?:boolean; summary?:string; [key:string]:unknown }
export interface FileChange { relativePath:string; operation:string; beforeContent:string; afterContent:string; beforeHash:string; afterHash:string; diff:string }
export interface CommandProposal { id:string; executable:string; args:string[]; directory:string; network:boolean; executesCode:boolean; timeoutSeconds:number; maxOutputBytes:number; hash:string }
export interface ProductAction { id:string; taskId?:string; kind:string; status:string; hash:string; expiresAt:string; createdAt:string; updatedAt:string; proposal:{workspaceId:string;root:string;file?:FileChange;command?:CommandProposal;policyVersion?:string;planVersion?:number;stepId?:string;gitStatus?:string;gitDirtyFile?:boolean}; result?:unknown; error?:string }
export interface AgentStep { id:string; kind:string; title:string; status:string; result?:unknown; error?:string; actionId?:string }
export interface AgentTask { id:string; workspaceId:string; conversationId?:string; goal:string; status:string; currentStep:number|string; steps:AgentStep[]; summary:string; createdAt:string; updatedAt:string; agent?:AgentRun }
export interface AgentRound {version:number;status:string;contextHash:string;files:string[];actionId?:string;durationMs:number;estimatedUsd:number|null;error?:string;plan?:{objective:string;assumptions:string[];expectedResults:string[];riskLevel:string;steps:{type:string;summary?:string;path?:string;commandId?:string}[]}}
export interface AgentRun {providerId:string;modelId:string;paths:string[];deadline:string;reservedUsd:number;rounds:AgentRound[];limits?:{requests:number;budgetUsd:number}}
export interface Skill { id:string; name:string; description:string; version?:string; inputSchema?:unknown; outputSchema?:unknown; tools:string[]; risk:string; schema:unknown; enabled:boolean }
export interface CommandSpec { id:string; program:string; args:string[]; executesCode:boolean; network:boolean; downloadUrl?:string; installPreview?:string[] }
export interface Diagnostic { component:string; installed:boolean; version:string; executable:string; status:string; problems:string[]; suggestions:string[]; downloadUrl:string; installPreview?:string[] }
export interface ProductState { workspaces:Workspace[]; agentTasks:AgentTask[]; skills:Skill[]; actions:ProductAction[]; onboarding:{completed:boolean}; commandCatalog:CommandSpec[]; tools?:unknown[] }
