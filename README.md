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
- Automatic backups (keeps last 5 for PM2 deployments)

### Deployment Support

- **Frontend** (React, Vue, Vite) - Static site deployment
- **Backend** (Node.js, NestJS) - PM2 process management
- **Docker** - Containerized applications with Docker Compose
- **Fullstack** - Backend + Frontend in one push
- **TypeScript** - Automatic build compilation
- **Multiple repos** - Single agent handles all projects

### Developer Experience

- Structured logging (console + file)
- Git operations (fetch, pull, stash, restore)
- Build validation with timeout handling
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

1. Auto-clone repository
2. Install dependencies & build
3. Generate nginx config
4. Request SSL certificate
5. Deploy files / Restart PM2
6. Run health checks
7. Site live with HTTPS

**Docker Deployment:**

1. Auto-clone repository
2. Build Docker images
3. Stop old containers
4. Start new containers
5. Run database migrations
6. Generate nginx config (first time only)
7. Request SSL certificate (first time only)
8. Health checks
9. Site live with HTTPS

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

### 3. Add Your Repository

Edit `internal/config/repos.go` and add your repository configuration:

**Frontend (Static Site):**

```go
"your-frontend": {
    Name:          "your-frontend",
    RepoDir:       "/home/youruser/your-frontend",
    WebRoot:       "/var/www/html/your-frontend",
    ProjectType:   types.ProjectTypeClient,
    Domain:        "yourdomain.com",
    DomainAliases: []string{"www.yourdomain.com"},
},
```

**Backend (PM2):**

```go
"your-api": {
    Name:          "your-api",
    RepoDir:       "/home/youruser/your-api",
    ProjectType:   types.ProjectTypeAPITS,
    ServerDir:     ".",
    ServerEntry:   "dist/main.js",
    PM2Ecosystem:  "ecosystem.config.js",
    Domain:        "api.yourdomain.com",
    Port:          3000,
},
```

**Docker Application:**

```go
"your-docker-app": {
    Name:               "your-docker-app",
    RepoDir:            "/home/youruser/your-docker-app",
    ProjectType:        types.ProjectTypeDocker,
    UseDocker:          true,
    DockerComposeFile:  "docker-compose.prod.yml",
    DockerEnvFile:      ".env.production",
    RequiresMigrations: true,
    MigrationCommand:   "npx prisma migrate deploy",
    Domain:             "api.yourdomain.com",
    Port:               5932,
    HealthCheckURL:     "http://localhost:5932/api/v1/health",
    HealthCheckTimeout: 30,
},
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

**Note:** You only need to rebuild the deploy agent if you modify the Go code (e.g., adding/changing repository configurations in `internal/config/repos.go`).

If you add a new repository:

```bash
# Edit config
vim internal/config/repos.go

# Rebuild
GOOS=linux GOARCH=amd64 go build -o deploy-agent

# Upload to server
scp deploy-agent user@your-server:/tmp/
ssh user@your-server
sudo mv /tmp/deploy-agent /home/youruser/scripts/deploy-agent

# Restart webhook server
pm2 restart git-hook
```

---

## Configuration

### Repository Configuration Fields

| Field                | Type     | Description                         | Required              |
| -------------------- | -------- | ----------------------------------- | --------------------- |
| `Name`               | string   | Repository name                     | Yes                   |
| `RepoDir`            | string   | Local path where repo is cloned     | Yes                   |
| `ProjectType`        | string   | Type of project (see Project Types) | Yes                   |
| `UseDocker`          | bool     | Enable Docker deployment            | For Docker            |
| `DockerComposeFile`  | string   | Docker Compose file name            | For Docker            |
| `DockerEnvFile`      | string   | Environment file for Docker         | For Docker            |
| `RequiresMigrations` | bool     | Run migrations after Docker deploy  | Optional              |
| `MigrationCommand`   | string   | Command to run migrations           | If RequiresMigrations |
| `WebRoot`            | string   | Path to serve static files          | For static sites      |
| `ServerDir`          | string   | Directory containing server code    | For PM2 backends      |
| `ServerEntry`        | string   | Entry point file for PM2            | For PM2 backends      |
| `PM2Ecosystem`       | string   | PM2 ecosystem config file           | For PM2 backends      |
| `Domain`             | string   | Primary domain name                 | Optional              |
| `DomainAliases`      | []string | Additional domains                  | Optional              |
| `Port`               | int      | Port where app runs                 | For backends          |
| `HealthCheckURL`     | string   | URL to check after deployment       | Optional              |
| `HealthCheckTimeout` | int      | Health check timeout in seconds     | Optional              |
| `FullStack`          | bool     | Deploy both frontend and backend    | Optional              |
| `ClientDir`          | string   | Frontend directory in fullstack     | For fullstack         |

### Project Types

| Type                | Description              | Build           | Deployment       | Nginx Config  |
| ------------------- | ------------------------ | --------------- | ---------------- | ------------- |
| `ProjectTypeClient` | React, Vue, Vite         | `npm run build` | Copy to web root | Static files  |
| `ProjectTypeAPIJS`  | Node.js backend          | None            | PM2 restart      | Reverse proxy |
| `ProjectTypeAPITS`  | NestJS, TypeScript       | `npm run build` | PM2 restart      | Reverse proxy |
| `ProjectTypeDocker` | Docker containerized app | Docker build    | Docker Compose   | Reverse proxy |

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

Set by webhook server (automatically):

```bash
GITHUB_REPO_NAME=your-repo
GITHUB_BRANCH=main
GITHUB_REPO_OWNER=username
GITHUB_PUSHER=username
GITHUB_COMMIT=abc123def456
GITHUB_REPO_FULL_NAME=username/your-repo
```

Optional (set in webhook server .env):

```bash
SSL_EMAIL=your-email@example.com
```

---

## Directory Structure

```
deploy-agent/
├── internal/
│   ├── build/        # Build orchestration
│   │   ├── client.go    # Frontend builds
│   │   ├── server.go    # Backend builds
│   │   └── docker.go    # Docker builds
│   ├── config/       # Repository configurations
│   │   ├── config.go    # Main config
│   │   └── repos.go     # Repository definitions
│   ├── deploy/       # Deployment executor
│   │   └── executor.go  # Main deployment logic
│   ├── git/          # Git operations
│   │   └── git.go       # Clone, pull, stash operations
│   ├── health/       # Health checks
│   │   └── health.go    # HTTP and PM2 health checks
│   ├── logger/       # Logging system
│   │   └── logger.go    # Colored console and file logging
│   ├── nginx/        # Nginx configuration
│   │   ├── nginx.go     # Config generation and management
│   │   └── templates.go # Config templates
│   ├── pm2/          # PM2 process management
│   │   └── pm2.go       # PM2 operations
│   └── ssl/          # SSL certificate automation
│       └── ssl.go       # Certbot integration
├── pkg/types/        # Shared type definitions
│   └── types.go         # Common types and enums
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

