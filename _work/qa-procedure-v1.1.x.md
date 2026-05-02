# Procédure QA manuelle — v1.1.x

## Pré-requis
- GhostDrive v1.1.x buildé (Windows AMD64)
- WinFsp installé
- Au moins un backend WebDAV disponible

## Scénarios

### TC-01 : Nouveau backend — état désactivé par défaut
1. Ouvrir Settings → ajouter un backend WebDAV
2. Vérifier : icône grise (désactivé) sur la carte
3. Vérifier : aucun nouveau lecteur dans l'Explorateur Windows
4. Vérifier : lettre de lecteur proposée dans le formulaire (ex. E:)

### TC-02 : Activation backend → montage drive
1. Activer le backend via le toggle
2. Vérifier : icône verte (connecté) sur la carte
3. Vérifier : nouveau lecteur E: apparaît dans l'Explorateur Windows
4. Vérifier : le lecteur porte le nom du backend

### TC-03 : Désactivation backend → démontage drive
1. Désactiver le backend via le toggle
2. Vérifier : icône grise
3. Vérifier : le lecteur E: disparaît sans redémarrage

### TC-04 : Conflit de lettre de lecteur
1. Ajouter un deuxième backend avec la même lettre qu'un backend existant
2. Vérifier : erreur bloquante dans le formulaire

### TC-05 : GetQuota indisponible (#89)
1. Configurer un backend WebDAV sans RFC 4331
2. Activer le backend
3. Vérifier : "Quota non disponible" affiché (pas "0 o libre")

### TC-06 : Redémarrage — backends enabled persistés
1. Activer un backend, vérifier le drive monté
2. Redémarrer GhostDrive
3. Vérifier : le drive est automatiquement remonté
