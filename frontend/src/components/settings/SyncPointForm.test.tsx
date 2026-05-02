/**
 * Tests — SyncPointForm (v1.1.x — #85 #88)
 *
 * Couvre :
 *   - Affichage du sélecteur de point de montage (mountPoint) à la création
 *   - Valeur par défaut enabled=false dans le brouillon soumis
 *   - Validation conflit mountPoint : message d'erreur affiché
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { SyncPointForm } from './SyncPointForm';
import type { BackendConfig, PluginDescriptor } from '../../types/ghostdrive';

// ── Mock Wails bindings ────────────────────────────────────────────────────────
//
// SyncPointForm appelle :
//   ghostdriveApi.getPluginDescriptors()
//   ghostdriveApi.getAvailableDriveLetters()
//   ghostdriveApi.addBackend()
//   GetGhostDriveRoot()   (wailsjs/go/app/App)
//   SelectDirectory()      (wailsjs/go/app/App)
//
// Note: vi.mock() factories are hoisted to the top of the file by Vitest.
// Variables defined with const/let are in the temporal dead zone at that point.
// vi.hoisted() creates variables that are available during the hoisting phase.

const { mockGetPluginDescriptors, mockGetAvailableDriveLetters, mockAddBackend } = vi.hoisted(() => ({
  mockGetPluginDescriptors:    vi.fn(),
  mockGetAvailableDriveLetters: vi.fn(),
  mockAddBackend:              vi.fn(),
}));

vi.mock('../../services/wails', () => ({
  ghostdriveApi: {
    getPluginDescriptors:     mockGetPluginDescriptors,
    getAvailableDriveLetters: mockGetAvailableDriveLetters,
    addBackend:               mockAddBackend,
    testBackendConnection:    vi.fn(),
    updateBackend:            vi.fn(),
  },
}));

vi.mock('../../../wailsjs/go/app/App', () => ({
  GetGhostDriveRoot: vi.fn().mockResolvedValue('C:\\GhostDrive'),
  SelectDirectory:   vi.fn().mockResolvedValue(''),
}));

// ── Descripteur de plugin pour les tests ────────────────────────────────────
const LOCAL_DESCRIPTOR: PluginDescriptor = {
  type:        'local',
  displayName: 'Local',
  description: 'Dossier local',
  params: [
    {
      key:         'rootPath',
      label:       'Dossier source',
      type:        'path',
      required:    true,
      default:     '',
      placeholder: 'C:\\src',
      options:     [],
      helpText:    '',
    },
  ],
};

// ── Helpers ────────────────────────────────────────────────────────────────────

function renderForm(props: Partial<React.ComponentProps<typeof SyncPointForm>> = {}) {
  const defaults = {
    onSuccess: vi.fn(),
    onCancel:  vi.fn(),
  };
  return render(<SyncPointForm {...defaults} {...props} />);
}

// Attend que le formulaire soit chargé (spinner "Chargement des plugins..." disparaît)
async function waitForFormReady() {
  await waitFor(() =>
    expect(screen.queryByText(/Chargement des plugins/)).toBeNull(),
  );
}

// ── Tests ──────────────────────────────────────────────────────────────────────

describe('SyncPointForm — sélecteur mountPoint (#88)', () => {

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetPluginDescriptors.mockResolvedValue([LOCAL_DESCRIPTOR]);
    mockGetAvailableDriveLetters.mockResolvedValue(['E:', 'F:', 'G:']);
  });

  it('affiche le sélecteur de lettre de lecteur en mode création', async () => {
    renderForm();
    await waitForFormReady();

    // La légende "Lettre de lecteur" doit être présente.
    expect(screen.getByText(/Lettre de lecteur/i)).toBeTruthy();
  });

  it('propose les lettres disponibles renvoyées par getAvailableDriveLetters', async () => {
    renderForm();
    await waitForFormReady();

    // Le <select> de lettre doit contenir au moins E:.
    const select = screen.getByRole('combobox', { name: /Lettre de lecteur/i });
    expect(select).toBeTruthy();
    const options = Array.from((select as HTMLSelectElement).options).map(o => o.value);
    expect(options).toContain('E:');
  });

  it('affiche le sélecteur de chemin libre (mode path)', async () => {
    renderForm();
    await waitForFormReady();

    // Sélectionner le mode "Chemin libre"
    const radioPath = screen.getByDisplayValue('path');
    fireEvent.click(radioPath);

    // Un input avec aria-label "Chemin du point de montage" doit apparaître.
    await waitFor(() =>
      expect(screen.getByLabelText(/Chemin du point de montage/i)).toBeTruthy(),
    );
  });

});

describe('SyncPointForm — enabled=false par défaut (#85)', () => {

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetPluginDescriptors.mockResolvedValue([LOCAL_DESCRIPTOR]);
    mockGetAvailableDriveLetters.mockResolvedValue(['E:', 'F:']);
    mockAddBackend.mockImplementation(
      (config: BackendConfig) => Promise.resolve({ ...config, id: 'new-id' }),
    );
  });

  it('soumet un objet avec enabled=false', async () => {
    const onSuccess = vi.fn();
    renderForm({ onSuccess });
    await waitForFormReady();

    // Remplir le nom du backend
    const nameInput = screen.getByPlaceholderText(/MonNAS/i);
    fireEvent.change(nameInput, { target: { value: 'TestBackend' } });

    // Remplir le rootPath (paramètre requis du plugin "local")
    const rootPathInput = screen.getByPlaceholderText(/C:\\src/i);
    fireEvent.change(rootPathInput, { target: { value: 'C:\\source' } });

    // Cliquer sur "Ajouter"
    const submitBtn = screen.getByRole('button', { name: /Ajouter/i });
    fireEvent.click(submitBtn);

    await waitFor(() => expect(mockAddBackend).toHaveBeenCalled());

    const submitted = mockAddBackend.mock.calls[0][0] as BackendConfig;
    expect(submitted.enabled).toBe(false);
  });

});

describe('SyncPointForm — validation conflit mountPoint (#88)', () => {

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetPluginDescriptors.mockResolvedValue([LOCAL_DESCRIPTOR]);
    // Simuler une lettre déjà utilisée : getAvailableDriveLetters renvoie E:
    // mais existingMountPoints contient E:
    mockGetAvailableDriveLetters.mockResolvedValue(['E:']);
    mockAddBackend.mockImplementation(
      (config: BackendConfig) => Promise.resolve({ ...config, id: 'new-id' }),
    );
  });

  it("affiche un message d'erreur quand le mountPoint est déjà utilisé", async () => {
    renderForm({
      existingMountPoints: ['E:'],
      // La lettre pré-sélectionnée sera E: (fournie par getAvailableDriveLetters)
    });
    await waitForFormReady();

    // Remplir le nom
    const nameInput = screen.getByPlaceholderText(/MonNAS/i);
    fireEvent.change(nameInput, { target: { value: 'BackendConflict' } });

    // Remplir le rootPath
    const rootPathInput = screen.getByPlaceholderText(/C:\\src/i);
    fireEvent.change(rootPathInput, { target: { value: 'C:\\source' } });

    // Cliquer "Ajouter" pour déclencher la validation
    const submitBtn = screen.getByRole('button', { name: /Ajouter/i });
    fireEvent.click(submitBtn);

    // Un message d'erreur de conflit mountPoint doit apparaître.
    await waitFor(() => {
      const alerts = screen.getAllByRole('alert');
      const hasConflictMsg = alerts.some(el =>
        el.textContent?.includes('point de montage') ||
        el.textContent?.includes('déjà utilisé'),
      );
      expect(hasConflictMsg).toBe(true);
    });

    // addBackend ne doit PAS avoir été appelé.
    expect(mockAddBackend).not.toHaveBeenCalled();
  });

});
