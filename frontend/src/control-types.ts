import type { ScreenRect } from './screen-types';

export type ControlApp = 'test_host' | 'notepad';
export type ControlAction = 'app.launch_test_host' | 'window.focus' | 'uia.invoke' | 'uia.set_value' | 'keyboard.type_text';

export interface ControlStatus {
  stopped: boolean;
  available: boolean;
  permissionMode: string;
  rawInput: boolean;
  notepadText: boolean;
  notepadTextStatus?: 'CONDITIONAL';
}

export interface ControlWindow {
  id: string;
  app: ControlApp;
  hwnd: string;
  pid: number;
  creationTime: string;
  imagePath: string;
  imageHash?: string;
  packageFamily?: string;
  appUserModelId?: string;
  sessionId: number;
  integrity: number;
  className: string;
  title: string;
  bounds: ScreenRect;
  foreground: boolean;
  observedAt: string;
}

export interface ControlElement {
  id: string;
  name: string;
  automationId: string;
  controlType: string;
  bounds: ScreenRect;
  enabled: boolean;
  offscreen: boolean;
  focused: boolean;
  password: boolean;
  processId: number;
  nativeHandle: string;
  patterns: string[] | null;
  parentPath: string;
  runtimeId: string;
  depth: number;
  blank: boolean;
  valueSha256?: string;
  valueWritable?: boolean;
}

export interface ControlObservation {
  id: string;
  window: ControlWindow;
  elements: ControlElement[] | null;
  truncated: boolean;
  modal: boolean;
  observedAt: string;
  expiresAt?: string;
}

export interface ControlProposal {
  id: string;
  taskId?: string;
  observationId?: string;
  sourceRequestId?: string;
  type: ControlAction;
  window?: ControlWindow;
  element?: ControlElement;
  text?: string;
  expected: string;
  observedAt: string;
  expiresAt: string;
  hash: string;
  targetHash: string;
  policyVersion: string;
  status: string;
}

export interface ComputerSuggestionTarget {
  observationId: string;
  elementId: string;
  selector: string;
  taskId: string;
}

export interface ComputerSuggestion {
  requestId: string;
  conversationId: string;
  selector: string;
  observedAt: string;
  actionType: 'uia.invoke' | 'uia.set_value';
  actionId?: string;
  summary?: string;
  status: string;
}

export interface ControlRecord {
  id: string;
  type: ControlAction;
  status: string;
  code?: string;
  at: string;
  verified: boolean;
}
