# Plan d'Implementation : Bugfix #116 — GhD: taille=0 et date=epoch

## Résumé

Dans le lecteur virtuel `GhD:` (WinFsp/cgofuse), tous les fichiers affichent
`taille=0 octets` et `date=01/01/1970` (epoch Unix) au lieu des valeurs réelles.
La cause racine est dans le plugin MooseFS : le protocole `READDIR` (flags=0)
ne retourne pas `Size` ni `MTime` par entrée. La couche FUSE appelle `List()`
qui utilise ces entrées vides directement pour remplir `Readdir`, d'où des
métadonnées fausses dans Explorer.

---

## Cause Racine

### 1. MooseFS (confirmée)

**Fichier** : `plugins/moosefs/internal/mfsclient/client.go` — méthode `ReadDir()`

La requête `CLTOMA_FUSE_READDIR` (428) est envoyée avec `flags=0x00`.
La réponse serveur ne contient alors que :
```
[namelen:8][name:namelen][inode:32][dtype:8]
```
Aucun bloc d'attrs (35 octets) n'est inclus.
Résultat : `DirEntry.Size = 0` et `DirEntry.MTime = 0` pour toutes les entrées.

**Fichier** : `plugins/moosefs/moosefs.go` — méthode `List()`

```go
fi := plugins.FileInfo{
    ...
    Size:    int64(e.Size),                  // ← toujours 0
    ModTime: time.Unix(int64(e.MTime), 0),   // ← toujours epoch (1970-01-01)
}
```

**Fichier** : `internal/placeholder/filesystem_windows.go` — `Readdir()`

La couche FUSE passe les `FileInfo` de `List()` directement à `fill()` comme
stats WinFsp. Explorer affiche donc taille=0 et date=epoch pour chaque fichier.

> **Note** : `Getattr()` utilise `backend.Stat()` → `c.GetAttr(nodeID)` qui retourne
> les vraies valeurs. Le bug n'est visible que dans le listing Explorer (Readdir),
> pas dans les propriétés individuelles d'un fichier (Getattr).

### 2. LOCAL (non affecté)

`List()` utilise `os.ReadDir()` → `entry.Info().Size()` / `entry.Info().ModTime()`.
Toujours correct.

### 3. WebDAV (potentiellement affecté selon le serveur)

`List()` utilise PROPFIND Depth=1. `fileInfoFromResponse()` parse `getcontentlength`
et `getlastmodified`. La plupart des serveurs standards (Nextcloud, ownCloud, Apache)
renvoient ces propriétés. À vérifier au cas par cas.

---

## Critères d'Acceptation

- [ ] Dans GhD:, les fichiers MooseFS affichent leur vraie taille dans Explorer
- [ ] Dans GhD:, les fichiers MooseFS affichent leur vraie date de modification
- [ ] `moosefs.List()` retourne des `FileInfo.Size > 0` pour des fichiers non vides
- [ ] `moosefs.List()` retourne des `FileInfo.ModTime` ≠ epoch pour des fichiers réels
- [ ] Le comportement de `Stat()` (Getattr) est inchangé — il était déjà correct
- [ ] Aucune régression sur les tests existants MooseFS

---

## Composants Impactés

- **Backend MooseFS** : `plugins/moosefs/moosefs.go` (méthode `List()`)
- **Tests MooseFS** : `plugins/moosefs/moosefs_test.go` (validation List() avec vrai cluster)
- **Optionnel / futur** : `plugins/moosefs/internal/mfsclient/client.go` (ReadDir avec flags attrs)
- **WebDAV** : vérification uniquement, pas de code à modifier a priori
- **FUSE** : `internal/placeholder/filesystem_windows.go` — fix défensif optionnel

---

## Tâches

### Phase 1 : Fix MooseFS — GetAttr par entrée dans List() (OPTION B — immédiat)

Cette option ne nécessite aucun changement de protocole. Elle utilise `c.GetAttr(e.NodeID)`
disponible depuis le ReadDir (le NodeID/inode est retourné par le serveur dans chaque entrée).

**1.1 — Modifier `moosefs.go` : `List()` appelle `GetAttr` par entrée**

- Fichier : `plugins/moosefs/moosefs.go`
- Méthode : `List()`
- Après `entries, err := c.ReadDir(nodeID)`, dans la boucle `for _, e := range entries` :

```go
// Avant (bugué) :
fi := plugins.FileInfo{
    Name:    e.Name,
    Path:    entryPath,
    IsDir:   e.IsDir,
    Size:    int64(e.Size),                // toujours 0
    ModTime: time.Unix(int64(e.MTime), 0), // toujours epoch
}

// Après (corrigé) :
fi := plugins.FileInfo{
    Name:  e.Name,
    Path:  entryPath,
    IsDir: e.IsDir,
}
// ReadDir (flags=0) ne retourne pas Size/MTime.
// Appel individuel GetAttr pour récupérer les vraies métadonnées.
if attr, attrErr := c.GetAttr(e.NodeID); attrErr == nil {
    fi.Size = int64(attr.Size)
    fi.ModTime = time.Unix(int64(attr.MTime), 0)
} else {
    logger.Warn("list %s: GetAttr(%d) failed: %v", dirPath, e.NodeID, attrErr)
}
```

