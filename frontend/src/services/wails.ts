import * as App from '../../wailsjs/go/main/App';
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

export const ghostdriveApi = {
  getConfig: (): Promise<AppConfig> => App.GetConfig(),
  saveConfig: (config: AppConfig): Promise<void> => App.SaveConfig(config),
  addBackend: (config: BackendConfig): Promise<BackendConfig> => App.AddBackend(config),
  removeBackend: (backendId: string): Promise<void> => App.RemoveBackend(backendId),
  testBackendConnection: (config: BackendConfig): Promise<BackendStatus> =>
    App.TestBackendConnection(config),
  getSyncState: (): Promise<SyncState> => App.GetSyncState(),
  startSync: (backendId: string): Promise<void> => App.StartSync(backendId),
  stopSync: (backendId: string): Promise<void> => App.StopSync(backendId),
  pauseSync: (backendId: string): Promise<void> => App.PauseSync(backendId),
  forceSync: (backendId: string): Promise<void> => App.ForceSync(backendId),
  listFiles: (backendId: string, path: string): Promise<FileInfo[]> =>
    App.ListFiles(backendId, path),
  downloadFile: (backendId: string, remotePath: string): Promise<void> =>
    App.DownloadFile(backendId, remotePath),
  openSyncFolder: (backendId: string): Promise<void> => App.OpenSyncFolder(backendId),
  getCacheStats: (): Promise<CacheStats> => App.GetCacheStats(),
  clearCache: (): Promise<void> => App.ClearCache(),
  getBackendStatuses: (): Promise<BackendStatus[]> => App.GetBackendStatuses(),
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
