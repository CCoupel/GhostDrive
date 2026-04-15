# Commande /feature

Workflow complet pour l'implementation d'une nouvelle fonctionnalite.

## Usage

```
/feature <description de la fonctionnalite>
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
/feature <description>
    |
    v
[PLAN] --> Plan d'implementation detaille
    |
    v
[DEV] --> Implementation (agents tech selon stack)
    |
    v
[TEST] --> Ecriture des tests
    |
    v
[REVIEW] --> Revue de code
    |
    v
[QA] --> Execution tests + validation
    |
    v
[DOC] --> Documentation
    |
    v
[DEPLOY] --> Deploiement (sur demande)
```

## Prompt a transmettre au CDP

Orchestre le workflow FEATURE pour {PROJECT_NAME}.

**Contexte projet :** Voir `context/COMMON.md` section 1
**Workflow CDP :** Voir `context/CDP_WORKFLOWS.md`
- Type : FEATURE
- Phases : section 3
- Dispatch DEV : section 4
- Validation : section 5
- Erreurs : section 6
- Regles : section 8

**Contexte DEV :** Voir `context/DEVELOPMENT.md`
**Contexte Qualite :** Voir `context/QUALITY.md`

**Demande utilisateur :** $ARGUMENTS

## Exemples

```
/feature Ajouter l'authentification OAuth2
/feature Implementer la page de profil utilisateur
/feature Creer l'endpoint API pour les notifications
/feature Ajouter le support du mode sombre
```

## Sortie Anticipee

A tout moment, l'utilisateur peut :
- Demander de passer une etape
- Arreter le workflow
- Modifier le plan

## Agent

Delegue au CDP (`cdp.md`) qui orchestre les agents specialises.
