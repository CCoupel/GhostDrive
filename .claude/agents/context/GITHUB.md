# GITHUB.md — Patterns GitHub Partages

Centralise tous les patterns `gh` CLI utilises dans les commandes et agents.
**Toujours reference ce fichier plutot que de dupliquer ces patterns.**

---

## 1. Prerequis et Verification

### 1.1 Verifier l'authentification

A executer en debut de toute commande interagissant avec GitHub :

```bash
gh auth status
```

Si la commande echoue :
```
GitHub CLI non authentifie.
Lancez : gh auth login
Puis relancez la commande.
```

### 1.2 Recuperer owner/repo (pattern standard)

```bash
# Retourne "owner/repo" ex: "monorg/monprojet"
gh repo view --json owner,name --jq '.owner.login + "/" + .name'

# Retourne le chemin API complet ex: "repos/monorg/monprojet"
gh repo view --json owner,name --jq '"repos/" + .owner.login + "/" + .name'
```

Stocker dans une variable pour reutilisation :
```bash
REPO=$(gh repo view --json owner,name --jq '.owner.login + "/" + .name')
# Utilisation : gh api repos/$REPO/milestones
```

---

## 2. Issues

### 2.1 Lister les issues

```bash
# Toutes les issues ouvertes
gh issue list --state open --limit 50

# Avec details JSON (pour affichage tableau)
gh issue list --state open --limit 100 \
  --json number,title,labels,assignees,milestone,updatedAt

# Filtres disponibles
gh issue list --state open --label "bug"
gh issue list --state open --assignee "@me"
gh issue list --state open --milestone "v1.2.0"
gh issue list --state closed --limit 20

# Issues sans milestone
gh issue list --state open --json number,title,labels,milestone \
  --jq '.[] | select(.milestone == null)'
```

### 2.2 Voir une issue

```bash
gh issue view <numero> --json number,title,body,labels,assignees,milestone,state
```

### 2.3 Creer une issue

```bash
gh issue create \
  --title "<titre>" \
  --body "<description>" \
  --label "<label1>,<label2>" \
  --assignee "<login>" \
  --milestone "<version>"
```

### 2.4 Modifier une issue

```bash
# Assigner un milestone
gh issue edit <numero> --milestone "<version>"

# Changer les labels
gh issue edit <numero> --add-label "bug" --remove-label "feature"

# Changer l'assignee
gh issue edit <numero> --add-assignee "<login>"
```

### 2.5 Fermer / Rouvrir

```bash
# Fermer avec commentaire
gh issue close <numero> --comment "<message>"

# Fermer sans commentaire
gh issue close <numero>

# Rouvrir
gh issue reopen <numero>
```

### 2.6 Commenter une issue

```bash
gh issue comment <numero> --body "<message>"
```

---

## 3. Milestones

### 3.1 Lister les milestones

```bash
# Tous les milestones ouverts (avec progression)
gh api repos/{owner}/{repo}/milestones \
  --jq '.[] | select(.state=="open") | {number, title, open_issues, closed_issues, due_on}'

# Milestone actif le plus avance
gh api repos/{owner}/{repo}/milestones \
  --jq '[.[] | select(.state=="open")] | sort_by(.due_on) | .[0]'

# Milestone correspondant a une version
gh api repos/{owner}/{repo}/milestones \
  --jq '.[] | select(.title=="<version>")'
```

### 3.2 Creer un milestone

```bash
# Sans echeance
gh api repos/{owner}/{repo}/milestones \
  --method POST \
  -f title="<version>" \
  -f description="Release <version>"

# Avec echeance (format ISO 8601)
gh api repos/{owner}/{repo}/milestones \
  --method POST \
  -f title="<version>" \
  -f description="Release <version>" \
  -f due_on="<YYYY-MM-DD>T23:59:59Z"
```

### 3.3 Cloturer un milestone

```bash
# Recuperer d'abord le numero du milestone
MILESTONE_NUM=$(gh api repos/{owner}/{repo}/milestones \
  --jq '.[] | select(.title=="<version>") | .number')

# Cloturer
gh api repos/{owner}/{repo}/milestones/$MILESTONE_NUM \
  --method PATCH \
  -f state="closed"
```

### 3.4 Progression d'un milestone

```bash
# Calcul : closed / (open + closed) * 100
gh api repos/{owner}/{repo}/milestones \
  --jq '.[] | select(.title=="<version>") |
    {
      title,
      total: (.open_issues + .closed_issues),
      done: .closed_issues,
      pct: (if (.open_issues + .closed_issues) > 0
            then (.closed_issues * 100 / (.open_issues + .closed_issues) | floor)
            else 0 end)
    }'
```

---

## 4. Pull Requests

### 4.1 Lister les PRs

```bash
gh pr list --state open
gh pr list --state merged --limit 10
gh pr list --base main --state open
```

### 4.2 Voir une PR

