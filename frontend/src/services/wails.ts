import * as App from '../../wailsjs/go/app/App';
import { EventsOn, WindowHide } from '../../wailsjs/runtime/runtime';
import type {
  AppConfig,
  BackendConfig,
  BackendStatus,
  SyncState,
  FileInfo,
  CacheStats,
  WailsEventName,
  WailsEventPayload,
} from '../types/ghostdrive';

// Wails generates class-based types with `string` for Go type aliases and
// `any` for time.Time fields. We cast at the boundary to preserve our strict
// union types throughout the rest of the app.

export const ghostdriveApi = {
  getConfig: (): Promise<AppConfig> =>
    App.GetConfig() as unknown as Promise<AppConfig>,

  saveConfig: (config: AppConfig): Promise<void> =>
    App.SaveConfig(config as any),

  addBackend: (config: BackendConfig): Promise<BackendConfig> =>
    App.AddBackend(config as any) as unknown as Promise<BackendConfig>,

  removeBackend: (backendId: string): Promise<void> =>
    App.RemoveBackend(backendId),

  testBackendConnection: (config: BackendConfig): Promise<BackendStatus> =>
    App.TestBackendConnection(config as any) as unknown as Promise<BackendStatus>,

  getSyncState: (): Promise<SyncState> =>
    App.GetSyncState() as unknown as Promise<SyncState>,

  startSync: (backendId: string): Promise<void> => App.StartSync(backendId),
  stopSync:  (backendId: string): Promise<void> => App.StopSync(backendId),
  pauseSync: (backendId: string): Promise<void> => App.PauseSync(backendId),
  forceSync: (backendId: string): Promise<void> => App.ForceSync(backendId),

  // App.js has these methods; App.d.ts declaration is incomplete — cast via any
  listFiles: (backendId: string, path: string): Promise<FileInfo[]> =>
    (App as any).ListFiles(backendId, path) as Promise<FileInfo[]>,
  downloadFile: (backendId: string, remotePath: string): Promise<void> =>
    (App as any).DownloadFile(backendId, remotePath) as Promise<void>,
  getCacheStats: (): Promise<CacheStats> =>
    (App as any).GetCacheStats() as Promise<CacheStats>,
  clearCache: (): Promise<void> =>
    (App as any).ClearCache() as Promise<void>,

  openSyncFolder: (backendId: string): Promise<void> =>
    App.OpenSyncFolder(backendId),

  getBackendStatuses: (): Promise<BackendStatus[]> =>
    App.GetBackendStatuses() as unknown as Promise<BackendStatus[]>,

  getAvailableBackendTypes: (): Promise<string[]> =>
    (App as any).GetAvailableBackendTypes() as Promise<string[]>,

  getVersion: (): Promise<string> => App.GetVersion(),
  quit: (): Promise<void> => App.Quit(),
};

export function onEvent<T extends WailsEventName>(
  event: T,
  callback: (payload: WailsEventPayload<T>) => void,
): () => void {
  return EventsOn(event, callback as (...data: unknown[]) => void);
}

export function hideWindow(): void {
  WindowHide();
}
