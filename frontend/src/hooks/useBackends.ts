import { useState, useEffect, useCallback } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { BackendConfig, BackendStatus } from '../types/ghostdrive';

interface BackendsState {
  configs: BackendConfig[];
  statuses: BackendStatus[];
  loading: boolean;
  error: Error | null;
}

export function useBackends() {
  const [state, setState] = useState<BackendsState>({
    configs: [],
    statuses: [],
    loading: true,
    error: null,
  });

  const load = useCallback(async () => {
    try {
      const [config, statuses] = await Promise.all([
        ghostdriveApi.getConfig(),
        ghostdriveApi.getBackendStatuses(),
      ]);
      setState(s => ({ ...s, configs: config.backends, statuses, loading: false, error: null }));
    } catch (err) {
      setState(s => ({ ...s, loading: false, error: err as Error }));
    }
  }, []);

  useEffect(() => {
    load();
    const interval = setInterval(load, 10000);
    const unsub = onEvent('backend:status-changed', (status) => {
      setState(s => ({
        ...s,
        statuses: s.statuses.map(st => st.backendId === status.backendId ? status : st),
      }));
    });
    return () => {
      clearInterval(interval);
      unsub();
    };
  }, [load]);

  const addBackend = useCallback(async (config: BackendConfig) => {
    const created = await ghostdriveApi.addBackend(config);
    setState(s => ({ ...s, configs: [...s.configs, created] }));
    return created;
  }, []);

  const removeBackend = useCallback(async (backendId: string) => {
    await ghostdriveApi.removeBackend(backendId);
    setState(s => ({
      ...s,
      configs: s.configs.filter(c => c.id !== backendId),
      statuses: s.statuses.filter(st => st.backendId !== backendId),
    }));
  }, []);

  const getStatus = useCallback(
    (backendId: string): BackendStatus | undefined =>
      state.statuses.find(s => s.backendId === backendId),
    [state.statuses],
  );

  return { ...state, reload: load, addBackend, removeBackend, getStatus };
}