### Traditional PM2 Deployment

```
1. Webhook triggered
2. Git pull latest code
3. npm install (if package.json changed)
4. npm run build (for TypeScript)
5. PM2 restart application
6. Generate/update nginx config (first deployment)
7. Request/renew SSL certificate (first deployment)
8. Health check
9. Rollback if health check fails
```

### Docker Deployment

```
1. Webhook triggered
2. Git pull latest code
3. docker-compose build (with caching)
4. docker-compose down (stop old containers)
5. docker-compose up -d (start new containers)
6. Run database migrations (if configured)
7. Generate nginx config (first deployment only)
8. Request SSL certificate (first deployment only)
9. Container health check
10. HTTP health check (if configured)
11. Rollback if any check fails
```

### Static Site Deployment

```
1. Webhook triggered
2. Git pull latest code
3. npm install
4. npm run build
5. Create backup of current site
6. Copy new build to web root
7. Generate nginx config (first deployment)
8. Request SSL certificate (first deployment)
9. Health check
10. Rollback to backup if health check fails
```

---

## Health Checks & Rollback

### Automatic Health Checks

After deployment, the agent performs:

**For PM2 Applications:**

1. PM2 Status Check - Verifies app is online
2. HTTP Response Check - Confirms endpoint returns 200 OK

**For Docker Applications:**

1. Container Status Check - All containers running
2. HTTP Response Check - API endpoint returns 200 OK

**For Static Sites:**

1. HTTP Response Check - Site loads successfully

### Automatic Rollback

**PM2 Deployments:**

- Restores previous deployment from backup
- Restarts PM2 with old code
- Site continues working

**Docker Deployments:**

- Stops failed containers
- Shows container logs for debugging
- Previous containers remain stopped
- Manual intervention required to restore

**Static Sites:**

- Restores previous build from backup
- Reloads nginx
- Site immediately reverts to working version

**Example:**

```
[INFO] Deploying client application...
[SUCCESS] Build completed in 15s
[INFO] Creating backup...
[SUCCESS] Backup created
[INFO] Deploying to web root...
[SUCCESS] Files deployed
[ERROR] Health check failed: HTTP returned 500
[WARNING] Attempting automatic rollback...
[SUCCESS] Previous deployment restored
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
- Check repository URL in `repos.go`
- Ensure git is installed: `git --version`

**Build timeouts**

- Increase timeout in `internal/build/builder.go`
- Check npm install isn't stuck
- Verify sufficient disk space: `df -h`

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
export GITHUB_REPO_NAME="your-repo"
export GITHUB_BRANCH="main"
export GITHUB_REPO_OWNER="username"
export GITHUB_PUSHER="username"
export GITHUB_COMMIT="test123"
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

- Nginx config validated before reload
- Build output validated before deployment
- Git operations use safe defaults
- All file operations use absolute paths

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

```go
Domain:        "example.com",
DomainAliases: []string{"www.example.com", "app.example.com"},
```

All aliases will be included in the nginx config and SSL certificate.

### Custom Ports

Backend APIs can run on any available port:

```go
Port: 8080,  // Accessible at api.yourdomain.com -> localhost:8080
```

Nginx automatically proxies to the specified port.

### Fullstack Deployment

Deploy frontend and backend together:

```go
"my-fullstack-app": {
    Name:          "my-fullstack-app",
    ProjectType:   types.ProjectTypeAPITS,
    FullStack:     true,
    ClientDir:     "client",
    ServerDir:     "server",
    Domain:        "myapp.com",
    Port:          3000,
    // ... other fields
}
```

This creates:

- `myapp.com` serving the frontend
- `api.myapp.com` proxying to backend on port 3000

### Docker with Custom Migrations

For complex migration scenarios:

```go
RequiresMigrations: true,
MigrationCommand:   "npm run migrate:prod && npm run seed:prod",
```

The agent will execute your custom migration command inside the running container.

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
- Examples: See `internal/config/repos.go` for configuration examples

---
