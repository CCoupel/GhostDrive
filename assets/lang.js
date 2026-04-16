/**
 * GhostDrive — i18n manager
 * Charge le fichier JSON de la locale et remplace tous les
 * elements data-i18n="cle" par la valeur correspondante.
 * La preference est stockee dans localStorage (cle: "gd_lang").
 * Defaut : "fr"
 */

(function () {
  'use strict';

  const STORAGE_KEY = 'gd_lang';
  const SUPPORTED = ['fr', 'en'];
  const DEFAULT_LANG = 'fr';

  let currentLang = DEFAULT_LANG;
  let translations = {};

  /**
   * Determine la langue initiale :
   * 1. localStorage
   * 2. navigator.language
   * 3. defaut "fr"
   */
  function detectLang() {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored && SUPPORTED.includes(stored)) return stored;

    const browser = (navigator.language || '').substring(0, 2).toLowerCase();
    if (SUPPORTED.includes(browser)) return browser;

    return DEFAULT_LANG;
  }

  /**
   * Charge le fichier JSON de la locale demandee.
   * @param {string} lang
   * @returns {Promise<object>}
   */
  async function loadLocale(lang) {
    const url = `locales/${lang}.json?v=${Date.now()}`;
    try {
      const res = await fetch(url);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return await res.json();
    } catch (err) {
      console.warn(`[i18n] Failed to load locale "${lang}":`, err);
      return {};
    }
  }

  /**
   * Applique les traductions au DOM.
   * Gere aussi les listes (arrays) via data-i18n-list.
   */
  function applyTranslations() {
    // Textes simples
    document.querySelectorAll('[data-i18n]').forEach((el) => {
      const key = el.getAttribute('data-i18n');
      const value = translations[key];
      if (value !== undefined && typeof value === 'string') {
        el.textContent = value;
      }
    });

    // Listes (li multiples) — genere les <li> dynamiquement
    document.querySelectorAll('[data-i18n-list]').forEach((el) => {
      const key = el.getAttribute('data-i18n-list');
      const value = translations[key];
      if (Array.isArray(value)) {
        el.innerHTML = value.map((item) => `<li>${escapeHtml(item)}</li>`).join('');
      }
    });

    // Attributs (placeholder, title, aria-label…)
    document.querySelectorAll('[data-i18n-attr]').forEach((el) => {
      const pairs = el.getAttribute('data-i18n-attr').split(',');
      pairs.forEach((pair) => {
        const [attr, key] = pair.trim().split(':');
        const value = translations[key];
        if (value !== undefined && typeof value === 'string') {
          el.setAttribute(attr.trim(), value);
        }
      });
    });
  }

  function escapeHtml(str) {
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  /**
   * Met a jour les boutons du commutateur.
   */
  function updateSwitcher(lang) {
    document.querySelectorAll('.lang-btn').forEach((btn) => {
      btn.classList.toggle('active', btn.getAttribute('data-lang') === lang);
    });
    document.documentElement.lang = lang;
  }

  /**
   * Point d'entree principal.
   * @param {string} lang
   */
  async function setLanguage(lang) {
    if (!SUPPORTED.includes(lang)) lang = DEFAULT_LANG;
    currentLang = lang;
    localStorage.setItem(STORAGE_KEY, lang);
    translations = await loadLocale(lang);
    applyTranslations();
    updateSwitcher(lang);
    document.dispatchEvent(new CustomEvent('langchange', { detail: { lang } }));
  }

  /**
   * Initialisation : attache les evenements et charge la locale.
   */
  function init() {
    const lang = detectLang();

    // Attache les boutons du commutateur
    document.addEventListener('click', (e) => {
      const btn = e.target.closest('.lang-btn');
      if (btn) {
        const newLang = btn.getAttribute('data-lang');
        if (newLang && newLang !== currentLang) {
          setLanguage(newLang);
        }
      }
    });

    setLanguage(lang);
  }

  // Expose l'API publique
  window.i18n = {
    set: setLanguage,
    get: () => currentLang,
    t: (key) => translations[key] || key,
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
