# Commande /qa

Executer les tests et valider la qualite du code.

## Usage

```
/qa [scope]
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

## Scopes

| Scope | Description |
|-------|-------------|
| (vide) | Suite complete de tests |
| `unit` | Tests unitaires uniquement |
| `integration` | Tests integration uniquement |
| `e2e` | Tests end-to-end uniquement |
| `coverage` | Rapport de couverture |
| `quick` | Smoke tests rapides |

## Exemples

```
/qa                  # Tests complets
/qa unit             # Tests unitaires
/qa e2e              # Tests E2E
/qa coverage         # Avec rapport couverture
/qa quick            # Tests rapides
```

## Rapport

Le rapport QA inclut :
- Resultats des tests par categorie
- Couverture de code
- Tests en echec avec details
- Tests lents identifies
- Verdict final (PRET / NON PRET)

## Seuils de Qualite

| Metrique | Minimum | Ideal |
|----------|---------|-------|
| Tests unitaires | 100% pass | 100% pass |
| Couverture | 70% | >85% |
| Build | Success | Success |

**Contexte Qualite :** Voir `context/QUALITY.md`

## Agent

Lance l'agent `qa` defini dans `.claude/agents/qa.template.md`
