import { Play, Pause, RefreshCw } from 'lucide-react';
import { TrayStatus } from './TrayStatus';
import { ghostdriveApi } from '../../services/wails';
import type { SyncState, BackendConfig } from '../../types/ghostdrive';

interface TrayMenuProps {
  syncState: SyncState;
  backends: BackendConfig[];
  onOpenSettings: () => void;
}

export function TrayMenu({ syncState, backends, onOpenSettings }: TrayMenuProps) {
  const isSyncing = syncState.status === 'syncing';
  const isPaused  = syncState.status === 'paused';
  const hasBackends = backends.length > 0;

  const handleSyncToggle = async () => {
    for (const b of backends) {
      if (isSyncing) await ghostdriveApi.pauseSync(b.id);
      else await ghostdriveApi.startSync(b.id);
    }
  };

  const handleForceSync = async () => {
    for (const b of backends) await ghostdriveApi.forceSync(b.id);
  };

  return (
    <div className="flex items-center justify-between px-3 py-2 bg-white border-b border-surface-border shrink-0">
      <TrayStatus syncState={syncState} onNavigateToSettings={onOpenSettings} />

      <div className="flex items-center gap-1">
        <ToolbarButton
          icon={isSyncing ? <Pause size={14} /> : <Play size={14} />}
          label={isSyncing ? 'Pause' : isPaused ? 'Reprendre' : 'Sync'}
          onClick={handleSyncToggle}
          disabled={!hasBackends}
        />
        <ToolbarButton
          icon={<RefreshCw size={14} />}
          label="Forcer"
          onClick={handleForceSync}
          disabled={!hasBackends}
        />
      </div>
    </div>
  );
}

function ToolbarButton({
  icon,
  label,
  onClick,
  disabled,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={label}
      className="flex items-center gap-1 px-2 py-1 text-xs rounded text-gray-600
        hover:bg-surface-secondary transition-colors
        disabled:opacity-40 disabled:cursor-not-allowed"
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </button>
  );
}
