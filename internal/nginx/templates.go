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
and adds standard security headers, gzip, and asset caching. When
spaMetaProxyPath is set (and a port is available to proxy to), the
fallback instead proxies unmatched paths to that path on the API for
server-side meta injection, falling back to index.html if the API is
unreachable.
*/
func (n *Site) generateClientConfig() string {
	serverNames := n.domain
	if len(n.domainAliases) > 0 {
		serverNames = n.domain + " " + strings.Join(n.domainAliases, " ")
	}

	rootLocation := `location / {
        try_files $uri $uri/ /index.html;
    }`

	if n.spaMetaProxyPath != "" && n.port > 0 {
		rootLocation = fmt.Sprintf(`location / {
        try_files $uri @spa_fallback;
    }

    location @spa_fallback {
        proxy_pass http://localhost:%d%s$request_uri;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_intercept_errors on;
        error_page 502 503 504 = /index.html;
    }`, n.port, n.spaMetaProxyPath)
	}

	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;
    root %s;
    index index.html;

    %s

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
}`, serverNames, n.webRoot, rootLocation)
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

	maxUpload := ""
	if n.nginxMaxUpload > 0 {
		maxUpload = fmt.Sprintf("    client_max_body_size %dm;\n\n", n.nginxMaxUpload)
	}

	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;

%s    location / {
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
}`, serverNames, maxUpload, n.port)
}
