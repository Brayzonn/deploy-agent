# Deploy Agent

Go-based deployment automation tool for zero-config deployments via GitHub webhooks.

## Features

### Automation

- Auto-clone repositories on first push
- Auto-generate nginx configs (static sites & reverse proxy)
- Auto-request SSL certificates via Let's Encrypt
- Auto-restart services (nginx, PM2)
- Health checks with automatic rollback on failure
- Automatic backups (keeps last 5)

### Deployment Support

- Frontend (React, Vue, Vite) → Static site deployment
- Backend (Node.js, NestJS) → PM2 process management
- Fullstack → Backend + Frontend in one push
- TypeScript → Automatic build compilation
- Multiple repos → Single agent handles all projects

### Developer Experience

- Structured logging (console + file)
- Git operations (fetch, pull, stash, restore)
- Build validation with timeout handling
- Rollback on failed health checks
- Color-coded console output

---

## How It Works

```
GitHub Push → Webhook Server → Deploy Agent → Your Site is Live
                                    ↓
                    [Clone → Build → Configure → Deploy → Verify]
```

**First Push (New Repo):**

1. Auto-clone repository
2. Install dependencies & build
3. Generate nginx config
4. Request SSL certificate
5. Deploy files
6. Run health checks
7. Site live with HTTPS

**Subsequent Pushes:**

1. Pull latest code
2. Build changes
3. Create backup
4. Deploy
5. Health check (rollback if fails)
6. Updated

---

## Quick Start

### 1. Install on Server

```bash
# Clone repo
git clone https://github.com/Brayzonn/deploy-agent
cd deploy-agent

# Build for Linux (from any OS)
GOOS=linux GOARCH=amd64 go build -o deploy-agent

# Upload to server
scp deploy-agent user@your-server:/tmp/
ssh user@your-server
sudo mv /tmp/deploy-agent /usr/local/bin/deploy-agent
sudo chmod +x /usr/local/bin/deploy-agent
```

### 2. Configure Sudo Permissions

```bash
# On server, create sudoers file
sudo visudo -f /etc/sudoers.d/deploy-agent
```

Add these lines:

```bash
# Replace 'youruser' with your actual username
youruser ALL=(ALL) NOPASSWD: /usr/bin/tee /etc/nginx/sites-available/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/ln -s /etc/nginx/sites-available/* /etc/nginx/sites-enabled/*
youruser ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
youruser ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload nginx
youruser ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart nginx
youruser ALL=(ALL) NOPASSWD: /usr/bin/certbot*
```

### 3. Configure Webhook Server

Update your webhook server `.env`:

```bash
DEPLOYMENT_SCRIPT=/usr/local/bin/deploy-agent
SSL_EMAIL=your-email@example.com  # For Let's Encrypt notifications
```

### 4. Add Your Repository

Edit `internal/config/repos.go`:

```go
"your-app": {
    Name:          "your-app",
    RepoDir:       "/home/user/your-app",
    WebRoot:       "/var/www/html/your-app",
    ProjectType:   types.ProjectTypeClient,
    Domain:        "yourdomain.com",
    DomainAliases: []string{"www.yourdomain.com"},
},
```

### 5. Push & Deploy

```bash
git push
# Watch it deploy automatically
```

---

## Configuration

### Repository Config

```go
"notifykit": {
    Name:          "notifykit",
    RepoDir:       "/home/user/notifykit",
    WebRoot:       "/var/www/html/notifykit",
    ProjectType:   types.ProjectTypeAPITS,
    FullStack:     false,
    ServerDir:     "server",
    ServerEntry:   "main.js",
    PM2Ecosystem:  "ecosystem.config.js",
    Domain:        "api.notifykit.dev",
    Port:          3000,
},
```

### Project Types

| Type     | Description        | Build           | Deployment  | Nginx Config  |
| -------- | ------------------ | --------------- | ----------- | ------------- |
| `CLIENT` | React, Vue, Vite   | `npm run build` | Web root    | Static files  |
| `API_JS` | Node.js            | None            | PM2 restart | Reverse proxy |
| `API_TS` | NestJS, TypeScript | `npm run build` | PM2 restart | Reverse proxy |

### Fullstack Example

```go
"my-app": {
    Name:          "my-app",
    ProjectType:   types.ProjectTypeAPITS,
    FullStack:     true,
    Domain:        "myapp.com",          // Frontend
    Port:          8080,                 // Backend on api.myapp.com
    // ... other fields
}
```

This creates:

- `myapp.com` → Static frontend
- `api.myapp.com` → Backend reverse proxy to `localhost:8080`

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

Optional:

