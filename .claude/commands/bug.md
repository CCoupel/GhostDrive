# Commande /bug

Workflow pour la correction d'un bug GhostDrive.

## Usage

```
/bug <description du bug>
```

## Argument recu

$ARGUMENTS

## Mots-cles de controle

**Reference :** Voir `context/COMMON.md` section 12

| Mot-cle | Action |
|---------|--------|
| `help` | Affiche l'aide et les mots-cles disponibles |
| `status` | Affiche l'etat du workflow en cours |
| `plan` | Affiche le plan sans executer |
| `resume <phase>` | Reprend a une phase |
| `skip <phase>` | Saute une phase |
| `jumpto <tache>` | Demarre a une tache precise du plan |

Si `$ARGUMENTS` commence par un mot-cle -> executer l'action correspondante.
Sinon -> workflow normal.

## Workflow

```
/bug <description>
    |
    v
[ANALYSE] --> Identifier la cause racine
    |
    v
[DEV] --> Correction minimale et ciblee
    |
    v
[TEST] --> Test de non-regression
    |
    v
[REVIEW] --> Revue de code
    |
    v
[QA] --> Validation complete
    |
    v
[DOC] --> CHANGELOG (Fixed) + mise a jour issue GitHub
```

## Etapes Detaillees

### 1. ANALYSE

- Explorer le code pour comprendre le probleme
- Identifier le composant concerne (sync engine / plugin / placeholder / UI)
- Reproduire le bug si possible
- Determiner la cause racine
- Lier a l'issue GitHub si elle existe

### 2. DEV

- Correction minimale et ciblee
- Eviter les changements non lies au bug
- Commit : `fix(scope): description (#N)`

### 3. TEST

**Obligatoire** : Test de non-regression
- Ajouter un test qui reproduit le bug avant le fix
- Valider que le fix corrige le probleme

### 4. REVIEW + QA

- `go test ./... -v -cover`
- `npm run test` (frontend si impacte)
- `go build ./cmd/ghostdrive` (build OK)

### 5. DOC

Mise a jour `CHANGELOG.md` :
```markdown
### Fixed
- Description du bug corrige (#issue)
```
Commenter et fermer l'issue GitHub.

## Exemples

```
/bug La sync s'arrete apres une deconnexion WebDAV
/bug Les placeholders ne se creent pas sur Windows 11
/bug L'UI ne rafraichit pas le statut de connexion
/bug Upload echoue pour les fichiers > 100MB sur MooseFS
```

## Differences avec /hotfix

| Aspect | /bug | /hotfix |
|--------|------|---------|
| Urgence | Normal | Critique (prod impacte) |
| Tests | Complets | Critiques uniquement |
| Review | Standard | Acceleree |
| Deploy | Via workflow normal | Direct PROD |

## Prompt a transmettre au CDP

Orchestre le workflow BUGFIX pour GhostDrive.

**Contexte projet :** Voir `context/COMMON.md` section 1
**Workflow CDP :** Voir `context/CDP_WORKFLOWS.md`
- Type : BUGFIX
- Phases : section 3
- Dispatch DEV : section 4
- Validation : section 5
- Erreurs : section 6
- Regles : section 8

**Contexte DEV :** Voir `context/DEVELOPMENT.md`
**Contexte Qualite :** Voir `context/QUALITY.md`

**Note GhostDrive** : Identifier d'abord le composant impacte (backend sync/plugin/placeholder ou frontend). Commit format : `fix(composant): description (#issue)`.

**Demande utilisateur :** $ARGUMENTS

## Agent

Delegue au CDP (`cdp.md`) avec mode bugfix.
