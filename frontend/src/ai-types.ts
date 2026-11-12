export type Capability = 'SUPPORTED'|'UNSUPPORTED'|'UNKNOWN';
export interface AICapabilities { text: Capability; vision: Capability; streaming: Capability; toolCalling: Capability; structuredOutput: Capability; computerUse: Capability; temperature: Capability; contextLimit: number|null; imageDetails: string[] }
export interface AIPricing { inputPerMillion:number; outputPerMillion:number; cachedInputPerMillion:number|null; currency:string; effectiveAt:string }
export interface AIModel { id:string; displayName:string; capabilities:AICapabilities; pricing:AIPricing|null }
export interface AIProfile { id:string; name:string; protocol:string; baseUrl:string; hasCredential:boolean; enabled:boolean; trustedHttp:boolean; timeoutSeconds?:number; models:AIModel[] }
export interface AISelection { defaultProviderId:string; defaultModelId:string; visionProviderId:string; visionModelId:string; maxOutputTokens:number; maxContextBytes:number; warningThreshold:number|null }
export interface AIAttachment { id:string; name:string; mime:string; bytes:number; width:number; height:number; sha256:string; previewDataUrl?:string }
export interface AIUsage { inputTokens:number|null; outputTokens:number|null; totalTokens:number|null; cachedInputTokens:number|null; reasoningTokens:number|null; usageAvailable:boolean; source:string }
export interface AIUsageRecord { requestId:string; conversationId:string; messageId:string; providerId:string; modelId:string; usage:AIUsage; pricing:AIPricing|null; estimatedCost:number|null; durationMs:number; firstTokenLatencyMs:number|null; httpStatus:number; status:string; errorCode?:string; createdAt:string; providerRequestId:string }
export interface AIResponse { requestId:string; content:string; status:string; errorCode?:string; usage:AIUsage; durationMs:number; firstTokenLatencyMs:number|null; httpStatus:number; providerId:string; modelId:string }
export interface AIStreamEvent { conversationId:string; messageId:string; requestId:string; sequence:number; contentDelta:string; status:string; errorCode?:string; response?:AIResponse }
export interface AIReply { requestId:string; messageId:string; conversationId:string }
