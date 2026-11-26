# Review server: Mise on the existing EC2 instance

For the owner's review. Mise runs beside the Laravel sites already on the server, in its own directory and in Docker,
with its own database and its own domain. Apache (already serving the Laravel sites) forwards Mise's hostnames to it.
Nothing else on the server changes.

What the owner gets:

| Address | What it is |
|---|---|
| `https://<domain>` | The public site (launch mode: meal kits, plans, chat) |
| `https://app.<domain>` | The customer app: sign up, subscribe (test payments), choose meals, chat, help centre |
| `https://admin.<domain>` | The admin console |
| `https://mail.<domain>` | Every email the system sends (sign-up confirmations, password resets, chat replies), behind a password. Nothing goes to real inboxes. |

Review mode, compared with a real launch: payments use the test provider only (no card is charged, no money moves), demo
meals and plans are loaded, emails are caught, staff sign in without an authenticator app, and search engines are told not
to index anything. Files: `docker-compose.prod.yml` + `docker-compose.review.yml`, `.env.review.example`,
`infrastructure/apache/mise-review.conf`.

Commands below are typed in a MobaXterm SSH session on the server. Replace `misemeals.com` with the real domain everywhere.

## 0. Before you start

**Domain.** At the domain's DNS provider, create five **A** records pointing at the server's **Elastic IP** (the same
public IP the Laravel sites use):

| Name | Type | Value |
|---|---|---|
| `@` (the domain itself) | A | server IP |
| `www` | A | server IP |
| `app` | A | server IP |
| `admin` | A | server IP |
| `mail` | A | server IP |

Records can take a few minutes to an hour to work. Check from your PC: `nslookup app.misemeals.com`.

**Server.** Ports 80 and 443 are already open for the Laravel sites; nothing else needs opening. Check memory and disk:

```sh
cat /etc/os-release | head -2     # Ubuntu, or Amazon Linux
free -h                           # 4 GB or more
df -h /                           # at least 15 GB free (Docker images and build cache)
```

## 1. Swap (once)

Building the web apps needs memory on top of what the Laravel sites use. 2 GB of swap avoids out-of-memory failures.
Skip if `free -h` already shows swap.

```sh
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

## 2. Install Docker (once)

**Ubuntu:**

```sh
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER
```

**Amazon Linux 2023:**

```sh
sudo dnf install -y docker git
sudo systemctl enable --now docker
sudo usermod -aG docker $USER
sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo curl -SL https://github.com/docker/compose/releases/latest/download/docker-compose-linux-$(uname -m) -o /usr/local/lib/docker/cli-plugins/docker-compose
sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
```

Close the MobaXterm tab and open a new one (so the `docker` group applies), then check: `docker compose version`.

## 3. Get the code (once)

The repository is private, so give the server a read-only **deploy key**:

```sh
ssh-keygen -t ed25519 -C "mise-review-server" -f ~/.ssh/mise_deploy -N ""
cat ~/.ssh/mise_deploy.pub
```

On GitHub: the repository → Settings → Deploy keys → Add deploy key → paste it, leave "Allow write access" off.
Then:

```sh
cat >> ~/.ssh/config <<'EOF'
Host github-mise
  HostName github.com
  User git
  IdentityFile ~/.ssh/mise_deploy
EOF
sudo mkdir -p /var/www/mise && sudo chown "$USER" /var/www/mise
git clone git@github-mise:whizzonby/mise-prelauch.git /var/www/mise
cd /var/www/mise
git checkout sprint-9.1/experience-payments-chat
```

(Use `main` instead once the sprint branches are merged.)

## 4. Settings (once)

```sh
cd /var/www/mise
cp .env.review.example .env.production
sed -i "s|^DOMAIN=.*|DOMAIN=misemeals.com|
s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$(openssl rand -hex 24)|
s|^TOKEN_SIGNING_KEY=.*|TOKEN_SIGNING_KEY=$(openssl rand -base64 48 | tr -d '\n')|
s|^DATA_ENCRYPTION_KEY=.*|DATA_ENCRYPTION_KEY=$(openssl rand -base64 32)|
s|^BFF_SHARED_KEY=.*|BFF_SHARED_KEY=$(openssl rand -hex 32)|
s|^MAILPIT_PASSWORD=.*|MAILPIT_PASSWORD=$(openssl rand -base64 12)|
s|hello@CHANGE_ME.com|hello@misemeals.com|" .env.production
chmod 600 .env.production
grep -c CHANGE_ME .env.production      # must print 0
grep '^MAILPIT_' .env.production       # note the inbox login for the owner
```

Keep `.env.production` on the server only; it is never committed. Back it up somewhere safe (the keys cannot be recreated:
changing `TOKEN_SIGNING_KEY` breaks links in emails already sent; changing `DATA_ENCRYPTION_KEY` forces staff to re-enrol MFA).

To save typing, define a shortcut for this session (add the line to `~/.bashrc` to keep it):

```sh
alias mise='docker compose -p mise -f /var/www/mise/docker-compose.prod.yml -f /var/www/mise/docker-compose.review.yml --env-file /var/www/mise/.env.production'
```

## 5. Build and start

Build one piece at a time (building everything at once can run out of memory). Each web app takes several minutes the first
time.

```sh
cd /var/www/mise
export RELEASE_COMMIT=$(git rev-parse --short HEAD)
mise build api && mise build marketing && mise build web && mise build admin
mise up -d
mise ps                       # migrate "exited (0)"; postgres, mailpit, api, worker, marketing, web, admin "running"
mise logs migrate | tail -3   # ends with: schema is at version 16
```

Check each app answers on the server itself:

```sh
curl -s http://127.0.0.1:3100/api/v1/health      # {"data":{"status":"ok"}...}
curl -sI http://127.0.0.1:3102/sign-in | head -1  # HTTP/1.1 200 OK
curl -sI http://127.0.0.1:3101/login | head -1    # HTTP/1.1 200 OK
```

## 6. Demo content and the owner's account

```sh
mise run --rm migrate /app/misectl catalog seed-demo
mise run --rm migrate /app/misectl commerce seed-demo
mise run --rm -e MISE_ADMIN_PASSWORD='choose-a-long-password' migrate \
  /app/misectl admin create -email owner@misemeals.com -name "Owner Name" -role super_admin
