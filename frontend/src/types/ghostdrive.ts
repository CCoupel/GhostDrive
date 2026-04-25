// GhostDrive — Types TypeScript partagés (Go ↔ Frontend)
// Version : 0.2.0

export type FileEventType = 'created' | 'modified' | 'deleted' | 'renamed';
export type SyncStatus = 'idle' | 'syncing' | 'paused' | 'error';
export type TransferDirection = 'upload' | 'download';
export type FileSource = 'local' | 'remote';
export type BackendType = 'webdav' | 'moosefs' | 'local';
export type TrayAction = 'open' | 'settings' | 'pause' | 'sync' | 'quit';

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
  backendId?: string;
}

export interface SyncError {
  backendId?: string;
  path: string;
  message: string;
  time: string;
}

export interface BackendSyncState {
  backendId: string;
  backendName: string;
  status: SyncStatus;
  progress: number;
  currentFile: string;
  pending: number;
  errors: SyncError[];
  lastSync: string;
}

export interface SyncState {
  status: SyncStatus;
  progress: number;
  currentFile: string;
  pending: number;
  errors: SyncError[];
  lastSync: string;
  backends: BackendSyncState[];
  activeTransfers: ProgressEvent[];
}

export interface BackendConfig {
  id: string;
  name: string;
  type: BackendType;
  enabled: boolean;
  /** Si true, la sync démarre automatiquement à la connexion du backend (défaut: false) */
  autoSync: boolean;
  params: Record<string, string>;
  syncDir: string;
  remotePath: string;
  /** Chemin local sur le PC (vide = Auto, le backend calcule). Ajouté v0.4.0 #51 */
  localPath: string;
  /** Avertissement non bloquant retourné par le backend (ex. rootPath déjà utilisé) */
  warning?: string;
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
  'tray:open-settings': undefined;
  'tray:action': { action: TrayAction };
};

export type WailsEventName = keyof WailsEventMap;
export type WailsEventPayload<T extends WailsEventName> = WailsEventMap[T];
