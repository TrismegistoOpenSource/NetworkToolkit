# Sicurezza

## Segnalare una vulnerabilità

Non aprire una *issue* pubblica: usa la segnalazione privata di GitHub
(scheda **Security** del repository → *Report a vulnerability*).

## Cosa sapere

NetworkToolkit cambia l'indirizzo MAC delle schede di rete, il che richiede permessi di amministratore: li chiede al sistema operativo nel modo normale. Lo speedtest contatta server pubblici di Cloudflare (`speed.cloudflare.com`). All'avvio il programma fa anche l'**autoverifica**: una volta per versione scarica tre file pubblici della release (somme di controllo, firma ed elenco dei file) per controllare di essere integro e firmato da Trismegisto Open Source; non parte nessun dato. Limiti e dettagli in [docs/autoverifica.md](docs/autoverifica.md).