```

More staff accounts: the same command with `-role admin`, `support`, `operations`, `finance` or `marketing`, or invite them
from the admin's Staff page (their invitation email arrives in the mail inbox).

## 7. Apache

**Ubuntu** (Apache is `apache2`):

```sh
sudo a2enmod proxy proxy_http proxy_wstunnel headers rewrite setenvif
sudo sed 's/example\.com/misemeals.com/g' /var/www/mise/infrastructure/apache/mise-review.conf \
  | sudo tee /etc/apache2/sites-available/mise.conf > /dev/null
sudo a2ensite mise
sudo apache2ctl configtest && sudo systemctl reload apache2
```

**Amazon Linux** (Apache is `httpd`; the proxy modules are loaded by default):

```sh
sudo sed -e 's/example\.com/misemeals.com/g' -e 's|${APACHE_LOG_DIR}|/var/log/httpd|g' \
  /var/www/mise/infrastructure/apache/mise-review.conf | sudo tee /etc/httpd/conf.d/mise.conf > /dev/null
sudo apachectl configtest && sudo systemctl reload httpd
```

`configtest` must say `Syntax OK` before reloading; if it does not, the Laravel sites are untouched and the error says which
line to fix.

## 8. HTTPS

If certbot is not installed yet: Ubuntu `sudo apt install -y certbot python3-certbot-apache`; Amazon Linux
`sudo dnf install -y certbot python3-certbot-apache`. Then (the DNS records from step 0 must already resolve):

```sh
sudo certbot --apache -d misemeals.com -d www.misemeals.com -d app.misemeals.com -d admin.misemeals.com -d mail.misemeals.com
```

Choose to redirect HTTP to HTTPS. Certbot renews automatically. The apps set secure cookies, so they only work over HTTPS.

## 9. Check

1. `https://misemeals.com`: the site loads, with "Chat with us" bottom-right.
2. `https://admin.misemeals.com`: sign in with the owner account from step 6.
3. `https://app.misemeals.com/register`: create a customer account; open `https://mail.misemeals.com` (the `MAILPIT_`
   login), click the confirmation link, sign in, subscribe to a plan and pay on the test page.
4. Send a chat message from the site; reply from the admin's **Support chat**; the reply appears in the chat at once.

## What the owner can review

The full customer journey (catalogue → plan → sign-up → test payment → choosing meals → dashboard), Mise Chat and the help
centre, and the admin: support inbox, help centre editor, payment providers, subscriptions, orders, kitchen, procurement,
inventory, delivery, analytics, the Launch page. Payments, prices, menus and policies are demo or placeholder content until
the business provides the real ones (`docs/launch/GO_LIVE_CHECKLIST.md`).

## Day to day

```sh
cd /var/www/mise
# Update to the latest code
git pull && export RELEASE_COMMIT=$(git rev-parse --short HEAD)
mise build api && mise build marketing && mise build web && mise build admin && mise up -d
# Status and logs
mise ps
mise logs -f --tail=100 api          # or worker, marketing, web, admin
# Restart everything / stop everything
mise restart
mise stop
# Free disk space taken by old images and build cache
docker image prune -f && docker builder prune -f
```

Migrations run automatically before the API starts after each update. The containers restart on their own after a reboot.

**Backup** (the review data lives in the `mise_mise-pgdata` Docker volume). A nightly dump kept for 7 days:

```sh
mkdir -p ~/mise-backups
( crontab -l 2>/dev/null; echo '30 3 * * * docker exec mise-postgres-1 pg_dump -U mise -Fc mise > $HOME/mise-backups/mise-$(date +\%a).dump' ) | crontab -
```

Restore one: `docker exec -i mise-postgres-1 pg_restore -U mise -d mise --clean < ~/mise-backups/mise-Mon.dump`.

## If something goes wrong

| Symptom | Check |
|---|---|
| `migrate` exited with an error | `mise logs migrate`: usually a typo in `.env.production` (a value still `CHANGE_ME`). |
| A site shows "502 Bad Gateway" | `mise ps`: is that app running? `mise logs <app>`. Apache is fine; the app is not answering on 127.0.0.1. |
| Build stops with "killed" / out of memory | Swap (step 1); build one app at a time; stop the stack (`mise stop`) while building. |
| Sign-in works, then you are signed out at once | The site is being opened over `http://`. Use `https://` (step 8). |
| No email | Open `https://mail.<domain>`; every email is there. `mise logs worker` shows sending errors. |
| Chat replies only appear after a refresh | Apache is buffering: check the `flushpackets=on` and `no-gzip` lines survived in the HTTPS copy certbot made (`*-le-ssl.conf`). |

## Removing it

```sh
mise down -v                  # stops Mise and DELETES its database volume
sudo a2dissite mise && sudo systemctl reload apache2     # Ubuntu (Amazon Linux: sudo rm /etc/httpd/conf.d/mise*.conf && sudo systemctl reload httpd)
sudo rm -rf /var/www/mise
```
