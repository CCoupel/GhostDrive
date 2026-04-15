# Commande /deploy

Deployer l'application vers un environnement cible.

## Usage

```
/deploy <environnement>
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

## Environnements

| Environnement | Description |
|---------------|-------------|
| `qualif` | Environnement de qualification/staging |
| `prod` | Production |

## Exemples

```
/deploy qualif    # Deploiement en qualification
/deploy prod      # Deploiement en production
```

## Prerequis

### Pour QUALIF
- [ ] Tests QA passes
- [ ] Build reussi

### Pour PROD
- [ ] QUALIF validee
- [ ] Tests complets OK
- [ ] Documentation a jour
- [ ] Confirmation utilisateur

## Workflow QUALIF

```
/deploy qualif
    |
    v
Build --> Push --> Smoke Tests --> Notification
```

## Workflow PROD

```
/deploy prod
    |
    v
Confirmation --> Merge main --> Tag --> CI/CD
    |
    |-- SI OK --> Release Notes --> Monitoring
    |
    |-- SI ECHEC --> Rollback --> Analyse
```

## Rollback

En cas de probleme :
```
/deploy rollback    # Revenir a la version precedente
```

**Contexte projet :** Voir `context/COMMON.md` section 1

## Agent

Lance l'agent `deploy` defini dans `.claude/agents/deploy.template.md`
