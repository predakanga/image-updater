package pkg

import (
	"github.com/spf13/cobra"
)

type Config struct {
	ListenAddr string `mapstructure:"listen_address" hcl:"listen_address,optional"`
	LogLevel   string `hcl:"log_level,optional"`

	Repositories []RepositoryConfig `hcl:"repository,block"`
	Deployments  []DeploymentConfig `hcl:"deployment,block"`
}

type RepositoryConfig struct {
	Name string `hcl:"name,label"`

	Url      string `hcl:"url"`
	Branch   string `hcl:"branch,optional"`
	Username string `hcl:"username"`
	Password string `hcl:"password"`

	CommitterName  string `hcl:"committer_name"`
	CommitterEmail string `hcl:"committer_email"`
}

type DeploymentConfig struct {
	Name       string   `hcl:"name,label"`
	Repository string   `hcl:"repository"`
	Path       string   `hcl:"path,optional"`
	Images     []string `hcl:"image"`
}

func AddFlags(cmd *cobra.Command, flagValues map[string]interface{}) {
	flags := cmd.Flags()

	flagValues["listen-addr"] = flags.StringP("listen-addr", "l", ":8080", "Metrics HTTP server address")
}
