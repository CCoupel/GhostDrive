# Commande /feat

Workflow complet pour l'implementation d'une nouvelle fonctionnalite GhostDrive.

## Usage

```
/feat <description de la fonctionnalite>
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
/feat <description>
    |
    v
[PLAN] --> Plan + contrats Wails (contracts/)
    |
    v
[DEV] --> Backend Go et/ou Frontend Wails+React
    |
    v
[TEST] --> Ecriture des tests
    |
    v
[REVIEW] --> Revue de code
    |
    v
[QA] --> go test + vitest + build Wails
    |
    v
[DOC] --> CHANGELOG + docs + mise a jour issue GitHub
    |
    v
[DEPLOY] --> Build qualif, puis /deploy prod sur confirmation
```

## Prompt a transmettre au CDP

Orchestre le workflow FEATURE pour GhostDrive.

**Contexte projet :** Voir `context/COMMON.md` section 1
**Workflow CDP :** Voir `context/CDP_WORKFLOWS.md`
- Type : FEATURE
- Phases : section 3
- Dispatch DEV : section 4 (backend Go / frontend Wails+React / les deux)
- Validation : section 5
- Erreurs : section 6
- Regles : section 8

**Contexte DEV :** Voir `context/DEVELOPMENT.md`
**Contexte Qualite :** Voir `context/QUALITY.md`

**Note GhostDrive** : Verifier si l'issue GitHub associee existe. Si oui, commenter le debut d'implementation. Si la feature concerne un plugin backend, utiliser `plugin(nom):` dans les commits.

**Demande utilisateur :** $ARGUMENTS

## Exemples

```
/feat Ajouter le plugin S3 (bucket AWS/MinIO)
/feat Implementer le cache local avec limite de taille configurable
/feat Afficher la progression de sync dans la tray UI
/feat Ajouter la configuration du mode placeholder dans les settings
```

## Agent

Delegue au CDP (`cdp.md`) qui orchestre les agents specialises.
