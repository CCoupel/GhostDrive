import { useState, useEffect } from 'react';
import { Plus, HardDrive, Sliders, Database } from 'lucide-react';
import { BackendConfigCard } from './BackendConfig';
import { SyncPointForm } from './SyncPointForm';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import { useBackends } from '../../hooks/useBackends';
import { useSyncStatus } from '../../hooks/useSyncStatus';
import type { AppConfig, BackendConfig, CacheStats } from '../../types/ghostdrive';

type Tab = 'backends' | 'prefs' | 'cache';

interface SettingsPageProps {
  appConfig: AppConfig | null;
  onConfigChange: (cfg: AppConfig) => void;
}

export function SettingsPage({ appConfig, onConfigChange }: SettingsPageProps) {
  const [tab, setTab] = useState<Tab>('backends');
  const [showAddModal, setShowAddModal] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);
  const { configs, statuses, loading: backendsLoading, removeBackend, reload } = useBackends();
  const { syncState } = useSyncStatus();

  const handleAddSuccess = (_config: BackendConfig) => {
    reload();
    setShowAddModal(false);
  };

  const handleRemove = async (id: string) => {
    setRemoveError(null);
    try {
      await removeBackend(id);
    } catch (e) {
      setRemoveError((e as Error).message ?? 'Erreur lors de la suppression.');
    }
  };

  const handleSavePrefs = async (updates: Partial<AppConfig>) => {
    if (!appConfig) return;
    const next: AppConfig = { ...appConfig, ...updates };
    await ghostdriveApi.saveConfig(next);
    onConfigChange(next);
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex border-b border-surface-border px-3">
        <TabButton active={tab === 'backends'} onClick={() => setTab('backends')}>
          <HardDrive size={13} /> Backends
        </TabButton>
        <TabButton active={tab === 'prefs'} onClick={() => setTab('prefs')}>
          <Sliders size={13} /> Préférences
        </TabButton>
        <TabButton active={tab === 'cache'} onClick={() => setTab('cache')}>
          <Database size={13} /> Cache
        </TabButton>
      </div>

      <div className="flex-1 overflow-y-auto p-3">
        {tab === 'backends' && (
          <div className="flex flex-col gap-2">
            {removeError && (
              <p role="alert" className="text-xs text-red-500 bg-red-50 rounded px-2 py-1.5">
                {removeError}
              </p>
            )}
            {backendsLoading && (
              <p className="text-sm text-gray-400 text-center py-6">Chargement...</p>
            )}
            {!backendsLoading && configs.length === 0 && (
              <p className="text-sm text-gray-400 text-center py-6">
                Aucun backend configuré.<br />Ajoutez-en un pour commencer.
              </p>
            )}

            {!backendsLoading && configs.map(cfg => (
              <BackendConfigCard
                key={cfg.id}
                config={cfg}
                status={statuses.find(s => s.backendId === cfg.id)}
                syncState={syncState.backends.find(b => b.backendId === cfg.id)}
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

        {tab === 'prefs' && appConfig && (
          <PrefsPanel config={appConfig} onSave={handleSavePrefs} />
        )}

        {tab === 'cache' && (
          <CachePanel />
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

function ToggleRow({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center justify-between py-1 cursor-pointer">
      <span className="text-sm text-gray-800">{label}</span>
      <input
        type="checkbox"
        checked={checked}
        onChange={e => onChange(e.target.checked)}
        className="accent-brand w-4 h-4"
        aria-label={label}
      />
    </label>
  );
}

function PrefsPanel({
  config,
  onSave,
}: {
  config: AppConfig;
  onSave: (updates: Partial<AppConfig>) => void;
}) {
  return (
    <div className="flex flex-col gap-4">
      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">Démarrage</h3>
        <div className="space-y-1">
          <ToggleRow
            label="Démarrer avec Windows"
            checked={config.autoStart}
            onChange={v => onSave({ autoStart: v })}
          />
          <ToggleRow
            label="Démarrer minimisé"
            checked={config.startMinimized}
            onChange={v => onSave({ startMinimized: v })}
          />
        </div>
      </section>

      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">Cache local</h3>
        <div className="space-y-1">
          <ToggleRow
            label="Activer le cache"
            checked={config.cacheEnabled}
            onChange={v => onSave({ cacheEnabled: v })}
          />
          {config.cacheEnabled && (
            <div className="flex items-center justify-between py-1">
              <label htmlFor="cache-max" className="text-sm text-gray-800">
                Taille max (Mo)
              </label>
              <input
                id="cache-max"
                type="number"
                min={64}
                max={102400}
                step={64}
                defaultValue={config.cacheSizeMaxMB}
                onBlur={e => onSave({ cacheSizeMaxMB: Number(e.target.value) })}
                className="w-24 rounded border border-surface-border px-2 py-1 text-sm text-right
                  focus:outline-none focus:ring-2 focus:ring-brand"
                aria-label="Taille maximale du cache en Mo"
              />
            </div>
          )}
        </div>
      </section>

      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">À propos</h3>
        <p className="text-xs text-gray-500">GhostDrive v{config.version}</p>
      </section>
    </div>
  );
}

function CachePanel() {
  const [stats, setStats] = useState<CacheStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [clearing, setClearing] = useState(false);

  useEffect(() => {
    ghostdriveApi.getCacheStats()
      .then(s => { setStats(s); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const handleClear = async () => {
    if (!window.confirm('Vider le cache local ? Les fichiers placeholders devront être re-téléchargés.')) return;
    setClearing(true);
    try {
      await ghostdriveApi.clearCache();
      const s = await ghostdriveApi.getCacheStats();
      setStats(s);
    } finally {
      setClearing(false);
    }
  };

  if (loading) {
    return <p className="text-sm text-gray-400 py-4 text-center">Chargement...</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      <section>
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">Statistiques</h3>
        {stats ? (
          <dl className="space-y-1">
            <Row label="Taille utilisée" value={`${stats.sizeMB} Mo`} />
            <Row label="Fichiers en cache" value={String(stats.fileCount)} />
            <Row label="Limite configurée" value={`${stats.maxSizeMB} Mo`} />
          </dl>
        ) : (
          <p className="text-sm text-gray-400">Statistiques indisponibles.</p>
        )}
      </section>

      <Button
        variant="secondary"
        onClick={handleClear}
        disabled={clearing || !stats || stats.fileCount === 0}
        className="w-full justify-center"
      >
        {clearing ? 'Vidage...' : 'Vider le cache'}
      </Button>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between py-0.5">
      <dt className="text-sm text-gray-600">{label}</dt>
      <dd className="text-sm font-medium text-gray-900">{value}</dd>
    </div>
  );
}
