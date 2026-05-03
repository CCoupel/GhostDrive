import { useState } from 'react';
import {
  Trash2, FolderOpen, RefreshCw, Play, Pause, Square,
  AlertTriangle, Power, PowerOff, Pencil, HardDrive,
} from 'lucide-react';
import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { ghostdriveApi } from '../../services/wails';
import type { BackendConfig, BackendStatus, BackendSyncState, DriveStatus } from '../../types/ghostdrive';
import { formatSpace } from '../../utils/formatBytes';

interface BackendConfigCardProps {
  config: BackendConfig;
  status?: BackendStatus;
  syncState?: BackendSyncState;
  /** État du drive virtuel propre à ce backend (v1.1.x #88) */
  driveStatus?: DriveStatus;
  onRemove: (id: string) => void;
  onToggleEnabled: (id: string, enabled: boolean) => Promise<void>;
  onToggleAutoSync: (id: string, autoSync: boolean) => Promise<void>;
  onEdit: (config: BackendConfig) => void;
}

export function BackendConfigCard({
  config, status, syncState, driveStatus, onRemove, onToggleEnabled, onToggleAutoSync, onEdit,
}: BackendConfigCardProps) {
  const [busy, setBusy] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState(false);
  const [toggleError, setToggleError] = useState<string | null>(null);

  const isEnabled    = config.enabled;
  const isConnected  = isEnabled && (status?.connected ?? false);
  const syncStatus   = syncState?.status ?? 'idle';
  const isSyncing    = syncStatus === 'syncing';
  const isPaused     = syncStatus === 'paused';
  const driveMounted = driveStatus?.mounted ?? false;
  const driveLetter  = config.mountPoint || driveStatus?.mountPoint || '';

  // 3-state status dot: grey = disabled, green = connected, red = error
  const dotClass = !isEnabled
    ? 'status-dot bg-gray-300'
    : isConnected
    ? 'status-dot status-dot-idle'
    : 'status-dot status-dot-error';

  const handleSyncToggle = async () => {
    setBusy(true);
    try {
      if (isSyncing) await ghostdriveApi.pauseSync(config.id);
      else await ghostdriveApi.startSync(config.id);
    } finally {
      setBusy(false);
    }
  };

  const handleStop = async () => {
    setBusy(true);
    try { await ghostdriveApi.stopSync(config.id); }
    finally { setBusy(false); }
  };

  const handleForce = async () => {
    setBusy(true);
    try { await ghostdriveApi.forceSync(config.id); }
    finally { setBusy(false); }
  };

  const handleToggleEnabled = async () => {
    setBusy(true);
    setToggleError(null);
    try {
      await onToggleEnabled(config.id, !config.enabled);
    } catch (e) {
      setToggleError(e instanceof Error ? e.message : 'Erreur activation backend');
    } finally {
      setBusy(false);
    }
  };

  const handleToggleAutoSync = async () => {
    setBusy(true);
    setToggleError(null);
    try {
      await onToggleAutoSync(config.id, !config.autoSync);
    } catch (e) {
      setToggleError(e instanceof Error ? e.message : 'Erreur modification sync auto');
    } finally {
      setBusy(false);
    }
  };

  const handleConfirmRemove = () => {
    setShowConfirmDelete(false);
    onRemove(config.id);
  };

  const syncLabel = isSyncing ? 'Pause' : isPaused ? 'Reprendre' : 'Sync';
  const SyncIcon  = isSyncing ? Pause : Play;

  return (
    <>
      <div className={`border border-surface-border rounded-lg p-3 bg-white${!isEnabled ? ' opacity-70' : ''}`}>
        <div className="flex items-start justify-between gap-2">
          {/* ── Left: info — cliquable pour éditer ─────────── */}
          <div
            className="flex-1 min-w-0 cursor-pointer"
            onClick={() => onEdit(config)}
            role="button"
            tabIndex={0}
            aria-label={`Modifier le backend ${config.name}`}
            onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') onEdit(config); }}
          >
            <div className="flex items-center gap-2 flex-wrap">
              <span className={dotClass} />
              <h3 className="font-medium text-gray-900 truncate">{config.name}</h3>
              <span className="text-xs px-1.5 py-0.5 rounded bg-surface-secondary text-gray-500 uppercase">
                {config.type}
              </span>

              {/* Disabled badge */}
              {!isEnabled && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-100 text-gray-400 font-medium">
                  Désactivé
                </span>
              )}

              {/* Drive mount point badge (v1.1.x #88) */}
              {driveLetter && (
                <span
                  className={`inline-flex items-center gap-0.5 text-xs px-1.5 py-0.5 rounded font-mono font-medium
                    ${driveMounted
                      ? 'bg-green-50 text-green-700'
                      : 'bg-gray-100 text-gray-400'
                    }`}
                  title={driveMounted ? `Drive ${driveLetter} monté` : `Drive ${driveLetter} non monté`}
                >
                  <HardDrive size={10} aria-hidden="true" />
                  {driveLetter}
                </span>
              )}

              {/* Sync status badge */}
              {isEnabled && syncStatus !== 'idle' && (
                <span className={`text-xs px-1.5 py-0.5 rounded font-medium
                  ${syncStatus === 'syncing' ? 'bg-blue-50 text-status-syncing' : ''}
                  ${syncStatus === 'paused'  ? 'bg-yellow-50 text-status-paused' : ''}
                  ${syncStatus === 'error'   ? 'bg-red-50 text-status-error' : ''}
                `}>
                  {syncStatus}
                </span>
              )}
            </div>

            <div className="mt-1 space-y-0.5">
              <p className="text-xs text-gray-400 truncate">
                <span className="font-medium text-gray-500">Local :</span>{' '}
                {(config.localPath ?? '') || config.syncDir || '—'}
              </p>
              <p className="text-xs text-gray-400 truncate">
                <span className="font-medium text-gray-500">Distant :</span>{' '}
                {config.params?.basePath || config.remotePath || '—'}
              </p>
              <p className="text-xs text-gray-400 truncate">
                {config.params?.url ?? config.params?.mountPath ?? ''}
              </p>
              {(config.warning ?? '') && (
                <p className="flex items-center gap-1 text-xs text-amber-600 truncate" title={config.warning}>
                  <AlertTriangle size={11} className="shrink-0" />
                  {config.warning}
                </p>
              )}
            </div>

            {status && isConnected && status.freeSpace >= 0 && (
              <p className="text-xs text-gray-400 mt-0.5">
                Libre : {formatSpace(status.freeSpace)} / Total : {formatSpace(status.totalSpace)}
              </p>
            )}
            {status && isConnected && status.freeSpace < 0 && (
              <p className="text-xs text-gray-400 mt-0.5">Quota non disponible</p>
            )}
            {status?.error && isEnabled && (
              <p className="text-xs text-red-500 mt-0.5 truncate" title={status.error}>
                {status.error}
              </p>
            )}

            {syncState && isSyncing && syncState.currentFile && (
              <p className="text-xs text-status-syncing mt-0.5 truncate">
                ↻ {syncState.currentFile}
              </p>
            )}
            {syncState && syncState.pending > 0 && (
              <p className="text-xs text-gray-400 mt-0.5">
                {syncState.pending} fichier{syncState.pending > 1 ? 's' : ''} en attente
              </p>
            )}
            {driveStatus?.lastError && (
              <p className="text-xs text-red-500 mt-0.5 truncate" title={driveStatus.lastError}>
                <HardDrive size={10} className="inline mr-0.5" aria-hidden="true" />
                {driveStatus.lastError}
              </p>
            )}
          </div>

          {/* ── Right: toggle buttons ───────────────────────── */}
          <div className="flex items-center gap-0.5 shrink-0">
            {/* AutoSync toggle */}
            <button
              onClick={handleToggleAutoSync}
              disabled={busy || !isEnabled}
              title={config.autoSync ? 'Désactiver sync auto' : 'Activer sync auto'}
              aria-label={config.autoSync ? 'Désactiver sync auto' : 'Activer sync auto'}
              className={`p-1 rounded transition-colors hover:bg-surface-secondary
                disabled:opacity-40 disabled:cursor-not-allowed
                ${config.autoSync && isEnabled ? 'text-brand' : 'text-gray-300'}`}
            >
              <RefreshCw size={14} />
            </button>

            {/* Enable/Disable toggle */}
            <button
              onClick={handleToggleEnabled}
              disabled={busy}
              title={isEnabled ? 'Désactiver ce backend' : 'Activer ce backend'}
              aria-label={isEnabled ? 'Désactiver ce backend' : 'Activer ce backend'}
              className={`p-1 rounded transition-colors hover:bg-surface-secondary
                disabled:opacity-40 disabled:cursor-not-allowed
                ${isEnabled ? 'text-status-idle' : 'text-gray-300'}`}
            >
              {isEnabled ? <Power size={14} /> : <PowerOff size={14} />}
            </button>
          </div>
        </div>

        {toggleError && (
          <p className="text-xs text-red-500 mt-1" role="alert">{toggleError}</p>
        )}

        {/* ── Action buttons ─────────────────────────────────── */}
        <div className="flex flex-wrap gap-1.5 mt-2.5">
          <Button size="sm" variant="ghost" onClick={() => onEdit(config)} aria-label="Modifier ce backend">
            <Pencil size={12} /> Modifier
          </Button>
          <Button size="sm" variant="ghost" onClick={() => ghostdriveApi.openSyncFolder(config.id)}>
            <FolderOpen size={12} /> Ouvrir
          </Button>
          <Button
            size="sm" variant="ghost"
            onClick={handleSyncToggle}
            disabled={busy || !isConnected}
          >
            <SyncIcon size={12} /> {syncLabel}
          </Button>
          {(isSyncing || isPaused) && (
            <Button size="sm" variant="ghost" onClick={handleStop} disabled={busy}>
              <Square size={12} /> Stop
            </Button>
          )}
          <Button
            size="sm" variant="ghost"
            onClick={handleForce}
            disabled={busy || !isConnected}
          >
            <RefreshCw size={12} className={busy && isSyncing ? 'animate-spin' : ''} /> Force
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setShowConfirmDelete(true)}
            className="text-red-500 hover:bg-red-50 ml-auto"
            aria-label="Supprimer ce backend"
          >
            <Trash2 size={12} />
          </Button>
        </div>
      </div>

      <Modal
        open={showConfirmDelete}
        onClose={() => setShowConfirmDelete(false)}
        title="Supprimer le backend"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowConfirmDelete(false)}>
              Annuler
            </Button>
            <Button variant="danger" onClick={handleConfirmRemove}>
              Supprimer
            </Button>
          </>
        }
      >
        <p className="text-sm text-gray-700">
          Supprimer <strong>{config.name}</strong> ? La synchronisation sera arrêtée
          et la configuration supprimée. Les fichiers locaux ne seront pas affectés.
        </p>
      </Modal>
    </>
  );
}
