/**
 * Non-regression tests — bugfix #84 : affichage du champ "Distant"
 *
 * Avant le fix : la carte d'un backend WebDAV affichait "Distant: /" (remotePath
 * par défaut) au lieu du basePath réel configuré dans les params.
 * Après le fix : `config.params?.basePath || config.remotePath || '—'`
 *
 * Référence commit : 35b8190
 */

import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { BackendConfigCard } from './BackendConfig';
import type { BackendConfig, BackendStatus } from '../../types/ghostdrive';

// ── Mock Wails API ─────────────────────────────────────────────────────────────
// ghostdriveApi n'est appelé que sur interactions utilisateur ; on le remplace
// pour éviter l'import des bindings wailsjs dans jsdom.
vi.mock('../../services/wails', () => ({
  ghostdriveApi: {
    pauseSync:      vi.fn(),
    startSync:      vi.fn(),
    stopSync:       vi.fn(),
    forceSync:      vi.fn(),
    openSyncFolder: vi.fn(),
  },
}));

// ── Helpers ────────────────────────────────────────────────────────────────────

/** Construit un BackendConfig minimal valide avec des overrides optionnels. */
function makeConfig(overrides: Partial<BackendConfig> = {}): BackendConfig {
  return {
    id:         'test-id',
    name:       'Test Backend',
    type:       'webdav',
    enabled:    true,
    autoSync:   false,
    params:     {},
    syncDir:    '/sync/test',
    remotePath: '/remote',
    localPath:  '/local/test',
    mountPoint: 'E:',
    ...overrides,
  };
}

/** Rend la carte et retourne le paragraphe "Distant". */
function renderCard(config: BackendConfig) {
  render(
    <BackendConfigCard
      config={config}
      onRemove={vi.fn()}
      onToggleEnabled={vi.fn()}
      onToggleAutoSync={vi.fn()}
      onEdit={vi.fn()}
    />,
  );

  // Le libellé "Distant :" est dans un <span> ; on remonte au <p> parent.
  const label = screen.getByText(/Distant\s*:/);
  const paragraph = label.closest('p');
  if (!paragraph) throw new Error('Paragraphe "Distant" introuvable dans le DOM');
  return paragraph;
}

// ── Tests ──────────────────────────────────────────────────────────────────────

