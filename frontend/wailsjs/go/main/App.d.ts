import type {
  AppConfig,
  BackendConfig,
  BackendStatus,
  SyncState,
  FileInfo,
  CacheStats,
} from '../../../src/types/ghostdrive';

export declare function GetConfig(): Promise<AppConfig>;
export declare function SaveConfig(config: AppConfig): Promise<void>;
export declare function AddBackend(config: BackendConfig): Promise<BackendConfig>;
export declare function RemoveBackend(backendID: string): Promise<void>;
export declare function TestBackendConnection(config: BackendConfig): Promise<BackendStatus>;
export declare function GetSyncState(): Promise<SyncState>;
export declare function StartSync(backendID: string): Promise<void>;
export declare function StopSync(backendID: string): Promise<void>;
export declare function PauseSync(backendID: string): Promise<void>;
export declare function ForceSync(backendID: string): Promise<void>;
export declare function ListFiles(backendID: string, path: string): Promise<FileInfo[]>;
export declare function DownloadFile(backendID: string, remotePath: string): Promise<void>;
export declare function OpenSyncFolder(backendID: string): Promise<void>;
export declare function GetCacheStats(): Promise<CacheStats>;
export declare function ClearCache(): Promise<void>;
export declare function GetBackendStatuses(): Promise<BackendStatus[]>;
export declare function GetVersion(): Promise<string>;
export declare function Quit(): Promise<void>;
