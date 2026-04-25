import { useState, useEffect, useCallback } from 'react';
import { TrayMenu } from './components/tray/TrayMenu';
import { SettingsPage } from './components/settings/SettingsPage';
import { ConfigPage } from './pages/ConfigPage';
import { AboutPage } from './pages/AboutPage';
import { useSyncStatus } from './hooks/useSyncStatus';
import { useBackends } from './hooks/useBackends';
import { ghostdriveApi } from './services/wails';
import type { AppConfig } from './types/ghostdrive';

const DEFAULT_CONFIG: AppConfig = {
  version: '0.4.0',
  backends: [],
  cacheEnabled: false,
  cacheDir: '',
  cacheSizeMaxMB: 1024,
  startMinimized: false,
  autoStart: false,
};

type View = 'backends' | 'configuration' | 'about';

export function App() {
  const [view, setView] = useState<View>('backends');
  const [appConfig, setAppConfig] = useState<AppConfig>(DEFAULT_CONFIG);

  const { syncState } = useSyncStatus();
  const { configs } = useBackends();

  // Stable reference — TrayStatus registers the 'tray:open-settings' Wails
  // event listener; keep the callback stable to avoid re-registering on every render.
  const handleOpenSettings = useCallback(() => setView('backends'), []);

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
        onOpenSettings={handleOpenSettings}
      />

      <nav className="flex border-b border-surface-border bg-white shrink-0">
        <NavTab active={view === 'backends'} onClick={() => setView('backends')}>
          Backends
        </NavTab>
        <NavTab active={view === 'configuration'} onClick={() => setView('configuration')}>
          Configuration
        </NavTab>
        <NavTab active={view === 'about'} onClick={() => setView('about')}>
          À propos
        </NavTab>
      </nav>

      <main className="flex-1 overflow-hidden">
        {view === 'configuration' ? (
          <ConfigPage appConfig={appConfig} onConfigChange={setAppConfig} />
        ) : view === 'about' ? (
          <AboutPage appConfig={appConfig} />
        ) : (
          <SettingsPage />
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
