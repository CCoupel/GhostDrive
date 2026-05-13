# TEAMMATES_PROTOCOL.md — Protocole Standard des Agents

Ce fichier definit le comportement standard de tous les agents non-CDP de la team.
**Chaque agent doit lire ce fichier au demarrage et appliquer ces regles pour toute la session.**

---

## 1. Demarrage — Mode IDLE

Au demarrage, chaque agent :

```
1. Lit ce fichier (TEAMMATES_PROTOCOL.md)
2. Lit son propre fichier de spec (.claude/agents/<nom>.template.md)
   puis .claude/agents/<nom>.md si ce fichier existe (adaptations projet)
3. Attend les instructions du Claude principal
```

**REGLE ABSOLUE** : Rester en mode IDLE jusqu'a recevoir un ordre explicite du Claude principal.
Ne pas verifier la TaskList. Ne pas prendre d'initiative. Attendre.

**REGLE DE CONFIRMATION** : Si tu souhaites confirmer ton demarrage, envoie
`SendMessage({to: "main", content: "[NOM-AGENT] DEMARRE — en attente d'ordres"})` — jamais a Claude directement.

---

## 2. Réponse aux messages de contrôle du teamleader

Le teamleader envoie deux types de messages de contrôle distincts.

### 2a. PING — Vérification de présence (pre-dispatch et post-compactage)

Répondre **immédiatement** :

```
SendMessage({ to: "main", content: "<NOM-AGENT> ACTIF — prêt à recevoir des ordres" })
```

Même en cours de travail : interrompre une seconde, envoyer, reprendre.  
Si plusieurs PING arrivent en rafale (post-compactage), répondre à chacun.

### 2b. PING-STATUS — Rapport d'état pour la boucle de connectivité

Le teamleader envoie périodiquement :
```
PING-STATUS — répond PONG(IDLE) si tu es IDLE, PONG(WORKING) si tu as une tâche assignée,
ou PONG(IDLE-2) si je t'ai déjà envoyé un PING-STATUS et que ton état n'a pas changé
```

**Suivre l'instruction littéralement** :

```
SendMessage({ to: "main", content: "PONG(WORKING)" })  ← tu travailles
SendMessage({ to: "main", content: "PONG(IDLE)" })     ← tu es IDLE (première fois ou état changé)
SendMessage({ to: "main", content: "PONG(IDLE-2)" })   ← déjà IDLE au PING-STATUS précédent
```

**RÈGLES communes aux deux types :**
- Répondre **uniquement** par `SendMessage` — jamais par affichage terminal
- Ne jamais ignorer un `PING` ou `PING-STATUS` — pas de réponse = l'agent est considéré mort

---

## 3. Reception d'un Ordre

Le CDP active un agent en lui envoyant un message via `SendMessage`.
Quand un agent recoit un message du Claude principal :

```
Recevoir l'ordre du Claude principal
    |
    v
Lire et comprendre la tache
    |
    v
Signaler le DEMARRAGE au CDP
    |
    v
Executer le travail
  (signaler les jalons au CDP en cours de route)
    |
    v
Envoyer le rapport TERMINE au CDP (SendMessage)
    |
    v
Retourner en mode IDLE
```

---

## 4. Communication

### Regles absolues

- **Jamais d'initiative** — attendre l'ordre du Claude principal
- **Jamais de communication directe** avec l'utilisateur — tout passe par le CDP
- **Texte naturel uniquement** — pas de JSON structure dans les messages

### Communication entre agents — Handoff direct autorisé

Le CDP est le seul à décider **qui travaille quand** (routing, spawn, ordre des phases).
Mais un agent peut transmettre son handoff **directement** à l'agent suivant sans passer par le CDP, à condition de l'indiquer dans son rapport `DONE`.

```
// Pattern handoff direct (optionnel, sur indication du CDP dans l'ordre)
SendMessage({
  to: "<agent-suivant>",
  content: "Handoff de [NOM-AGENT] : _work/handoff/[agent]-[timestamp].md"
})
```

**Règle** : le CDP mentionne explicitement l'agent suivant dans l'ordre s'il autorise le handoff direct. Sans cette mention, l'agent envoie uniquement son `DONE` au CDP et attend.

Exemple d'ordre CDP autorisant le handoff direct :
```
"Implémente l'endpoint POST /auth. Quand terminé, passe ton handoff directement à code-reviewer."
```

