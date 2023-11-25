package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"github.com/predakanga/image-updater-webhook/pkg"
	"time"
)

const localConfigName = ".image-updater.conf"
const globalConfigPath = "/etc/image-updater.conf"
const baseLogLevel = log.InfoLevel

var cfgFile string
var verbosity int
var flagValues = make(map[string]interface{})

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   os.Args[0],
	Short: "Webhook server to update image manifests in git repos",

	Run: func(cmd *cobra.Command, args []string) {
		// Bump up the log level if requested
		desiredLevel := baseLogLevel
		if verbosity > 0 {
			desiredLevel = baseLogLevel + log.Level(verbosity)
			if desiredLevel > log.TraceLevel {
				desiredLevel = log.TraceLevel
			}
		}
		log.SetLevel(desiredLevel)
		log.Infof("Log level: %v", desiredLevel)

		// Then search for a valid config file
		configPath := cfgFile
		if configPath == "" {
			configPath = globalConfigPath
			if homeDir, err := os.UserHomeDir(); err == nil {
				homeCfg := path.Join(homeDir, localConfigName)
				if _, err := os.Stat(homeCfg); err == nil {
					configPath = homeCfg
				}
			}
		}

		cfg, err := loadConfig(configPath, cmd.Flags())
		if err != nil {
			log.WithError(err).Fatal("Config file loading failed")
		}
		// Before anything else, update our log level if required
		if cfg.LogLevel != "" {
			newLevel, err := log.ParseLevel(cfg.LogLevel)
			if err != nil {
				log.WithError(err).Fatal("Invalid log level provided")
			}
			if newLevel > desiredLevel {
				log.SetLevel(newLevel)
				log.Debugf("Log level is now: %v", newLevel)
			}
		}
		log.Debugf("Config loaded: %+v", cfg)

		// Create the app server
		srv := pkg.NewServer(cfg)
		// Set up interrupts
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt)
		go func() {
			<-sigChan
			ctx, cancel := context.WithTimeout(context.TODO(), 30*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				os.Exit(0)
			}
		}()
		// And run forever
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Fatal("Metric server initialization failed")
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(version string) {
	rootCmd.Version = version
	cobra.CheckErr(rootCmd.Execute())
}

// rootCmd represents the base command when called without any subcommands

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is $HOME/.image-updater.conf or /etc/image-updater.conf)")
	rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "Increase log verbosity")

	pkg.AddFlags(rootCmd, flagValues)
}

func initConfig() {
	// Apply env vars
	allFlags := rootCmd.Flags()
	for _, envVar := range os.Environ() {
		if !strings.HasPrefix(envVar, "IMAGE_UPDATER_") {
			continue
		}
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		flagName := strings.ReplaceAll(strings.ToLower(key[14:]), "_", "-")

		if allFlags.Lookup(flagName) != nil {
			_ = allFlags.Set(flagName, val)
		}
	}
}

func loadConfig(configPath string, flags *pflag.FlagSet) (pkg.Config, error) {
	var toRet pkg.Config

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

	diags = gohcl.DecodeBody(cfgBody.Body, nil, &toRet)
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
