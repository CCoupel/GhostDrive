import { Settings, FolderOpen, Play, Pause, RefreshCw, X } from 'lucide-react';
import { TrayStatus } from './TrayStatus';
import { ghostdriveApi, hideWindow } from '../../services/wails';
import type { SyncState, BackendConfig } from '../../types/ghostdrive';

interface TrayMenuProps {
  syncState: SyncState;
  backends: BackendConfig[];
  onOpenSettings: () => void;
}

export function TrayMenu({ syncState, backends, onOpenSettings }: TrayMenuProps) {
  const isSyncing = syncState.status === 'syncing';
  const isPaused  = syncState.status === 'paused';

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
    <div className="w-[360px] bg-white rounded-lg shadow-lg overflow-hidden">
      <TrayStatus
        status={syncState.status}
        pending={syncState.pending}
        currentFile={syncState.currentFile}
      />

      <div className="border-t border-surface-border" />

      <div className="py-1">
        <MenuItem
          icon={isSyncing ? <Pause size={15} /> : <Play size={15} />}
          label={isSyncing ? 'Mettre en pause' : isPaused ? 'Reprendre' : 'Démarrer la sync'}
          onClick={handleSyncToggle}
          disabled={backends.length === 0}
        />
        <MenuItem
          icon={<RefreshCw size={15} />}
          label="Synchronisation forcée"
          onClick={handleForceSync}
          disabled={backends.length === 0}
        />

        {backends.slice(0, 3).map(b => (
          <MenuItem
            key={b.id}
            icon={<FolderOpen size={15} />}
            label={`Ouvrir "${b.name}"`}
            onClick={() => ghostdriveApi.openSyncFolder(b.id)}
          />
        ))}
      </div>

      <div className="border-t border-surface-border" />

      <div className="py-1">
        <MenuItem
          icon={<Settings size={15} />}
          label="Paramètres"
          onClick={onOpenSettings}
        />
        <MenuItem
          icon={<X size={15} />}
          label="Quitter GhostDrive"
          onClick={() => ghostdriveApi.quit()}
          danger
        />
      </div>

      <div className="px-3 py-1.5 bg-surface-secondary border-t border-surface-border">
        <button
          className="text-xs text-gray-400 hover:text-gray-600 w-full text-right"
          onClick={hideWindow}
        >
          Masquer
        </button>
      </div>
    </div>
  );
}

interface MenuItemProps {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
}

function MenuItem({ icon, label, onClick, disabled, danger }: MenuItemProps) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`
        w-full flex items-center gap-2.5 px-3 py-1.5 text-sm text-left transition-colors
        disabled:opacity-40 disabled:cursor-not-allowed
        ${danger ? 'text-red-600 hover:bg-red-50' : 'text-gray-800 hover:bg-surface-secondary'}
      `.trim()}
    >
      <span className="shrink-0">{icon}</span>
      {label}
    </button>
  );
}
