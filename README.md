# Deploy Agent

A Go binary that deploys your projects to a VPS on every GitHub push: clone, build, nginx, SSL, health check, rollback.

**It does not listen for webhooks itself.** A webhook server on the same machine is required — it receives the GitHub push, verifies the signature, and executes this binary once per push with the push metadata in `GITHUB_*` environment variables (see [Webhook Server](#webhook-server)).

## Features

- **Zero-config pushes** — push to the deploy branch, site goes live; everything else is ignored
- **Four project types** — static frontends (`CLIENT`), Node/TypeScript backends via PM2 (`API_JS`/`API_TS`), Docker Compose apps (`DOCKER`), and fullstack (frontend + backend from one repo)
- **nginx managed for you** — configs generated per site (dual-stack IPv4/IPv6), regenerated automatically when the template or repo settings change, plus a default TLS catch-all so unmatched domains get a refused handshake instead of another site's certificate
- **SSL via Let's Encrypt** — certbot requests and renews certificates; a failed certbot run on a regenerated config restores the previous working config so a live site is never left HTTPS-down
- **Health checks with rollback** — failed deploys roll back to the previous commit (PM2) or previous images (Docker) and verify the old version actually serves
- **Slack notifications** — a "deployment started" message on every deploy (doubling as a health check of the alert channel) and a failure alert with the error when a deploy fails
- **Safe concurrency** — per-repo locks queue concurrent pushes; a global edge lock serializes all nginx/certbot work across repos
- **Config as data** — repos live in a JSON file on the server; add or change projects without rebuilding the binary
- **Deterministic and bounded** — deploys exactly the pushed commit; every command (git, npm, docker, certbot) has a hard timeout

## How It Works

```
GitHub push → webhook server → deploy-agent
                                   ↓
              lock → sync to commit → build → deploy → nginx/SSL → health check
                                                                      ↓ on failure
                                                                   rollback
```

Per project type:

- **Static site** — `npm run build`, back up the current site (last 5 kept), atomically swap the new build into the web root; a failed deploy never replaces the serving site
- **PM2 backend** — `npm install`/`build`, restart via PM2; failed health check resets git to the previous commit, rebuilds, restarts, and re-verifies
- **Docker** — tag running images as `:previous`, build (old containers keep serving), run migrations if configured, swap containers; failed container-state or HTTP health check restores the previous images
- **Fullstack** — backend on `api.<domain>` (configurable), then frontend on `<domain>`

## Requirements

- Linux VPS, non-root user with sudo
- `git`, `nginx`, `certbot` (`python3-certbot-nginx`), Node.js 18+ with `npm`
- `pm2` for PM2 deployments; `docker` + compose plugin for Docker deployments (user in the `docker` group)
- A webhook server (below)

Passwordless sudo for the commands the agent runs — `sudo visudo -f /etc/sudoers.d/deploy-agent`:

```
youruser ALL=(ALL) NOPASSWD: /usr/bin/mkdir -p /var/www/html/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/tee /etc/nginx/sites-available/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/cat /etc/nginx/sites-available/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/cp /etc/nginx/sites-available/* *
youruser ALL=(ALL) NOPASSWD: /usr/bin/cp * /etc/nginx/sites-available/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/ln -s /etc/nginx/sites-available/* /etc/nginx/sites-enabled/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/rm -f /etc/nginx/sites-available/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/rm -f /etc/nginx/sites-enabled/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
youruser ALL=(ALL) NOPASSWD: /usr/sbin/nginx -t
youruser ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload nginx
youruser ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart nginx
youruser ALL=(ALL) NOPASSWD: /usr/bin/certbot*
```

The `cat`/`cp`/`rm` entries are used by config regeneration (read, backup/restore, cleanup) — older installs that predate it need these added.

Verify with `sudo nginx -t` — it must not ask for a password.

## Webhook Server

The agent is a one-shot process: the webhook server runs it once per push and must

1. verify the GitHub webhook signature,
2. execute the binary with these variables set:

```
GITHUB_REPO_NAME        repo name
GITHUB_REPO_FULL_NAME   owner/repo
GITHUB_REPO_OWNER       owner
GITHUB_BRANCH           pushed branch
GITHUB_COMMIT           full 40-character commit hash
GITHUB_PUSHER           pusher username
```

3. pass its own environment through (so `SSL_EMAIL`, `SLACK_WEBHOOK_URL`, etc. reach the agent — in Node: `exec(script, { env: { ...process.env, GITHUB_...: ... } })`).

Any webhook server that does this works; a small Express app with signature verification is enough. All values are validated at startup — short or fake commit hashes and names with path separators are rejected.

## Install

```bash
git clone https://github.com/Brayzonn/deploy-agent
cd deploy-agent
GOOS=linux GOARCH=amd64 go build -o deploy-agent
scp deploy-agent user@server:/home/youruser/scripts/deploy-agent
```

Point the webhook server at it (its `.env`):

```bash
DEPLOYMENT_SCRIPT=/home/youruser/scripts/deploy-agent
SSL_EMAIL=you@example.com                    # Let's Encrypt notifications
SLACK_WEBHOOK_URL=https://hooks.slack.com/…  # optional; notifications disabled (with a logged warning) when unset
```

Rebuild only when the Go code changes — repo config changes are picked up on the next deploy.

## Configuration

Repos live in `/etc/deploy-agent/repos.json` (override with `DEPLOY_AGENT_REPOS_FILE`). **The file is required**; only repos listed in it can deploy. Start from the template:

```bash
sudo mkdir -p /etc/deploy-agent
sudo cp repos.example.json /etc/deploy-agent/repos.json
```

Unknown field names are rejected at load time; repo dirs must be absolute and web roots must live under `/var/www/html/`.

**Static site:**

```json
"your-frontend": {
  "repo_dir": "/home/youruser/your-frontend",
  "web_root": "/var/www/html/your-frontend",
  "project_type": "CLIENT",
  "domain": "yourdomain.com",
  "domain_aliases": ["www.yourdomain.com"]
}
```

**PM2 backend:**

```json
"your-api": {
  "repo_dir": "/home/youruser/your-api",
  "project_type": "API_TS",
  "server_dir": ".",
  "server_entry": "dist/main.js",
  "pm2_ecosystem": "ecosystem.config.js",
  "domain": "api.yourdomain.com",
  "port": 3000
}
```

**Docker app:**

```json
"your-docker-app": {
  "repo_dir": "/home/youruser/your-docker-app",
  "project_type": "DOCKER",
  "use_docker": true,
  "docker_compose_file": "docker-compose.prod.yml",
  "docker_env_file": ".env.production",
  "requires_migrations": true,
  "migration_command": "npx prisma migrate deploy",
  "migration_service": "api",
  "infra_services": ["postgres", "redis"],
  "domain": "api.yourdomain.com",
  "port": 5932,
  "health_check_url": "http://localhost:5932/api/v1/health",
  "health_check_timeout": 30
}
```

**Fullstack (one repo, two sites):**

```json
"my-fullstack-app": {
  "repo_dir": "/home/youruser/my-fullstack-app",
  "project_type": "API_TS",
  "full_stack": true,
  "client_dir": "client",
  "server_dir": "server",
  "web_root": "/var/www/html/my-fullstack-app",
  "domain": "myapp.com",
  "port": 3000
}
```

Deploys the backend on `api.myapp.com` (override with `server_domain`) and the frontend on `myapp.com`.

### Fields

| Field                   | Type     | Description                                              | Required              |
| ----------------------- | -------- | -------------------------------------------------------- | --------------------- |
| `name`                  | string   | Repository name (defaults to the JSON key)               | Optional              |
| `deploy_branch`         | string   | Only pushes to this branch deploy (default `main`)       | Optional              |
| `repo_dir`              | string   | Absolute path where the repo is cloned                   | Yes                   |
| `project_type`          | string   | `CLIENT`, `API_JS`, `API_TS`, or `DOCKER`                | Yes                   |
| `use_docker`            | bool     | Enable Docker deployment                                 | For Docker            |
| `docker_compose_file`   | string   | Compose file name                                        | For Docker            |
| `docker_env_file`       | string   | Env file for Docker (default `.env.production`)          | Optional              |
| `requires_migrations`   | bool     | Run migrations before swapping containers                | Optional              |
| `migration_command`     | string   | Migration command                                        | If migrations enabled |
| `migration_service`     | string   | Compose service to run migrations in (default `api`)     | Optional              |
| `infra_services`        | []string | Compose services started before migrations               | If migrations enabled |
| `web_root`              | string   | Static file path (under `/var/www/html/`)                | For static sites      |
| `server_dir`            | string   | Directory containing server code                         | For PM2 backends      |
| `server_entry`          | string   | PM2 entry point (fallback when no ecosystem file)        | For PM2 backends      |
| `pm2_ecosystem`         | string   | PM2 ecosystem config file                                | Optional              |
| `domain`                | string   | Primary domain                                           | Optional              |
| `domain_aliases`        | []string | Additional domains (same config + certificate)           | Optional              |
| `server_domain`         | string   | Fullstack API domain (default `api.` + `domain`)         | Optional              |
| `port`                  | int      | Port the app listens on                                  | For backends          |
| `health_check_url`      | string   | URL polled after deploy until it returns 200             | Optional              |
| `health_check_timeout`  | int      | Health check deadline in seconds (default 30)            | Optional              |
| `full_stack`            | bool     | Deploy backend and frontend from one repo                | Optional              |
| `client_dir`            | string   | Frontend directory in fullstack/hybrid repos             | For fullstack         |
| `client_domain`         | string   | Frontend domain for Docker + client hybrid repos         | For hybrid            |
| `client_domain_aliases` | []string | Frontend domain aliases for hybrid repos                 | Optional              |
| `client_web_root`       | string   | Frontend web root for hybrid repos                       | For hybrid            |

### Env file overlays (optional)

Gitignored env files (`.env.production` etc.) can be staged outside the repo at `/etc/deploy-agent/env/<repo>/` (override with `DEPLOY_AGENT_ENV_DIR`), mirroring their path inside the repo:

```bash
sudo mkdir -p /etc/deploy-agent/env/your-docker-app
sudo cp .env.production /etc/deploy-agent/env/your-docker-app/.env.production
sudo chmod -R go-rwx /etc/deploy-agent/env
```

Every deploy copies overlay files into the checkout (mode 600), so fresh clones work and editing a master copy rolls out on the next deploy. Repos without an overlay are untouched; an absent env file never fails a deploy (an app that needs one fails its own startup, which the health check turns into a rollback).

## nginx and SSL

- Configs are generated into `/etc/nginx/sites-available/<domain>` — static file server for `CLIENT`, reverse proxy for everything else, listening on both IPv4 and IPv6
- Each generated config carries a fingerprint of its body; when the template or the repo's settings change, the next deploy **regenerates** the config (backing up the old one), and certbot re-adds the SSL block. Manually created configs are adopted the same way, with a loud warning — manual customizations are not carried over
- If certbot fails after a regeneration (including Let's Encrypt rate limits), the pre-deploy config is restored so the site keeps serving HTTPS
- A `default-tls` catch-all is created once: TLS handshakes for domains no vhost matches are refused (`ssl_reject_handshake`) instead of being answered with another site's certificate
- Certificates renew via certbot's `--keep-until-expiring`; nothing manual

## Health Checks and Rollback

| Type        | Check                                                        | On failure                                                            |
| ----------- | ------------------------------------------------------------ | --------------------------------------------------------------------- |
| PM2         | PM2 process online + HTTP poll until 200                     | Reset to previous commit, rebuild, restart, re-verify                  |
| Docker      | Container state (incl. compose `healthcheck:`) + HTTP poll   | Restore `:previous` images, verify running image IDs match             |
| Static site | None needed                                                  | Atomic swap means the old site is still serving; backup restored if the swap itself broke |

First-ever deploys have nothing to roll back to: the agent stops with explicit "manual intervention required" messaging rather than redeploying failed code. Migrations run before the swap and should be backward-compatible — a rollback restores containers, not the database schema.

## Slack Notifications

With `SLACK_WEBHOOK_URL` set, every deploy posts a **"Deployment started"** message (repo, branch, commit, pusher, deployment ID) and failures post an alert with the error. The start message doubles as an end-to-end check of the alert channel: every deploy log records whether the post succeeded, and an unset URL logs a "notifications DISABLED" warning instead of staying silent. Notification problems never fail a deploy.

## Logs

Each deploy writes `~/logs/deployments/deployment_YYYYMMDD_HHMMSS_PID.log` (also colored console output via the webhook server's stdout).

```bash
tail -f $(ls -t ~/logs/deployments/deployment_*.log | head -1)   # follow latest
grep ERROR ~/logs/deployments/*.log                              # find failures
```

## Manual Run

Deploy without a push — the commit must be a real, full 40-character hash on the branch:

```bash
export GITHUB_REPO_NAME="your-repo"
export GITHUB_REPO_FULL_NAME="username/your-repo"
export GITHUB_REPO_OWNER="username"
export GITHUB_BRANCH="main"
export GITHUB_PUSHER="username"
export GITHUB_COMMIT="$(git ls-remote git@github.com:username/your-repo.git main | cut -f1)"
export SSL_EMAIL="you@example.com"
/home/youruser/scripts/deploy-agent
```

## Troubleshooting

- **"permission denied"** — sudoers not set up; `sudo nginx -t` must run without a password prompt
- **"repos file does not exist"** — install `/etc/deploy-agent/repos.json` (see Configuration)
- **Repo not deploying** — it must be listed in `repos.json`, and the push must be to its `deploy_branch`; check SSH access with `ssh -T git@github.com`
- **"timed out after X"** — every command has a hard deadline (npm install 10m, build 15m, docker build 30m); the error names the phase
- **Certbot failures** — check DNS (`dig yourdomain.com`), ports 80/443, and `/var/log/letsencrypt/letsencrypt.log`; Let's Encrypt allows 5 certificates per domain per week
- **Health check fails but the app works** — raise `health_check_timeout`, verify `health_check_url` responds locally with `curl`
- **No Slack messages** — the deploy log says why: "DISABLED" means the URL didn't reach the agent (check the webhook server's `.env` and that it spreads `process.env` into the child environment); an HTTP error is logged with Slack's response body

## License

MIT
