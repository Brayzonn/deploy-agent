# Deploy Agent

Go-based deployment automation tool for zero-config deployments via GitHub webhooks with support for traditional PM2 deployments and Docker containerized applications.

## Features

### Automation

- Auto-clone repositories on first push
- Auto-generate nginx configs (static sites, reverse proxy, Docker apps)
- Auto-request SSL certificates via Let's Encrypt (with smart renewal detection)
- Auto-restart services (nginx, PM2, Docker containers)
- Database migrations for Docker deployments
- Health checks with automatic rollback on failure
- Automatic backups before static-site deploys (keeps last 5 per site)
- Per-repo deploy locking — concurrent pushes queue instead of interleaving
- Branch filtering — only pushes to the configured deploy branch are deployed
- Slack notifications on failed deployments

### Deployment Support

- **Frontend** (React, Vue, Vite) - Static site deployment
- **Backend** (Node.js, NestJS) - PM2 process management
- **Docker** - Containerized applications with Docker Compose
- **Fullstack** - Backend + Frontend in one push
- **TypeScript** - Automatic build compilation
- **Multiple repos** - Single agent handles all projects

### Developer Experience

- Repo configuration in a JSON file — add or change projects without recompiling
- Structured logging (console + file)
- Deterministic git sync — deploys exactly the commit the webhook announced
- Hard timeouts on every command (git, npm, docker, pm2, certbot)
- Build validation — stale output is cleaned so empty builds fail loudly
- Rollback on failed health checks
- Color-coded console output
- Detailed deployment logs per run

---

## How It Works

```
GitHub Push → Webhook Server → Deploy Agent → Your Site is Live
                                    ↓
                    [Clone → Build → Configure → Deploy → Verify]
```

**Traditional Deployment (PM2):**

1. Auto-clone repository / sync to the pushed commit
2. Install dependencies & build
3. Generate nginx config
4. Request SSL certificate
5. Deploy files / Restart PM2
6. Run health checks (rollback to previous commit on failure)
7. Site live with HTTPS

**Docker Deployment:**

1. Auto-clone repository / sync to the pushed commit
2. Build Docker images (old containers keep serving during the build)
3. Start infra services & run database migrations (if configured)
4. Swap to new containers
5. Generate nginx config (first time only)
6. Request SSL certificate (first time only)
7. Health checks (rollback to previous images on failure)
8. Site live with HTTPS

---

## Quick Start

### Prerequisites

Before installing the deploy agent, ensure your VPS has the following:

#### System Requirements

- **OS**: Linux (Ubuntu 22.04+ or Debian 11+ recommended)
- **User**: Non-root user with sudo access
- **RAM**: Minimum 1GB (2GB+ recommended for Docker)
- **Storage**: At least 10GB free space

#### Required Software

**For All Deployments:**

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Git
sudo apt install git -y

# Install Node.js and npm (v18 or higher recommended)
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt install nodejs -y

# Verify installation
node --version
npm --version

# Install Nginx
sudo apt install nginx -y

# Install Certbot for SSL
sudo apt install certbot python3-certbot-nginx -y

# Install PM2 globally (for PM2 deployments)
sudo npm install -g pm2
```

**For Docker Deployments:**

```bash
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Install Docker Compose
sudo apt install docker-compose-plugin -y

# Add your user to docker group (replace 'youruser' with actual username)
sudo usermod -aG docker youruser

# Apply group changes (logout and login, or run)
newgrp docker

# Verify Docker works without sudo
docker ps
docker compose version

