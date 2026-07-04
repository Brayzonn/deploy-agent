package nginx

import (
	"fmt"
	"strings"
)

// ============================================================
// Config templates
// ============================================================

/*
generateClientConfig builds a static-file server block: serves
webRoot directly, falls back to index.html for client-side routing,
and adds standard security headers, gzip, and asset caching.
*/
func (n *Site) generateClientConfig() string {
	serverNames := n.domain
	if len(n.domainAliases) > 0 {
		serverNames = n.domain + " " + strings.Join(n.domainAliases, " ")
	}

	return fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    root %s;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_types text/plain text/css text/xml text/javascript application/json application/javascript application/xml+rss;

    # Cache static assets
    location ~* \.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }
}`, serverNames, n.webRoot)
}

/*
generateAPIConfig builds a reverse-proxy server block that forwards
requests to the app's local port, preserving headers needed for
WebSocket upgrades and client IP/protocol propagation.
*/
func (n *Site) generateAPIConfig() string {
	serverNames := n.domain
	if len(n.domainAliases) > 0 {
		serverNames = n.domain + " " + strings.Join(n.domainAliases, " ")
	}

	return fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    location / {
        proxy_pass http://localhost:%d;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }
}`, serverNames, n.port)
}
