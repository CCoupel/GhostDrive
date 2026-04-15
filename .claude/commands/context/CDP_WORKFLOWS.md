# CDP_WORKFLOWS.md - Workflows Orchestres par CDP

Ce fichier centralise les patterns partages par les commandes `/feature`, `/bugfix`, `/hotfix`, et `/refactor`.

---

## 1. Contexte CDP

```yaml
Agent: CDP (Chef De Projet)
Role: Orchestrer workflows multi-agents avec validation utilisateur
```

---

## 2. Workflows Disponibles

| Commande | Type | Workflow | Version |
|----------|------|----------|---------|
| `/feature` | FEATURE | Complet | Incremente Y |
| `/bugfix` | BUGFIX | Simplifie | Incremente Z |
| `/hotfix` | HOTFIX | Accelere | Incremente Z + suffix |
| `/refactor` | REFACTOR | Leger | Aucun changement |

---

## 3. Workflow Standard CDP

```
[INIT] -> [ANALYSE] -> [DEV] -> [REVIEW] -> [QA] -> [DOC] -> [DEPLOY] -> [FIN]
```

### Variantes par type

| Phase | FEATURE | BUGFIX | HOTFIX | REFACTOR |
|-------|---------|--------|--------|----------|
| Backlog (GitHub Issues) | Oui | Non | Non | Non |
| Plan | Oui | Souvent | Non | Rarement |
| Dev | Complet | Cible | Minimal | Structure |
| Review | Oui | Oui | Rapide | Oui |
| QA | Complet | Regression | Critique | Complet |
| Doc | Oui | Si majeur | Post-mortem | Non |
| Deploy QUALIF | Oui | Oui | Optionnel | Oui |

---

## 4. Phases Communes

### Phase Init (Git)

```bash
# FEATURE
git checkout main && git pull origin main
git checkout -b feature/<nom-court>

# BUGFIX
git checkout main && git pull origin main
git checkout -b bugfix/<nom-court>

# HOTFIX (depuis production)
git checkout main && git pull origin main
git checkout -b hotfix/<nom-court>

# REFACTOR
git checkout main && git pull origin main
git checkout -b refactor/<nom-court>
```

### Phase Versionnement

| Type | Action | Exemple |
|------|--------|---------|
| FEATURE | Incremente Y, reset Z | 2.40.3 -> 2.41.0 |
| BUGFIX | Incremente Z | 2.40.0 -> 2.40.1 |
| HOTFIX | Incremente Z + suffix | 2.40.1 -> 2.40.2-hotfix |
| REFACTOR | Aucun | 2.40.1 (inchange) |

### Phase Dev (Dispatch)

```
Analyser le scope :
|-- Backend seul -> dev-backend
|-- Frontend seul -> dev-frontend
|-- Les deux (dependants) -> dev-backend PUIS dev-frontend
|-- Les deux (independants) -> dev-backend ET dev-frontend (parallele)
```

### Phase Review

```
Lancer code-reviewer
|-- APPROVED -> Phase QA
|-- APPROVED WITH RESERVATIONS -> Phase QA (noter reserves)
|-- REJECTED -> Retour Phase Dev (cycle++)
```

### Phase QA

```
Lancer QA
|-- VALIDATED -> Phase Doc
|-- VALIDATED WITH RESERVATIONS -> Demander confirmation utilisateur
|-- NOT VALIDATED -> Retour Phase Dev (cycle++)

Si cycle > 3 -> ESCALADE utilisateur
```

---

## 5. Points de Validation Utilisateur

| Point | Conditions | Options |
|-------|------------|---------|
| Backlog (GitHub Issues) | FEATURE uniquement | Confirmer / Refuser / Autre |
| Plan | Si creation plan | Valider / Modifier / Refuser |
| QA reserves | Reserves mineures | Continuer / Corriger |
| Escalade | 3 cycles atteints | Continuer / Abandonner |

---

## 6. Gestion des Erreurs CDP

| Situation | Action |
|-----------|--------|
| Issue GitHub non trouvee | Proposer creation via `gh issue create` ou continuer sans |
| Plan refuse | Demander modifications |
| Review rejetee | Retour DEV avec corrections |
| QA echoue | Retour DEV avec erreurs |
| Build echoue | Retour DEV avec erreur build |
| 3 cycles atteints | Escalade utilisateur |

---

## 7. Rapport Final CDP

