/**
 * Tests — Toast component (v2.2 #144 Avertissement Conflict)
 *
 * Couvre :
 *   - Rendu (message, icône, accessibilité)
 *   - Dismiss sur clic card + clic bouton ×
 *   - Auto-dismiss après 5 000 ms
 *   - Pas de dismiss avant l'échéance
 *   - Cleanup du timer au démontage
 */

import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { Toast } from './Toast';

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderToast(overrides: Partial<React.ComponentProps<typeof Toast>> = {}) {
  const defaults = {
    message:   'Conflit détecté : documents/rapport.docx (résolu automatiquement)',
    type:      'conflict' as const,
    onDismiss: vi.fn(),
  };
  return render(<Toast {...defaults} {...overrides} />);
}

afterEach(() => {
  vi.useRealTimers();
});

// ── Tests Rendu ───────────────────────────────────────────────────────────────

describe('Toast — rendu', () => {
  it('affiche le message transmis en prop', () => {
    renderToast({ message: 'Mon message de test' });
    expect(screen.getByText('Mon message de test')).toBeTruthy();
  });

  it('a role="alert" et aria-live="polite"', () => {
    const { container } = renderToast();
    const el = container.querySelector('[role="alert"]');
    expect(el).toBeTruthy();
    expect(el?.getAttribute('aria-live')).toBe('polite');
  });

  it('a aria-atomic="true"', () => {
    const { container } = renderToast();
    const el = container.querySelector('[role="alert"]');
    expect(el?.getAttribute('aria-atomic')).toBe('true');
  });

  it('affiche un bouton fermer avec aria-label', () => {
    renderToast();
    const btn = screen.getByRole('button', { name: /fermer la notification/i });
    expect(btn).toBeTruthy();
  });

  it('applique la classe amber pour le type conflict', () => {
    const { container } = renderToast({ type: 'conflict' });
    const el = container.querySelector('[role="alert"]');
    expect(el?.className).toContain('amber');
  });

  it('applique la classe red pour le type error', () => {
    const { container } = renderToast({ type: 'error' });
    const el = container.querySelector('[role="alert"]');
    expect(el?.className).toContain('red');
  });

  it('applique la classe blue pour le type info', () => {
    const { container } = renderToast({ type: 'info' });
    const el = container.querySelector('[role="alert"]');
    expect(el?.className).toContain('blue');
  });
});

// ── Tests Dismiss ─────────────────────────────────────────────────────────────

describe('Toast — dismiss', () => {
  it('appelle onDismiss au clic sur la card', () => {
    const onDismiss = vi.fn();
    renderToast({ onDismiss });
    fireEvent.click(screen.getByRole('alert'));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it('appelle onDismiss au clic sur le bouton ×', () => {
    const onDismiss = vi.fn();
    renderToast({ onDismiss });
    fireEvent.click(screen.getByRole('button', { name: /fermer la notification/i }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it('ne pas appeler onDismiss deux fois lors du clic sur le bouton × (stopPropagation)', () => {
    const onDismiss = vi.fn();
    renderToast({ onDismiss });
    fireEvent.click(screen.getByRole('button', { name: /fermer la notification/i }));
    // stopPropagation doit empêcher le clic de remonter sur la card
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});

// ── Tests Auto-Dismiss ────────────────────────────────────────────────────────

describe('Toast — auto-dismiss (timer)', () => {
  it('appelle onDismiss après 5000 ms', async () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    renderToast({ onDismiss });

    await act(async () => {
      vi.advanceTimersByTime(5_000);
    });

    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it('ne pas appeler onDismiss avant 5000 ms', async () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    renderToast({ onDismiss });

    await act(async () => {
      vi.advanceTimersByTime(4_999);
    });

    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('annule le timer au démontage du composant', async () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    const { unmount } = renderToast({ onDismiss });

    unmount();

    await act(async () => {
      vi.advanceTimersByTime(10_000); // bien au-delà de 5 s
    });

    // Le timer ayant été nettoyé, onDismiss ne doit pas être appelé.
    expect(onDismiss).not.toHaveBeenCalled();
  });
});
