import { useState } from 'react'
import { SyncStatusPanel } from './components/status/SyncStatus'
import { TrayMenu } from './components/tray/TrayMenu'
import { SettingsPage } from './components/settings/SettingsPage'
import { useSyncStatus } from './hooks/useSyncStatus'
import { useBackends } from './hooks/useBackends'
import type { AppConfig } from './types/ghostdrive'

export function App() {
  const [showSettings, setShowSettings] = useState(false)
  const [appConfig, setAppConfig] = useState<AppConfig>({
    version: '0.1.0',
    backends: [],
    cacheEnabled: false,
    cacheDir: '',
    cacheSizeMaxMB: 1024,
    startMinimized: false,
    autoStart: false,
  })

  const { syncState, activeTransfers } = useSyncStatus()
  const { configs } = useBackends()

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100">
      <TrayMenu
        syncState={syncState}
        backends={configs}
        onOpenSettings={() => setShowSettings(true)}
      />
      <main className="container mx-auto p-4">
        {showSettings
          ? <SettingsPage appConfig={appConfig} onConfigChange={setAppConfig} />
          : <SyncStatusPanel syncState={syncState} activeTransfers={activeTransfers} />}
      </main>
    </div>
  )
}
