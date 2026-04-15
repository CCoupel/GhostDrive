# Commande /secu

Audit de securite du projet avec workflow dedie.

## Usage

```
/secu [scope]
```

## Scopes Disponibles

| Scope | Description |
|-------|-------------|
| (vide) ou `all` | Audit complet |
| `backend` | Code backend uniquement |
| `frontend` | Code frontend uniquement |
| `deps` | Dependances uniquement |
| `secrets` | Detection de secrets |
| `config` | Configuration securite |
| `owasp` | Verification OWASP Top 10 |

## Exemples

```
/secu              # Audit complet
/secu deps         # Audit dependances seulement
/secu backend      # Audit code backend
/secu secrets      # Recherche de secrets exposes
```

## Workflow

```
/secu [scope]
    |
    v
[SCAN] --> Analyse statique (SAST)
    |
    v
[DEPS] --> Audit dependances (npm audit, etc.)
    |
    v
[SECRETS] --> Detection credentials hardcodes
    |
    v
[CONFIG] --> Verification configuration
    |
    v
[OWASP] --> Check OWASP Top 10
    |
    v
[REPORT] --> Rapport detaille + score
    |
    v
[FIX] --> Corrections (optionnel)
```

## Severites

| Niveau | SLA | Action |
|--------|-----|--------|
| CRITIQUE | 24h | Bloquer, corriger immediatement |
| HAUTE | 1 semaine | Corriger avant prochaine release |
| MOYENNE | 1 mois | Planifier correction |
| FAIBLE | Backlog | Best effort |

## Rapport

Le rapport inclut :
- Score de securite global (X/100)
- Vulnerabilites par severite
- Dependances vulnerables avec versions de fix
- Secrets detectes
- Recommandations OWASP
- Suggestions de correction

## Integration

- **Pre-release** : Executer `/secu` avant chaque release majeure
- **CI/CD** : Integrer `/secu deps,secrets` dans le pipeline
- **Revue** : Inclure dans la checklist de code review

## Agent

Lance l'agent `security` defini dans `.claude/agents/security.template.md`
