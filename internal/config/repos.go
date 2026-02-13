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
			ServerEntry:  "main.js",
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
		"MEDHUB": {
			Name:        "MEDHUB",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "MEDHUB"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "MEDHUB"),
			ProjectType: types.ProjectTypeAPITS,
			FullStack:   false,
			ClientDir:   "client",
			ServerDir:   "server",
			ServerEntry: "app.js",
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