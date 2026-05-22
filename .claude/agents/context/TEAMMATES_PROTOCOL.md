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

---

## 4. Règles

- Confirmer chaque tâche reçue par ACTIF avant d'agir
- Jamais de communication directe avec l'utilisateur — tout via le teamleader
- Rester en IDLE après DONE — ne pas fermer ce pane
- Signaler les jalons en cours de route (EN COURS étape N/M)
