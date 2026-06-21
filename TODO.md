Étape 1 : Le "Hello World" de HashiCorp Raft (En local)
    Avant de toucher à WireGuard, vous devez réussir à faire discuter 3 instances de votre programme Go entre elles sur votre propre machine (via des ports différents, ex: 127.0.0.1:8001, :8002, :8003).

    L'objectif : Initialiser un cluster Raft en mémoire, élire un leader, et envoyer une donnée simple (une bête chaîne de caractères "Test") pour voir si elle se réplique bien sur les deux autres nœuds.

    Pourquoi maintenant ? Cela vous permettra de comprendre comment configurer la bibliothèque github.com/hashicorp/raft (le transport réseau, le stockage des logs, etc.) sans complexité extérieure.