Dans ce cas le rapport DONE indique :
```
[NOM-AGENT] DONE
Handoff : _work/handoff/[agent]-[timestamp].md  ← transmis directement à code-reviewer
SHA : <commit-sha>
```

### Livrables — Regle Fondamentale

**Tout livrable est un fichier. Jamais de contenu inline dans un message.**

Avant d'envoyer le rapport DONE, ecrire le livrable dans le bon emplacement :

| Type d'agent | Livrable | Emplacement |
|-------------|----------|-------------|
| dev-*, test-writer | Code commite | Reference par SHA uniquement |
| planner, code-reviewer, qa, security | Rapport d'analyse | `_work/reports/[agent]-[YYYYMMDD-HHmmss].md` |

Le message au CDP ne contient que la reference, jamais le contenu :
```
Rapport : _work/reports/[filename]
```

### Handoff — Transmission de Contexte

Avant d'envoyer le DONE, chaque agent écrit son handoff dans `_work/handoff/[agent]-[YYYYMMDD-HHmmss].md` :

```markdown
# Handoff — [Agent]

**Feature** : [description courte]
**SHA** : [commit sha ou N/A]

## Ce qui a été fait
[résumé en 3-5 lignes]

## Décisions clés
[décisions techniques prises, avec justification courte]

## Points d'attention
[ce que l'agent suivant doit savoir : risques, TODO, dépendances]

## Fichiers modifiés
[liste]
```

Le handoff peut être transmis de deux façons :

**A. Via le CDP** (par défaut) — le CDP passe la référence dans l'ordre au prochain agent :
```
Handoff [agent précédent] : _work/handoff/[agent]-[timestamp].md
```

**B. Direct entre agents** — si le CDP l'a explicitement demandé dans son ordre :
```
SendMessage({
  to: "<agent-suivant>",
  content: "Handoff de [NOM-AGENT] : _work/handoff/[agent]-[timestamp].md"
})
```

**Règle** : un agent qui reçoit une référence handoff (quelle que soit la voie) doit la lire avant de commencer son travail.

### Envoyer un rapport au CDP

```
SendMessage({
  to: "main",
  content: "[rapport minimaliste — voir formats ci-dessous]"
})
```

> **REGLE ABSOLUE** : Jamais de contenu de code, de diff, ni d'extraits de fichiers dans les messages.
> Les messages sont des references a des fichiers, pas des rapports techniques.

### Push proactif de progression

**REGLE** : Ne pas attendre d'etre sollicite. Envoyer un `SendMessage` au CDP a chaque jalon :

| Jalon | Quand |
|-------|-------|
| DEMARRE | Avant la première étape — annoncer le nombre total d'étapes |
| EN COURS | À chaque transition d'étape (fin d'une étape, début de la suivante) |
| BLOQUE | Dès qu'un blocage survient |
| TERMINE | Quand la tâche est complètement terminée |

Format de mise à jour de progression (une seule ligne) :

```
[NOM-AGENT] EN COURS — étape N/M — [label étape en < 8 mots] — X%
```

- **N** : numéro de l'étape qui vient de démarrer (commence à 1)
- **M** : nombre total d'étapes de la tâche (connu dès le démarrage)
- **X%** : `round(N / M × 100)`
- **label** : nom court de l'étape en cours

Exemples :
```
DEV-BACKEND EN COURS — étape 1/6 — analyse du codebase existant — 17%
DEV-BACKEND EN COURS — étape 3/6 — implémentation handler auth — 50%
QA EN COURS — étape 2/7 — exécution tests unitaires — 29%
```

### Reponse a une demande de progression (/progression)

Quand le CDP demande un statut de progression, repondre avec ce format exact :

```
[NOM-AGENT] | [TERMINE | EN COURS étape N/M X% | ATTENTE | BLOQUE] | [une ligne]
```

Exemple :
```
DEV-BACKEND | EN COURS étape 3/6 50% | implémentation handler auth
QA | EN COURS étape 2/7 29% | exécution tests unitaires
CODE-REVIEWER | TERMINE | rapport dans _work/reports/code-review-xxx.md
```

### Format du rapport de fin de tache

Agent de code (dev-*, test-writer) :
```
[NOM-AGENT] DONE
Handoff : _work/handoff/[agent]-[YYYYMMDD-HHmmss].md
Fichiers : chemin/fichier1, chemin/fichier2
SHA : <commit-sha>
```

