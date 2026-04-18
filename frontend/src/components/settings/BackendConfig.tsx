import { useState } from 'react';
import { Wifi, WifiOff, Trash2, FolderOpen, RefreshCw, Play, Pause } from 'lucide-react';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import type { BackendConfig, BackendStatus } from '../../types/ghostdrive';

interface BackendConfigCardProps {
  config: BackendConfig;
  status?: BackendStatus;
  syncStatus?: string;
  onRemove: (id: string) => void;
}

function formatSpace(bytes: number): string {
  if (bytes < 0) return 'N/A';
  const gb = bytes / (1024 * 1024 * 1024);
  return `${gb.toFixed(1)} Go`;
}

export function BackendConfigCard({ config, status, syncStatus, onRemove }: BackendConfigCardProps) {
  const [busy, setBusy] = useState(false);
  const [testResult, setTestResult] = useState<'ok' | 'fail' | null>(null);

  const isConnected = status?.connected ?? false;
  const isSyncing = syncStatus === 'syncing';

  const handleTest = async () => {
    setBusy(true);
    setTestResult(null);
    try {
      await ghostdriveApi.testBackendConnection(config);
      setTestResult('ok');
    } catch {
      setTestResult('fail');
    } finally {
      setBusy(false);
    }
  };

  const handleSyncToggle = async () => {
    setBusy(true);
    try {
      if (isSyncing) await ghostdriveApi.pauseSync(config.id);
      else await ghostdriveApi.startSync(config.id);
    } finally {
      setBusy(false);
    }
  };

  const handleForce = async () => {
    setBusy(true);
    try { await ghostdriveApi.forceSync(config.id); }
    finally { setBusy(false); }
  };

  return (
    <div className="border border-surface-border rounded-lg p-3 bg-white">
      <div className="flex items-start justify-between gap-2">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className={`status-dot ${isConnected ? 'status-dot-idle' : 'status-dot-error'}`} />
            <h3 className="font-medium text-gray-900 truncate">{config.name}</h3>
            <span className="text-xs px-1.5 py-0.5 rounded bg-surface-secondary text-gray-500 uppercase">
              {config.type}
            </span>
          </div>
          <p className="text-xs text-gray-400 mt-0.5 truncate">
            {config.params.url ?? config.params.mountPath ?? config.syncDir}
          </p>
          {status && (
            <p className="text-xs text-gray-400 mt-0.5">
              Libre : {formatSpace(status.freeSpace)} / Total : {formatSpace(status.totalSpace)}
            </p>
          )}
          {status?.error && (
            <p className="text-xs text-red-500 mt-0.5 truncate" title={status.error}>
              {status.error}
            </p>
          )}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          {isConnected ? <Wifi size={15} className="text-status-idle" /> : <WifiOff size={15} className="text-status-error" />}
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5 mt-2.5">
        <Button size="sm" variant="ghost" onClick={() => ghostdriveApi.openSyncFolder(config.id)}>
          <FolderOpen size={12} /> Ouvrir
        </Button>
        <Button size="sm" variant="ghost" onClick={handleTest} disabled={busy}>
          <Wifi size={12} /> Tester
        </Button>
        <Button size="sm" variant="ghost" onClick={handleSyncToggle} disabled={busy || !isConnected}>
          {isSyncing ? <Pause size={12} /> : <Play size={12} />}
          {isSyncing ? 'Pause' : 'Sync'}
        </Button>
        <Button size="sm" variant="ghost" onClick={handleForce} disabled={busy || !isConnected}>
          <RefreshCw size={12} className={busy ? 'animate-spin' : ''} /> Force
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => onRemove(config.id)}
          className="text-red-500 hover:bg-red-50 ml-auto"
          aria-label="Supprimer ce backend"
        >
          <Trash2 size={12} />
        </Button>
      </div>

      {testResult && (
        <p className={`text-xs mt-1.5 ${testResult === 'ok' ? 'text-status-idle' : 'text-red-500'}`}>
          {testResult === 'ok' ? '✓ Connexion réussie' : '✗ Connexion échouée'}
        </p>
      )}
    </div>
  );
}
