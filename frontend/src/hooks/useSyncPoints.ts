import { useState, useCallback } from 'react';
import { ghostdriveApi } from '../services/wails';
import type { FileInfo } from '../types/ghostdrive';

interface SyncPointsState {
  files: FileInfo[];
  loading: boolean;
  error: Error | null;
  currentPath: string;
}

export function useSyncPoints(backendId: string) {
  const [state, setState] = useState<SyncPointsState>({
    files: [],
    loading: false,
    error: null,
    currentPath: '/',
  });

  const navigate = useCallback(async (path: string) => {
    setState(s => ({ ...s, loading: true, error: null }));
    try {
      const files = await ghostdriveApi.listFiles(backendId, path);
      setState({ files, loading: false, error: null, currentPath: path });
    } catch (err) {
      setState(s => ({ ...s, loading: false, error: err as Error }));
    }
  }, [backendId]);

  const download = useCallback(async (remotePath: string) => {
    await ghostdriveApi.downloadFile(backendId, remotePath);
  }, [backendId]);

  const openFolder = useCallback(async () => {
    await ghostdriveApi.openSyncFolder(backendId);
  }, [backendId]);

  return { ...state, navigate, download, openFolder };
}
