export interface Settings { appLanguage: 'en-US'|'zh-CN'; agentLanguage: 'follow-app'|'auto'|'en-US'|'zh-CN'; theme: 'light'|'dark'|'system'; startup: 'welcome'|'last' }
export interface AppInfo { version: string; goVersion: string; wailsVersion: string; dataDir: string; workspace: string; mode: string; provider: string; model: string }
import type { ScreenMetadata, VisionResult } from './screen-types';
export interface Message { id: string; conversationId: string; role: string; content: string; createdAt: string; taskId?: string; providerId?:string; modelId?:string; requestId?:string; status?:string; errorCode?:string; attachments?:import('./ai-types').AIAttachment[]; usage?:import('./ai-types').AIUsage; screenCapture?:ScreenMetadata; vision?:VisionResult }
export interface Conversation { id: string; title: string; createdAt: string; updatedAt: string; messages: Message[]; tasks: Task[]; playground?:boolean }
export interface Step { id: string; nameKey: string; descriptionKey: string; status: string; toolName?: string; result?: string; errorCode?: string }
export interface Operation { taskId: string; toolName: string; fileName: string; normalizedPath: string; content: string; contentHash: string; bytes: number; operation: string; expiresAt: string; approvalId: string }
export interface Approval { id: string; operation: Operation; status: string; createdAt: string; expiresAt: string; decidedAt?: string }
export interface Verification { path: string; expectedBytes: number; actualBytes: number; expectedHash: string; actualHash: string; contentMatched: boolean; passed: boolean; created: boolean; code?: string }
export interface Task { id: string; conversationId: string; title: string; description: string; status: string; currentStep: string; steps: Step[]; approval?: Approval; verification?: Verification; summary?: string; errorCode?: string; createdAt: string; updatedAt: string }
export interface AuditEvent { id: string; taskId?: string; code: string; params?: Record<string,string>; createdAt: string }
export interface ToolExecution { id: string; taskId: string; toolName: string; path: string; status: string; result?: Verification; errorCode?: string; createdAt: string; updatedAt: string }
export interface Model { providerId: string; id: string; text: string; vision: string; toolCalling: string; streaming: string; computerUse: string; structuredOutput: string; contextLength: number; pricingMetadata: Record<string,unknown> }
export interface ProviderProfile { id: string; name: string; baseUrl: string; enabled: boolean; status: string; models: Model[] }
export interface SecurityStatus { workspace: string; maxFileBytes: number; toolNames: string[]; pendingApprovals: number; activeTasks: number; stopAvailable: boolean; externalNetwork: boolean; desktopControl: boolean }
export type Route = 'home'|'assistant'|'computer'|'developer'|'workspaces'|'workflows'|'skills'|'models'|'security'|'history'|'settings'|'playground'|'usage';
