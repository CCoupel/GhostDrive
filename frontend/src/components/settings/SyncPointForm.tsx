import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Input } from '../ui/Input';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import type { BackendConfig, BackendType } from '../../types/ghostdrive';

const webdavSchema = z.object({
  name:     z.string().min(1, 'Requis'),
  syncDir:  z.string().min(1, 'Requis'),
  url:      z.string().url('URL invalide'),
  username: z.string().min(1, 'Requis'),
  password: z.string().min(1, 'Requis'),
});

const moosefsSchema = z.object({
  name:      z.string().min(1, 'Requis'),
  syncDir:   z.string().min(1, 'Requis'),
  mountPath: z.string().min(1, 'Requis'),
});

type WebDAVForm  = z.infer<typeof webdavSchema>;
type MooseFSForm = z.infer<typeof moosefsSchema>;

interface SyncPointFormProps {
  onSuccess: (config: BackendConfig) => void;
  onCancel: () => void;
}

export function SyncPointForm({ onSuccess, onCancel }: SyncPointFormProps) {
  const [backendType, setBackendType] = useState<BackendType>('webdav');
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);

  const webdavForm = useForm<WebDAVForm>({ resolver: zodResolver(webdavSchema) });
  const moosefsForm = useForm<MooseFSForm>({ resolver: zodResolver(moosefsSchema) });

  const handleSubmit = async (formData: WebDAVForm | MooseFSForm) => {
    setTestError(null);
    setTesting(true);

    const draft: Omit<BackendConfig, 'id'> = {
      name:    formData.name,
      type:    backendType,
      enabled: true,
      syncDir: formData.syncDir,
      params:  backendType === 'webdav'
        ? { url: (formData as WebDAVForm).url, username: (formData as WebDAVForm).username, password: (formData as WebDAVForm).password }
        : { mountPath: (formData as MooseFSForm).mountPath },
    };

    try {
      await ghostdriveApi.testBackendConnection({ ...draft, id: '' } as BackendConfig);
    } catch (e) {
      setTestError('Connexion impossible — vérifiez les paramètres.');
      setTesting(false);
      return;
    }

    try {
      const created = await ghostdriveApi.addBackend({ ...draft, id: '' } as BackendConfig);
      onSuccess(created);
    } catch (e) {
      setTestError((e as Error).message ?? 'Erreur lors de l\'ajout.');
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <div>
        <p className="text-xs font-medium text-gray-700 mb-1.5">Type de backend</p>
        <div className="flex gap-2" role="group" aria-label="Type de backend">
          {(['webdav', 'moosefs'] as BackendType[]).map(t => (
            <button
              key={t}
              type="button"
              onClick={() => setBackendType(t)}
              className={`flex-1 py-1.5 text-sm rounded border transition-colors
                ${backendType === t
                  ? 'border-brand bg-brand-light text-brand font-medium'
                  : 'border-surface-border text-gray-600 hover:bg-surface-secondary'
                }`}
              aria-pressed={backendType === t}
            >
              {t === 'webdav' ? 'WebDAV' : 'MooseFS'}
            </button>
          ))}
        </div>
      </div>

      {backendType === 'webdav' ? (
        <form
          id="sync-point-form"
          onSubmit={webdavForm.handleSubmit(d => handleSubmit(d))}
          className="flex flex-col gap-3"
        >
          <Input label="Nom" required placeholder="Mon NAS" {...webdavForm.register('name')} error={webdavForm.formState.errors.name?.message} />
          <Input label="URL WebDAV" required placeholder="https://nas.local/dav" {...webdavForm.register('url')} error={webdavForm.formState.errors.url?.message} />
          <Input label="Utilisateur" required autoComplete="username" {...webdavForm.register('username')} error={webdavForm.formState.errors.username?.message} />
          <Input label="Mot de passe" type="password" required autoComplete="current-password" {...webdavForm.register('password')} error={webdavForm.formState.errors.password?.message} />
          <Input label="Dossier local de sync" required placeholder="C:\GhostDrive\NAS" {...webdavForm.register('syncDir')} error={webdavForm.formState.errors.syncDir?.message} />
        </form>
      ) : (
        <form
          id="sync-point-form"
          onSubmit={moosefsForm.handleSubmit(d => handleSubmit(d))}
          className="flex flex-col gap-3"
        >
          <Input label="Nom" required placeholder="MooseFS Cluster" {...moosefsForm.register('name')} error={moosefsForm.formState.errors.name?.message} />
          <Input label="Chemin de montage FUSE" required placeholder="/mnt/moosefs" {...moosefsForm.register('mountPath')} error={moosefsForm.formState.errors.mountPath?.message} />
          <Input label="Dossier local de sync" required placeholder="C:\GhostDrive\MooseFS" {...moosefsForm.register('syncDir')} error={moosefsForm.formState.errors.syncDir?.message} />
        </form>
      )}

      {testError && (
        <p role="alert" className="text-xs text-red-500 bg-red-50 rounded px-2 py-1.5">
          {testError}
        </p>
      )}

      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onCancel} type="button">Annuler</Button>
        <Button variant="primary" type="submit" form="sync-point-form" disabled={testing}>
          {testing ? 'Test en cours...' : 'Ajouter'}
        </Button>
      </div>
    </div>
  );
}
