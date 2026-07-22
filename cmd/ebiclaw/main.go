// EbiClaw (Tsukasa) - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 EbiClaw contributors

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/agent"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/auth"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/cron"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/gateway"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/migrate"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/model"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/onboard"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/skills"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/status"
	"github.com/n-seiji/ebiclaw/cmd/ebiclaw/internal/version"
	"github.com/n-seiji/ebiclaw/pkg/config"
)

func NewEbiclawCommand() *cobra.Command {
	short := fmt.Sprintf("%s tsukasa - Personal AI Assistant %s\n\n", internal.Logo, config.GetVersion())

	cmd := &cobra.Command{
		Use:     "ebiclaw",
		Short:   short,
		Example: "ebiclaw version",
	}

	cmd.AddCommand(
		onboard.NewOnboardCommand(),
		agent.NewAgentCommand(),
		auth.NewAuthCommand(),
		gateway.NewGatewayCommand(),
		status.NewStatusCommand(),
		cron.NewCronCommand(),
		migrate.NewMigrateCommand(),
		skills.NewSkillsCommand(),
		model.NewModelCommand(),
		version.NewVersionCommand(),
	)

	return cmd
}

const (
	colorBlue = "\033[1;38;2;62;93;185m"
	colorRed  = "\033[1;38;2;213;70;70m"
	banner    = "\r\n" +
		colorBlue + "████████╗███████╗██╗   ██╗" + colorRed + "██╗  ██╗ █████╗ ███████╗ █████╗ \n" +
		colorBlue + "╚══██╔══╝██╔════╝██║   ██║" + colorRed + "██║ ██╔╝██╔══██╗██╔════╝██╔══██╗\n" +
		colorBlue + "   ██║   ███████╗██║   ██║" + colorRed + "█████╔╝ ███████║███████╗███████║\n" +
		colorBlue + "   ██║   ╚════██║██║   ██║" + colorRed + "██╔═██╗ ██╔══██║╚════██║██╔══██║\n" +
		colorBlue + "   ██║   ███████║╚██████╔╝" + colorRed + "██║  ██╗██║  ██║███████║██║  ██║\n" +
		colorBlue + "   ╚═╝   ╚══════╝ ╚═════╝ " + colorRed + "╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝\n " +
		"\033[0m\r\n"
)

func main() {
	fmt.Printf("%s", banner)

	tz_env := os.Getenv("TZ")
	if tz_env != "" {
		fmt.Println("TZ environment:", tz_env)
		zoneinfo_env := os.Getenv("ZONEINFO")
		fmt.Println("ZONEINFO environment:", zoneinfo_env)
		loc, err := time.LoadLocation(tz_env)
		if err != nil {
			fmt.Println("Error loading time zone:", err)
		} else {
			fmt.Println("Time zone loaded successfully:", loc)
			time.Local = loc //nolint:gosmopolitan // We intentionally set local timezone from TZ env
		}
	}

	cmd := NewEbiclawCommand()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
