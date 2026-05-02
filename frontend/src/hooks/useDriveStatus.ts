import { useState, useEffect } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { DriveStatus } from '../types/ghostdrive';

// ── useDriveStatuses ─────────────────────────────────────────────────────────

/**
 * Tracks per-backend virtual drive states.
 *
 * Initialises by calling GetDriveStatuses() on mount.
 * Subscribes to drive:mounted / drive:unmounted / drive:error Wails events
 * to keep the map up to date without polling.
 *
 * Returns:
 * - driveStatuses: Record<backendID, DriveStatus>
 * - anyMounted: true if at least one drive is currently mounted
 *
 * On non-Windows builds WinFsp is unavailable — errors are silently ignored
 * and the map stays empty.
 */
export function useDriveStatuses() {
  const [driveStatuses, setDriveStatuses] = useState<Record<string, DriveStatus>>({});

  useEffect(() => {
    let active = true;

    // Initial load
    ghostdriveApi.getDriveStatuses()
      .then(s => { if (active) setDriveStatuses(s ?? {}); })
      .catch(() => { /* WinFsp not available — keep empty map */ });

    // drive:mounted → upsert into map
    const unsubMounted = onEvent('drive:mounted', (data) => {
      if (!active) return;
      const { backendID, backendName, mountPoint, backendPaths } = data;
      if (!backendID) return;
      setDriveStatuses(prev => ({
        ...prev,
        [backendID]: {
          mounted: true,
          mountPoint,
          backendID,
          backendName,
          backendPaths,
          lastError: '',
        },
      }));
    });

    // drive:unmounted → remove from map (or mark as unmounted)
    const unsubUnmounted = onEvent('drive:unmounted', (data) => {
      if (!active) return;
      const { backendID } = data;
      if (!backendID) return;
      setDriveStatuses(prev => {
        const updated = { ...prev };
        if (updated[backendID]) {
          updated[backendID] = {
            ...updated[backendID],
            mounted: false,
            lastError: '',
          };
        }
        return updated;
      });
    });

    // drive:error → update lastError for the affected backend
    const unsubError = onEvent('drive:error', (data) => {
      if (!active) return;
      const { backendID, backendName, error } = data;
      if (!backendID) return;
      setDriveStatuses(prev => ({
        ...prev,
        [backendID]: {
          ...(prev[backendID] ?? {
            mounted: false,
            mountPoint: '',
            backendID,
            backendName,
            backendPaths: {},
          }),
          backendID,
          backendName,
          lastError: error || 'Erreur drive inconnue',
        },
      }));
    });

    return () => {
      active = false;
      unsubMounted();
      unsubUnmounted();
      unsubError();
    };
  }, []);

  const anyMounted = Object.values(driveStatuses).some(s => s.mounted);

  return { driveStatuses, anyMounted };
}

// ── useDriveStatus (compat shim — single backend or global) ─────────────────

/**
 * @deprecated depuis v1.1.x — utiliser useDriveStatuses() à la place.
 * Conservé pour les composants non encore migrés.
 */
export function useDriveStatus() {
  const { driveStatuses, anyMounted } = useDriveStatuses();

  // Best-effort: return state of the first mounted drive, or empty
  const firstMounted = Object.values(driveStatuses).find(s => s.mounted);
  const fallback: DriveStatus = {
    mounted: false,
    mountPoint: '',
    backendID: '',
    backendName: '',
    backendPaths: {},
    lastError: '',
  };
  const status = firstMounted ?? fallback;

  return {
    mounted:      anyMounted,
    mountPoint:   status.mountPoint,
    backendPaths: status.backendPaths,
    lastError:    status.lastError || null,
  };
}
