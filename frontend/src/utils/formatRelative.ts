export function formatRelative(timestamp: string): string {
  if (!timestamp) return '—';
  const diff = Date.now() - new Date(timestamp).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return "à l'instant";
  const m = Math.floor(s / 60);
  if (m < 60) return `il y a ${m} min`;
  const h = Math.floor(m / 60);
  if (h < 24) return `il y a ${h} h`;
  return new Date(timestamp).toLocaleDateString('fr-FR', { day: '2-digit', month: 'short' });
}