```markdown
## Rapport de Workflow [TYPE]

**Informations**
- Type : [FEATURE|BUGFIX|HOTFIX|REFACTOR]
- Branche : [nom]
- Version : [X.Y.Z]
- Duree : [temps]
- Cycles : [nombre]

**Livrables**
- Code : [fichiers modifies]
- Tests : [ajoutes/modifies]
- Documentation : [mise a jour]

**Prochaines etapes**
- Valider QUALIF
- `/deploy PROD` apres validation
```

---

## 8. Regles par Type

### FEATURE

- Scope large autorise
- Refactoring autorise
- Tests nouveaux requis
- Documentation complete
- QUALIF obligatoire

### BUGFIX

- Scope minimal obligatoire
- Pas de refactoring
- Test non-regression OBLIGATOIRE
- Doc si majeur
- QUALIF obligatoire

### HOTFIX

- Fix minimal UNIQUEMENT
- Pas de refactoring
- Test critique obligatoire
- QUALIF optionnel si urgent
- Post-mortem requis

### REFACTOR

- Comportement identique obligatoire
- Tests AVANT refactoring
- Incremental (petits changements)
- Pas de documentation
- QUALIF pour validation

---

## 9. Mots-Cles de Controle

Les commandes CDP reconnaissent des mots-cles speciaux pour interroger ou reprendre un workflow.

**Reference complete :** Voir `context/COMMON.md` section 12

### Handling des Mots-Cles

```
Reception $ARGUMENTS
    |
    |-- Premier mot = "help" ?
    |   |-- Afficher aide et mots-cles disponibles
    |
    |-- Premier mot = "status" ?
    |   |-- Afficher etat workflow actuel
    |
    |-- Premier mot = "plan" ?
    |   |-- Afficher plan sans executer
    |
    |-- Premier mot = "resume" ?
    |   |-- Extraire <phase>, valider, reprendre
    |
    |-- Premier mot = "skip" ?
    |   |-- Extraire <phase>, marquer skippee, continuer
    |
    |-- Premier mot = "jumpto" ?
    |   |-- Extraire <tache>, rechercher, positionner
    |
    |-- Sinon -> Workflow normal
```

### Etat Persistant

Pour supporter `status`/`resume`/`jumpto`, le CDP maintient un etat :

```yaml
workflow_state:
  type: FEATURE|BUGFIX|HOTFIX|REFACTOR
  description: "..."
  branch: feature/xxx
  current_phase: dev|review|qa|doc|deploy
  phase_status:
    init: completed
    plan: completed
    dev: in_progress
    review: pending
    qa: pending
    doc: pending
    deploy: pending
  tasks:
    - name: "Backend API"
      status: completed
    - name: "Frontend composant"
      status: in_progress
    - name: "Tests"
      status: pending
  cycles: 1
  started_at: "2025-01-15T10:00:00"
```

### Etat Global CDP

Pour la commande `/cdp`, l'orchestrateur maintient un etat global :

```yaml
cdp_state:
  active_workflow:
    type: FEATURE|BUGFIX|HOTFIX|REFACTOR
    description: "..."
    branch: feature/xxx
    current_phase: dev
  context_additions:
    - "Information supplementaire 1"
    - "Information supplementaire 2"
  notes:
    - "Note pour le rapport final"
  priority: normal|high|low
  paused: false
  config:
    max_cycles: 3
    auto_commit: false
    parallel_agents: true
  history:
    - {type: BUGFIX, description: "...", completed_at: "..."}
```

---

## 10. Commande /cdp

La commande `/cdp` permet le controle direct de l'orchestrateur :

| Mot-cle | Action |
|---------|--------|
| `help` | Aide sur /cdp |
| `status` | Vue globale (tous workflows) |
| `abort` | Abandonner workflow actuel |
| `pause` | Mettre en pause |
| `resume` | Reprendre apres pause |
| `context "..."` | Ajouter contexte aux sous-agents |
| `note "..."` | Ajouter note au rapport final |
| `priority <level>` | Changer priorite (high/normal/low) |
| `config` | Afficher configuration CDP |

**Difference cle** :
- `/feature status` -> etat du workflow FEATURE
- `/cdp status` -> vue globale de l'orchestrateur

---

## Usage

Dans les commandes CDP, referencer ce fichier :

```markdown
**Workflow CDP :** Voir `context/CDP_WORKFLOWS.md`
- Type : FEATURE|BUGFIX|HOTFIX|REFACTOR
- Phases : section 3
- Validation : section 5
- Erreurs : section 6
- Mots-cles controle : section 9
- Commande /cdp : section 10
```
