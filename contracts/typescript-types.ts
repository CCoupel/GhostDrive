// GhostDrive — Types TypeScript partagés (Go ↔ Frontend)
// Générés depuis contracts/types.go — ne pas modifier côté frontend.
// Version : 0.1.0

// ─── Enums ──────────────────────────────────────────────────────────────────

export type FileEventType = "created" | "modified" | "deleted" | "renamed";

export type SyncStatus = "idle" | "syncing" | "paused" | "error";

export type TransferDirection = "upload" | "download";

export type FileSource = "local" | "remote";

// ─── Modèles ────────────────────────────────────────────────────────────────

export interface FileInfo {
  name: string;
  path: string;
  size: number;
  isDir: boolean;
  modTime: string; // ISO 8601
  etag: string;
  isPlaceholder: boolean;
  isCached: boolean;
}

export interface FileEvent {
  type: FileEventType;
  path: string;
  oldPath?: string;
  timestamp: string; // ISO 8601
  source: FileSource;
}

export interface SyncError {
  path: string;
  message: string;
  time: string; // ISO 8601
}

export interface SyncState {
  status: SyncStatus;
  progress: number; // 0.0 à 1.0
  currentFile: string;
  pending: number;
  errors: SyncError[];
  lastSync: string; // ISO 8601
}

export interface BackendConfig {
  id: string;
  name: string;
  type: "webdav" | "moosefs";
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

// ─── Wails Bindings (window.go.App.*) ────────────────────────────────────────

export interface WailsApp {
  // Configuration
  GetConfig(): Promise<AppConfig>;
  SaveConfig(config: AppConfig): Promise<void>;
  AddBackend(config: BackendConfig): Promise<BackendConfig>;
  RemoveBackend(backendID: string): Promise<void>;
  TestBackendConnection(config: BackendConfig): Promise<BackendStatus>;

  // Synchronisation
  GetSyncState(): Promise<SyncState>;
  StartSync(backendID: string): Promise<void>;
  StopSync(backendID: string): Promise<void>;
  PauseSync(backendID: string): Promise<void>;
  ForceSync(backendID: string): Promise<void>;

  // Fichiers
  ListFiles(backendID: string, path: string): Promise<FileInfo[]>;
  DownloadFile(backendID: string, remotePath: string): Promise<void>;
  OpenSyncFolder(backendID: string): Promise<void>;

  // Cache
  GetCacheStats(): Promise<CacheStats>;
  ClearCache(): Promise<void>;

  // Système
  GetBackendStatuses(): Promise<BackendStatus[]>;
  GetVersion(): Promise<string>;
  Quit(): Promise<void>;
}

// ─── Wails Events ────────────────────────────────────────────────────────────
// Écoute : EventsOn("event:name", (payload: Type) => { ... })

export type WailsEventMap = {
  "sync:state-changed": SyncState;
  "sync:progress": ProgressEvent;
  "sync:file-event": FileEvent;
  "sync:error": SyncError;
  "backend:status-changed": BackendStatus;
  "placeholder:hydration-started": { path: string; size: number };
  "placeholder:hydration-done": { path: string };
  "app:ready": { version: string; backendsCount: number };
};

export type WailsEventName = keyof WailsEventMap;
export type WailsEventPayload<T extends WailsEventName> = WailsEventMap[T];
