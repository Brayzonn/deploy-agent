# Deploy Agent

Go-based deployment automation tool for managing multiple repositories via GitHub webhooks

## Features

- Git operations (fetch, pull, stash, restore)
- Automatic dependency installation (npm ci/install)
- TypeScript/JavaScript builds with timeout handling
- PM2 process management (start, restart, health checks)
- Frontend deployment to web root with nginx restart
- Fullstack deployment support (backend + frontend)
- Automatic backups (keeps last 5)
- Structured logging (console + file)
- Error handling with rollback support

## How It Works

```
GitHub Push → Webhook Server → Deploy Agent → Deploys Your App
```

The webhook server (Node.js) receives GitHub events, validates signatures, and executes this binary with environment variables set.

## Installation

### On Your Server:

```bash
# Clone and build
git clone https://github.com/Brayzonn/deploy-agent
cd deploy-agent
go build -o deploy-agent

# Make executable and move to deployment location
chmod +x deploy-agent
sudo mv deploy-agent /usr/local/bin/deploy-agent
```

### For Cross-Platform Build (from Mac to Linux):

```bash
# Build for Linux server from your Mac
GOOS=linux GOARCH=amd64 go build -o deploy-agent

# Upload to server
scp deploy-agent user@your-server:/usr/local/bin/
```

## Usage

### With Webhook Server (Production):

The deploy-agent is called automatically by your webhook server. Configure your webhook server's `.env`:

```bash
DEPLOYMENT_SCRIPT=/usr/local/bin/deploy-agent
```

When GitHub pushes occur, the webhook server sets these env vars and executes the agent:

```javascript
// Webhook server automatically sets:
GITHUB_REPO_NAME = notifykit;
GITHUB_BRANCH = main;
GITHUB_REPO_OWNER = Brayzonn;
GITHUB_PUSHER = Brayzonn;
GITHUB_COMMIT = abc123def456;
GITHUB_REPO_FULL_NAME = Brayzonn / notifykit;
```

### Manual Testing:

```bash
export GITHUB_REPO_NAME="notifykit"
export GITHUB_BRANCH="main"
export GITHUB_REPO_OWNER="Brayzonn"
export GITHUB_PUSHER="Brayzonn"
export GITHUB_COMMIT="abc123def456"
export GITHUB_REPO_FULL_NAME="Brayzonn/notifykit"

./deploy-agent
```

## Configuration

### Adding a New Repository:

Edit `internal/config/repos.go`:

```go
"your-repo-name": {
    Name:        "your-repo-name",
    RepoDir:     "/home/user/your-repo-name",
    WebRoot:     "/var/www/html/your-repo-name",
    ProjectType: types.ProjectTypeClient, // or API_JS, API_TS
    FullStack:   false,
    ClientDir:   "client",
    ServerDir:   "server",
    ServerEntry: "main.js",
    PM2Ecosystem: "ecosystem.config.js", // for fullstack/backend
},
```

### Project Types:

| Type     | Description                       | Build Step      | Deployment                       |
| -------- | --------------------------------- | --------------- | -------------------------------- |
| `CLIENT` | Frontend only (React, Vue, Vite)  | `npm run build` | Copy to web root + restart nginx |
| `API_JS` | Node.js backend (no TypeScript)   | None            | PM2 restart                      |
| `API_TS` | TypeScript backend (NestJS, etc.) | `npm run build` | PM2 restart                      |

### Fullstack Projects:

Set `FullStack: true` to deploy both backend (PM2) and frontend (web root) in sequence.

## Directory Structure

```
├── internal/
│   ├── build/       # npm install, build, validation
│   ├── config/      # repo configs, env validation
│   ├── deploy/      # deployment orchestration
│   ├── git/         # git operations
│   ├── logger/      # colored logging
│   └── pm2/         # PM2 process management
├── pkg/types/       # shared types
└── main.go          # entry point
```

## Logs

- **Console**: Color-coded output (blue=info, green=success, yellow=warning, red=error)
- **Files**: `~/logs/deployments/deployment_YYYYMMDD_HHMMSS_PID.log`

## Requirements

- **Go 1.21+** (for building)
- **Git** (on deployment server)
- **Node.js & npm** (for builds)
- **PM2** (for backend deployments)
- **Nginx** (for frontend deployments)

## Webhook Server

This agent works with a webhook server (Node.js/Express) that:

1. Receives GitHub webhook events
2. Validates signatures
3. Executes this binary with env vars

See: [webhook-server setup guide](#) (add your webhook server repo link)

## License

MIT
