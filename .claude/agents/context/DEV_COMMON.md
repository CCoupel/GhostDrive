# Regles Communes aux Agents DEV

> **Ce fichier contient les regles communes a tous les agents de developpement.**
> Agents concernes : tous les agents `dev-*`
>
> **Prerequis** : Respecter `context/COMMON.md` (regles generales) et `context/PROJECT_CONTEXT.md` (contexte technique)

---

## Etape Critique : Increment de Version

**AVANT TOUT CHANGEMENT DE CODE**, vous DEVEZ :

1. **Lire** la version actuelle depuis `{VERSION_FILE}`
2. **Incrementer** le numero z (patch) : `X.Y.Z` -> `X.Y.Z+1`
3. **Committer** : `chore(version): Bump to X.Y.Z+1`

### Regles de Versioning

| Qui | Incremente | Quand |
|-----|------------|-------|
| **PLAN** | y (minor) | Nouvelle feature (`X.Y.0` -> `X.Y+1.0`) |
| **DEV** | z (patch) | Chaque cycle de developpement (`X.Y.0` -> `X.Y.1`) |
| **DOC** | Reset z=0 | Finalisation release (`X.Y.N` -> `X.Y.0`) |

---

## Format des Commits

```
<type>(<scope>): <description>

<optional body>
```

### Types Autorises

| Type | Usage |
|------|-------|
| `feat` | Nouvelle fonctionnalite |
| `fix` | Correction de bug |
| `refactor` | Refactoring sans changement de comportement |
| `test` | Ajout/modification de tests |
| `docs` | Documentation uniquement |
| `style` | Formatage (sans changement de logique) |
| `chore` | Maintenance (version, config, deps) |
| `perf` | Amelioration de performance |

### Exemples

```bash
feat(api): Add user authentication endpoint
fix(auth): Handle expired tokens gracefully
test(api): Add tests for user registration
refactor(utils): Extract validation helpers
chore(version): Bump to 1.2.3
```

---

## Contrats API (OBLIGATOIRE)

**AVANT d'implementer**, consultez les contrats definis par l'agent PLAN :

```
contracts/
├── api-endpoints.md      # Endpoints API
├── data-models.md        # Modeles partages
└── events.md             # Evenements temps-reel
```

### Workflow Contract-First

1. **Lire** les contrats definis dans le plan
2. **Implementer** selon les contrats
3. **Modifier** les contrats si contrainte technique (avec justification)

### Modification de Contrat

Si vous devez modifier un contrat, documentez-le dans votre summary :

```markdown
## Modification de Contrat

**Fichier** : `contracts/api-endpoints.md`
**Endpoint** : POST /users

**Original** :
| Champ | Type |
|-------|------|
| name | string |

**Modifie** :
| Champ | Type |
|-------|------|
| name | string |
| created_at | datetime | <- Ajoute

**Raison** : [Justification technique]
```

---

## Verifications Obligatoires

### Avant de Terminer

| Verification | Description |
|--------------|-------------|
| Build | Le projet compile sans erreur |
| Tests | Tous les tests passent |
| Version | La version correspond au fichier de config |
| Lint | Pas d'erreurs de linting |

### Validation Serveur (si applicable)

Apres le build, verifier que le serveur demarre correctement :

1. Lancer le serveur
2. Verifier la version via endpoint `/version` ou equivalent
3. Verifier les logs (pas d'erreur critique)
4. Arreter proprement le serveur

**En cas d'echec** :
- Verifier les logs du serveur
- S'assurer que les ports sont disponibles
- Verifier que le build a reussi
- **Maximum 2 tentatives** avant escalade a l'utilisateur

---

## Standards de Code (Generiques)

### Regles Universelles

- **Naming** : Respecter les conventions du langage utilise
- **Error handling** : Toujours gerer les erreurs, ne jamais les ignorer
- **Thread-safety** : Proteger l'etat partage (mutex, locks, etc.)
- **Tests** : Chaque fonction publique doit avoir des tests

### Documentation du Code

- Documenter les fonctions publiques/exportees
- Ajouter des commentaires pour la logique complexe
- Garder le code auto-documentant (bon nommage)

---

## Ce que les Agents DEV NE DOIVENT PAS Faire

| Interdit | Responsable |
|----------|-------------|
| Modifier la documentation | DOC agent |
| Deployer | DEPLOY agent |
| Incrementer y (version minor) | PLAN agent |
| Executer les tests E2E | QA agent |
| Ecrire les scenarios E2E | TEST-WRITER agent |

---

## Coordination Entre Agents DEV

### Ordre d'Execution Standard

Le backend DOIT etre complete AVANT le frontend si :
- Nouvelles APIs ou endpoints
- Nouveaux modeles de donnees
- Nouvelles actions temps-reel (WebSocket, SSE, etc.)
- Modifications de protocole

### Parallelisation Possible

Frontend et autres agents peuvent etre parallelises APRES le backend si les modifications sont independantes.

---

## Format de Summary

Chaque agent DEV doit produire un summary structure :

```markdown
# [Agent] Implementation Summary

## Version
- Previous: X.Y.Z
- Current: X.Y.Z+1

## Files Modified

### [fichier]
- [Description des modifications]

## Tests Results
- Total: N tests
- Passed: N
- Failed: 0
- Coverage: XX%

## Commits
1. `chore(version): Bump to X.Y.Z+1`
2. `feat(scope): Description`

## Verification
- [x] Build OK
- [x] Tests PASS
- [x] Server starts OK (if applicable)
- [x] Version verified
- [x] [Autres verifications specifiques]
```
