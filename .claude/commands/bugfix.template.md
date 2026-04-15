# Commande /bugfix

Workflow pour la correction d'un bug.

## Usage

```
/bugfix <description du bug>
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
/bugfix <description>
    |
    v
[ANALYSE] --> Identifier la cause racine
    |
    v
[PLAN] --> Plan de correction (si complexe)
    |
    v
[DEV] --> Implementation du fix
    |
    v
[TEST] --> Test de non-regression
    |
    v
[REVIEW] --> Revue de code
    |
    v
[QA] --> Validation
    |
    v
[DOC] --> CHANGELOG (Fixed)
```

## Etapes Detaillees

### 1. ANALYSE

- Explorer le code pour comprendre le probleme
- Identifier le(s) fichier(s) concerne(s)
- Reproduire le bug si possible
- Determiner la cause racine

### 2. PLAN (optionnel)

Pour les bugs complexes uniquement :
- Plusieurs fichiers impactes
- Risque de regression
- Changement d'architecture

### 3. DEV

- Correction minimale et ciblee
- Eviter les changements non lies au bug
- Ajouter des commentaires si logique complexe

### 4. TEST

**Obligatoire** : Test de non-regression
- Reproduit le bug avant le fix
- Valide que le fix corrige le probleme
- S'assure qu'il n'y a pas de regression

### 5. REVIEW

- Verification que le fix est correct
- Pas d'effets de bord
- Code propre et maintenable

### 6. QA

- Execution de tous les tests
- Verification specifique du scenario du bug
- Build OK

### 7. DOC

Mise a jour CHANGELOG.md :
```markdown
### Fixed
- Description du bug corrige (#issue)
```

## Exemples

```
/bugfix Le score ne s'affiche pas apres une partie
/bugfix Crash au demarrage sur iOS 15
/bugfix L'API retourne 500 sur /users sans parametres
/bugfix Le bouton submit reste desactive apres erreur
```

## Differences avec /hotfix

| Aspect | /bugfix | /hotfix |
|--------|---------|---------|
| Urgence | Normal | Critique (prod down) |
| Tests | Complets | Critiques uniquement |
| Review | Standard | Acceleree |
| Deploy | Via workflow normal | Direct PROD |

## Prompt a transmettre au CDP

Orchestre le workflow BUGFIX pour {PROJECT_NAME}.

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

**Demande utilisateur :** $ARGUMENTS

## Agent

Delegue au CDP (`cdp.md`) avec mode bugfix.