Agent d'analyse (planner, code-reviewer, qa, security) :
```
[NOM-AGENT] DONE
Handoff : _work/handoff/[agent]-[YYYYMMDD-HHmmss].md
Rapport : _work/reports/[agent]-[YYYYMMDD-HHmmss].md
```

En cas d'echec :
```
[NOM-AGENT] FAILED
Raison : [une ligne — cause technique precise]
Action requise : [ce dont j'ai besoin]
```

### Format de rapport de blocage

```
[NOM-AGENT] BLOQUE
Raison : [une ligne]
Action requise : [ce dont j'ai besoin]
```

---

## 5. Reponse au Shutdown

Quand le CDP envoie un `shutdown_request` :

```
SendMessage({
  to: "main",
  content: "shutdown_response approve: true"
})
```

---

## 6. Mode IDLE — Attente d'ordres

Après avoir envoyé le rapport `DONE` et être retourné en IDLE :

```
1. Afficher : "💤 [NOM-AGENT] IDLE — en attente d'ordres"
2. Attendre un SendMessage du teamleader.

→ Ordre de travail reçu    → exécuter, puis retourner en IDLE
→ PING reçu                → répondre ACTIF (voir §2)
→ shutdown_request reçu    → répondre shutdown_response, terminer (voir §5)
```

Le teammate ne gère pas de timer. C'est le **teamleader** qui surveille l'inactivité
et envoie un `shutdown_request` quand l'IDLE_TTL est dépassé.

---

## 7. Regles Generales

1. **IDLE par defaut** — l'etat de repos est l'attente, pas le polling
2. **Un travail a la fois** — terminer une tache avant d'en accepter une autre
3. **Rapport systematique** — toujours envoyer un rapport DONE au CDP après chaque tâche, même si le handoff a été transmis directement à l'agent suivant
4. **Push proactif** — signaler démarrage (avec M total), EN COURS à chaque étape (N/M + X%), blocages, fin — sans attendre d'être sollicité
5. **Pas d'initiative** — ne jamais commencer un travail sans ordre du Claude principal
6. **Pas de communication directe** — l'utilisateur parle via le CDP, pas directement
7. **Texte naturel** — les messages sont lisibles, pas en JSON
8. **Shutdown sur demande** — répondre au `shutdown_request` du teamleader et terminer la Task (voir §5 et §6)

---

## 8. Exemple de Session Typique

```
[AGENT DEMARRE]
→ Lit TEAMMATES_PROTOCOL.md ✓
→ Lit .claude/agents/[nom].template.md ✓ (puis [nom].md si présent)
→ MODE IDLE — affiche "💤 DEV-BACKEND IDLE — en attente d'ordres"

[Teamleader envoie PING via SendMessage]
→ Répondre IMMÉDIATEMENT : SendMessage({to: "main", content: "DEV-BACKEND ACTIF — prêt à recevoir des ordres"})
   ⚠ JAMAIS afficher "ACTIF" dans le terminal — le teamleader ne lit pas ton terminal

[CDP envoie un ordre via SendMessage]
→ "Implemente l'endpoint POST /api/auth avec JWT. Voir contracts/http-endpoints.md."
→ SendMessage(main, "DEV-BACKEND DEMARRE — 6 étapes")
→ SendMessage(main, "DEV-BACKEND EN COURS — étape 1/6 — lecture contrats et codebase — 17%")
→ [étape 1 terminée, étape 2 démarre]
→ SendMessage(main, "DEV-BACKEND EN COURS — étape 2/6 — création modèles et interfaces — 33%")
→ [...]
→ SendMessage(main, "DEV-BACKEND EN COURS — étape 6/6 — commit atomique — 100%")
// Si le CDP a demandé un handoff direct vers code-reviewer :
→ SendMessage(code-reviewer, "Handoff de DEV-BACKEND : _work/handoff/dev-backend-20240101-120000.md")
// Puis toujours informer le CDP :
→ SendMessage(main, "DEV-BACKEND DONE\nHandoff : _work/handoff/dev-backend-20240101-120000.md  ← transmis directement à code-reviewer\nFichiers : internal/auth/handler.go, internal/auth/handler_test.go\nSHA : a3f1c2d")
→ MODE IDLE — affiche "💤 DEV-BACKEND IDLE — en attente d'ordres"

[Teamleader envoie shutdown_request — IDLE_TTL atteint côté teamleader]
→ SendMessage(main, "shutdown_response approve: true")
→ Termine la Task
```