describe('BackendConfigCard — champ "Distant" (#84)', () => {

  it('affiche params.basePath quand il est défini (cas WebDAV)', () => {
    const config = makeConfig({
      type:       'webdav',
      params:     { basePath: '/dav/nas', url: 'https://nas.local' },
      remotePath: '/remote',
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('/dav/nas');
    // Régression : NE doit PAS afficher remotePath à la place
    expect(p.textContent).not.toContain('/remote');
  });

  it('affiche remotePath quand params.basePath est absent', () => {
    const config = makeConfig({
      type:       'webdav',
      params:     { url: 'https://nas.local' },   // pas de basePath
      remotePath: '/my/remote/path',
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('/my/remote/path');
  });

  it('affiche "—" quand ni basePath ni remotePath ne sont définis', () => {
    const config = makeConfig({
      type:       'webdav',
      params:     {},   // pas de basePath
      remotePath: '',   // vide → falsy
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('—');
  });

  // ── Non-régression #107 — MooseFS subDir ──────────────────────────────────
  // Avant le fix : MooseFS affichait "/" (remotePath par défaut) au lieu de
  // la valeur de params.subDir configurée par l'utilisateur.
  // Après le fix : `config.params?.basePath || config.params?.subDir || config.remotePath || '—'`

  it('affiche params.subDir pour un backend MooseFS (#107)', () => {
    const config = makeConfig({
      type:       'moosefs',
      params:     { masterHost: '192.168.1.10', subDir: '/MEDIA' },
      remotePath: '/',   // valeur par défaut — NE doit PAS être affichée
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('/MEDIA');
    // Régression : NE doit PAS afficher uniquement "/" (remotePath par défaut)
    // Note : 'Distant : /MEDIA' contiendrait 'Distant : /' en tant que préfixe,
    // donc on vérifie l'égalité exacte plutôt qu'une inclusion.
    expect(p.textContent?.replace(/\s+/g, ' ').trim()).toBe('Distant : /MEDIA');
  });

  it('affiche remotePath quand params.subDir est absent (MooseFS sans subDir configuré)', () => {
    const config = makeConfig({
      type:       'moosefs',
      params:     { masterHost: '192.168.1.10' },   // pas de subDir
      remotePath: '/fallback',
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('/fallback');
  });

  it('affiche "—" quand params.subDir est vide et remotePath absent (MooseFS full-fallback)', () => {
    // subDir="" est falsy → tombe sur remotePath → aussi falsy → '—'.
    // Vérifie que la chaîne basePath || subDir || remotePath || '—' atteint bien '—'.
    const config = makeConfig({
      type:       'moosefs',
      params:     { masterHost: '192.168.1.10', subDir: '' },
      remotePath: '',
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('—');
    expect(p.textContent).not.toContain('/');
  });

  it('affiche "—" quand subDir et remotePath sont tous deux absents (MooseFS no-config)', () => {
    // Cas MooseFS configuré sans subDir ni remotePath : doit afficher '—'.
    const config = makeConfig({
      type:       'moosefs',
      params:     { masterHost: '192.168.1.10' },
      remotePath: '',
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('—');
  });

  it('non impacté par le fix — backend local affiche remotePath (pas de basePath ni subDir)', () => {
    // Le fix ajoute params.subDir dans la chaîne ; pour un backend local (rootPath),
    // ni basePath ni subDir ne sont définis → remotePath doit continuer à s'afficher.
    const config = makeConfig({
      type:       'local',
      params:     { rootPath: 'C:\\Users\\user\\Documents' },   // pas de basePath, pas de subDir
      remotePath: '/local/mirror',
    });

    const p = renderCard(config);

    expect(p.textContent).toContain('/local/mirror');
    // Régression : la présence de params.rootPath ne doit pas perturber l'affichage.
    expect(p.textContent).not.toContain('rootPath');
  });

});

// ── Tests quota display (#87) ──────────────────────────────────────────────────
//
// Avant le fix : la ligne "Libre / Total" s'affichait même quand freeSpace = -1
// (valeur sentinelle indiquant que le plugin ne connaît pas le quota).
// Après le fix :
//   - freeSpace >= 0  → "Libre : X / Total : Y"
//   - freeSpace < 0   → "Quota non disponible"
//
// Référence commit : fix(ui): quota display guards freeSpace >= 0 (#87)

/** Construit un BackendStatus avec des overrides optionnels. */
function makeStatus(overrides: Partial<BackendStatus> = {}): BackendStatus {
  return {
    backendId:  'test-id',
    connected:  true,
    freeSpace:  1073741824,   // 1 Go — valeur par défaut positive
    totalSpace: 5368709120,   // 5 Go
    ...overrides,
  };
}

describe('BackendConfigCard — affichage quota (#87)', () => {

  it('affiche "Libre :" et "Total :" quand freeSpace >= 0', () => {
    const config = makeConfig({ enabled: true });
    const status = makeStatus({ freeSpace: 1073741824, totalSpace: 5368709120 });

    render(
      <BackendConfigCard
        config={config}
        status={status}
        onRemove={vi.fn()}
        onToggleEnabled={vi.fn()}
        onToggleAutoSync={vi.fn()}
        onEdit={vi.fn()}
      />,
    );

    // Le paragraphe "Libre : X / Total : Y" doit être présent.
    expect(screen.getByText(/Libre\s*:/)).toBeTruthy();
    expect(screen.getByText(/Libre\s*:/).textContent).toContain('Total');
  });

  it('affiche "Quota non disponible" quand freeSpace < 0', () => {
    const config = makeConfig({ enabled: true });
    const status = makeStatus({ freeSpace: -1, totalSpace: -1 });

    render(
      <BackendConfigCard
        config={config}
        status={status}
        onRemove={vi.fn()}
        onToggleEnabled={vi.fn()}
        onToggleAutoSync={vi.fn()}
        onEdit={vi.fn()}
      />,
    );

    expect(screen.getByText('Quota non disponible')).toBeTruthy();
  });

  it('n\'affiche PAS "Libre :" quand freeSpace < 0', () => {
    const config = makeConfig({ enabled: true });
    const status = makeStatus({ freeSpace: -1, totalSpace: -1 });

    render(
      <BackendConfigCard
        config={config}
        status={status}
        onRemove={vi.fn()}
        onToggleEnabled={vi.fn()}
        onToggleAutoSync={vi.fn()}
        onEdit={vi.fn()}
      />,
    );

    // Régression : la ligne "Libre :" ne doit pas apparaître.
    expect(screen.queryByText(/Libre\s*:/)).toBeNull();
  });

});
