#!/usr/bin/env bash
# Runs Mise directly on an Ubuntu server, without Docker.
#
#   ./infrastructure/native/mise.sh setup     install Node.js, pnpm and Go if missing
#   ./infrastructure/native/mise.sh build     compile the API and build both sites
#   ./infrastructure/native/mise.sh start     install systemd services, migrate, start everything
#   ./infrastructure/native/mise.sh https     serve the domain over HTTPS (adds Mise to nginx or Apache if one
#                                             already runs here; otherwise installs Caddy)
#   ./infrastructure/native/mise.sh admin EMAIL "NAME"   create an admin (asks for the password)
#   ./infrastructure/native/mise.sh update    git pull, rebuild, restart
#   ./infrastructure/native/mise.sh status    show the four services
#   ./infrastructure/native/mise.sh logs [api|worker|marketing|admin]
#   ./infrastructure/native/mise.sh clean     delete build caches to free disk space
#
# Settings are read from .env.production in the repository root.
# Guide: docs/architecture/deploy-single-server.md
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="$ROOT/.env.production"
RUN_USER="${SUDO_USER:-$(id -un)}"
NODE_MAJOR=22
PNPM_VERSION=11.9.0
SERVICES=(mise-api mise-worker mise-marketing mise-admin)
export PATH="/usr/local/go/bin:$PATH"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
die() { printf 'mise.sh: %s\n' "$*" >&2; exit 1; }

# Reads KEY=value lines literally. The file is not sourced, because values such
# as "Mise <hello@example.com>" are not valid shell.
setting() { grep -E "^$1=" "$ENV_FILE" | tail -1 | cut -d= -f2-; }

load_env() {
  [ -f "$ENV_FILE" ] || die "$ENV_FILE not found. Copy .env.production.example to .env.production and fill it in."
  local line
  while IFS= read -r line; do
    if [[ "$line" =~ ^[A-Z0-9_]+= ]]; then export "$line"; fi
  done < "$ENV_FILE"
  DOMAIN="$(setting DOMAIN)"
  [ -n "$DOMAIN" ] && [ "$DOMAIN" != "example.com" ] || die "Set DOMAIN in .env.production."
  # Fixed for this layout: the API listens on this machine only, behind Caddy.
  export MISE_ENV=production HTTP_ADDR=127.0.0.1:8090 TRUSTED_PROXY_HOPS=1 RATE_LIMITS_DISABLED=false
  export PUBLIC_SITE_URL="https://$DOMAIN" CORS_ORIGINS="https://$DOMAIN"
}

cmd_setup() {
  say "Checking tools"
  sudo apt-get update -qq
  sudo apt-get install -y -qq curl git ca-certificates build-essential >/dev/null

  if ! command -v node >/dev/null || [ "$(node -p 'process.versions.node.split(".")[0]')" -lt "$NODE_MAJOR" ]; then
    say "Installing Node.js $NODE_MAJOR"
    curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | sudo -E bash - >/dev/null
    sudo apt-get install -y -qq nodejs >/dev/null
  fi
  command -v pnpm >/dev/null || { say "Installing pnpm"; sudo npm install --global "pnpm@$PNPM_VERSION" >/dev/null; }

  local want have arch
  want="$(grep -E '^go ' "$ROOT/services/api/go.mod" | awk '{print $2}')"
  have="$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//' || true)"
  if [ "$have" != "$want" ]; then
    say "Installing Go $want"
    case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; *) die "unsupported CPU $(uname -m)" ;; esac
    curl -fsSL "https://go.dev/dl/go${want}.linux-${arch}.tar.gz" -o /tmp/go.tgz
    sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
  fi
  echo "node $(node --version), pnpm $(pnpm --version), $(go version | awk '{print $3}')"
}

