import { useState } from 'react';
import { Wifi, WifiOff, Trash2, FolderOpen, RefreshCw, Play, Pause, Square } from 'lucide-react';
import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { ghostdriveApi } from '../../services/wails';
import type { BackendConfig, BackendStatus, BackendSyncState } from '../../types/ghostdrive';
import { formatSpace } from '../../utils/formatBytes';

interface BackendConfigCardProps {
  config: BackendConfig;
  status?: BackendStatus;
  syncState?: BackendSyncState;
  onRemove: (id: string) => void;
}


export function BackendConfigCard({ config, status, syncState, onRemove }: BackendConfigCardProps) {
  const [busy, setBusy] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState(false);

  const isConnected = status?.connected ?? false;
  const syncStatus  = syncState?.status ?? 'idle';
  const isSyncing   = syncStatus === 'syncing';
  const isPaused    = syncStatus === 'paused';

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

  const handleConfirmRemove = async () => {
    setShowConfirmDelete(false);
    onRemove(config.id);
  };

  const syncLabel = isSyncing ? 'Pause' : isPaused ? 'Reprendre' : 'Sync';
  const SyncIcon  = isSyncing ? Pause : Play;

  return (
    <>
      <div className="border border-surface-border rounded-lg p-3 bg-white">
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className={`status-dot ${isConnected ? 'status-dot-idle' : 'status-dot-error'}`} />
              <h3 className="font-medium text-gray-900 truncate">{config.name}</h3>
              <span className="text-xs px-1.5 py-0.5 rounded bg-surface-secondary text-gray-500 uppercase">
                {config.type}
              </span>
              {syncStatus !== 'idle' && (
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
                {config.syncDir || '—'}
              </p>
              <p className="text-xs text-gray-400 truncate">
                <span className="font-medium text-gray-500">Distant :</span>{' '}
                {config.remotePath || '—'}
              </p>
              <p className="text-xs text-gray-400 truncate">
                {config.params.url ?? config.params.mountPath ?? ''}
              </p>
            </div>

            {status && isConnected && (
              <p className="text-xs text-gray-400 mt-0.5">
                Libre : {formatSpace(status.freeSpace)} / Total : {formatSpace(status.totalSpace)}
              </p>
            )}
            {status?.error && (
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
          </div>

          <div className="flex items-center gap-1 shrink-0">
            {isConnected
              ? <Wifi size={15} className="text-status-idle" />
              : <WifiOff size={15} className="text-status-error" />
            }
          </div>
        </div>

        <div className="flex flex-wrap gap-1.5 mt-2.5">
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
