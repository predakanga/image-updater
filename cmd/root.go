package cmd

import (
	"context"
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
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

		cfg, err := pkg.LoadConfig(configPath, cmd.Flags())
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

	pkg.AddFlags(rootCmd)
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
