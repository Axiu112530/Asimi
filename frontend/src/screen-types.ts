export interface ScreenRect { x: number; y: number; width: number; height: number }

export interface ScreenDisplay {
  id: string;
  name: string;
  primary: boolean;
  physicalBounds: ScreenRect;
  logicalBounds: ScreenRect;
  dpi: number;
  scale: number;
}

export interface CaptureCapabilities {
  backend: string;
  display: boolean;
  region: boolean;
  window: boolean;
  control: boolean;
  maxPixels: number;
  maxDimension: number;
  maxBytes: number;
  ttlSeconds: number;
}

interface CaptureDetails {
  captureId: string;
  displayId: string;
  displayName: string;
  physicalBounds: ScreenRect;
  logicalBounds: ScreenRect;
  width: number;
  height: number;
  dpi: number;
  format: string;
  byteSize: number;
  capturedAt: string;
  backend: string;
  sha256: string;
}

export interface ScreenPreview extends CaptureDetails {
  previewDataUrl: string;
  expiresAt: string;
}

export interface ScreenMetadata extends CaptureDetails {
  providerId: string;
  modelId: string;
  requestId: string;
  conversationId: string;
}

export interface ScreenUploadReview {
  consentId: string;
  taskId?: string;
  suggestionSelector?: string;
  providerName: string;
  modelId: string;
  apiHost: string;
  bytes: number;
  sha256: string;
  expiresAt: string;
}

export interface ScreenAnalysisInput {
  consentId: string;
  content: string;
  providerId: string;
  modelId: string;
  imageDetail: string;
  conversationId?: string;
  confirmProviderSwitch: boolean;
  stream: boolean;
  keepComputerOpen?: boolean;
}

export interface VisionResult {
  parsed: boolean;
  summary?: string;
  detectedText?: string[];
  applications?: string[];
  windows?: string[];
  uiElements?: { type: string; label: string; bounds?: ScreenRect; confidence?: number; description?: string }[];
  issues?: string[];
  suggestions?: string[];
  uncertainties?: string[];
}
