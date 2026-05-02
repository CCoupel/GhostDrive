# Changelog des Contrats API

Ce fichier documente tous les changements de contrats API (Wails bindings, événements, modèles).
Les changements **BREAKING** doivent être validés par le CDP avant implémentation.

---

## [20260430] — v1.1.0 plugin-describe (#78 #79 #80)

- **[BREAKING]** `plugins.StorageBackend` — méthode `Describe() PluginDescriptor` ajoutée :
  tout plugin tiers doit l'implémenter pour compiler
- **[BREAKING]** `plugins/local` — suppression de `init()`/`Register()` :
  l'enregistrement est désormais explicite via `ServeInProcess` dans `app.Startup()`
- **[NEW]** `plugins.ParamType`, `plugins.ParamSpec`, `plugins.PluginDescriptor` — types Go
- **[NEW]** `rpc Describe (DescribeRequest) returns (DescribeResponse)` — RPC gRPC
- **[NEW]** `func ServeInProcess(impl StorageBackend) (*GRPCBackend, func(), error)` —
  transport in-process via bufconn pour les plugins statiques
- **[NEW]** `GetPluginDescriptors() []plugins.PluginDescriptor` — binding Wails
- **[NEW]** `ParamType`, `ParamSpec`, `PluginDescriptor` — types TypeScript dans `ghostdrive.ts`
- **[CHANGED]** `SyncPointForm.tsx` — Zone 2 (Remote) générée dynamiquement depuis les `ParamSpec`
  du plugin sélectionné (rétrocompatible fonctionnellement pour `local`)
- **[NEW]** Contrat `contracts/plugin-describe.md` — spécification complète

---

## [20260430] — v0.8.0 plugin-webdav

- **[CHANGED]** `contracts/backend-config.md` — params WebDAV étendus (rétro-compatible) :
  ajout `token`, `authType`, `pollInterval` (ms), `tlsSkipVerify` ; défauts preservés
- **[NEW]** Plugin dynamique gRPC `ghostdrive-webdav` — implémentation complète `StorageBackend`
  WebDAV : Basic Auth, Bearer Token, PROPFIND/PUT/GET/DELETE/MOVE/MKCOL, Watch polling, quota
- **[CHANGED]** `plugins/loader` — `loadPlugin` passe en mode factory : chaque `plugins.Get()`
  spawne un nouveau subprocess, permettant plusieurs backends WebDAV avec des configs différentes

---

## [20260428] — v0.6.0 plugin-loader

- **[NEW]** `GetLoadedPlugins() []PluginInfo` — liste des plugins dynamiques chargés depuis `<AppDir>/plugins/*.exe`
- **[NEW]** `ReloadPlugins() error` — rescan du dossier plugins sans redémarrage de l'app
- **[NEW]** Événements : `plugin:loaded`, `plugin:failed`, `plugin:restarting`, `plugin:reloaded`
- **[NEW]** Type `PluginInfo` — voir `contracts/plugin-loader-bindings.md`
- **[CHANGED]** `GetAvailableBackendTypes()` — inclut désormais les plugins dynamiques en plus des statiques (rétrocompatible, aucune modification frontend requise)
- **[NEW]** Contrat `contracts/plugin-loader-bindings.md` — spécification complète des bindings Wails plugin-loader
- **[NEW]** Plan `contracts/PLAN_v0.6.x.md` — plan d'implémentation v0.6.x plugin-loader
