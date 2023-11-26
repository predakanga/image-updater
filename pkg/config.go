package pkg

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"os"
	"path"
)

type Config struct {
	ListenAddr string   `mapstructure:"listen_address" hcl:"listen_address,optional"`
	LogLevel   string   `hcl:"log_level,optional"`
	AllowedIPs []string `hcl:"allowed_ips,optional"`
	SecretKey  string   `hcl:"secret_key,optional"`
	ArgoToken  string   `hcl:"argocd_token"`
	ArgoUrl    string   `hcl:"argocd_url"`

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
	Name          string   `hcl:"name,label"`
	Repository    string   `hcl:"repository"`
	Path          string   `hcl:"path,optional"`
	Images        []string `hcl:"image"`
	CommitMessage string   `hcl:"message,optional"`
	ArgoName      string   `hcl:"argocd_app,optional"`
}

var flagValues = make(map[string]interface{})

func AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()

	flagValues["listen-addr"] = flags.StringP("listen-addr", "l", ":8080", "Metrics HTTP server address")
}

func LoadConfig(configPath string, flags *pflag.FlagSet) (Config, error) {
	var toRet Config

	// Parse the config file
	// NB: Done manually because hclsimple requires that the filename end in .hcl
	log.Debugf("Loading config file: %s", configPath)
	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		return toRet, fmt.Errorf("could not read config file: %w", err)
	}
	cfgBody, diags := hclsyntax.ParseConfig(cfgBytes, path.Base(configPath), hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return toRet, fmt.Errorf("could not parse config file: %w", diags)
	}

	// Start by populating the config with our default flags
	if err := mapstructure.Decode(flagValues, &toRet); err != nil {
		log.WithError(err).Fatalf("Could not create default config")
	}

	evalCtx := hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: map[string]function.Function{
			"env": envFunc,
		},
	}
	diags = gohcl.DecodeBody(cfgBody.Body, &evalCtx, &toRet)
	if diags.HasErrors() {
		return toRet, fmt.Errorf("invalid config file: %w", diags)
	}

	// Now that we have our config struct, merge any non-default flags with it
	// NB: Visit() only visits non-default flags
	changedFlags := make(map[string]interface{})
	flags.Visit(func(flag *pflag.Flag) {
		changedFlags[flag.Name] = flagValues[flag.Name]
	})
	if err := mapstructure.Decode(changedFlags, &toRet); err != nil {
		return toRet, fmt.Errorf("could not finalize config: %w", err)
	}

	return toRet, nil
}

var envFunc = function.New(&function.Spec{
	Description: "Returns an environment variable, or a default value if that variable is not set.",
	Params: []function.Parameter{
		{
			Name: "name",
			Type: cty.String,
		},
	},
	VarParam: &function.Parameter{
		Name: "default",
		Type: cty.String,
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if len(args) > 2 {
			return cty.NilVal, fmt.Errorf("expected 2 arguments at most")
		}

		value := os.Getenv(args[0].AsString())
		if value == "" && len(args) > 1 {
			value = args[1].AsString()
		}

		return cty.StringVal(value), nil
	},
})