cmd_build() {
  load_env
  say "Building the API, worker and misectl"
  mkdir -p "$ROOT/bin"
  (cd "$ROOT/services/api" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$ROOT/bin/" ./cmd/...)

  say "Installing JavaScript dependencies"
  (cd "$ROOT" && pnpm install --frozen-lockfile --filter "@mise/marketing..." --filter "@mise/admin...")

  say "Building the site"
  # The browser calls /api/v1 on the site's own address; Caddy sends those
  # requests to the API (see the https command).
  (cd "$ROOT" && NEXT_TELEMETRY_DISABLED=1 NEXT_PUBLIC_SITE_URL="https://$DOMAIN" NEXT_PUBLIC_API_URL="" \
    pnpm --filter @mise/marketing build)
  say "Building the admin"
  (cd "$ROOT" && NEXT_TELEMETRY_DISABLED=1 pnpm --filter @mise/admin build)

  # A standalone Next.js server expects its static files beside it.
  local app out
  for app in marketing admin; do
    out="$ROOT/apps/$app/.next/standalone/apps/$app"
    rm -rf "$out/.next/static" "$out/public"
    cp -r "$ROOT/apps/$app/.next/static" "$out/.next/static"
    cp -r "$ROOT/apps/$app/public" "$out/public"
  done
  echo "Build finished."
}

unit() { # name, description, working dir, command, extra Environment= lines
  sudo tee "/etc/systemd/system/$1.service" >/dev/null <<UNIT
[Unit]
Description=$2
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
User=$RUN_USER
WorkingDirectory=$3
EnvironmentFile=$ENV_FILE
$5
ExecStart=$4
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
}

cmd_start() {
  load_env
  [ -x "$ROOT/bin/api" ] || die "Nothing is built yet. Run: $0 build"
  local go_env node
  go_env="Environment=MISE_ENV=production HTTP_ADDR=127.0.0.1:8090 TRUSTED_PROXY_HOPS=1 RATE_LIMITS_DISABLED=false PUBLIC_SITE_URL=https://$DOMAIN CORS_ORIGINS=https://$DOMAIN"
  node="$(command -v node)"

  say "Applying database migrations"
  "$ROOT/bin/misectl" migrate up

  say "Installing services"
  unit mise-api "Mise API" "$ROOT" "$ROOT/bin/api" "$go_env"
  unit mise-worker "Mise worker (email)" "$ROOT" "$ROOT/bin/worker" "$go_env"
  unit mise-marketing "Mise site" "$ROOT/apps/marketing/.next/standalone/apps/marketing" "$node server.js" \
    "Environment=NODE_ENV=production PORT=3100 HOSTNAME=127.0.0.1 API_INTERNAL_URL=http://127.0.0.1:8090"
  unit mise-admin "Mise admin" "$ROOT/apps/admin/.next/standalone/apps/admin" "$node server.js" \
    "Environment=NODE_ENV=production PORT=3101 HOSTNAME=127.0.0.1 API_INTERNAL_URL=http://127.0.0.1:8090 ADMIN_COOKIE_SECURE=true"
  sudo systemctl daemon-reload
  sudo systemctl enable --quiet "${SERVICES[@]}"
  sudo systemctl restart "${SERVICES[@]}"
  sleep 4
  cmd_status
}

# Requests the certificates and turns on the HTTP -> HTTPS redirect, using the
# certbot plugin for the web server already on this machine.
issue_certificates() { # nginx|apache
  if ! command -v certbot >/dev/null || ! certbot plugins 2>/dev/null | grep -qi "$1"; then
    say "Installing certbot"
    sudo apt-get install -y -qq certbot "python3-certbot-$1" >/dev/null
  fi
  say "Requesting HTTPS certificates"
  sudo certbot "--$1" --redirect -d "$DOMAIN" -d "www.$DOMAIN" -d "admin.$DOMAIN" ||
    die "certbot did not finish. The site is reachable over http:// for now. Check that DNS for $DOMAIN, www.$DOMAIN and admin.$DOMAIN points at this server and ports 80/443 are open, then run: $0 https"
  echo "Serving https://$DOMAIN and https://admin.$DOMAIN (sign in at https://admin.$DOMAIN/login)."
}

# Adds Mise to an nginx that already serves other sites. Only this one file is
# written; it is removed again if nginx rejects it, so other sites are unaffected.
https_nginx() {
  local conf=/etc/nginx/sites-available/mise.conf link=/etc/nginx/sites-enabled/mise.conf
  [ -d /etc/nginx/sites-available ] || { conf=/etc/nginx/conf.d/mise.conf; link=""; }
  say "Adding $DOMAIN to nginx"
  # If certbot has already added HTTPS to this file, leave it alone.
  if [ -f "$conf" ] && grep -q "managed by Certbot" "$conf"; then
    echo "$conf already has HTTPS configured; not rewriting it."
  else
    sudo tee "$conf" >/dev/null <<NGINX
# Mise. Written by infrastructure/native/mise.sh; certbot adds the HTTPS parts.
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN www.$DOMAIN;

    # Only the endpoints the public site uses reach the API. The admin API is
    # not exposed on the public domain.
    location ~ ^/api/v1/(leads(/.*)?|referrals/.*|events|health|ready)\$ {
        proxy_pass http://127.0.0.1:8090;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location / {
        proxy_pass http://127.0.0.1:3100;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-Host \$host;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}

server {
    listen 80;
    listen [::]:80;
    server_name admin.$DOMAIN;

    location / {
        proxy_pass http://127.0.0.1:3101;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-Host \$host;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
NGINX
    [ -z "$link" ] || sudo ln -sf "$conf" "$link"
    if ! sudo nginx -t 2>/tmp/mise-nginx-test.log; then
      cat /tmp/mise-nginx-test.log >&2
      sudo rm -f "$conf" ${link:+"$link"}
      die "nginx rejected the new config, so it was removed again. Nothing else was changed."
    fi
    sudo systemctl reload nginx
  fi
  issue_certificates nginx
}

# The same for Apache.
https_apache() {
  local conf=/etc/apache2/sites-available/mise.conf
  say "Adding $DOMAIN to Apache"
  if [ -f /etc/apache2/sites-available/mise-le-ssl.conf ]; then
    echo "HTTPS is already configured for Mise in Apache; not rewriting it."
  else
    sudo a2enmod -q proxy proxy_http headers rewrite >/dev/null
    sudo tee "$conf" >/dev/null <<APACHE
# Mise. Written by infrastructure/native/mise.sh; certbot adds the HTTPS copy.
<VirtualHost *:80>
    ServerName $DOMAIN
    ServerAlias www.$DOMAIN
    ProxyPreserveHost On
    ProxyRequests Off
    # Only the endpoints the public site uses reach the API. The admin API is
    # not exposed on the public domain.
    ProxyPassMatch "^(/api/v1/(?:leads(?:/.*)?|referrals/.*|events|health|ready))\$" "http://127.0.0.1:8090\$1"
    ProxyPass / http://127.0.0.1:3100/
    ProxyPassReverse / http://127.0.0.1:3100/
</VirtualHost>

<VirtualHost *:80>
    ServerName admin.$DOMAIN
    ProxyPreserveHost On
    ProxyRequests Off
    ProxyPass / http://127.0.0.1:3101/
    ProxyPassReverse / http://127.0.0.1:3101/
</VirtualHost>
APACHE
    sudo a2ensite -q mise >/dev/null
    if ! sudo apache2ctl configtest 2>/tmp/mise-apache-test.log; then
      cat /tmp/mise-apache-test.log >&2
      sudo a2dissite -q mise >/dev/null || true
      sudo rm -f "$conf"
      die "Apache rejected the new config, so it was removed again. Nothing else was changed."
    fi
    sudo systemctl reload apache2
  fi
  issue_certificates apache
}

cmd_https() {
  load_env
  # A web server that already serves other sites gets Mise added to it.
  if systemctl is-active --quiet nginx; then https_nginx; return; fi
  if systemctl is-active --quiet apache2; then https_apache; return; fi

  if ! command -v caddy >/dev/null; then
    local busy
    busy="$(sudo ss -ltnp 2>/dev/null | grep -E ':(80|443)\b' || true)"
    [ -z "$busy" ] || die "Ports 80/443 are already in use, so Caddy cannot be installed alongside:
$busy
Point that web server at 127.0.0.1:3100 (site), 127.0.0.1:3101 (admin) and 127.0.0.1:8090 (/api/v1) instead."
    say "Installing Caddy"
    sudo apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https >/dev/null
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
    sudo apt-get update -qq && sudo apt-get install -y -qq caddy >/dev/null
  fi

  say "Configuring HTTPS for $DOMAIN"
  # Only the endpoints the public site uses reach the API. The admin API is not
  # exposed on the public domain; the admin app calls it from this machine.
  sudo tee /etc/caddy/Caddyfile >/dev/null <<CADDY
$DOMAIN {
	encode zstd gzip
	@api path /api/v1/leads /api/v1/leads/* /api/v1/referrals/* /api/v1/events /api/v1/health /api/v1/ready
	handle @api {
		reverse_proxy 127.0.0.1:8090
	}
	handle {
		reverse_proxy 127.0.0.1:3100
	}
}

www.$DOMAIN {
	redir https://$DOMAIN{uri} permanent
}

admin.$DOMAIN {
	encode zstd gzip
	reverse_proxy 127.0.0.1:3101
}
CADDY
  sudo systemctl enable --quiet caddy
  sudo systemctl reload caddy 2>/dev/null || sudo systemctl restart caddy
  echo "Caddy is serving https://$DOMAIN and https://admin.$DOMAIN."
  echo "Certificates are issued once DNS for those names points at this server and ports 80 and 443 are open."
}

cmd_admin() {
  load_env
  [ $# -ge 2 ] || die 'usage: mise.sh admin EMAIL "NAME" [ROLE]'
  local password confirm
  read -r -s -p "Password for $1 (at least 12 characters): " password; echo
  read -r -s -p "Again: " confirm; echo
  [ "$password" = "$confirm" ] || die "The passwords do not match."
  MISE_ADMIN_PASSWORD="$password" "$ROOT/bin/misectl" admin create -email "$1" -name "$2" -role "${3:-super_admin}"
}

cmd_update() {
  say "Fetching the latest code"
  git -C "$ROOT" pull --ff-only
  cmd_build
  cmd_start
}

cmd_status() {
  local s
  for s in "${SERVICES[@]}"; do printf '%-16s %s\n' "$s" "$(systemctl is-active "$s" 2>/dev/null || true)"; done
  curl -fsS --max-time 5 http://127.0.0.1:8090/api/v1/ready >/dev/null 2>&1 && echo "API: ready (database reachable)" || echo "API: NOT ready - run: $0 logs api"
  curl -fsS --max-time 5 -o /dev/null http://127.0.0.1:3100/robots.txt 2>/dev/null && echo "Site: responding" || echo "Site: NOT responding - run: $0 logs marketing"
  curl -fsS --max-time 5 -o /dev/null http://127.0.0.1:3101/login 2>/dev/null && echo "Admin: responding" || echo "Admin: NOT responding - run: $0 logs admin"
}

cmd_logs() { sudo journalctl -u "mise-${1:-api}" -n 60 --no-pager; }

cmd_clean() {
  say "Removing build caches"
  go clean -cache -modcache 2>/dev/null || true
  (cd "$ROOT" && pnpm store prune >/dev/null 2>&1 || true)
  rm -rf "$ROOT/apps/marketing/.next/cache" "$ROOT/apps/admin/.next/cache"
  df -h "$ROOT" | tail -1
}

case "${1:-}" in
  setup|build|start|https|update|status|clean) "cmd_$1" ;;
  admin) shift; cmd_admin "$@" ;;
  logs) shift; cmd_logs "$@" ;;
  *) sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
