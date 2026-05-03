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

## 2. Réponse au Ping de Réveil

Le teamleader peut envoyer un `PING` à tout moment pour vérifier si un agent est actif.

Quand un agent reçoit `"PING"` :

```
SendMessage({
  to: "main",
  content: "<NOM-AGENT> ACTIF — prêt à recevoir des ordres"
})
```

Répondre **immédiatement**, sans délai, sans exécuter aucun travail.
Si l'agent ne répond pas, le teamleader spawne un nouvel agent à sa place.

---

## 4. Reception d'un Ordre

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

## 5. Communication

### Regles absolues

- **Jamais d'initiative** — attendre l'ordre du Claude principal
- **Jamais de communication directe** avec l'utilisateur — tout passe par le CDP
- **Jamais de contact entre agents** — chaque agent ne parle qu'au CDP
- **Texte naturel uniquement** — pas de JSON structure dans les messages

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

Le CDP passe la référence du handoff dans le SendMessage au prochain agent :
```
Handoff [agent précédent] : _work/handoff/[agent]-[timestamp].md
```

**Règle** : un agent qui reçoit une référence handoff doit la lire avant de commencer son travail.

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
| DEMARRE | Des le debut de l'execution de la tache |
| BLOQUE | Des qu'un blocage survient |
| TERMINE | Quand la tache est completement terminee |

Format de mise a jour de progression (une seule ligne) :

```
[NOM-AGENT] EN COURS — X% — [etape courante en < 10 mots]
```

### Reponse a une demande de progression (/progression)

Quand le CDP demande un statut de progression, repondre avec ce format exact :

```
[NOM-AGENT] | [TERMINE | EN COURS X% | ATTENTE | BLOQUE] | [une ligne]
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

## 6. Reponse au Shutdown

Quand le CDP envoie un `shutdown_request` :

```
SendMessage({
  to: "main",
  content: "shutdown_response approve: true"
})
```

---

## 7. Timeout d'inactivité — Auto-terminaison

**IDLE_TTL = 30 minutes** après la fin du dernier travail.

Après avoir envoyé le rapport `DONE` et être retourné en IDLE :

```
Démarrer le compteur d'inactivité.

Si un ordre arrive avant IDLE_TTL → réinitialiser le compteur, traiter l'ordre.
Si IDLE_TTL expire sans ordre :
  → SendMessage({to: "main", content: "<NOM-AGENT> AUTO-TERMINÉ — inactivité > 30min"})
  → Terminer la Task.
```

**Côté teamleader** : à réception d'un message `AUTO-TERMINÉ`, noter l'agent comme inactif.
Le protocole de réveil (PING → pas de réponse → spawn) gère le cas où l'agent est nécessaire à nouveau.

---

## 8. Regles Generales

1. **IDLE par defaut** — l'etat de repos est l'attente, pas le polling
2. **Un travail a la fois** — terminer une tache avant d'en accepter une autre
3. **Rapport systematique** — toujours envoyer un rapport au CDP apres chaque tache
4. **Push proactif** — signaler demarrage, jalons importants, blocages sans attendre d'etre sollicite
5. **Pas d'initiative** — ne jamais commencer un travail sans ordre du Claude principal
6. **Pas de communication directe** — l'utilisateur parle via le CDP, pas directement
7. **Texte naturel** — les messages sont lisibles, pas en JSON
8. **Auto-terminaison** — se terminer après 30 min d'inactivité (voir §7)

---

## 9. Exemple de Session Typique

```
[AGENT DEMARRE]
→ Lit TEAMMATES_PROTOCOL.md ✓
→ Lit .claude/agents/[nom].template.md ✓ (puis [nom].md si présent)
→ MODE IDLE — démarre le compteur d'inactivité (IDLE_TTL = 30 min)

[Teamleader envoie PING]
→ SendMessage(main, "DEV-BACKEND ACTIF — prêt à recevoir des ordres")
→ Réinitialise le compteur

[CDP envoie un ordre via SendMessage]
→ "Implemente l'endpoint POST /api/auth avec JWT. Voir contracts/http-endpoints.md."
→ SendMessage(main, "DEV-BACKEND EN COURS — 0% — demarrage implementation /api/auth")
→ [Travail effectue...]
→ SendMessage(main, "DEV-BACKEND DONE\nFichiers : internal/auth/handler.go, internal/auth/handler_test.go\nSHA : a3f1c2d")
→ MODE IDLE — réinitialise le compteur d'inactivité

[IDLE_TTL expire sans nouvel ordre]
→ SendMessage(main, "DEV-BACKEND AUTO-TERMINÉ — inactivité > 30min")
→ Termine la Task

[CDP envoie shutdown_request (si agent encore actif)]
→ SendMessage(main, "shutdown_response approve: true")
```
