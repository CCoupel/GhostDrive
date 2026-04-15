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
Executer le travail
    |
    v
Envoyer le rapport au CDP (SendMessage)
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

### Format du rapport de fin de tache

```
**[NOM-AGENT] TERMINE**
---------------------------------------
Tache : [description de la tache recue]
Resultat : SUCCES / ECHEC
[Details pertinents : fichiers modifies, verdict, points importants...]
---------------------------------------
```

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
4. **Pas d'initiative** — ne jamais commencer un travail sans ordre du CDP
5. **Pas de communication directe** — l'utilisateur parle via le CDP, pas directement
6. **Texte naturel** — les messages sont lisibles, pas en JSON

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
