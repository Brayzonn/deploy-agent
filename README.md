# Deploy Agent

Deploy Agent is a Go binary that deploys your projects to a VPS each time you push to GitHub. It clones and builds the repo, writes the nginx configuration, and gets SSL certificates. Then it runs a health check (a request that tests if the app works). If the health check fails, it rolls back (returns to the previous version).

The agent does not receive webhooks. A webhook server on the same machine receives the push from GitHub and tests the signature. Then the webhook server runs the binary and gives it the push details in `GITHUB_*` environment variables. See [Webhook Server](#webhook-server).

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

- Deploys the pushed commit when the push goes to the deploy branch
- Supports static frontends (`CLIENT`), Node/TypeScript backends on PM2 (`API_JS`, `API_TS`), Docker Compose apps (`DOCKER`), and fullstack repos
- Writes an nginx configuration for each site and writes it again when the repo configuration changes
- Gets SSL certificates from Let's Encrypt through certbot
- Rolls back a deploy when the health check fails
- Sends a Slack message when a deploy starts and when a deploy fails
- Puts pushes to the same repo in a queue, so that only one deploy runs at a time
- Reads the repo configuration from a JSON file, so you do not rebuild the binary to add a project
- Puts a time limit on every command

## How It Works

```
GitHub push → webhook server → deploy-agent
                                   ↓
              lock → sync to commit → build → deploy → nginx/SSL → health check
                                                                      ↓ on failure
                                                                   rollback
```

Each project type deploys in a different way:

- Static site: The agent runs `npm run build` and keeps the last 5 builds as backups. Then it swaps the new build into the web root.
- PM2 backend: The agent installs, builds, and restarts the app through PM2. If the health check fails, the agent resets to the previous commit and deploys again.
- Docker: The agent tags the running images as `:previous` and builds the new images. If migrations are in the configuration, it runs them. Then it swaps the containers. If the health check fails, it restores the previous images.
- Docker + client: The agent deploys the API with Docker and the frontend as a static site. If `spa_meta_proxy_path` is set, the frontend deploys first. Then the agent recreates the API container, so that the container reads the new build.
- Fullstack: The agent deploys the backend on `api.<domain>` and the frontend on `<domain>`.

## Requirements

- A Linux VPS with a non-root user that has sudo. This document calls this user the deploy user.
- Go 1.25+ to build the binary
- `git`, `nginx` 1.19.4+, `certbot` with `python3-certbot-nginx`, and Node.js 18+ with `npm`
- `pm2` for PM2 backends
- `docker` with the compose plugin for Docker apps. The deploy user must be in the `docker` group.
- A webhook server. See [Webhook Server](#webhook-server).

The deploy user must have write access to `/var/www/html`. On the server, run:

```bash
sudo chown youruser /var/www/html
```

The agent runs nginx and certbot with sudo, and sudo must not ask for a password. On the server, run `sudo visudo -f /etc/sudoers.d/deploy-agent` and add these lines:

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

To make sure that the sudoers file works, run `sudo nginx -t` as the deploy user. The command must run without a password prompt.

## Webhook Server

The webhook server must test the GitHub signature. Then it must run the binary with these variables:

```
GITHUB_REPO_NAME        repo name
GITHUB_REPO_FULL_NAME   owner/repo
GITHUB_REPO_OWNER       owner
GITHUB_BRANCH           pushed branch
GITHUB_COMMIT           full 40-character commit hash
GITHUB_PUSHER           pusher username
```

The webhook server must also give its own environment variables to the agent, so that `SSL_EMAIL` and `SLACK_WEBHOOK_URL` reach the agent. For example, in Node:

```js
exec(script, { env: { ...process.env, GITHUB_REPO_NAME: name /* ... */ } });
```

A small Express app that tests the signature is sufficient. At startup, the agent makes sure that each value is valid. It rejects a commit hash that is not 40 characters long and a name that contains a path separator.

## Install

1. On your machine, clone the repo and build the binary for Linux. Then copy the binary to the server:

   ```bash
   git clone https://github.com/Brayzonn/deploy-agent
   cd deploy-agent
   GOOS=linux GOARCH=amd64 go build -o deploy-agent
   scp deploy-agent user@server:/home/youruser/scripts/deploy-agent
   ```

2. On the server, add these variables to the `.env` file of the webhook server:

   ```bash
   DEPLOYMENT_SCRIPT=/home/youruser/scripts/deploy-agent
   SSL_EMAIL=you@example.com                    # Let's Encrypt notices
   SLACK_WEBHOOK_URL=https://hooks.slack.com/…  # optional
   ```

Build the binary again only when the Go code changes. A change to `repos.json` applies on the next deploy.

## Configuration

The agent reads the list of repos from `/etc/deploy-agent/repos.json`. To use a different path, set `DEPLOY_AGENT_REPOS_FILE`. Only the repos in this file can deploy.

To start from the example file, run on the server:

```bash
sudo mkdir -p /etc/deploy-agent
sudo cp repos.example.json /etc/deploy-agent/repos.json
```

The agent rejects unknown fields. `repo_dir` must be an absolute path. Each web root must be under `/var/www/html/`.

Example for a static site:

```json
"your-frontend": {
  "repo_dir": "/home/youruser/your-frontend",
  "web_root": "/var/www/html/your-frontend",
  "project_type": "CLIENT",
  "domain": "yourdomain.com",
  "domain_aliases": ["www.yourdomain.com"]
}
```

Example for a PM2 backend:

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

Example for a Docker app:

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

Example for a Docker API with a static frontend:

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

Example for a fullstack repo (one repo, two sites):

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

This example deploys the backend on `api.myapp.com` and the frontend on `myapp.com`. To use a different backend domain, set `server_domain`.

### Fields

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `name` | string | Repo name. Default is the JSON key | No |
| `deploy_branch` | string | Branch that starts a deploy. Default `main` | No |
| `repo_dir` | string | Absolute path to the clone | Yes |
| `project_type` | string | `CLIENT`, `API_JS`, `API_TS`, or `DOCKER` | Yes |
| `use_docker` | bool | Set to `true` for Docker apps | Docker |
| `docker_compose_file` | string | Name of the Compose file | Docker |
| `docker_env_file` | string | Environment file for Compose. Default `.env.production` | No |
| `requires_migrations` | bool | Run migrations before the container swap | No |
| `migration_command` | string | Command that runs the migrations | With migrations |
| `migration_service` | string | Compose service that runs the migrations. Default `api` | No |
| `infra_services` | []string | Compose services that start before the migrations | With migrations |
| `web_root` | string | Directory that nginx serves the built site from | Static sites |
| `server_dir` | string | Directory with the server code | PM2 |
| `server_entry` | string | PM2 entry point when there is no ecosystem file | PM2 |
| `pm2_ecosystem` | string | PM2 ecosystem file | No |
| `domain` | string | Main domain | No |
| `domain_aliases` | []string | More domains on the same site and certificate | No |
| `server_domain` | string | Backend domain for a fullstack repo. Default `api.<domain>` | No |
| `port` | int | Port that the app listens on | Backends |
| `nginx_max_upload_mb` | int | Upload size limit for the backend. The nginx default is 1 MB | No |
| `health_check_url` | string | URL that the agent requests after the deploy until it returns 200 | No |
| `health_check_timeout` | int | Seconds to wait for the health check. Default 30 | No |
| `full_stack` | bool | Deploy the backend and the frontend from one repo | No |
| `client_dir` | string | Frontend directory | Fullstack, Docker + client |
| `client_domain` | string | Frontend domain | Docker + client |
| `client_domain_aliases` | []string | More frontend domains | No |
| `client_web_root` | string | Directory that nginx serves the frontend from | Docker + client |
| `spa_meta_proxy_path` | string | Path on `localhost:<port>` that renders frontend pages with server-side meta tags. If the app is down, the site serves `index.html`. Requires `port` | No |

### Environment files

An environment file holds secrets and is not in Git, for example `.env.production`. You can keep these files in `/etc/deploy-agent/env/<repo>/` on the server. Give each file the same path that it has in the repo. To use a different directory, set `DEPLOY_AGENT_ENV_DIR`.

```bash
sudo mkdir -p /etc/deploy-agent/env/your-docker-app
sudo cp .env.production /etc/deploy-agent/env/your-docker-app/.env.production
sudo chmod -R go-rwx /etc/deploy-agent/env
```

Each deploy copies these files into the checkout with mode 600. If an environment file is missing, the deploy does not fail.

## nginx and SSL

- The agent writes each nginx configuration to `/etc/nginx/sites-available/<domain>`. nginx serves a static site as files and uses a reverse proxy for all other project types. Each site listens on IPv4 and IPv6.
- If the template or the repo configuration changes, the next deploy writes a new nginx configuration. The agent keeps a backup of the old configuration, with 5 backups for each domain.
- If you wrote an nginx configuration by hand, the agent replaces it in the same way. Your changes are lost.
- certbot adds HTTPS and redirects HTTP to HTTPS. If you add an alias, certbot adds it to the existing certificate.
- If certbot fails after the agent writes a new configuration, the agent restores the previous configuration. The site stays on HTTPS.
- A `default-tls` site refuses HTTPS requests for a domain that has no configuration. Thus such a domain never gets the certificate of a different site.
- The certbot timer renews the certificates. To see the timer, run `systemctl list-timers | grep certbot`.

## Health Checks and Rollback

| Type | Health check | If the health check fails |
| --- | --- | --- |
| PM2 | The process is online and the URL returns 200 | Reset to the previous commit, build, and restart |
| Docker | The containers are healthy and the URL returns 200 | Restore the `:previous` images |
| Static site | No health check | The old site stays live until the swap succeeds |

The first deploy of a repo has no previous version. If it fails, the agent stops and logs that manual intervention is required.

Rollback restores the containers but not the database. Thus each migration must be backward-compatible: the previous version of the app must work with the new schema.

## Slack Notifications

If you set `SLACK_WEBHOOK_URL`, the agent sends a Slack message when a deploy starts. It also sends an alert with the error when a deploy fails. If the URL is not set, the deploy log shows a warning. A Slack error never makes a deploy fail.

## Logs

Each deploy writes a log file to `~/logs/deployments/deployment_YYYYMMDD_HHMMSS_PID.log`. The agent also writes its output to the stdout of the webhook server.

```bash
tail -f $(ls -t ~/logs/deployments/deployment_*.log | head -1)   # follow the latest
grep ERROR ~/logs/deployments/*.log                              # find failures
```

## Manual Run

To deploy without a push, set the variables yourself on the server. `GITHUB_COMMIT` must be the full 40-character hash of a commit on the branch.

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

| Problem | Cause and fix |
| --- | --- |
| Permission denied | The sudoers file is not set up, or the deploy user cannot write to `/var/www/html`. See [Requirements](#requirements). |
| `repos file does not exist` | Create `/etc/deploy-agent/repos.json`. See [Configuration](#configuration). |
| A repo does not deploy | Make sure that the repo is in `repos.json` and that the push went to its `deploy_branch`. Make sure that `ssh -T git@github.com` works on the server. |
| `timed out after X` | The time limits are 10 minutes for `npm install`, 15 minutes for builds, and 30 minutes for Docker builds. The error gives the name of the step. |
| `ERR_SSL_UNRECOGNIZED_NAME_ALERT` in the browser | The domain has no nginx configuration. Add the domain to `domain_aliases` or `client_domain_aliases` and deploy again. |
| certbot fails | Run `dig yourdomain.com` to make sure that the DNS records are correct. Make sure that ports 80 and 443 are open. Read `/var/log/letsencrypt/letsencrypt.log`. Let's Encrypt allows 5 certificates per week for the same set of domains. |
| The health check fails, but the app works | Increase `health_check_timeout`. On the server, send a request to `health_check_url` with `curl`. |
| No Slack messages | The deploy log gives the reason. `DISABLED` means that `SLACK_WEBHOOK_URL` did not reach the agent. See [Webhook Server](#webhook-server). |

## License

MIT
