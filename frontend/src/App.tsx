import { useState, useEffect } from 'react';
import { SyncStatusPanel } from './components/status/SyncStatus';
import { TrayMenu } from './components/tray/TrayMenu';
import { SettingsPage } from './components/settings/SettingsPage';
import { useSyncStatus } from './hooks/useSyncStatus';
import { useBackends } from './hooks/useBackends';
import { ghostdriveApi } from './services/wails';
import type { AppConfig } from './types/ghostdrive';

const DEFAULT_CONFIG: AppConfig = {
  version: '0.2.0',
  backends: [],
  cacheEnabled: false,
  cacheDir: '',
  cacheSizeMaxMB: 1024,
  startMinimized: false,
  autoStart: false,
};

type View = 'status' | 'settings';

export function App() {
  const [view, setView] = useState<View>('status');
  const [appConfig, setAppConfig] = useState<AppConfig>(DEFAULT_CONFIG);

  const { syncState, activeTransfers, errors, recentEvents } = useSyncStatus();
  const { configs } = useBackends();

  useEffect(() => {
    ghostdriveApi.getConfig()
      .then(setAppConfig)
      .catch(() => {});
  }, []);

  return (
    <div className="flex flex-col h-screen bg-surface-secondary text-gray-900 overflow-hidden">
      <TrayMenu
        syncState={syncState}
        backends={configs}
        onOpenSettings={() => setView('settings')}
      />

      <nav className="flex border-b border-surface-border bg-white shrink-0">
        <NavTab active={view === 'status'} onClick={() => setView('status')}>
          État
        </NavTab>
        <NavTab active={view === 'settings'} onClick={() => setView('settings')}>
          Paramètres
        </NavTab>
      </nav>

      <main className="flex-1 overflow-hidden">
        {view === 'settings' ? (
          <SettingsPage appConfig={appConfig} onConfigChange={setAppConfig} />
        ) : (
          <div className="h-full overflow-y-auto">
            <SyncStatusPanel
              syncState={syncState}
              activeTransfers={activeTransfers}
              errors={errors}
              recentEvents={recentEvents}
            />
          </div>
        )}
      </main>
    </div>
  );
}

function NavTab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm font-medium transition-colors border-b-2 -mb-px
        ${active
          ? 'border-brand text-brand'
          : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
        }`}
    >
      {children}
    </button>
  );
}
