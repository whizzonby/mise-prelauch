# Deploying on a single server

The low-cost way to run Mise: one server you already have, the code cloned into a
directory, a database of its own on your existing PostgreSQL, and the domain pointed at
the server with Route 53. The larger AWS layout in `deployment.md` is for later.

What runs on the server, all in Docker: the API, the email worker, the marketing site,
the admin, a one-off migration job, and (optionally) Caddy for HTTPS.

## 1. What the server needs

- Linux with Docker Engine and the Compose plugin (`docker compose version` works).
- At least 2 GB of RAM free for Mise (building the two web apps is the heaviest step).
- Ports 80 and 443 open to the internet, or an existing web server on them.
- Outbound internet (to download images and send email).

## 2. Create the database

On your PostgreSQL server, as an admin user:

```sql
CREATE USER mise WITH PASSWORD 'a-long-random-password';
CREATE DATABASE mise OWNER mise;
```

Mise only touches this database. Its tables are created by the migration job in step 5.

If PostgreSQL runs on the same server as Docker, containers reach it at
`host.docker.internal`. PostgreSQL must listen on an address the Docker network can
reach (`listen_addresses` in `postgresql.conf`) and allow the `mise` user from the
Docker subnet (a line in `pg_hba.conf`, for example
`host mise mise 172.16.0.0/12 scram-sha-256`), then reload PostgreSQL.

## 3. Point the domain at the server (Route 53)

In the hosted zone for your domain, create three records pointing at the server's public IP:

| Name | Type | Value |
|---|---|---|
| `example.com` | A | server IP |
| `www.example.com` | A | server IP |
| `admin.example.com` | A | server IP |

Use the server's Elastic IP (or static IP) so the address does not change on reboot.

## 4. Get the code and configure it

```sh
sudo mkdir -p /opt/mise && sudo chown "$USER" /opt/mise
git clone <repository-url> /opt/mise
cd /opt/mise
cp .env.production.example .env.production
nano .env.production
```

Fill in `DOMAIN`, `DATABASE_URL`, a fresh `TOKEN_SIGNING_KEY` (`openssl rand -base64 48`),
and the email settings. `.env.production` stays on the server; it is never committed.

## 5. Start it

If nothing else on the server uses ports 80 and 443, let Caddy handle HTTPS:

```sh
docker compose -f docker-compose.prod.yml --env-file .env.production --profile caddy up -d --build
```

If the server already runs nginx for other sites, leave Caddy out:

```sh
docker compose -f docker-compose.prod.yml --env-file .env.production up -d --build
```

and add two sites to nginx, with HTTPS from your usual certificate setup (for example certbot):

```nginx
server {
    server_name example.com www.example.com;
    location / {
        proxy_pass http://127.0.0.1:3100;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    server_name admin.example.com;
    location / {
        proxy_pass http://127.0.0.1:3101;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

The first build takes several minutes. Check everything is up:

```sh
docker compose -f docker-compose.prod.yml --env-file .env.production ps
docker compose -f docker-compose.prod.yml --env-file .env.production logs migrate
```

`migrate` should have exited with "schema is at version 2".

## 6. Create the first admin

```sh
docker compose -f docker-compose.prod.yml --env-file .env.production run --rm \
  -e MISE_ADMIN_PASSWORD='a-strong-password-of-12-or-more' \
  migrate /app/misectl admin create -email you@example.com -name "Your Name" -role super_admin
```

Then sign in at `https://admin.<domain>`.

## Updating

```sh
cd /opt/mise
git pull
docker compose -f docker-compose.prod.yml --env-file .env.production up -d --build
```

(Add `--profile caddy` if you use it.) Migrations run automatically before the API starts.

## Backups

The data lives in your PostgreSQL server, so it is covered by whatever backs that up. If
nothing does yet, at minimum run a nightly `pg_dump` of the `mise` database to storage
off the server.

## Cost

Nothing beyond the server and database you already pay for, plus Route 53 (about US$0.50
a month per hosted zone) and email sending if you use a paid provider.
