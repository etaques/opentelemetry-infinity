package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "embed"

	"github.com/leoparente/otlpinf/config"
	"github.com/leoparente/otlpinf/otlpinf"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

//go:embed VERSION
var version string

var (
	Debug      bool
	ServerHost string
	ServerPort uint64
)

func Run(cmd *cobra.Command, args []string) {

	initConfig()

	// configuration
	var config config.Config
	config.Version = version
	config.OtlpInf.Debug = Debug
	config.OtlpInf.ServerHost = ServerHost
	config.OtlpInf.ServerPort = ServerPort

	err := viper.Unmarshal(&config)
	if err != nil {
		cobra.CheckErr(fmt.Errorf("opentelemetry-infinity start up error (config): %w", err))
		os.Exit(1)
	}

	// logger
	var logger *zap.Logger
	atomicLevel := zap.NewAtomicLevel()
	if Debug {
		atomicLevel.SetLevel(zap.DebugLevel)
	} else {
		atomicLevel.SetLevel(zap.InfoLevel)
	}
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		os.Stdout,
		atomicLevel,
	)
	logger = zap.New(core, zap.AddCaller())
	defer func(logger *zap.Logger) {
		_ = logger.Sync()
	}(logger)

	// new otlpinf
	a, err := otlpinf.New(logger, &config)
	if err != nil {
		logger.Error("otlpinf start up error", zap.Error(err))
		os.Exit(1)
	}

	// handle signals
	done := make(chan bool, 1)
	rootCtx, cancelFunc := context.WithCancel(context.WithValue(context.Background(), "routine", "mainRoutine"))

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
		for {
			select {
			case <-sigs:
				logger.Warn("stop signal received, stopping otlpinf")
				a.Stop(rootCtx)
				cancelFunc()
			case <-rootCtx.Done():
				logger.Warn("mainRoutine context cancelled")
				done <- true
				return
			}
		}
	}()

	// start otlpinf
	err = a.Start(rootCtx, cancelFunc)
	if err != nil {
		logger.Error("otlpinf startup error", zap.Error(err))
		os.Exit(1)
	}

	<-done
}

func initConfig() {
	v := viper.New()
	v.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	v.SetEnvKeyReplacer(replacer)
	// note: viper seems to require a default (or a BindEnv) to be overridden by environment variables
	v.SetDefault("otlp_inf.debug", false)
	v.SetDefault("otlp_inf.server_host", "localhost")
	v.SetDefault("otlp_inf.server_port", 10222)
	cobra.CheckErr(viper.MergeConfigMap(v.AllSettings()))
}

func main() {

	rootCmd := &cobra.Command{
		Use: "opentelemetry-infinity",
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run opentelemetry-infinity",
		Long:  `Run opentelemetry-infinity`,
		Run:   Run,
	}

	runCmd.PersistentFlags().BoolVarP(&Debug, "debug", "d", false, "Enable verbose (debug level) output")
	runCmd.PersistentFlags().StringVarP(&ServerHost, "server_host", "a", "localhost", "Define REST Host")
	runCmd.PersistentFlags().Uint64VarP(&ServerPort, "server_port", "p", 10222, "Define REST Port")

	rootCmd.AddCommand(runCmd)
	rootCmd.Execute()
}
