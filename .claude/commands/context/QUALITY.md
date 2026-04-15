# QUALITY.md - Patterns de Qualite

Ce fichier centralise les patterns partages par les commandes `/code-review`, `/qa`, `/test-write`, et `/review`.

---

## 1. Agents de Qualite

| Commande | Agent | Role |
|----------|-------|------|
| `/code-review` | code-reviewer | Analyser le code |
| `/qa` | QA | Executer les tests |
| `/test-write` | test-writer | Ecrire les tests |
| `/review` | Workflow | Revue periodique complete |

---

## 2. Matrice Qualite

| Commande | Ecrit tests | Execute tests | Analyse code | Workflow |
|----------|-------------|---------------|--------------|----------|
| `/test-write` | Oui | Non | Non | Non |
| `/qa` | Non | Oui | Non | Non |
| `/code-review` | Non | Non | Oui | Non |
| `/review` | Oui | Oui | Oui | Oui |

---

## 3. Niveaux de Severite

| Niveau | Symbole | Description | Action |
|--------|---------|-------------|--------|
| Critical | CRITICAL | Bloquant (securite, bug majeur) | Rejet obligatoire |
| Warning | WARNING | Important mais non-bloquant | Signaler |
| Rationalization | RATIONALIZATION | Duplication > 70% | Recommander |
| Suggestion | SUGGESTION | Amelioration optionnelle | Proposer |

---

## 4. Framework de Review

### Categories d'Analyse

| Categorie | Verifications |
|-----------|---------------|
| Qualite | Naming, fonctions courtes, comments, errors |
| Securite | Injection, XSS, secrets, validation |
| Performance | Boucles, re-renders, structures |
| Architecture | Conformite aux patterns du projet |
| Rationalisation | Duplications, patterns repetes |

### Focus Specialises

```bash
/code-review security         # OWASP Top 10
/code-review performance      # Optimisations
/code-review rationalization   # Duplications
```

---

## 5. Verdicts Standards

### Code Review

| Verdict | Signification | Suite |
|---------|---------------|-------|
| APPROVED | Code pret | -> QA |
| APPROVED WITH RESERVATIONS | Mineur a noter | -> QA (noter) |
| REJECTED | Issues critiques | -> Retour DEV |

### QA

| Verdict | Signification | Suite |
|---------|---------------|-------|
| VALIDATED | Tests OK | -> DOC |
| VALIDATED WITH RESERVATIONS | Mineurs echoues | -> Confirmation |
| NOT VALIDATED | Tests critiques KO | -> Retour DEV |

---

## 6. Criteres de Validation QA

| Critere | VALIDATED | RESERVATIONS | NOT VALIDATED |
|---------|-----------|--------------|---------------|
| Tests | 100% pass | 1-2 non-critiques KO | >2 KO ou critiques KO |
| Coverage | > 70% | 60-70% | < 60% |
| Build | OK | OK | KO |

---

## 7. Commandes de Test

### Tests Unitaires

```bash
# Tous les tests
{TEST_CMD}

# Avec couverture
{COVERAGE_CMD}
```

---

## 8. Structure Rapport Review

```markdown
## Rapport de Revue de Code

**Overview**
- Fichiers analyses : [nombre]
- Lignes de code : [nombre]
- Statut global : [APPROVED|RESERVATIONS|REJECTED]

**Points positifs**
- [liste]

**Issues detectees**
- CRITICAL : [liste]
- WARNING : [liste]
- RATIONALIZATION : [liste]
- SUGGESTION : [liste]

**Securite**
- [analyse OWASP]

**Performance**
- [analyse]

**Architecture**
- [conformite projet]

**Rationalisation**
- [duplications detectees]

**Tests**
- [qualite des tests]

**Recommandations**
- [liste prioritaire]

**Decision** : [VERDICT]
```

---

## 9. Structure Rapport QA

```markdown
## Rapport QA

**Resume**
- Date : [date]
- Branche : [branche]
- Version : [X.Y.Z]
- Statut : [VALIDATED|RESERVATIONS|NOT VALIDATED]

**Tests Unitaires**
- Total : [nombre]
- Passes : [nombre]
- Echoues : [nombre]
- Coverage : [%]

**Tests E2E**
- Scenarios : [nombre]
- Passes : [nombre]
- Echoues : [nombre]

**Build**
- Statut : [OK|KO]

**Echecs detailles**
- [liste avec messages d'erreur]

**Decision** : [VERDICT]
```

---

## 10. Regles Critiques

| Agent | Regle |
|-------|-------|
| code-reviewer | NE PAS corriger le code (juste reviewer) |
| code-reviewer | NE PAS approuver si issue critique securite |
| QA | NE PAS modifier de code (juste tester) |
| test-writer | NE PAS executer les tests (juste ecrire) |
| Tous | NE PAS ignorer les duplications |

---

## 11. Workflow /review (Revue Periodique)

```
[Verifications] -> PLAN -> [Validation] -> DEV -> QA -> [Validation] -> DOC -> DEPLOY(QUALIF)
```

### Etapes

1. **Analyse initiale** : code-reviewer sur codebase
2. **Plan** : Creer plan de corrections/ameliorations
3. **Validation utilisateur** : Confirmer le plan
4. **Developpement** : Appliquer les corrections
5. **QA** : Valider les changements
6. **Documentation** : Mettre a jour si necessaire

---

## Usage

Dans les commandes Qualite, referencer ce fichier :

```markdown
**Contexte Qualite :** Voir `context/QUALITY.md`
- Framework review : section 4
- Verdicts : section 5
- Tests : section 7
- Rapports : sections 8-9
```
