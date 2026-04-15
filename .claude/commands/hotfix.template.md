# Commande /hotfix

Workflow accelere pour les corrections critiques en production.

## Usage

```
/hotfix <description du probleme critique>
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

## Quand utiliser

- Production cassee ou degradee
- Faille de securite active
- Perte de donnees en cours
- Impact business majeur

## Workflow Accelere

```
/hotfix <description>
    |
    v
[ANALYSE RAPIDE] --> Identification immediate
    |                 (pas de plan detaille)
    v
[FIX] --> Correction minimale
    |
    v
[TESTS CRITIQUES] --> Uniquement les tests essentiels
    |
    v
[DEPLOY PROD] --> Deploiement direct
    |
    v
[POST-MORTEM] --> Documentation de l'incident
```

## Etapes Detaillees

### 1. ANALYSE RAPIDE (max 15 min)

- Identifier le symptome exact
- Localiser le code responsable
- Determiner le fix minimal

**Pas de plan detaille** - On agit vite.

### 2. FIX

- Correction la plus simple possible
- Un seul commit
- Pas de refactoring
- Pas de features supplementaires

### 3. TESTS CRITIQUES

Uniquement :
- Test du scenario casse
- Smoke tests de base
- Build OK

**Pas de suite complete** - Sera fait apres.

### 4. DEPLOY PROD

Deploiement direct en production :

```bash
# Branche depuis main
git checkout -b hotfix/<name> main

# Fix + commit
git commit -m "fix: <description>"

# Merge et tag
git checkout main
git merge --no-ff hotfix/<name>
git tag v<version>-hotfix
git push origin main --tags
```

### 5. POST-MORTEM

Apres le fix, documenter :

```markdown
## Incident Report

**Date** : YYYY-MM-DD HH:MM
**Duree** : X heures
**Impact** : Description de l'impact

### Chronologie
- HH:MM - Detection du probleme
- HH:MM - Debut d'investigation
- HH:MM - Fix deploye
- HH:MM - Service restaure

### Cause Racine
Description technique de la cause.

### Fix Applique
Description du fix.

### Actions Preventives
- [ ] Action 1
- [ ] Action 2

### Lecons Apprises
- Point 1
- Point 2
```

## Exemples

```
/hotfix Base de donnees saturee, requetes timeout
/hotfix Faille XSS sur le formulaire de login
/hotfix Crash API suite au dernier deploy
/hotfix Certificat SSL expire
```

## Apres le Hotfix

1. **Tests complets** en background
2. **Revue de code** post-mortem
3. **Backport** vers les branches de dev si necessaire
4. **Communication** a l'equipe

## Regles Critiques

1. **Fix minimal** - Pas le moment d'ameliorer
2. **Un seul probleme** - Un hotfix = un bug
3. **Documenter** - Pour ne pas reproduire
4. **Communiquer** - Equipe informee
5. **Valider** - Monitoring post-deploy

## Prompt a transmettre au CDP

Orchestre le workflow HOTFIX pour {PROJECT_NAME}.

**Contexte projet :** Voir `context/COMMON.md` section 1
**Workflow CDP :** Voir `context/CDP_WORKFLOWS.md`
- Type : HOTFIX
- Phases : section 3
- Dispatch DEV : section 4
- Validation : section 5
- Erreurs : section 6
- Regles : section 8

**Contexte DEV :** Voir `context/DEVELOPMENT.md`

**Demande utilisateur :** $ARGUMENTS

## Agent

Mode special du CDP avec etapes reduites.
