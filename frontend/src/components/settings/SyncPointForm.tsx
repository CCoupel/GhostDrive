import { useState, useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { CheckCircle, XCircle } from 'lucide-react';
import { Input } from '../ui/Input';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import type { BackendConfig, BackendStatus, BackendType } from '../../types/ghostdrive';
import { formatSpace } from '../../utils/formatBytes';

const commonFields = {
  name:       z.string().min(1, 'Requis').max(64, 'Max 64 caractères'),
  syncDir:    z.string().min(1, 'Requis'),
  remotePath: z.string().min(1, 'Requis').refine(v => v.startsWith('/'), 'Doit commencer par /'),
};

const webdavSchema = z.object({
  ...commonFields,
  url:      z.string().url('URL invalide'),
  username: z.string().min(1, 'Requis'),
  password: z.string().min(1, 'Requis'),
  insecure: z.boolean().optional(),
});

const moosefsSchema = z.object({
  ...commonFields,
  master:    z.string().min(1, 'Requis'),
  port:      z.string().optional(),
  mountPath: z.string().min(1, 'Requis').refine(v => v.startsWith('/'), 'Doit commencer par /'),
});

type WebDAVForm  = z.infer<typeof webdavSchema>;
type MooseFSForm = z.infer<typeof moosefsSchema>;

type TestState =
  | { status: 'idle' }
  | { status: 'testing' }
  | { status: 'ok'; result: BackendStatus }
  | { status: 'fail'; message: string };

interface SyncPointFormProps {
  onSuccess: (config: BackendConfig) => void;
  onCancel: () => void;
}

function buildDraft(
  formData: WebDAVForm | MooseFSForm,
  backendType: BackendType,
): Omit<BackendConfig, 'id'> {
  const base = {
    name:       formData.name,
    type:       backendType,
    enabled:    true,
    syncDir:    formData.syncDir,
    remotePath: formData.remotePath,
  };
  if (backendType === 'webdav') {
    const d = formData as WebDAVForm;
    return {
      ...base,
      params: {
        url:      d.url,
        username: d.username,
        password: d.password,
        ...(d.insecure ? { insecure: 'true' } : {}),
      },
    };
  }
  const d = formData as MooseFSForm;
  return {
    ...base,
    params: {
      master:    d.master,
      mountPath: d.mountPath,
      ...(d.port ? { port: d.port } : {}),
    },
  };
}


export function SyncPointForm({ onSuccess, onCancel }: SyncPointFormProps) {
  const [availableTypes, setAvailableTypes] = useState<BackendType[]>([]);
  const [typesLoading, setTypesLoading] = useState(true);
  const [backendType, setBackendType] = useState<BackendType>('webdav');
  const [testState, setTestState] = useState<TestState>({ status: 'idle' });
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    ghostdriveApi.getAvailableBackendTypes()
      .then(types => {
        setAvailableTypes(types as BackendType[]);
        if (types.length > 0) setBackendType(types[0] as BackendType);
      })
      .catch(() => setAvailableTypes([]))
      .finally(() => setTypesLoading(false));
  }, []);

  const webdavForm  = useForm<WebDAVForm>({ resolver: zodResolver(webdavSchema) });
  const moosefsForm = useForm<MooseFSForm>({ resolver: zodResolver(moosefsSchema) });

  const isWebDAV = backendType === 'webdav';

  const handleTest = async (formData: WebDAVForm | MooseFSForm) => {
    setTestState({ status: 'testing' });
    try {
      const result = await ghostdriveApi.testBackendConnection(
        { ...buildDraft(formData, backendType), id: '' } as BackendConfig,
      );
      setTestState({ status: 'ok', result });
    } catch (e) {
      setTestState({ status: 'fail', message: (e as Error).message ?? 'Connexion impossible' });
    }
  };

  const handleAdd = async (formData: WebDAVForm | MooseFSForm) => {
    setSubmitError(null);
    setSubmitting(true);
    try {
      const created = await ghostdriveApi.addBackend(
        { ...buildDraft(formData, backendType), id: '' } as BackendConfig,
      );
      onSuccess(created);
    } catch (e) {
      setSubmitError((e as Error).message ?? "Erreur lors de l'ajout.");
    } finally {
      setSubmitting(false);
    }
  };

  const triggerTest = isWebDAV
    ? webdavForm.handleSubmit(handleTest)
    : moosefsForm.handleSubmit(handleTest);

  if (typesLoading) {
    return <p className="text-sm text-gray-400 text-center py-6">Chargement des plugins...</p>;
  }

  if (availableTypes.length === 0) {
    return (
      <div className="text-center py-6">
        <p className="text-sm text-gray-500 mb-1">Aucun plugin de stockage disponible.</p>
        <p className="text-xs text-gray-400">Les plugins WebDAV et MooseFS seront disponibles dans la prochaine version.</p>
        <Button variant="secondary" onClick={onCancel} className="mt-4">Fermer</Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <p className="text-xs font-medium text-gray-700 mb-1.5">Type de backend</p>
        <div className="flex gap-2" role="group" aria-label="Type de backend">
          {availableTypes.map(t => (
            <button
              key={t}
              type="button"
              onClick={() => { setBackendType(t); setTestState({ status: 'idle' }); setSubmitError(null); }}
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

      {isWebDAV ? (
        <form
          id="sync-point-form"
          onSubmit={webdavForm.handleSubmit(handleAdd)}
          className="flex flex-col gap-3"
        >
          <Input
            label="Nom" required placeholder="Mon NAS"
            {...webdavForm.register('name')}
            error={webdavForm.formState.errors.name?.message}
          />
          <Input
            label="URL WebDAV" required placeholder="https://nas.local/dav"
            {...webdavForm.register('url')}
            error={webdavForm.formState.errors.url?.message}
          />
          <Input
            label="Utilisateur" required autoComplete="username"
            {...webdavForm.register('username')}
            error={webdavForm.formState.errors.username?.message}
          />
          <Input
            label="Mot de passe" type="password" required autoComplete="new-password"
            {...webdavForm.register('password')}
            error={webdavForm.formState.errors.password?.message}
          />
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              {...webdavForm.register('insecure')}
              className="accent-brand w-4 h-4"
            />
            <span className="text-sm text-gray-700">Accepter les certificats auto-signés</span>
          </label>
          <Input
            label="Dossier local de sync" required placeholder="C:\GhostDrive\NAS"
            {...webdavForm.register('syncDir')}
            error={webdavForm.formState.errors.syncDir?.message}
          />
          <Input
            label="Chemin distant" required placeholder="/GhostDrive"
            hint="Chemin racine sur le serveur WebDAV"
            {...webdavForm.register('remotePath')}
            error={webdavForm.formState.errors.remotePath?.message}
          />
        </form>
      ) : (
        <form
          id="sync-point-form"
          onSubmit={moosefsForm.handleSubmit(handleAdd)}
          className="flex flex-col gap-3"
        >
          <Input
            label="Nom" required placeholder="MooseFS Cluster"
            {...moosefsForm.register('name')}
            error={moosefsForm.formState.errors.name?.message}
          />
          <Input
            label="Adresse du Master" required placeholder="192.168.1.1"
            {...moosefsForm.register('master')}
            error={moosefsForm.formState.errors.master?.message}
          />
          <Input
            label="Port" placeholder="9421"
            hint="Défaut : 9421"
            {...moosefsForm.register('port')}
          />
          <Input
            label="Chemin de montage FUSE" required placeholder="/mnt/moosefs"
            {...moosefsForm.register('mountPath')}
            error={moosefsForm.formState.errors.mountPath?.message}
          />
          <Input
            label="Dossier local de sync" required placeholder="C:\GhostDrive\MooseFS"
            {...moosefsForm.register('syncDir')}
            error={moosefsForm.formState.errors.syncDir?.message}
          />
          <Input
            label="Chemin distant" required placeholder="/GhostDrive"
            hint="Chemin racine sur MooseFS"
            {...moosefsForm.register('remotePath')}
            error={moosefsForm.formState.errors.remotePath?.message}
          />
        </form>
      )}

      {testState.status === 'ok' && (
        <div className="flex items-center gap-1.5 text-xs text-status-idle bg-green-50 rounded px-2 py-1.5">
          <CheckCircle size={13} />
          <span>
            Connexion réussie — Libre&nbsp;: {formatSpace(testState.result.freeSpace)}
            {' / '}Total&nbsp;: {formatSpace(testState.result.totalSpace)}
          </span>
        </div>
      )}
      {testState.status === 'fail' && (
        <div
          role="alert"
          className="flex items-center gap-1.5 text-xs text-red-500 bg-red-50 rounded px-2 py-1.5"
        >
          <XCircle size={13} />
          <span>{testState.message}</span>
        </div>
      )}
      {submitError && (
        <p role="alert" className="text-xs text-red-500 bg-red-50 rounded px-2 py-1.5">
          {submitError}
        </p>
      )}

      <div className="flex justify-end gap-2">
        <Button variant="secondary" type="button" onClick={onCancel}>
          Annuler
        </Button>
        <Button
          variant="ghost"
          type="button"
          disabled={testState.status === 'testing'}
          onClick={() => void triggerTest()}
        >
          {testState.status === 'testing' ? 'Test...' : 'Tester la connexion'}
        </Button>
        <Button variant="primary" type="submit" form="sync-point-form" disabled={submitting}>
          {submitting ? 'Ajout...' : 'Ajouter'}
        </Button>
      </div>
    </div>
  );
}