- Note : `c` est un `*mfsclient.Client` — `GetAttr` est déjà une méthode publique.
- Note : En cas d'erreur GetAttr, on laisse Size=0/ModTime=epoch (no-op, dégradation gracieuse).

**1.2 — Mettre à jour le commentaire dans `client.go`**

- Fichier : `plugins/moosefs/internal/mfsclient/client.go`
- Ligne 415 : `// Size and MTime not provided by ReadDir — will remain zero`
- Changer en : `// Size and MTime not included in ReadDir response (flags=0 — see moosefs.go List() for GetAttr workaround)`

---

### Phase 2 : Fix défensif FUSE (OPTIONNEL — défense en profondeur)

Si d'autres backends présentent le même problème dans le futur, ajouter dans
`filesystem_windows.go` une protection dans `Readdir()` :

- Fichier : `internal/placeholder/filesystem_windows.go`
- Dans la boucle Readdir (non-root), si `!e.IsDir && e.Size == 0 && e.ModTime.Unix() == 0` :
  appel optionnel à `r.backend.Stat(ctx, entryPath)` pour récupérer les vraies valeurs.

> Ce fix n'est PAS bloquant pour la résolution du bug #116 — il s'agit d'une
> amélioration de robustesse. À discuter avec l'équipe.

---

### Phase 3 : Optimisation future — ReadDir avec attrs (OPTION A)

MooseFS 4.x supporte un mode READDIR qui inclut un bloc attrs de 35 octets par
entrée (sans GetAttr supplémentaire). La `DirEntry` struct est déjà préparée pour
cela (champs `Size uint64` et `MTime uint32`).

- Fichier : `plugins/moosefs/internal/mfsclient/client.go`
- Identifier la valeur exacte du flag dans le protocole MooseFS 4.x (source MooseFS)
- Modifier la requête : `PutUint8(req, FLAGS_INCLUDE_ATTRS)` (flag à documenter)
- Modifier le parsing de la réponse : après chaque `[namelen:8][name][inode:32][dtype:8]`,
  appeler `ParseAttrs(ans, off)` pour lire le bloc de 35 bytes d'attrs et peupler `e.Size` et `e.MTime`.
- Supprimer le GetAttr par entrée dans `moosefs.go` `List()` (phase 1).
- **Ne pas implémenter maintenant** — nécessite validation sur le cluster de test.

---

## Tests Requis

- [ ] **Test unitaire** (mock client) : `moosefs.List()` retourne `Size > 0` et `ModTime != epoch`
  après la correction. Fichier : `plugins/moosefs/moosefs_test.go`
- [ ] **Test intégration** (cluster réel) : `moosefs.List("/")` sur un répertoire avec
  fichiers réels retourne des `FileInfo.Size` et `FileInfo.ModTime` corrects.
  Fichier : `plugins/moosefs/integration_test.go`
- [ ] **Test régression** : `moosefs.Stat()` retourne toujours les bonnes valeurs (inchangé).
- [ ] **Vérification manuelle** : Explorer sur GhD: affiche les bonnes tailles et dates.

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| N+1 appels GetAttr (performance) | Élevée | Moyen | Acceptable pour des répertoires <1000 fichiers (LAN ~1ms/call) ; Option A résoudra à terme |
| GetAttr échoue pour une entrée | Faible | Faible | Dégradation gracieuse : Size=0/epoch comme avant, log WARN |
| Reconnexion en cours pendant la boucle | Faible | Faible | Déjà géré par la logique retry existante dans `List()` |
| WebDAV affecté aussi | Moyen | Moyen | Vérifier sur serveur réel ; si oui, fix dans `fileInfoFromResponse` ou PROPFIND |

---

## Estimation

- Complexité : **Faible** (Phase 1 : ~10 lignes de code)
- Nombre de fichiers modifiés : **2** (moosefs.go, client.go commentaire)
- Tests à mettre à jour : **1-2** fichiers

## Notes

- La `DirEntry` struct dans `protocol.go` a déjà `Size uint64` et `MTime uint32` prévus
  pour l'Option A future — ne pas supprimer ces champs.
- Le bug #116 existant dans le code (`// Notify the frontend`) est distinct et déjà
  implémenté (frontend notifications) — ne pas confondre avec ce bugfix de métadonnées.
- Confirmer si WebDAV est affecté en testant contre le serveur cible du projet.