# Enable Docker to start on boot
sudo systemctl enable docker
sudo systemctl start docker
```

#### Configure Sudo Permissions

Create a sudoers file for the deploy agent:

```bash
# Open sudoers editor
sudo visudo -f /etc/sudoers.d/deploy-agent
```

Add these lines (replace `youruser` with your actual username):

```bash
# Nginx configuration
youruser ALL=(ALL) NOPASSWD: /usr/bin/mkdir -p /var/www/html/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/tee /etc/nginx/sites-available/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/ln -s /etc/nginx/sites-available/* /etc/nginx/sites-enabled/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
youruser ALL=(ALL) NOPASSWD: /usr/sbin/nginx -t
youruser ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload nginx
youruser ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart nginx
youruser ALL=(ALL) NOPASSWD: /bin/systemctl restart nginx

# SSL certificates
youruser ALL=(ALL) NOPASSWD: /usr/bin/certbot*
```

Save and exit (Ctrl+X, then Y, then Enter).

Verify sudo permissions work:

```bash
sudo nginx -t
# Should NOT ask for password
```

---

### 1. Install Deploy Agent

```bash
# Clone repository
git clone https://github.com/Brayzonn/deploy-agent
cd deploy-agent

# Build for Linux (from any OS - local machine or VPS)
GOOS=linux GOARCH=amd64 go build -o deploy-agent

# If building on VPS, upload to server
scp deploy-agent user@your-server:/tmp/

# SSH to server and move to scripts directory
ssh user@your-server
sudo mv /tmp/deploy-agent /home/youruser/scripts/deploy-agent
sudo chmod +x /home/youruser/scripts/deploy-agent
```

### 2. Configure Webhook Server

Update your webhook server `.env`:

```bash
DEPLOYMENT_SCRIPT=/home/youruser/scripts/deploy-agent
SSL_EMAIL=your-email@example.com  # For Let's Encrypt notifications
```

### 3. Add Your Repositories

Repo configs live in a JSON file on the server: `/etc/deploy-agent/repos.json` (override the path with the `DEPLOY_AGENT_REPOS_FILE` env var). **This file is required** — without it every deploy fails with an error telling you to install it. Start from the example in this repo:

```bash
sudo mkdir -p /etc/deploy-agent
sudo cp repos.example.json /etc/deploy-agent/repos.json
sudo nano /etc/deploy-agent/repos.json
```

Adding or changing a project is just editing this file — no recompiling, no redeploying the binary. Unknown/typo'd field names are rejected at load time, and paths are validated (repo dirs must be absolute, web roots must live under `/var/www/html/`). Only repos in this file can deploy — anything else is rejected.

**Frontend (Static Site):**

```json
"your-frontend": {
  "repo_dir": "/home/youruser/your-frontend",
  "web_root": "/var/www/html/your-frontend",
  "project_type": "CLIENT",
  "domain": "yourdomain.com",
  "domain_aliases": ["www.yourdomain.com"]
}
```

**Backend (PM2):**

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

**Docker Application:**

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

### 4. Setup GitHub Webhook

1. **Go to your GitHub repository** → Settings → Webhooks → Add webhook

2. **Configure webhook:**

```
   Payload URL: http://your-server-ip:9000/githubwebhook
   Content type: application/json
   Secret: [your webhook secret from webhook server .env]
   Events: Just the push event
```

3. **Save webhook**

### 5. Push & Deploy

```bash
git push origin main
# Watch it deploy automatically
```

Monitor deployment on your VPS:

```bash
# View live deployment logs
tail -f ~/logs/deployments/deployment_*.log

```

---

**Note:** You only need to rebuild the deploy agent when the Go code itself changes. Adding or changing repositories does **not** require a rebuild:

```bash
# On the server — edit config, done
sudo nano /etc/deploy-agent/repos.json
```

The next deploy picks the change up automatically.

---

## Configuration

### Repository Configuration Fields

Fields are snake_case JSON keys in `/etc/deploy-agent/repos.json`; each repo is keyed by its GitHub repository name.

| Field                   | Type     | Description                                              | Required              |
| ----------------------- | -------- | -------------------------------------------------------- | --------------------- |
| `name`                  | string   | Repository name (defaults to the JSON key)               | Optional              |
| `deploy_branch`         | string   | Only pushes to this branch deploy (default `main`)       | Optional              |
| `repo_dir`              | string   | Local path where repo is cloned (must be absolute)       | Yes                   |
| `project_type`          | string   | `CLIENT`, `API_JS`, `API_TS`, or `DOCKER`                | Yes                   |
| `use_docker`            | bool     | Enable Docker deployment                                 | For Docker            |
| `docker_compose_file`   | string   | Docker Compose file name                                 | For Docker            |
| `docker_env_file`       | string   | Environment file for Docker (default `.env.production`)  | Optional              |
| `requires_migrations`   | bool     | Run migrations before swapping containers                | Optional              |
| `migration_command`     | string   | Command to run migrations                                | If migrations enabled |
| `migration_service`     | string   | Compose service to run migrations in (default `api`)     | Optional              |
| `infra_services`        | []string | Compose services started before migrations               | If migrations enabled |
| `web_root`              | string   | Path to serve static files (under `/var/www/html/`)      | For static sites      |
| `server_dir`            | string   | Directory containing server code                         | For PM2 backends      |
| `server_entry`          | string   | Entry point file for PM2 (fallback when no ecosystem)    | For PM2 backends      |
| `pm2_ecosystem`         | string   | PM2 ecosystem config file                                | Optional              |
| `domain`                | string   | Primary domain name                                      | Optional              |
| `domain_aliases`        | []string | Additional domains                                       | Optional              |
| `server_domain`         | string   | Fullstack API domain (default `api.` + `domain`)         | Optional              |
| `port`                  | int      | Port where app runs                                      | For backends          |
| `health_check_url`      | string   | URL polled after deployment until it returns 200         | Optional              |
| `health_check_timeout`  | int      | Health check deadline in seconds (default 30)            | Optional              |
| `full_stack`            | bool     | Deploy both frontend and backend                         | Optional              |
| `client_dir`            | string   | Frontend directory in fullstack/hybrid repos             | For fullstack         |
| `client_domain`         | string   | Frontend domain for Docker + client hybrid repos         | For hybrid            |
| `client_domain_aliases` | []string | Frontend domain aliases for hybrid repos                 | Optional              |
| `client_web_root`       | string   | Frontend web root for hybrid repos                       | For hybrid            |

### Project Types

| Type     | Description              | Build           | Deployment            | Nginx Config  |
| -------- | ------------------------ | --------------- | --------------------- | ------------- |
| `CLIENT` | React, Vue, Vite         | `npm run build` | Atomic web-root swap  | Static files  |
| `API_JS` | Node.js backend          | None            | PM2 restart           | Reverse proxy |
| `API_TS` | NestJS, TypeScript       | `npm run build` | PM2 restart           | Reverse proxy |
| `DOCKER` | Docker containerized app | Docker build    | Docker Compose        | Reverse proxy |

### Docker Configuration

**Directory Structure:**

```
your-docker-app/
├── docker-compose.prod.yml
├── .env.production
├── docker/
│   └── api/
│       └── Dockerfile.prod
└── ... (your application code)
```

**Example docker-compose.prod.yml:**

```yaml
services:
  api:
    build:
      context: .
      dockerfile: docker/api/Dockerfile.prod
    container_name: your-app-api
    restart: unless-stopped
    ports:
      - "5932:3000"
    env_file:
      - .env.production
    depends_on:
      - postgres
      - redis
    networks:
      - app-network

  postgres:
    image: postgres:15-alpine
    container_name: your-app-postgres
    restart: unless-stopped
    env_file:
      - .env.production
    volumes:
      - postgres-data:/var/lib/postgresql/data
    networks:
      - app-network

  redis:
    image: redis:7-alpine
    container_name: your-app-redis
    restart: unless-stopped
    env_file:
      - .env.production
    command: redis-server --appendonly yes --requirepass "${REDIS_PASSWORD}"
    volumes:
      - redis-data:/data
    networks:
      - app-network

networks:
  app-network:
    driver: bridge

volumes:
  postgres-data:
  redis-data:
```

**Example .env.production:**

```bash
NODE_ENV=production
PORT=3000

# Database
DATABASE_URL="postgresql://user:password@postgres:5432/dbname"
POSTGRES_USER=user
POSTGRES_PASSWORD=password
POSTGRES_DB=dbname

# Redis
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=your-redis-password

# Application
JWT_SECRET=your-secret
# ... other env vars
```

**Important Docker Notes:**

- Only expose ports for services that need external access (typically just the API)
- Use Docker service names (e.g., `postgres`, `redis`) for internal communication
- Keep database and Redis ports internal to the Docker network
- Use host nginx to handle SSL and reverse proxy to Docker containers
- The deploy agent automatically handles container lifecycle and migrations

---

## Environment Variables

Set by webhook server (automatically). All values are validated at startup — the commit must be a full 40-character hash, and names may not contain path separators or traversal:

```bash
GITHUB_REPO_NAME=your-repo
GITHUB_BRANCH=main
GITHUB_REPO_OWNER=username
GITHUB_PUSHER=username
GITHUB_COMMIT=<full 40-char commit hash>
GITHUB_REPO_FULL_NAME=username/your-repo
```

Optional (set in webhook server .env):

```bash
SSL_EMAIL=your-email@example.com            # Let's Encrypt notifications
SLACK_WEBHOOK_URL=https://hooks.slack.com/… # failure alerts (off when unset)
DEPLOY_AGENT_REPOS_FILE=/path/to/repos.json # overrides /etc/deploy-agent/repos.json
```

---

## Directory Structure

```
deploy-agent/
├── internal/
│   ├── build/        # Build orchestration
│   │   ├── builder.go   # npm install/build
│   │   ├── client.go    # Frontend builds + atomic web-root swap
│   │   ├── server.go    # Backend builds
│   │   └── docker.go    # Docker builds, rollback, compose v1/v2 detection
│   ├── config/       # Configuration
│   │   ├── config.go    # Agent config + webhook input validation
│   │   └── repos.go     # Repos-file loading and validation
│   ├── deploy/       # Deployment executor
│   │   └── executor.go  # Orchestration, routing, rollback
│   ├── git/          # Git operations
│   │   └── git.go       # Clone, sync-to-commit, rollback reset
│   ├── health/       # Health checks
│   │   └── health.go    # HTTP polling and container-state checks
│   ├── lock/         # Per-repo deploy locking (flock)
│   ├── logger/       # Colored console and file logging
│   ├── nginx/        # Nginx config generation and management
│   ├── notify/       # Slack failure notifications
│   ├── pm2/          # PM2 process management
│   ├── run/          # Command execution with hard timeouts
│   └── ssl/          # Certbot integration
├── pkg/types/        # Shared type definitions
├── repos.example.json # Template for /etc/deploy-agent/repos.json
├── main.go           # Entry point
├── go.mod            # Go dependencies
└── README.md         # This file
```

---

## Logs

### Console Output

Color-coded for easy reading:

- Blue: Info
- Green: Success
- Yellow: Warning
- Red: Error

### Log Files

**Location:** `~/logs/deployments/deployment_YYYYMMDD_HHMMSS_PID.log`

Each deployment creates its own log file with:

- Timestamp
- Process ID
- Complete deployment history

**View logs:**

```bash
# View latest deployment
tail -f ~/logs/deployments/deployment_*.log

# View specific deployment
cat ~/logs/deployments/deployment_20260218_150923_2628327.log

# Search logs
grep "ERROR" ~/logs/deployments/*.log

# List recent deployments
ls -lt ~/logs/deployments/ | head -10
```

**Useful aliases (add to ~/.bashrc):**

```bash
alias deploy-latest='tail -f $(ls -t ~/logs/deployments/deployment_*.log 2>/dev/null | head -1)'
alias deploy-history='ls -lt ~/logs/deployments/ | head -20'
```

---

## Deployment Workflows

Every deployment starts the same way: the branch is checked against `deploy_branch` (other branches are ignored), a per-repo lock is taken (concurrent pushes queue), and the checkout is force-synced to exactly the pushed commit.

### Traditional PM2 Deployment

```
1. Webhook triggered → branch check, lock, sync to commit
2. npm install (npm ci when a lock file exists)
3. npm run build (for TypeScript projects)
4. Generate nginx config / request SSL (first deployment)
5. PM2 restart (or first start via ecosystem file / server entry)
6. Health check
7. On failure: reset git to previous commit, rebuild, restart,
   re-check health — or stop loudly if there is nothing to roll back to
```

### Docker Deployment

```
1. Webhook triggered → branch check, lock, sync to commit
2. Tag running images as :previous (rollback safety net)
3. docker compose build (old containers keep serving)
4. Start infra services & run migrations (if configured)
5. docker compose up -d (swap to the new containers)
6. Generate nginx config / request SSL (first deployment)
7. Container-state health check (respects compose healthcheck:)
8. HTTP health check (if configured; warning only)
9. On failure: restore the recorded :previous images and verify the
   running image IDs actually match before claiming success
```

### Static Site Deployment

```
1. Webhook triggered → branch check, lock, sync to commit
2. npm install + npm run build
3. Back up current site (last 5 backups kept)
4. Generate nginx config / request SSL (first deployment)
5. Stage new build next to the web root
6. Atomically swap it in (two renames — no 404 window)
7. On failure: the old site is still serving; if the swap itself
   broke, restore from the backup
```

---

## Health Checks & Rollback

### Automatic Health Checks

After deployment, the agent performs:

**For PM2 Applications:**

1. PM2 Status Check - Verifies app is online
2. HTTP Response Check - Polls the configured `health_check_url` (or the domain) until it returns 200 or the timeout expires

**For Docker Applications:**

1. Container State Check - Polls actual container state (running + compose `healthcheck:` status); exited or unhealthy containers fail immediately
2. HTTP Response Check - Polls `health_check_url` if configured (logged as a warning on failure, doesn't trigger rollback)

**For Static Sites:**

- No post-deploy health check; the atomic swap means a failed deploy never replaces the serving site

### Automatic Rollback

**PM2 Deployments:**

- Resets git to the previous commit, rebuilds, and restarts PM2
- Re-runs the health check and only reports success if the old version actually serves
- If there is no previous commit (first deploy), it stops and says manual intervention is required instead of redeploying the failed code

**Docker Deployments:**

- Restores the images that were running before the deploy (recorded exactly, per service, before the build)
- Verifies the running image IDs match the previous ones before claiming success
- Shows container logs for debugging
- First-ever deploys have nothing to restore: the stack is brought down with explicit "manual intervention" messaging

**Static Sites:**

- A failed build or staging step never touches the serving site
- If the swap itself failed, the pre-deploy backup is swapped back in and nginx restarted

**Example:**

```
[INFO] Deploying server API...
[SUCCESS] Build completed in 15s
[INFO] Restarting application...
[ERROR] Health check failed: timed out waiting for 200 from http://localhost:3000/health
[WARNING] Rolling back server deployment...
[WARNING] Resetting repository to commit abc1234...
[SUCCESS] Server rolled back successfully and passed health check
[ERROR] Deployment failed (but site still works)
```

---

## SSL Certificate Management

The deploy agent intelligently manages SSL certificates:

### First Deployment

- Requests new SSL certificate from Let's Encrypt
- Configures nginx with HTTPS

### Subsequent Deployments

- Checks if valid certificate exists
- Skips certificate request if cert is valid
- Only renews if certificate is expiring (within 30 days)

### Certificate Renewal

- Automatic via `--keep-until-expiring` flag
- Certificates valid for 90 days
- Auto-renewal starts at 60 days remaining
- No manual intervention needed

### Rate Limiting

- Let's Encrypt: 5 certificates per week per domain
- Agent prevents hitting rate limits by checking before requesting

---

## Nginx Configuration

### Automatic Generation

The deploy agent automatically generates nginx configurations:

**Static Sites:**

```nginx
server {
    listen 80;
    server_name yourdomain.com www.yourdomain.com;
    root /var/www/html/your-app;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;

    # Gzip compression
    gzip on;
    gzip_types text/plain text/css application/json application/javascript;
}
```

**Backend APIs (PM2 or Docker):**

```nginx
server {
    listen 80;
    server_name api.yourdomain.com;

    location / {
        proxy_pass http://localhost:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**After SSL certificate is obtained, certbot automatically updates the config to redirect HTTP to HTTPS.**

---

## Troubleshooting

### General Issues

**Deployment fails with "permission denied"**

- Verify sudo permissions: `sudo nginx -t` should not ask for password
- Check sudoers file: `sudo cat /etc/sudoers.d/deploy-agent`
- Ensure user is in correct groups: `groups yourusername`

**Repository not cloning**

- Verify SSH keys are configured: `ssh -T git@github.com`
- Check the repo exists in `/etc/deploy-agent/repos.json` (unknown repos are rejected)
- Ensure git is installed: `git --version`

**Deploy fails with "repos file does not exist"**

- Install the config file: `sudo cp repos.example.json /etc/deploy-agent/repos.json` and edit it
- Or point `DEPLOY_AGENT_REPOS_FILE` at your file's location

**Build timeouts**

- Every command has a hard deadline (npm install 10m, npm build 15m, docker build 30m); a "timed out after X" error names the phase
- To change a limit, adjust the timeout constants in the relevant package (e.g. `internal/build/builder.go`) and rebuild
- Check npm install isn't stuck; verify sufficient disk space: `df -h`

### Docker-Specific Issues

**Containers not starting**

- Check logs: `docker logs container-name`
- Verify `.env.production` exists and is valid
- Check port conflicts: `sudo lsof -i :5932`
- Ensure Docker daemon is running: `sudo systemctl status docker`

**Permission denied on Docker commands**

- Verify user is in docker group: `groups`
- Restart shell after adding to group: `newgrp docker`
- Check Docker socket permissions: `ls -la /var/run/docker.sock`

**Database connection issues in Docker**

- Use service names, not `localhost` (e.g., `postgres:5432`)
- Verify `POSTGRES_PASSWORD` matches between services
- Check database credentials in `.env.production`
- Ensure password is not URL-encoded in `POSTGRES_PASSWORD`

**Migrations failing**

- Check migration command is correct
- Verify database is initialized
- Check Prisma binary targets match architecture
- View detailed logs: `docker logs container-name --tail=100`

### SSL Issues

**SSL certificate request fails**

- Verify DNS points to your server: `dig yourdomain.com`
- Check ports 80 and 443 are open
- Ensure domain is accessible: `curl http://yourdomain.com`
- View certbot logs: `sudo cat /var/log/letsencrypt/letsencrypt.log`

**Rate limit reached**

- Let's Encrypt limit: 5 certs per week per domain
- Wait one week or use staging environment for testing
- Check existing certificates: `sudo certbot certificates`

### PM2 Issues

**PM2 app not starting**

- Check logs: `pm2 logs app-name`
- Verify `ecosystem.config.js` exists
- Check port availability: `sudo lsof -i :3000`
- View PM2 status: `pm2 status`

**PM2 app crashes on deployment**

- Check application logs: `pm2 logs app-name --lines 100`
- Verify environment variables
- Check Node.js version compatibility

### Health Check Issues

**Health check fails but site works manually**

- Increase timeout in configuration
- Check DNS propagation: `nslookup yourdomain.com`
- Verify health check URL is correct
- Test endpoint manually: `curl -I http://localhost:3000/health`

**Nginx issues**

- Test configuration: `sudo nginx -t`
- Check nginx logs: `sudo tail /var/log/nginx/error.log`
- Verify site is enabled: `ls -la /etc/nginx/sites-enabled/`
- Reload nginx: `sudo systemctl reload nginx`

---

## Manual Testing

Test deployment without pushing to GitHub:

```bash
# Set required environment variables
# GITHUB_COMMIT must be a real, full 40-character commit hash that
# exists on the branch — short or fake values are rejected.
export GITHUB_REPO_NAME="your-repo"
export GITHUB_BRANCH="main"
export GITHUB_REPO_OWNER="username"
export GITHUB_PUSHER="username"
export GITHUB_COMMIT="$(git ls-remote git@github.com:username/your-repo.git main | cut -f1)"
export GITHUB_REPO_FULL_NAME="username/your-repo"
export SSL_EMAIL="your-email@example.com"

# Run deploy agent
/home/youruser/scripts/deploy-agent

# Check logs
tail -f ~/logs/deployments/*.log
```

---

## Security

### Access Control

- Sudo permissions limited to specific commands only
- No blanket sudo access required
- Web root safety checks prevent deletion of critical directories
- Docker containers run as non-root users when possible

### Validation

- Only repos in `/etc/deploy-agent/repos.json` can deploy (strict allowlist)
- All webhook inputs validated (repo/owner/branch names, full 40-hex commit)
- Web roots must live under `/var/www/html/` — enforced at config load and again before any delete
- Nginx config validated before reload; broken configs this run created are removed
- Build output validated before deployment (stale output cleaned first)

### Backup & Recovery

- Automatic backups before each deployment (PM2/static sites)
- Failed deployments don't affect running site
- Rollback capabilities for quick recovery
- Deployment logs retained for audit trail

### Docker Security

- Containers run in isolated network
- Database and Redis not exposed to host
- Environment variables not logged
- Secrets managed through `.env.production` file

---

## Advanced Usage

### Custom Build Commands

For projects needing custom build steps, modify your `ecosystem.config.js`:

```javascript
module.exports = {
  apps: [
    {
      name: "my-app",
      script: "dist/main.js",
      instances: 2,
      exec_mode: "cluster",
      env_production: {
        NODE_ENV: "production",
        PORT: 3000,
      },
      error_file: "~/logs/my-app-error.log",
      out_file: "~/logs/my-app-out.log",
    },
  ],
};
```

### Multiple Domains

Add multiple domains to serve the same application:

```json
"domain": "example.com",
"domain_aliases": ["www.example.com", "app.example.com"]
```

All aliases will be included in the nginx config and SSL certificate.

### Custom Ports

Backend APIs can run on any available port:

```json
"port": 8080
```

Nginx automatically proxies to the specified port.

### Fullstack Deployment

Deploy frontend and backend together:

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

This creates:

- `myapp.com` serving the frontend
- `api.myapp.com` proxying to backend on port 3000 (override with `server_domain`)

### Docker with Custom Migrations

For complex migration scenarios:

```json
"requires_migrations": true,
"migration_command": "npm run migrate:prod",
"migration_service": "api",
"infra_services": ["postgres", "redis"]
```

The agent starts the infra services, waits until they're actually healthy, then runs the migration command in a throwaway container of the configured service — before the new code goes live. Migrations should be backward-compatible with the previous release: a rollback restores containers, not the database schema.

---

## Performance Optimization

### Docker Build Caching

- Layer caching speeds up rebuilds
- Multi-stage builds reduce image size
- Only changed layers are rebuilt

### PM2 Clustering

- Use cluster mode for better CPU utilization
- Configure in `ecosystem.config.js`

### Nginx Caching

- Static assets cached with long expiry
- Gzip compression enabled automatically
- Browser caching headers set

---

## Monitoring

### Check Deployment Status

```bash
# View recent deployments
ls -lt ~/logs/deployments/ | head -10

# Check specific deployment
cat ~/logs/deployments/deployment_TIMESTAMP.log

# Monitor live deployment
tail -f ~/logs/deployments/deployment_*.log
```

### Check Application Status

**PM2 Applications:**

```bash
pm2 status
pm2 logs app-name
pm2 monit
```

**Docker Applications:**

```bash
docker ps
docker logs container-name
docker stats
docker-compose -f docker-compose.prod.yml ps
```

**Nginx:**

```bash
sudo systemctl status nginx
sudo tail -f /var/log/nginx/error.log
sudo tail -f /var/log/nginx/access.log
```

---

## Best Practices

### Repository Structure

- Keep production configs in repository
- Use separate `.env` files for different environments
- Document deployment requirements in README
- Include `ecosystem.config.js` for PM2 apps
- Include `docker-compose.prod.yml` for Docker apps

### Environment Variables

- Never commit `.env.production` to git
- Use strong passwords for production databases
- Rotate secrets regularly
- Use different credentials for dev and prod

### Docker

- Always use `.env.production` for production configs
- Don't expose database/Redis ports to host
- Use service names for internal communication
- Keep images small with multi-stage builds
- Run containers as non-root when possible

### Testing

- Test deployments in staging first
- Use `--dry-run` or test environment
- Verify health check endpoints work
- Monitor first production deployment closely

---

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Test your changes thoroughly
4. Update documentation as needed
5. Submit a pull request with clear description

---

## License

MIT License - See LICENSE file for details

---

## Support

- Issues: [GitHub Issues](https://github.com/Brayzonn/deploy-agent/issues)
- Documentation: This README
- Examples: See `repos.example.json` for configuration examples

---
