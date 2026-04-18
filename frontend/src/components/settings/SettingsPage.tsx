import { useState } from 'react';
import { Plus, HardDrive, Sliders } from 'lucide-react';
import { BackendConfigCard } from './BackendConfig';
import { SyncPointForm } from './SyncPointForm';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import { useBackends } from '../../hooks/useBackends';
import { useSyncStatus } from '../../hooks/useSyncStatus';
import type { AppConfig, BackendConfig } from '../../types/ghostdrive';

interface SettingsPageProps {
  appConfig: AppConfig | null;
  onConfigChange: (cfg: AppConfig) => void;
}

export function SettingsPage({ appConfig, onConfigChange }: SettingsPageProps) {
  const [tab, setTab] = useState<'backends' | 'app'>('backends');
  const [showAddModal, setShowAddModal] = useState(false);
  const { configs, statuses, removeBackend, reload } = useBackends();
  const { syncState } = useSyncStatus();

  const handleAddSuccess = (config: BackendConfig) => {
    reload();
    setShowAddModal(false);
    void config;
  };

  const handleRemove = async (id: string) => {
    if (!window.confirm('Supprimer ce backend ?')) return;
    await removeBackend(id);
  };

  const handleCacheToggle = async () => {
    if (!appConfig) return;
    const next: AppConfig = { ...appConfig, cacheEnabled: !appConfig.cacheEnabled };
    await ghostdriveApi.saveConfig(next);
    onConfigChange(next);
  };

  const handleClearCache = async () => {
    if (!window.confirm('Vider le cache local ?')) return;
    await ghostdriveApi.clearCache();
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex border-b border-surface-border px-3">
        <TabButton active={tab === 'backends'} onClick={() => setTab('backends')}>
          <HardDrive size={13} /> Backends
        </TabButton>
        <TabButton active={tab === 'app'} onClick={() => setTab('app')}>
          <Sliders size={13} /> Application
        </TabButton>
      </div>

      <div className="flex-1 overflow-y-auto p-3">
        {tab === 'backends' && (
          <div className="flex flex-col gap-2">
            {configs.length === 0 && (
              <p className="text-sm text-gray-400 text-center py-6">
                Aucun backend configuré.<br />Ajoutez-en un pour commencer.
              </p>
            )}

            {configs.map(cfg => (
              <BackendConfigCard
                key={cfg.id}
                config={cfg}
                status={statuses.find(s => s.backendId === cfg.id)}
                syncStatus={syncState.status}
                onRemove={handleRemove}
              />
            ))}

            <Button
              variant="secondary"
              onClick={() => setShowAddModal(true)}
              className="w-full justify-center mt-1"
            >
              <Plus size={14} /> Ajouter un backend
            </Button>
          </div>
        )}

        {tab === 'app' && appConfig && (
          <AppSettingsPanel
            config={appConfig}
            onCacheToggle={handleCacheToggle}
            onClearCache={handleClearCache}
          />
        )}
      </div>

      <Modal
        open={showAddModal}
        onClose={() => setShowAddModal(false)}
        title="Ajouter un backend"
      >
        <SyncPointForm
          onSuccess={handleAddSuccess}
          onCancel={() => setShowAddModal(false)}
        />
      </Modal>
    </div>
  );
}

function TabButton({
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
      className={`flex items-center gap-1.5 px-3 py-2 text-sm transition-colors ${
        active ? 'tab-active' : 'tab-inactive'
      }`}
    >
      {children}
    </button>
  );
}

interface AppSettingsPanelProps {
  config: AppConfig;
  onCacheToggle: () => void;
  onClearCache: () => void;
}

function AppSettingsPanel({ config, onCacheToggle, onClearCache }: AppSettingsPanelProps) {
  return (
    <div className="flex flex-col gap-4">
      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">Cache local</h3>
        <div className="flex flex-col gap-2">
          <label className="flex items-center justify-between py-1.5 cursor-pointer">
            <span className="text-sm text-gray-800">Activer le cache</span>
            <input
              type="checkbox"
              checked={config.cacheEnabled}
              onChange={onCacheToggle}
              className="accent-brand w-4 h-4"
              aria-label="Activer le cache local"
            />
          </label>
          {config.cacheEnabled && (
            <div className="flex items-center justify-between">
              <span className="text-xs text-gray-500">Taille max : {config.cacheSizeMaxMB} Mo</span>
              <Button size="sm" variant="secondary" onClick={onClearCache}>
                Vider le cache
              </Button>
            </div>
          )}
        </div>
      </section>

      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">Démarrage</h3>
        <div className="space-y-2">
          <ToggleRow label="Démarrer avec Windows" checked={config.autoStart} onChange={() => {}} />
          <ToggleRow label="Démarrer minimisé" checked={config.startMinimized} onChange={() => {}} />
        </div>
      </section>

      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">À propos</h3>
        <p className="text-xs text-gray-500">GhostDrive v{config.version}</p>
      </section>
    </div>
  );
}

function ToggleRow({ label, checked, onChange }: { label: string; checked: boolean; onChange: () => void }) {
  return (
    <label className="flex items-center justify-between py-1 cursor-pointer">
      <span className="text-sm text-gray-800">{label}</span>
      <input
        type="checkbox"
        checked={checked}
        onChange={onChange}
        className="accent-brand w-4 h-4"
        aria-label={label}
      />
    </label>
  );
}