```bash
gh pr view <numero> --json number,title,body,author,headRefName,baseRefName,files,commits,state
```

### 4.3 Creer une PR

```bash
gh pr create \
  --title "<titre>" \
  --body "<description>" \
  --base main \
  --head <branche> \
  --label "<label>"
```

### 4.4 Merger une PR

```bash
# Squash merge (recommande pour main)
gh pr merge <numero> --squash --delete-branch

# Merge classique
gh pr merge <numero> --merge
```

### 4.5 Commenter / Reviewer une PR

```bash
# Commenter
gh pr comment <numero> --body "<message>"

# Approuver
gh pr review <numero> --approve --body "<commentaire>"

# Demander des modifications
gh pr review <numero> --request-changes --body "<corrections requises>"
```

---

## 5. Releases et Tags

### 5.1 Lister les releases

```bash
gh release list --limit 10
gh release list --json tagName,name,publishedAt,isPrerelease
```

### 5.2 Creer une release

```bash
# Depuis un tag existant
gh release create <tag> \
  --title "<titre>" \
  --notes-file RELEASE_NOTES.md

# Avec notes inline
gh release create <tag> \
  --title "v1.2.0 - <titre>" \
  --notes "<description>"

# Pre-release
gh release create <tag> --prerelease --title "<titre>" --notes "<description>"
```

### 5.3 Obtenir la derniere release

```bash
gh api repos/{owner}/{repo}/releases/latest --jq '{tag_name, name, published_at}'
```

### 5.4 Tags Git

```bash
# Dernier tag semantique
git tag --sort=-version:refname | head -1

# Lister les tags
git tag --list --sort=-version:refname

# Creer un tag annote
git tag -a <version> -m "Release <version>"
git push origin <version>

# Supprimer un tag (si besoin de rollback)
git tag -d <version>
git push origin --delete <version>
```

---

## 6. CI/CD (GitHub Actions)

### 6.1 Surveiller les workflows

```bash
# Lister les runs recents
gh run list --limit 10

# Suivre un run en temps reel
gh run watch

# Voir les details d'un run
gh run view <run-id>

# Verifier le statut du dernier run sur main
gh run list --branch main --limit 1 --json status,conclusion,headBranch \
  --jq '.[0] | {status, conclusion}'
```

### 6.2 Relancer un workflow

```bash
gh run rerun <run-id>
gh run rerun <run-id> --failed-only
```

### 6.3 Declencher un workflow manuellement

```bash
gh workflow run <nom-workflow>.yml \
  --ref main \
  --field version="v1.2.0"
```

---

## 7. Gestion d'Erreurs

### 7.1 Patterns d'erreurs courants

| Erreur | Cause | Solution |
|--------|-------|----------|
| `gh: command not found` | CLI non installe | `brew install gh` / voir cli.github.com |
| `Not Found (HTTP 404)` | Repo inaccessible ou inexistant | Verifier les droits et le nom du repo |
| `Bad credentials (HTTP 401)` | Token expire | `gh auth login` |
| `API rate limit exceeded` | Trop de requetes | Attendre ou utiliser un token avec plus de quota |
| `Resource not accessible` | Droits insuffisants | Verifier les scopes du token : `gh auth status` |

### 7.2 Verifier les scopes du token

```bash
gh auth status
# Doit inclure : repo, read:org (selon les besoins)
```

### 7.3 Template de verification pre-commande

```bash
# Verifier auth
if ! gh auth status &>/dev/null; then
  echo "Erreur : non authentifie sur GitHub. Lancez : gh auth login"
  exit 1
fi

# Verifier que c'est un repo GitHub
if ! gh repo view &>/dev/null; then
  echo "Erreur : ce repertoire n'est pas lie a un repo GitHub."
  exit 1
fi
```

---

## 8. Conventions du Projet

### 8.1 Nommage

| Element | Convention | Exemple |
|---------|------------|---------|
| Milestone | Version SemVer | `v1.2.0` |
| Tag | Prefixe `v` + SemVer | `v1.2.0` |
| Branche feature | `feature/<nom-court>` | `feature/auth-oauth` |
| Branche bugfix | `fix/<nom-court>` | `fix/crash-login` |
| Branche hotfix | `hotfix/<nom-court>` | `hotfix/security-patch` |

### 8.2 Labels standards

| Label | Usage |
|-------|-------|
| `feature`, `enhancement` | Nouvelle fonctionnalite → `/feature` |
| `bug`, `fix`, `defect` | Bug → `/bugfix` |
| `hotfix`, `urgent`, `critical` | Correctif urgent → `/hotfix` |
| `refactor`, `tech-debt` | Refactoring → `/refactor` |
| `security`, `vulnerability` | Securite → `/secu` |
| `roadmap` | Visible sur le site marketing |
| `in progress` | En cours de traitement |

### 8.3 Format des commits avec issue

```bash
feat(scope): Description (#42)
fix(scope): Description (#38)
```
