import * as App from '../../wailsjs/go/app/App';
import { EventsOn, WindowHide } from '../../wailsjs/runtime/runtime';
import type {
  AppConfig,
  BackendConfig,
  BackendStatus,
  DriveStatus,
  PluginDescriptor,
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

  updateBackend: (config: BackendConfig): Promise<BackendConfig> =>
    App.UpdateBackend(config as any) as unknown as Promise<BackendConfig>,

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

  /** Retourne les descripteurs de tous les plugins disponibles (local + dynamiques). */
  getPluginDescriptors: (): Promise<PluginDescriptor[]> =>
    (App as any).GetPluginDescriptors() as Promise<PluginDescriptor[]>,

  setBackendEnabled: (backendId: string, enabled: boolean): Promise<void> =>
    App.SetBackendEnabled(backendId, enabled),

  setAutoSync: (backendId: string, autoSync: boolean): Promise<void> =>
    App.SetAutoSync(backendId, autoSync),

  getVersion: (): Promise<string> => App.GetVersion(),
  quit: (): Promise<void> => App.Quit(),

  // Drive virtuel (WinFsp) — par backend depuis v1.1.x #88

  /**
   * Retourne l'état de montage de tous les drives virtuels (map backendID → DriveStatus).
   * Remplace getDriveStatus() depuis v1.1.x.
   */
  getDriveStatuses: (): Promise<Record<string, DriveStatus>> =>
    (App as any).GetDriveStatuses() as Promise<Record<string, DriveStatus>>,

  /**
   * @deprecated depuis v1.1.x — utiliser getDriveStatuses() à la place.
   * Conservé pour compatibilité pendant la migration.
   */
  getDriveStatus: (): Promise<DriveStatus> =>
    App.GetDriveStatus() as unknown as Promise<DriveStatus>,

  /** Retourne les lettres de lecteur Windows disponibles (ex: ['E:', 'F:', 'G:']). */
  getAvailableDriveLetters: (): Promise<string[]> =>
    (App as any).GetAvailableDriveLetters() as Promise<string[]>,
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
