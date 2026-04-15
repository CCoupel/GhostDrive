# Commande /review

Lancer une revue de code manuellement.

## Usage

```
/review [scope]
```

## Argument recu

$ARGUMENTS

## Mots-cles de controle

**Reference :** Voir `context/COMMON.md` section 12

| Mot-cle | Action |
|---------|--------|
| `help` | Affiche l'aide et les mots-cles disponibles |
| `status` | Affiche l'etat du workflow en cours |
| `plan` | Affiche le plan sans executer |
| `resume <phase>` | Reprend a une phase |
| `skip <phase>` | Saute une phase |
| `jumpto <tache>` | Demarre a une tache precise du plan |

Si `$ARGUMENTS` commence par un mot-cle -> executer l'action correspondante.
Sinon -> workflow normal.

## Scopes de Code

| Scope | Description |
|-------|-------------|
| (vide) | Revue des changements non commites |
| `staged` | Revue des fichiers stages uniquement |
| `branch` | Revue de toute la branche vs main |
| `commit <sha>` | Revue d'un commit specifique |
| `file <path>` | Revue d'un fichier specifique |

## Modes de Revue

| Mode | Description | Focus |
|------|-------------|-------|
| (vide) | Revue generale | Qualite, bugs, conventions |
| `security` | Audit securite | OWASP Top 10, secrets, injection, auth |
| `performance` | Audit performance | Algorithmes, N+1, cache, memoire |
| `rationalization` | Rationalization | Duplications, abstractions, dette technique |

### Mode `security`

Focus sur :
- Vulnerabilites OWASP Top 10 (injection SQL, XSS, CSRF, ...)
- Secrets ou credentials hardcodes
- Validation des entrees aux frontieres
- Gestion des erreurs (ne pas exposer les details internes)
- Authentification et autorisation
- Dependances vulnerables

### Mode `performance`

Focus sur :
- Algorithmes inefficaces (O(n²) evitable)
- Requetes N+1 en base de donnees
- Opportunites de mise en cache
- Fuites memoire potentielles
- Appels bloquants dans du code asynchrone
- Assets non optimises (images, bundles)

### Mode `rationalization`

Focus sur :
- Code duplique (>70% de similarite)
- Patterns repetes 3+ fois → abstraction possible
- Fonctions trop longues ou trop complexes
- Dead code (code jamais execute)
- Abstractions prematurees (over-engineering)
- Opportunites de simplification

## Exemples

```
/review                        # Changements en cours (revue generale)
/review staged                 # Fichiers stages
/review branch                 # Toute la branche
/review commit abc123          # Commit specifique
/review file src/api.ts        # Fichier specifique
/review security               # Audit securite complet
/review branch security        # Audit securite de la branche
/review performance            # Audit performance
/review rationalization        # Recherche de duplications
```

## Rapport

Le rapport inclut :
- 🔴 Problemes critiques (bloquants)
- 🟡 Problemes majeurs (a corriger)
- 🟠 Suggestions de rationalization (si mode actif)
- 🔵 Suggestions mineures (optionnel)
- ✅ Points positifs

**Verdict final** : `APPROUVE` / `APPROUVE AVEC RESERVES` / `REFUSE`

**Contexte Qualite :** Voir `context/QUALITY.md`

## Agent

Lance l'agent `code-reviewer` defini dans `.claude/agents/code-reviewer.template.md`
