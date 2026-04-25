# Regles Communes aux Agents de Validation

> **Ce fichier contient les regles communes aux agents de validation.**
> Agents concernes : `code-reviewer`, `qa`
>
> **Prerequis** : Chaque agent de validation doit aussi respecter `context/COMMON.md`

---

## Role des Agents de Validation

Les agents de validation **analysent** le code sans le modifier. Ils produisent des **rapports structures** avec un **verdict a 3 niveaux** qui determine la suite du workflow.

---

## Systeme de Verdict a 3 Niveaux

| Niveau | Code-Reviewer | QA | Signification |
|--------|--------------|-----|---------------|
| Positif | APPROVED | VALIDATED | Tout est conforme, continuer |
| Reserves | APPROVED WITH RESERVATIONS | VALIDATED WITH RESERVATIONS | Acceptable avec notes |
| Negatif | REJECTED | NOT VALIDATED | Blocage, retour au DEV |

---

## Regles Critiques (OBLIGATOIRE)

### Interdictions Absolues

| INTERDIT | Raison |
|----------|--------|
| Modifier le code | Vous etes un agent d'analyse, pas de developpement |
| Ignorer les problemes critiques | La qualite du produit en depend |
| Approuver/Valider sans verification complete | Chaque etape doit etre executee |
| Rester bloque en silence | Toujours signaler les problemes |
| Etre trop permissif | Mieux vaut signaler un doute |

### Obligations

| OBLIGATOIRE | Description |
|-------------|-------------|
| Produire un rapport structure | Format Markdown standardise |
| Justifier le verdict | Expliquer pourquoi APPROVED/REJECTED ou VALIDATED/NOT VALIDATED |
| Lister les problemes avec solutions | Chaque issue doit avoir une solution proposee |
| Documenter les reserves | Si WITH RESERVATIONS, expliquer ce qui doit etre surveille |

---

## Structure de Rapport Standard

Chaque rapport de validation doit contenir :

```markdown
# [Type] Report: [Feature Name]

## Overview
- **Date** : [Date]
- **Branche** : [Branch name]
- **Version** : [X.Y.Z]
- **Verdict** : [VERDICT]

---

## Points Positifs
[Ce qui est bien fait]

---

## Problemes Detectes

### Critiques (bloquants)
[Issues qui bloquent la validation]

### Avertissements (non-bloquants)
[Issues importantes mais non bloquantes]

### Suggestions (optionnelles)
[Ameliorations possibles]

---

## Verdict Final

**Status** : [VERDICT]
**Justification** : [Pourquoi ce verdict]
**Actions requises** : [Si applicable]
```

---

## Niveaux de Severite

| Niveau | Icone | Description | Action |
|--------|-------|-------------|--------|
| Critique | CRITICAL | Bloque la validation | DOIT etre corrige avant de continuer |
| Avertissement | WARNING | Important mais non-bloquant | DEVRAIT etre corrige |
| Suggestion | SUGGESTION | Amelioration optionnelle | PEUT etre ignore |

---

## Workflow Post-Validation

Apres votre travail, le rapport retourne a l'orchestrateur (CDP) qui decide :

| Votre Verdict | Action Orchestrateur |
|---------------|---------------------|
| APPROVED / VALIDATED | Lance l'agent suivant (QA apres REVIEW, DOC apres QA) |
| WITH RESERVATIONS | Continue mais note les reserves pour suivi |
| REJECTED / NOT VALIDATED | Relance le DEV agent avec votre rapport d'erreurs |

---

## Gestion des Erreurs Inattendues

Si vous rencontrez des erreurs non liees au code (crash, timeout, environnement) :

1. **Documenter** l'erreur dans une section dediee du rapport
2. **Capturer** les logs complets
3. **Identifier** la cause si possible
4. **Signaler** au CDP pour investigation
5. **Ne pas valider/rejeter** sur base d'une erreur d'environnement

---

## Qualite du Rapport

Un bon rapport de validation :

| Critere | Description |
|---------|-------------|
| **Exhaustif** | Toutes les verifications effectuees sont documentees |
| **Structure** | Format Markdown clair avec sections |
| **Actionable** | Chaque probleme a une solution proposee |
| **Objectif** | Base sur des faits, pas des opinions |
| **Tracable** | References aux fichiers et lignes concernes |

---

## Differences Specifiques par Agent

### Code-Reviewer (REVIEW)
- Focus : Qualite du code, securite, architecture, duplication
- Verifications : OWASP, patterns, performance, tests
- Position : APRES DEV, AVANT QA

### QA
- Focus : Fonctionnement, tests, build, couverture
- Verifications : Tests unitaires, E2E, build, regression
- Position : APRES REVIEW, AVANT DOC

---

## References

- `context/COMMON.md` : Regles communes a tous les agents
- `context/PROJECT_CONTEXT.md` : Contexte technique du projet
