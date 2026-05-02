import { useState, useMemo } from 'react';
import { Plus } from 'lucide-react';
import { BackendConfigCard } from './BackendConfig';
import { SyncPointForm } from './SyncPointForm';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { useBackends } from '../../hooks/useBackends';
import { useSyncStatus } from '../../hooks/useSyncStatus';
import { useDriveStatuses } from '../../hooks/useDriveStatus';
import type { BackendConfig } from '../../types/ghostdrive';

export function SettingsPage() {
  const [showAddModal, setShowAddModal]       = useState(false);
  const [editingConfig, setEditingConfig]     = useState<BackendConfig | null>(null);
  const [removeError, setRemoveError]         = useState<string | null>(null);

  const {
    configs, statuses, loading: backendsLoading,
    removeBackend, reload, setEnabled, setAutoSync,
  } = useBackends();
  const { syncState } = useSyncStatus();
  const { driveStatuses } = useDriveStatuses();

  // Stable reference — prevents infinite re-render in SyncPointForm useMemo
  const existingNames = useMemo(() => configs.map(c => c.name), [configs]);

  // Points de montage existants pour la validation d'unicité dans SyncPointForm
  const existingMountPoints = useMemo(
    () => configs.map(c => c.mountPoint ?? '').filter(Boolean),
    [configs],
  );

  const handleAddSuccess = (_config: BackendConfig) => {
    reload();
    setShowAddModal(false);
  };

  const handleEditSuccess = (_config: BackendConfig) => {
    reload();
    setEditingConfig(null);
  };

  const handleRemove = async (id: string) => {
    setRemoveError(null);
    try {
      await removeBackend(id);
    } catch (e) {
      setRemoveError((e as Error).message ?? 'Erreur lors de la suppression.');
    }
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-y-auto p-3">
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
              driveStatus={driveStatuses[cfg.id]}
              onRemove={handleRemove}
              onToggleEnabled={setEnabled}
              onToggleAutoSync={setAutoSync}
              onEdit={setEditingConfig}
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
      </div>

      <Modal
        open={showAddModal}
        onClose={() => setShowAddModal(false)}
        title="Ajouter un backend"
      >
        <SyncPointForm
          key={String(showAddModal)}
          onSuccess={handleAddSuccess}
          onCancel={() => setShowAddModal(false)}
          existingNames={existingNames}
          existingMountPoints={existingMountPoints}
        />
      </Modal>

      <Modal
        open={editingConfig !== null}
        onClose={() => setEditingConfig(null)}
        title="Modifier le backend"
      >
        {editingConfig && (
          <SyncPointForm
            key={editingConfig.id}
            initialConfig={editingConfig}
            onSuccess={handleEditSuccess}
            onCancel={() => setEditingConfig(null)}
            existingNames={existingNames}
            existingMountPoints={existingMountPoints}
          />
        )}
      </Modal>
    </div>
  );
}
