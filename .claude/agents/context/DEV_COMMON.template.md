# Regles Communes aux Agents DEV

> **Ce fichier contient les regles communes a tous les agents de developpement.**
> Agents concernes : tous les agents `dev-*`
>
> **Prerequis** : Respecter `context/COMMON.md` (regles generales) et `context/PROJECT_CONTEXT.md` (contexte technique)

---

## Etape Critique : Gestion de Version

Format : `X.Y.Z.a` en dev/qualif (le `a` disparait en prod). Resume local : `context/COMMON.md`.
Reference complete (cycle de vie detaille, exemple, regle du milestone) : `commands/context/COMMON.md` section 5 — fichier distinct, non accessible depuis un agent, mentionne ici a titre indicatif.

`X.Y.Z` est fixe integralement par le titre du milestone GitHub actif — **le milestone est la seule source de verite**, aucun agent DEV ne le calcule ni ne l'incremente. Tout developpement est rattache a un milestone (plus de cycle hors milestone). `a` est un **compteur de build, gere exclusivement par `deploy`** (tache BUILD, agnostique a l'environnement) : les agents DEV ne l'incrementent jamais et n'ont pas a y toucher lors d'un commit normal.

**Vous ne modifiez jamais `` vous-meme.** L'ecriture initiale `X.Y.Z.0` (a l'ouverture du cycle, sur la branche rattachee au milestone) est faite en amont, avant que DEV ne commence a commiter. Pour tout commit normal (feature comme bugfix), ne jamais toucher `` — `a` sera incremente par `deploy` au prochain deploiement QUALIF, pas par vous.

### Regles de Versioning

| Qui | Incremente | Quand |
|-----|------------|-------|
| **CDP** | Ecriture initiale `X.Y.Z.0` (depuis le titre du milestone) | Phase Init (Git) — creation de la branche, avant meme l'appel a PLAN |
| **DEV** | — (jamais) | — |
| **DEPLOY** | `a` (`a+1`) | Avant chaque BUILD — commit dedie, garantit un artefact unique par build |
| **DEPLOY** | `a` (suppression) | Promotion dev -> prod — version livree = `X.Y.Z` exact du milestone |

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
chore(version): Start bugfix cycle 1.2.4.0
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
| Incrementer a (version de build) | DEPLOY agent |
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
- X.Y.Z : [inchangee | Z incremente — nouveau cycle bugfix, voir Commits]
- `a` : gere par `deploy`, non modifie par cet agent

## Files Modified

### [fichier]
- [Description des modifications]

## Tests Results
- Total: N tests
- Passed: N
- Failed: 0
- Coverage: XX%

## Commits
1. `feat(scope): Description`

## Verification
- [x] Build OK
- [x] Tests PASS
- [x] Server starts OK (if applicable)
- [x] Version verified
- [x] [Autres verifications specifiques]
```
