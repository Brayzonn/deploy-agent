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
		"notifykit": {
			Name:        "notifykit",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "notifykit"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "notifykit"),
			ProjectType: types.ProjectTypeAPITS,
			FullStack:   false,
			ServerDir:   "server",
			ServerEntry: "src/main.js",
			Domain:       "api.notifykit.dev",               
    		Port:         3000,            
			PM2Ecosystem: "ecosystem.config.js",
		},
		"notifykit-web": {
			Name:        "notifykit-web",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "notifykit-web"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "notifykit-web"),
			ProjectType: types.ProjectTypeClient,
			FullStack:   false,
			ClientDir:   "client",
			Domain:        "notifykit.dev",                    
    		DomainAliases: []string{"www.notifykit.dev"},  
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