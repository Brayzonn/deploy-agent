package config

import (
	"fmt"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

func GetRepoConfig(repoName, repoOwner string) (*types.RepoConfig, error) {
	configs := map[string]*types.RepoConfig{
		"zoneyhub": {
			Name:        "zoneyhub",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "zoneyhub"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "zoneyhub"),
			ProjectType: types.ProjectTypeClient,
			FullStack:   false,
			ClientDir:   "client",
			ServerDir:   "server",
			ServerEntry: "app.js",
		},
		"my-music-stats": {
			Name:         "my-music-stats",
			RepoDir:      fmt.Sprintf("/home/zoney/%s", "my-music-stats"),
			WebRoot:      "/var/www/html/weeklies",
			ProjectType:  types.ProjectTypeAPITS,
			FullStack:    true,
			ClientDir:    "client",
			ServerDir:    "server",
			ServerEntry:  "src/main.js",
			PM2Ecosystem: "ecosystem.config.js",
		},
		"URL-Shortener-App": {
			Name:        "URL-Shortener-App",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "URL-Shortener-App"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "URL-Shortener-App"),
			ProjectType: types.ProjectTypeAPIJS,
			FullStack:   false,
			ClientDir:   "client",
			ServerDir:   "server",
			ServerEntry: "app.js",
		},
		"MINES": {
			Name:                "MINES",
			RepoDir:             fmt.Sprintf("/home/zoney/%s", "MINES"),
			ProjectType:         types.ProjectTypeDocker,
			UseDocker:           true,
			DockerComposeFile:   "docker-compose.prod.yml",
			DockerEnvFile:       ".env.production",
			ServerDir:           "server",
			RequiresMigrations:  true,
			MigrationCommand:    "npm run db:migrate",
			InfraServices:       []string{"postgres", "redis"},
			Domain:              "api.mines.zoneyhub.com",
			Port:                4600,
			HealthCheckURL:      "http://localhost:4600/api/v1/health",
			HealthCheckTimeout:  30,
			ClientDir:           "client",
			ClientDomain:        "mines.zoneyhub.com",
			ClientDomainAliases: []string{},
			ClientWebRoot:       "/var/www/html/MINES",
		},
		"notifykit": {
			Name:               "notifykit",
			RepoDir:            fmt.Sprintf("/home/zoney/%s", "notifykit"),
			ProjectType:        types.ProjectTypeDocker,
			UseDocker:          true,
			DockerComposeFile:  "docker-compose.prod.yml",
			DockerEnvFile:      ".env.production",
			RequiresMigrations: true,
			MigrationCommand:   "npx prisma migrate deploy",
			InfraServices:      []string{"db"},
			Domain:             "api.notifykit.dev",
			Port:               5932,
			HealthCheckURL:     "http://localhost:5932/api/v1/health",
			HealthCheckTimeout: 30,
		},
		"notifykit-web": {
			Name:        "notifykit-web",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "notifykit-web"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "notifykit-web"),
			ProjectType: types.ProjectTypeClient,
			FullStack:   false,
			ClientDir:   "client",
			Domain:      "notifykit.dev",                    
    		DomainAliases: []string{"www.notifykit.dev"},  
		},
		"notifykit-admin": {
			Name:        "notifykit-admin",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "notifykit-admin"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "notifykit-admin"),
			ProjectType: types.ProjectTypeClient,
			FullStack:   false,
			ClientDir:   ".",
			Domain:      "admin.notifykit.dev",                   
    		DomainAliases: []string{},  
		},
		"NotifyKit-Docs": {
			Name:          "NotifyKit-Docs",
			RepoDir:       fmt.Sprintf("/home/zoney/%s", "NotifyKit-Docs"),
			WebRoot:       "/var/www/html/NotifyKit-Docs",
			ProjectType:   types.ProjectTypeClient,
			FullStack:     false,
			ClientDir:     ".",
			Domain:        "docs.notifykit.dev",
			DomainAliases: []string{},
		},
	}

	if config, exists := configs[repoName]; exists {
		return config, nil
	}

	return &types.RepoConfig{
		Name:        repoName,
		RepoDir:     fmt.Sprintf("/home/%s/%s", repoOwner, repoName),
		WebRoot:     fmt.Sprintf("/var/www/html/%s", repoName),
		ProjectType: types.ProjectTypeClient,
		FullStack:   false,
		ClientDir:   "client",
		ServerDir:   "server",
		ServerEntry: "app.js",
	}, nil
}