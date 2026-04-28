# Changelog des Contrats API

Ce fichier documente tous les changements de contrats API (Wails bindings, événements, modèles).
Les changements **BREAKING** doivent être validés par le CDP avant implémentation.

---

## [20260428] — v0.6.0 plugin-loader

- **[NEW]** `GetLoadedPlugins() []PluginInfo` — liste des plugins dynamiques chargés depuis `<AppDir>/plugins/*.exe`
- **[NEW]** `ReloadPlugins() error` — rescan du dossier plugins sans redémarrage de l'app
- **[NEW]** Événements : `plugin:loaded`, `plugin:failed`, `plugin:restarting`, `plugin:reloaded`
- **[NEW]** Type `PluginInfo` — voir `contracts/plugin-loader-bindings.md`
- **[CHANGED]** `GetAvailableBackendTypes()` — inclut désormais les plugins dynamiques en plus des statiques (rétrocompatible, aucune modification frontend requise)
- **[NEW]** Contrat `contracts/plugin-loader-bindings.md` — spécification complète des bindings Wails plugin-loader
- **[NEW]** Plan `contracts/PLAN_v0.6.x.md` — plan d'implémentation v0.6.x plugin-loader
