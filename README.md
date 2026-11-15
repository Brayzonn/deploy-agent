# Deploy Agent

A Go binary that deploys projects to a VPS on every GitHub push. It clones, builds, configures nginx and SSL, runs health checks, and rolls back on failure.

The agent does not listen for webhooks. A webhook server on the same machine receives the push, verifies the signature, and runs the binary with the push details in `GITHUB_*` environment variables. See [Webhook Server](#webhook-server).

- [Features](#features)
- [How It Works](#how-it-works)
- [Requirements](#requirements)
- [Webhook Server](#webhook-server)
- [Install](#install)
- [Configuration](#configuration)
- [nginx and SSL](#nginx-and-ssl)
- [Health Checks and Rollback](#health-checks-and-rollback)
- [Slack Notifications](#slack-notifications)
- [Logs](#logs)
- [Manual Run](#manual-run)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Features

- Deploys the exact pushed commit when the push is to the deploy branch
- Supports static frontends (`CLIENT`), Node/TypeScript backends on PM2 (`API_JS`, `API_TS`), Docker Compose apps (`DOCKER`), and fullstack repos
- Generates an nginx config per site and regenerates it when settings change
- Gets SSL certificates from Let's Encrypt through certbot
- Rolls back failed deploys after a health check
- Sends Slack messages when a deploy starts and when it fails
- Queues concurrent pushes to the same repo
- Reads repo settings from a JSON file, so adding a project needs no rebuild
- Puts a time limit on every command

## How It Works

```
GitHub push → webhook server → deploy-agent
                                   ↓
              lock → sync to commit → build → deploy → nginx/SSL → health check
                                                                      ↓ on failure
                                                                   rollback
```

- **Static site**: runs `npm run build`, keeps the last 5 builds as backups, and swaps the new build into the web root.
- **PM2 backend**: installs, builds, and restarts through PM2. If the health check fails, it resets to the previous commit and redeploys.
- **Docker**: tags the running images as `:previous`, builds, runs migrations if configured, and swaps the containers. If the health check fails, it restores the previous images.
- **Docker + client**: deploys the API with Docker and the frontend as a static site. With `spa_meta_proxy_path` set, the frontend deploys first and the API container is recreated so it reads the new build.
- **Fullstack**: deploys the backend on `api.<domain>` and the frontend on `<domain>`.

## Requirements

- Linux VPS with a non-root user that has sudo
- Go 1.25+ to build the binary
- `git`, `nginx` 1.19.4+, `certbot` with `python3-certbot-nginx`, Node.js 18+ with `npm`
- `pm2` for PM2 backends
- `docker` with the compose plugin for Docker apps, with the deploy user in the `docker` group
- A webhook server (see below)

The deploy user must be able to write to `/var/www/html`:

```bash
sudo chown youruser /var/www/html
```

nginx and certbot need passwordless sudo. Run `sudo visudo -f /etc/sudoers.d/deploy-agent` and add:

```
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
youruser ALL=(ALL) NOPASSWD: /usr/bin/certbot*
```

Check it with `sudo nginx -t`. It should run without asking for a password.

## Webhook Server

The webhook server must verify the GitHub signature and run the binary with these variables set:

```
GITHUB_REPO_NAME        repo name
GITHUB_REPO_FULL_NAME   owner/repo
GITHUB_REPO_OWNER       owner
GITHUB_BRANCH           pushed branch
GITHUB_COMMIT           full 40-character commit hash
GITHUB_PUSHER           pusher username
```

It must also pass its own environment through, so `SSL_EMAIL` and `SLACK_WEBHOOK_URL` reach the agent. In Node:

```js
exec(script, { env: { ...process.env, GITHUB_REPO_NAME: name /* ... */ } });
```

A small Express app with signature verification is enough. The agent checks every value at startup and rejects short commit hashes and names that contain path separators.

## Install

```bash
git clone https://github.com/Brayzonn/deploy-agent
cd deploy-agent
GOOS=linux GOARCH=amd64 go build -o deploy-agent
scp deploy-agent user@server:/home/youruser/scripts/deploy-agent
```

Add these to the webhook server's `.env`:

```bash
DEPLOYMENT_SCRIPT=/home/youruser/scripts/deploy-agent
SSL_EMAIL=you@example.com                    # Let's Encrypt notices
SLACK_WEBHOOK_URL=https://hooks.slack.com/…  # optional
```

Rebuild only when the Go code changes. Changes to the repo config apply on the next deploy.

## Configuration

Repos are listed in `/etc/deploy-agent/repos.json`. Set `DEPLOY_AGENT_REPOS_FILE` to use a different path. Only repos in this file can deploy.

```bash
sudo mkdir -p /etc/deploy-agent
sudo cp repos.example.json /etc/deploy-agent/repos.json
```

Unknown fields are rejected. `repo_dir` must be an absolute path and web roots must be under `/var/www/html/`.

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

**Docker API + static frontend:**

```json
"your-app": {
  "repo_dir": "/home/youruser/your-app",
  "project_type": "DOCKER",
  "use_docker": true,
  "docker_compose_file": "docker-compose.prod.yml",
  "server_dir": "server",
  "client_dir": "client",
  "domain": "api.yourdomain.com",
  "port": 4100,
  "health_check_url": "http://localhost:4100/health",
  "client_domain": "yourdomain.com",
  "client_domain_aliases": ["www.yourdomain.com"],
  "client_web_root": "/var/www/html/your-app"
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

This deploys the backend on `api.myapp.com` and the frontend on `myapp.com`. Set `server_domain` to change the backend domain.

### Fields

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `name` | string | Repo name. Defaults to the JSON key | No |
| `deploy_branch` | string | Branch that triggers deploys. Default `main` | No |
| `repo_dir` | string | Absolute path to the clone | Yes |
| `project_type` | string | `CLIENT`, `API_JS`, `API_TS`, or `DOCKER` | Yes |
| `use_docker` | bool | Set to `true` for Docker apps | Docker |
| `docker_compose_file` | string | Compose file name | Docker |
| `docker_env_file` | string | Env file for Compose. Default `.env.production` | No |
| `requires_migrations` | bool | Run migrations before swapping containers | No |
| `migration_command` | string | Migration command | With migrations |
| `migration_service` | string | Compose service that runs migrations. Default `api` | No |
| `infra_services` | []string | Compose services started before migrations | With migrations |
| `web_root` | string | Where the built site is served from | Static sites |
| `server_dir` | string | Directory with the server code | PM2 |
| `server_entry` | string | PM2 entry point when there is no ecosystem file | PM2 |
| `pm2_ecosystem` | string | PM2 ecosystem file | No |
| `domain` | string | Main domain | No |
| `domain_aliases` | []string | Extra domains on the same site and certificate | No |
| `server_domain` | string | Fullstack backend domain. Default `api.<domain>` | No |
| `port` | int | Port the app listens on | Backends |
| `nginx_max_upload_mb` | int | Upload size limit for the backend. nginx defaults to 1 MB | No |
| `health_check_url` | string | URL checked after deploy until it returns 200 | No |
| `health_check_timeout` | int | Seconds to wait for the health check. Default 30 | No |
| `full_stack` | bool | Deploy backend and frontend from one repo | No |
| `client_dir` | string | Frontend directory | Fullstack, Docker + client |
| `client_domain` | string | Frontend domain | Docker + client |
| `client_domain_aliases` | []string | Extra frontend domains | No |
| `client_web_root` | string | Where the frontend is served from | Docker + client |
| `spa_meta_proxy_path` | string | Path on `localhost:<port>` that renders frontend pages with server-side meta tags. Falls back to `index.html` if the app is down. Needs `port` | No |

### Env files

Gitignored env files such as `.env.production` can be kept at `/etc/deploy-agent/env/<repo>/`, using the same path they have inside the repo. Set `DEPLOY_AGENT_ENV_DIR` to use a different folder.

```bash
sudo mkdir -p /etc/deploy-agent/env/your-docker-app
sudo cp .env.production /etc/deploy-agent/env/your-docker-app/.env.production
sudo chmod -R go-rwx /etc/deploy-agent/env
```

Each deploy copies these files into the checkout with mode 600. A missing env file does not fail the deploy.

## nginx and SSL

- Configs are written to `/etc/nginx/sites-available/<domain>`. Static sites are served as files and everything else is a reverse proxy. Both IPv4 and IPv6 are enabled.
- When the template or the repo settings change, the next deploy regenerates the config. The old config is backed up, with 5 kept per domain. Hand-written configs are replaced the same way and their custom changes are lost.
- certbot adds HTTPS and redirects HTTP to it. A new alias is added to the existing certificate.
- If certbot fails after a regeneration, the previous config is restored so the site stays on HTTPS.
- A `default-tls` site refuses HTTPS requests for domains that have no config, so they never get another site's certificate.
- Certificates are renewed by certbot's own timer. Check it with `systemctl list-timers | grep certbot`.

## Health Checks and Rollback

| Type | Check | On failure |
| --- | --- | --- |
| PM2 | Process is online and the URL returns 200 | Reset to the previous commit, rebuild, restart |
| Docker | Containers are healthy and the URL returns 200 | Restore the `:previous` images |
| Static site | None | The old site keeps serving until the swap succeeds |

A first deploy has nothing to roll back to, so the agent stops and logs that manual intervention is required. Rollback restores containers but not the database, so migrations should be backward-compatible.

## Slack Notifications

Set `SLACK_WEBHOOK_URL` to get a message when a deploy starts and an alert with the error when it fails. If the URL is not set, the deploy log shows a warning. Slack errors never fail a deploy.

## Logs

Each deploy writes `~/logs/deployments/deployment_YYYYMMDD_HHMMSS_PID.log`. Output also goes to the webhook server's stdout.

```bash
tail -f $(ls -t ~/logs/deployments/deployment_*.log | head -1)   # follow the latest
grep ERROR ~/logs/deployments/*.log                              # find failures
```

## Manual Run

To deploy without a push, set the variables yourself. The commit must be a full 40-character hash on the branch.

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

- **Permission denied**: sudoers is not set up, or the deploy user cannot write to `/var/www/html`.
- **"repos file does not exist"**: create `/etc/deploy-agent/repos.json`.
- **Repo not deploying**: check that it is in `repos.json`, that the push went to its `deploy_branch`, and that `ssh -T git@github.com` works.
- **"timed out after X"**: the limits are 10 minutes for npm install, 15 for builds, and 30 for Docker builds. The error names the step.
- **`ERR_SSL_UNRECOGNIZED_NAME_ALERT` in the browser**: the domain has no nginx config. Add it to `domain_aliases` or `client_domain_aliases` and redeploy.
- **certbot fails**: check DNS with `dig yourdomain.com`, check that ports 80 and 443 are open, and read `/var/log/letsencrypt/letsencrypt.log`. Let's Encrypt allows 5 certificates per week for the same set of domains.
- **Health check fails but the app works**: raise `health_check_timeout` and test `health_check_url` with `curl` on the server.
- **No Slack messages**: the deploy log says why. "DISABLED" means `SLACK_WEBHOOK_URL` did not reach the agent.

## License

MIT
