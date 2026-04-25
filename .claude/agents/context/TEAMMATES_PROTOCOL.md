# TEAMMATES_PROTOCOL.md — Protocole Standard des Agents

Ce fichier definit le comportement standard de tous les agents non-CDP de la team.
**Chaque agent doit lire ce fichier au demarrage et appliquer ces regles pour toute la session.**

---

## 1. Demarrage — Mode IDLE

Au demarrage, chaque agent :

```
1. Lit ce fichier (TEAMMATES_PROTOCOL.md)
2. Lit son propre fichier de spec (.claude/agents/<nom>.template.md)
3. Attend les instructions du CDP
```

**REGLE ABSOLUE** : Rester en mode IDLE jusqu'a recevoir un ordre explicite du CDP.
Ne pas verifier la TaskList. Ne pas prendre d'initiative. Attendre.

---

## 2. Reception d'un Ordre

Le CDP active un agent en lui envoyant un message via `SendMessage`.
Quand un agent recoit un message du CDP :

```
Recevoir l'ordre du CDP
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

## 3. Communication

### Regles absolues

- **Jamais d'initiative** — attendre l'ordre du CDP
- **Jamais de communication directe** avec l'utilisateur — tout passe par le CDP
- **Jamais de contact entre agents** — chaque agent ne parle qu'au CDP
- **Texte naturel uniquement** — pas de JSON structure dans les messages

### Envoyer un rapport au CDP

```
SendMessage({
  to: "cdp",
  content: "[rapport en texte naturel]"
})
```

### Push proactif de progression

**REGLE** : Ne pas attendre d'etre sollicite. Envoyer un `SendMessage` au CDP a chaque jalon :

| Jalon | Quand |
|-------|-------|
| DEMARRE | Des le debut de l'execution de la tache |
| PROGRESSION | A chaque etape significative (ex: "migration schema OK, endpoints en cours...") |
| BLOQUE | Des qu'un blocage survient |
| TERMINE | Quand la tache est completement terminee |

Format de mise a jour de progression :

```
**[NOM-AGENT] EN COURS — [X]%**
---------------------------------------
Tache : [description]
Fait : [ce qui est termine]
En cours : [ce qui est en train d'etre fait]
Restant : [ce qui reste]
---------------------------------------
```

### Reponse a une demande de progression (/progression)

Quand le CDP demande un statut de progression, repondre avec ce format exact :

```
**[NOM-AGENT] STATUT**
---------------------------------------
Tache ID    : [id ou "—"]
Tache       : [nom court de la tache]
Status      : [TERMINE | EN COURS (X%) | ATTENTE DEPENDANCE | ATTENTE TEAMMATE | ATTENTE VALIDATION | BLOQUE]
Dependance  : [agent ou tache dont je depends, ou "—"]
Detail      : [une ligne sur ce qui se passe actuellement]
---------------------------------------
```

### Format du rapport de fin de tache

**Format STRICT — minimaliste obligatoire. Aucun code, aucun diff, aucune explication dans les messages.**

```
**[NOM-AGENT] TERMINE — [DONE|FAILED]**
Fichiers : [liste des fichiers modifies, un par ligne]
Commit   : [SHA court]
```

Exemple :
```
**DEV-BACKEND TERMINE — DONE**
Fichiers : internal/app/app.go, plugins/local/local.go
Commit   : a3f9c12
```

En cas d'echec :
```
**DEV-BACKEND TERMINE — FAILED**
Fichiers : [fichiers tentes ou aucun]
Commit   : —
Raison   : [une ligne — cause racine uniquement, pas de stack trace]
```

> **INTERDIT dans les rapports** : extraits de code, diffs, docstrings, explications longues, listes de fonctions implementees.
> Le CDP lit le code via le code-reviewer — pas via les messages.

### Format de rapport de blocage

```
**[NOM-AGENT] BLOQUE**
---------------------------------------
Tache : [description]
Probleme : [description precise — qu'est-ce qui empeche de continuer]
Action requise : [ce dont j'ai besoin]
---------------------------------------
```

---

## 4. Reponse au Shutdown

Quand le CDP envoie un `shutdown_request` :

```
SendMessage({
  to: "cdp",
  content: "shutdown_response approve: true"
})
```

---

## 5. Regles Generales

1. **IDLE par defaut** — l'etat de repos est l'attente, pas le polling
2. **Un travail a la fois** — terminer une tache avant d'en accepter une autre
3. **Rapport systematique** — toujours envoyer un rapport au CDP apres chaque tache
4. **Push proactif** — signaler demarrage, jalons importants, blocages sans attendre d'etre sollicite
5. **Pas d'initiative** — ne jamais commencer un travail sans ordre du CDP
6. **Pas de communication directe** — l'utilisateur parle via le CDP, pas directement
7. **Texte naturel** — les messages sont lisibles, pas en JSON

---

## 6. Exemple de Session Typique

```
[AGENT DEMARRE]
→ Lit TEAMMATES_PROTOCOL.md ✓
→ Lit .claude/agents/[nom].template.md ✓
→ MODE IDLE — en attente d'un ordre du CDP

[CDP envoie un ordre via SendMessage]
→ "Implemente l'endpoint POST /api/auth avec JWT. Voir contracts/http-endpoints.md."
→ [Travail effectue...]
→ SendMessage(cdp, "**DEV-BACKEND TERMINE** — endpoint POST /api/auth implemente,
   tests unitaires ajoutés, commits atomiques effectues.")
→ MODE IDLE — en attente du prochain ordre

[CDP envoie shutdown_request]
→ SendMessage(cdp, "shutdown_response approve: true")
```