```bash
SSL_EMAIL=your-email@example.com  # For Let's Encrypt
```

---

## Directory Structure

```
deploy-agent/
├── internal/
│   ├── build/        # Build orchestration (npm, validation)
│   ├── config/       # Repository configurations
│   ├── deploy/       # Deployment executor
│   ├── git/          # Git operations (clone, pull, stash)
│   ├── health/       # Health checks (HTTP, PM2)
│   ├── logger/       # Colored console + file logging
│   ├── nginx/        # Nginx config generation
│   ├── pm2/          # PM2 process management
│   └── ssl/          # SSL certificate automation
├── pkg/types/        # Shared type definitions
├── main.go           # Entry point
└── README.md
```

---

## Logs

### Console Output

Color-coded for easy reading:

- Blue = Info
- Green = Success
- Yellow = Warning
- Red = Error

### Log Files

Located at: `~/logs/deployments/deployment_YYYYMMDD_HHMMSS_PID.log`

View live logs:

```bash
# On server
tail -f ~/logs/deployments/*.log
```

---

## Health Checks & Rollback

### Automatic Health Checks

After deployment, the agent verifies:

1. **PM2 Status** (backend) - Is the app online?
2. **HTTP Response** (frontend/backend) - Returns 200 OK?

### Automatic Rollback

If health checks fail:

1. Previous deployment is restored from backup
2. Nginx is restarted
3. Users continue seeing working site
4. Deployment marked as failed

Example:

```
[INFO] Deploying client...
[SUCCESS] Build completed
[SUCCESS] Files deployed
[ERROR] Health check failed: HTTP returned 500
[WARNING] Attempting automatic rollback...
[SUCCESS] Previous deployment restored
[ERROR] Deployment failed (but site still works)
```

---

## Manual Testing

Test without pushing to GitHub:

```bash
# Set environment variables
export GITHUB_REPO_NAME="your-repo"
export GITHUB_BRANCH="main"
export GITHUB_REPO_OWNER="username"
export GITHUB_PUSHER="username"
export GITHUB_COMMIT="test123"
export GITHUB_REPO_FULL_NAME="username/your-repo"

# Run deploy agent
/usr/local/bin/deploy-agent
```

---

## Requirements

### Server Requirements

- **OS**: Linux (Ubuntu/Debian recommended)
- **Git**: For cloning repositories
- **Node.js & npm**: For building projects
- **PM2**: For backend process management
- **Nginx**: For serving sites
- **Certbot**: For SSL certificates

### Build Requirements

- **Go 1.21+**: For compiling the agent

### Installation

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install git nodejs npm nginx certbot python3-certbot-nginx

# Install PM2
sudo npm install -g pm2

# Install Go (for building)
# Download from https://go.dev/dl/
```

---

## Troubleshooting

### Deployment fails with "permission denied"

Check sudo permissions: `sudo nginx -t` (should not ask for password)

### SSL certificate request fails

- Verify DNS points to your server
- Check port 80 is open
- Try: `sudo certbot certificates` to see existing certs

### PM2 app not starting

- Check logs: `pm2 logs app-name`
- Verify `ecosystem.config.js` exists in repo
- Check port is not already in use

### Health check fails but site works

- Adjust timeout in `internal/health/health.go`
- Check domain DNS propagation
- Verify nginx config: `sudo nginx -t`

### Build timeout

- Increase timeout in `internal/build/builder.go`
- Check npm install isn't stuck

---

## Security

- Sudo permissions limited to specific commands
- Web root safety checks (prevents deletion of `/`, `/home`)
- Nginx config validation before reload
- Automatic backups before deployment
- Failed deployments don't affect running site

---

## Advanced Usage

### Custom Build Commands

If your project needs custom build steps, modify `ecosystem.config.js`:

```javascript
module.exports = {
  apps: [
    {
      name: "my-app",
      script: "dist/main.js",
      env_production: {
        NODE_ENV: "production",
      },
    },
  ],
};
```

### Multiple Domains

For projects with multiple domains, add them to `DomainAliases`:

```go
Domain:        "example.com",
DomainAliases: []string{"www.example.com", "app.example.com"},
```

### Custom Ports

Backend APIs can run on any port:

```go
Port: 3000,  // Backend accessible at api.yourdomain.com → localhost:3000
```

---

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Test your changes
4. Submit a pull request

---

## License

MIT License - See LICENSE file for details

---

## Support

- **Issues**: [GitHub Issues](https://github.com/Brayzonn/deploy-agent/issues)
- **Docs**: This README
- **Examples**: See `internal/config/repos.go` for configuration examples

---
