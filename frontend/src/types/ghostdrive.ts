// GhostDrive — Types TypeScript partagés (Go ↔ Frontend)
// Source : contracts/typescript-types.ts — ne pas modifier manuellement.
// Version : 0.1.0

export type FileEventType = 'created' | 'modified' | 'deleted' | 'renamed';
export type SyncStatus = 'idle' | 'syncing' | 'paused' | 'error';
export type TransferDirection = 'upload' | 'download';
export type FileSource = 'local' | 'remote';
export type BackendType = 'webdav' | 'moosefs';

export interface FileInfo {
  name: string;
  path: string;
  size: number;
  isDir: boolean;
  modTime: string;
  etag: string;
  isPlaceholder: boolean;
  isCached: boolean;
}

export interface FileEvent {
  type: FileEventType;
  path: string;
  oldPath?: string;
  timestamp: string;
  source: FileSource;
}

export interface SyncError {
  path: string;
  message: string;
  time: string;
}

export interface SyncState {
  status: SyncStatus;
  progress: number;
  currentFile: string;
  pending: number;
  errors: SyncError[];
  lastSync: string;
}

export interface BackendConfig {
  id: string;
  name: string;
  type: BackendType;
  enabled: boolean;
  params: Record<string, string>;
  syncDir: string;
}

export interface BackendStatus {
  backendId: string;
  connected: boolean;
  error?: string;
  freeSpace: number;
  totalSpace: number;
}

export interface ProgressEvent {
  path: string;
  direction: TransferDirection;
  bytesDone: number;
  bytesTotal: number;
  percent: number;
}

export interface CacheStats {
  sizeMB: number;
  fileCount: number;
  maxSizeMB: number;
}

export interface AppConfig {
  version: string;
  backends: BackendConfig[];
  cacheEnabled: boolean;
  cacheDir: string;
  cacheSizeMaxMB: number;
  startMinimized: boolean;
  autoStart: boolean;
}

export type WailsEventMap = {
  'sync:state-changed': SyncState;
  'sync:progress': ProgressEvent;
  'sync:file-event': FileEvent;
  'sync:error': SyncError;
  'backend:status-changed': BackendStatus;
  'placeholder:hydration-started': { path: string; size: number };
  'placeholder:hydration-done': { path: string };
  'app:ready': { version: string; backendsCount: number };
};

export type WailsEventName = keyof WailsEventMap;
export type WailsEventPayload<T extends WailsEventName> = WailsEventMap[T];
