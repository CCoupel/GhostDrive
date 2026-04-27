import { useState, useEffect } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { DriveStatus } from '../types/ghostdrive';

// Wire format: Go field names (no JSON tags → capitalized).
const INITIAL_STATUS: DriveStatus = {
  Mounted: false,
  MountPoint: '',
  BackendPaths: {},
  LastError: '',
};

/**
 * Listens to drive:mounted / drive:unmounted / drive:error Wails events.
 * Initialises state by calling GetDriveStatus() on mount.
 * On non-Windows builds WinFsp is unavailable — errors are silently ignored.
 */
export function useDriveStatus() {
  const [status, setStatus] = useState<DriveStatus>(INITIAL_STATUS);

  useEffect(() => {
    let active = true;

    ghostdriveApi.getDriveStatus()
      .then(s => { if (active) setStatus(s ?? INITIAL_STATUS); })
      .catch(() => { /* WinFsp not available — keep initial state */ });

    const unsubMounted = onEvent('drive:mounted', (data) => {
      if (!active) return;
      setStatus(data as DriveStatus);
    });

    const unsubUnmounted = onEvent('drive:unmounted', () => {
      if (!active) return;
      setStatus(INITIAL_STATUS);
    });

    const unsubError = onEvent('drive:error', (data) => {
      if (!active) return;
      // Backend emits full DriveStatus for drive:error — read LastError directly
      setStatus(prev => ({
        ...prev,
        ...data,
        LastError: data.LastError || 'Erreur drive inconnue',
      }));
    });

    return () => {
      active = false;
      unsubMounted();
      unsubUnmounted();
      unsubError();
    };
  }, []);

  return {
    mounted:    status.Mounted,
    mountPoint: status.MountPoint,
    backendPaths: status.BackendPaths,
    lastError:  status.LastError || null,
  };
}
