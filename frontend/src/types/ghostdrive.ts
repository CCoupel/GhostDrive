// GhostDrive — Types TypeScript partagés (Go ↔ Frontend)
// Version : 0.2.0

export type FileEventType = 'created' | 'modified' | 'deleted' | 'renamed';
export type SyncStatus = 'idle' | 'syncing' | 'paused' | 'error';
export type TransferDirection = 'upload' | 'download';
export type FileSource = 'local' | 'remote';
export type BackendType = 'local' | string;
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

// ── Plugin Descriptor (v1.1.0 — #78 / #79) ────────────────────────────────

export type ParamType = 'string' | 'password' | 'path' | 'select' | 'bool' | 'number';

export interface ParamSpec {
  key:         string;
  label:       string;
  type:        ParamType;
  required:    boolean;
  default:     string;
  placeholder: string;
  options:     string[];
  helpText:    string;
}

export interface PluginDescriptor {
  type:        string;
  displayName: string;
  description: string;
  params:      ParamSpec[];
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
  /** Point de montage du drive virtuel : lettre Windows ("E:") ou chemin absolu. Ajouté v1.1.x #88 */
  mountPoint: string;
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
  /** Available on download progress events emitted by DownloadFile */
  backendId?: string;
  /** Remote path of the file being downloaded */
  remotePath?: string;
}

/**
 * DriveStatus — matches placeholder.DriveStatus (Go, all fields have json tags → camelCase wire).
 * Updated v1.1.x: added backendID, backendName; all fields now camelCase.
 */
export interface DriveStatus {
  mounted: boolean;
  mountPoint: string;
  backendID: string;    // ID of the backend that owns this drive (v1.1.x #88)
  backendName: string;  // Human-readable backend name (v1.1.x #88)
  backendPaths: Record<string, string>; // backendID → path under drive root
  lastError: string;    // empty string when no error
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
  ghostDriveRoot?: string;
  /** Point de montage du drive virtuel GhD: — lettre ("G:") ou chemin ("C:\GhostDrive\GhD\") */
  mountPoint?: string;
  /** @deprecated Utilisez mountPoint. Conservé pour rétrocompatibilité JSON. */
  driveLetter?: string;
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
  /** drive:mounted — emitted after a per-backend drive is mounted (v1.1.x: includes backendID/backendName) */
  'drive:mounted': {
    backendID: string;
    backendName: string;
    mountPoint: string;
    backendPaths: Record<string, string>;
  };
  /** drive:unmounted — emitted after a per-backend drive is unmounted (v1.1.x: includes backendID/backendName) */
  'drive:unmounted': {
    backendID: string;
    backendName: string;
    mountPoint: string;
  };
  /** drive:error — emitted when a drive mount/unmount fails (v1.1.x: includes backendID/backendName) */
  'drive:error': {
    backendID: string;
    backendName: string;
    error: string;
  };
};

export type WailsEventName = keyof WailsEventMap;
export type WailsEventPayload<T extends WailsEventName> = WailsEventMap[T];
