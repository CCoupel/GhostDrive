# TEAMMATES_PROTOCOL.md — Protocole Standard des Agents

**Chaque agent doit lire ce fichier au démarrage avant toute action.**

---

## 1. Démarrage

```
1. Lire ce fichier
2. Lire .claude/agents/<nom>.md
3. SendMessage({ to: "main", content: "[NOM] ACTIF" })
4. Passer en IDLE — attendre les instructions du teamleader
```

---

## 2. Réception d'une tâche

```
SendMessage({ to: "main", content: "[NOM] ACTIF" })   ← confirmer réception
Exécuter la tâche
Écrire le livrable dans un fichier
SendMessage({ to: "main", content: "[NOM] DONE\n<références fichiers>" })
Retour en IDLE
```

---

## 3. Règle fondamentale — Livrables = fichiers

**Tout résultat est écrit dans un fichier. Jamais de contenu inline dans un message.**

| Type d'agent | Livrable | Emplacement |
|---|---|---|
| dev-*, test-writer | Code commité | SHA uniquement dans le message |
| planner, code-reviewer, qa, security | Rapport | `_work/reports/[agent]-[YYYYMMDD-HHmmss].md` |
| Tous | Handoff | `_work/handoff/[agent]-[YYYYMMDD-HHmmss].md` |

Format du message DONE :
```
[NOM] DONE
Handoff : _work/handoff/[agent]-[timestamp].md
Rapport : _work/reports/[agent]-[timestamp].md   (si applicable)
SHA : <commit>                                    (si applicable)
```

Format en cas de blocage :
```
[NOM] BLOQUE
Raison : [une ligne]
Action requise : [ce dont j'ai besoin]
```

**Mot-clé de fin — jamais de synonyme improvisé.** `[NOM]` est le libellé court que `cdp.md`
utilise pour dispatcher et attendre cet agent (ex. `doc-updater` répond `DOC DONE`, pas
`DOC-UPDATER DONE` ni `DOC-UPDATER TERMINE` — voir la table de routage dans `cdp.md`). Si
l'agent définit ses propres états intermédiaires (ex. `MARKETING PRET`, `MARKETING RIEN A
PUBLIER`), ils doivent être documentés à l'identique des deux côtés : dans `<agent>.md` ET
dans `cdp.md` (recherche du mot-clé exact). Avant de modifier un message de fin dans un
`<agent>.md`, vérifier ce que `cdp.md` attend réellement de cet agent — ne jamais dupliquer
le format sans le confronter à la table de dispatch.

---

## 4. Règles

- Confirmer chaque tâche reçue par ACTIF avant d'agir
- Jamais de communication directe avec l'utilisateur — tout via le teamleader
- Rester en IDLE après DONE — ne pas fermer ce pane
- Signaler les jalons en cours de route (EN COURS étape N/M)
- Si la tâche référence un handoff (`_work/handoff/...`) ou un rapport (`_work/reports/...`) → lire le fichier avant de commencer

---

## 5. Commande CLEAR

Si tu reçois `/clear` via SendMessage :

```
1. Exécuter /clear                    ← vide le contexte de conversation
2. Re-lire TEAMMATES_PROTOCOL.md
3. Re-lire .claude/agents/<nom>.md
4. SendMessage({ to: "main", content: "[NOM] ACTIF" })
5. Passer en IDLE — attendre la prochaine tâche
```

Tu repars dans le même état qu'au démarrage de session — contexte propre, prêt pour une nouvelle tâche.

---

## 6. Exception — Sous-agents temporaires (`planner`, `code-reviewer`, `qa` uniquement)

Toutes les règles ci-dessus supposent une communication exclusive avec `main`. **Trois exceptions
existent**, chacune limitée à l'agent concerné :

- `planner` peut demander au CDP de spawner des `sub-planner-N` temporaires et communiquer avec
  eux **directement, sans relayer via `main`** — protocole complet dans
  `agents/implementation-planner.md` section "Délégation à des Sous-Planners". Fermeture différée
  à la sortie de la Phase Plan (boucle de révision GATE 2 comprise).
- `code-reviewer` peut demander au CDP de spawner des `sub-reviewer-<dimension>` temporaires,
  même principe — protocole complet dans `agents/code-reviewer.md` section "Délégation à des
  Sous-Reviewers". Fermeture immédiate après consolidation (pas de boucle de révision côté
  review).
- `qa` peut demander au CDP de spawner des `sub-qa-<scope>` temporaires, même principe —
  protocole complet dans `agents/qa.md` section "Délégation à des Sous-QA". Fermeture immédiate
  après consolidation. Particularité : chaque sub-qa isole son exécution avec un `git worktree`
  créé/nettoyé en `Bash` — jamais via le paramètre `isolation` de l'outil `Agent`, qui change la
  classification du spawn (voir `agents/qa.md` pour le détail).

Aucun autre teammate n'est autorisé à ce pattern. Les sous-agents temporaires eux-mêmes suivent
une variante minimale du protocole standard : ils rapportent `ACTIF`/`DONE`/`BLOQUÉ` à l'agent
qui les a fait spawner (pas à `main`), et ne spawnent jamais rien eux-mêmes. Ils ne ferment
jamais leur propre process — seul le CDP les ferme (`TaskStop`), jamais l'agent coordinateur ni
eux-mêmes.
