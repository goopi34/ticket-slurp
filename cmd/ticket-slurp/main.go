// Package main is the entrypoint for the ticket-slurp CLI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ticket-slurp/internal/analysis"
	"ticket-slurp/internal/atlassian"
	"ticket-slurp/internal/config"
	"ticket-slurp/internal/pipeline"
	"ticket-slurp/internal/report"
	"ticket-slurp/internal/slack"
)

// Build-time variables injected by goreleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var cfgFile string

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ticket-slurp",
		Short: "Identify untracked engineering work from Slack conversations",
		Long: `ticket-slurp harvests messages from Slack conversations you participated in,
runs AI analysis to surface potential engineering tickets, cross-references
against Jira to exclude already-tracked work, and reports the delta.`,
		Version:      fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./ticket-slurp.yaml)")

	cobra.OnInitialize(func() {
		initConfig(cfgFile)
	})

	root.AddCommand(newRunCmd())

	return root
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the ticket identification pipeline",
		RunE:  runPipeline,
	}
}

func runPipeline(cmd *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load and validate configuration.
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Build report format.
	reporter, err := report.New(cfg.Output.Format)
	if err != nil {
		return fmt.Errorf("create reporter: %w", err)
	}

	// Build Slack client.
	slackClient := slack.NewHTTPClient(
		cfg.Slack.XOXC,
		cfg.Slack.XOXD,
		cfg.Channels.Whitelist,
		cfg.Channels.Blacklist,
		nil,
	)

	// Build LLM generator and analyzer.
	gen, err := analysis.NewGollmGenerator(cfg.LLM)
	if err != nil {
		return fmt.Errorf("create LLM generator: %w", err)
	}
	analyzer := analysis.NewGollmAnalyzer(gen, cfg.LLM.SystemPrompt)

	// Build Atlassian MCP client.
	atlassianClient := atlassian.NewMCPClient(cfg.Atlassian.MCPURL, cfg.Atlassian.ProjectKeys, nil)

	// Wire the pipeline.
	runner := pipeline.New(cfg, slackClient, analyzer, atlassianClient, reporter, logger)

	// Set up signal-aware context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run the pipeline, writing the report to stdout.
	if err := runner.Run(ctx, cmd.OutOrStdout()); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("interrupted: %w", ctx.Err())
		}
		return err
	}

	return nil
}

func initConfig(cfgFile string) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("ticket-slurp")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		// Config file not required at init time; errors surfaced during run.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintln(os.Stderr, "config warning:", err)
		}
	}
}
