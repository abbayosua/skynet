package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/abbayosua/skynet/internal/config"
	"github.com/abbayosua/skynet/internal/scheduler"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:     "schedule",
	Aliases: []string{"sched", "cron"},
	Short:   "Manage scheduled recurring jobs",
	Long: `Manage scheduled recurring jobs that run AI prompts at specified intervals.
Jobs run as non-interactive background tasks with auto-approved permissions.`,
}

var (
	scheduleAddName      string
	scheduleAddInterval  string
	scheduleAddPrompt    string
	scheduleAddDesc      string
	scheduleAddTimeout   int
	scheduleAddContinue  bool
	scheduleListAll      bool
	scheduleJSON         bool
	scheduleAddEnabled   bool
)

var scheduleAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new scheduled job",
	Example: `  skynet schedule add --name daily-check --interval hourly --prompt "check server status"
  skynet schedule add --name tester --interval 15m --prompt "testing" --continue
  skynet schedule add --name weekly-report --interval "24h" --prompt "summarize this week" --timeout 300`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sched, err := newSchedulerFromConfig(cmd)
		if err != nil {
			return err
		}

		job := &scheduler.Job{
			Name:        scheduleAddName,
			Prompt:      scheduleAddPrompt,
			Description: scheduleAddDesc,
			Interval:    scheduleAddInterval,
			TimeoutSec:  scheduleAddTimeout,
			Continue:    scheduleAddContinue,
			Enabled:     true,
		}
		if err := sched.AddJob(job); err != nil {
			return fmt.Errorf("failed to add job: %w", err)
		}

		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(charmtone.Charple)
		fmt.Printf("%s %s (%s) — interval: %s\n",
			labelStyle.Render("✓"),
			job.Name, job.ID, job.Interval)
		return nil
	},
}

var scheduleListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all scheduled jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		sched, err := newSchedulerFromConfig(cmd)
		if err != nil {
			return err
		}

		jobs := sched.ListJobs()
		if len(jobs) == 0 {
			fmt.Println("No scheduled jobs.")
			return nil
		}

		if scheduleJSON {
			for _, j := range jobs {
				if !scheduleListAll && !j.Enabled {
					continue
				}
				cont := ""
			if j.Continue {
				cont = "continue"
			}
			fmt.Printf("%s\t%s\t%s\t%s\truns=%d\tlast=%s\n",
					j.ID, j.Name, j.Interval, cont, j.RunCount, j.LastRunAt.Format(time.RFC3339))
			}
			return nil
		}

		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(charmtone.Charple)
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		for _, j := range jobs {
			if !scheduleListAll && !j.Enabled {
				continue
			}
			status := mutedStyle.Render("active")
			if !j.Enabled {
				status = mutedStyle.Render("disabled")
			}
			mode := ""
			if j.Continue {
				mode = " [continue]"
			}
			fmt.Printf("%s (%s)%s\n", labelStyle.Render(j.Name), j.ID, mode)
			fmt.Printf("  Interval: %s | Status: %s | Runs: %d\n", j.Interval, status, j.RunCount)
			if j.LastRunAt.IsZero() {
				fmt.Printf("  Last run: never\n")
			} else {
				fmt.Printf("  Last run: %s\n", j.LastRunAt.Format(time.RFC3339))
				fmt.Printf("  Result: %s\n", j.LastResult)
			}
			fmt.Println()
		}
		return nil
	},
}

var scheduleGetCmd = &cobra.Command{
	Use:     "get <id>",
	Short:   "Show details of a scheduled job",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sched, err := newSchedulerFromConfig(cmd)
		if err != nil {
			return err
		}

		job, ok := sched.GetJob(args[0])
		if !ok {
			return fmt.Errorf("job %q not found", args[0])
		}

		status := "active"
		if !job.Enabled {
			status = "disabled"
		}

		if scheduleJSON {
			fmt.Printf("id=%s name=%s prompt=%s interval=%s status=%s continue=%v timeout=%d runs=%d\n",
				job.ID, job.Name, job.Prompt, job.Interval, status, job.Continue, job.TimeoutSec, job.RunCount)
			return nil
		}

		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(charmtone.Charple)
		fmt.Printf("%s: %s\n", labelStyle.Render("Name"), job.Name)
		fmt.Printf("%s:   %s\n", labelStyle.Render("ID"), job.ID)
		fmt.Printf("%s:    %s\n", labelStyle.Render("Prompt"), job.Prompt)
		fmt.Printf("%s: %s\n", labelStyle.Render("Interval"), job.Interval)
		mode := ""
		if job.Continue {
			mode = " [continue]"
		}
		fmt.Printf("%s:  %s%s\n", labelStyle.Render("Status"), status, mode)
		fmt.Printf("%s: %d\n", labelStyle.Render("Timeout"), job.TimeoutSec)
		fmt.Printf("%s: %d\n", labelStyle.Render("Runs"), job.RunCount)
		fmt.Printf("%s:   %s\n", labelStyle.Render("Created"), job.CreatedAt.Format(time.RFC3339))
		fmt.Printf("%s:   %s\n", labelStyle.Render("Updated"), job.UpdatedAt.Format(time.RFC3339))
		if !job.LastRunAt.IsZero() {
			fmt.Printf("%s:  %s\n", labelStyle.Render("Last run"), job.LastRunAt.Format(time.RFC3339))
			fmt.Printf("%s: %s\n", labelStyle.Render("Result"), job.LastResult)
		}
		return nil
	},
}

var scheduleUpdateCmd = &cobra.Command{
	Use:     "update <id>",
	Short:   "Update a scheduled job",
	Args:    cobra.ExactArgs(1),
	Example: `  skynet schedule update daily-check --interval 6h --enabled false`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sched, err := newSchedulerFromConfig(cmd)
		if err != nil {
			return err
		}

		existing, ok := sched.GetJob(args[0])
		if !ok {
			return fmt.Errorf("job %q not found", args[0])
		}

		if scheduleAddName != "" {
			existing.Name = scheduleAddName
		}
		if scheduleAddPrompt != "" {
			existing.Prompt = scheduleAddPrompt
		}
		if scheduleAddDesc != "" {
			existing.Description = scheduleAddDesc
		}
		if scheduleAddInterval != "" {
			existing.Interval = scheduleAddInterval
		}
		if cmd.Flags().Changed("timeout") {
			existing.TimeoutSec = scheduleAddTimeout
		}
		if cmd.Flags().Changed("enabled") {
			existing.Enabled = scheduleAddEnabled
		}
		if cmd.Flags().Changed("continue") {
			existing.Continue = scheduleAddContinue
		}

		if err := sched.UpdateJob(existing); err != nil {
			return fmt.Errorf("failed to update job: %w", err)
		}

		fmt.Printf("Updated %s (%s)\n", existing.Name, existing.ID)
		return nil
	},
}

var scheduleDeleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a scheduled job",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sched, err := newSchedulerFromConfig(cmd)
		if err != nil {
			return err
		}

		if err := sched.DeleteJob(args[0]); err != nil {
			return fmt.Errorf("failed to delete job: %w", err)
		}

		fmt.Printf("Deleted job %s\n", args[0])
		return nil
	},
}

func init() {
	scheduleAddCmd.Flags().StringVarP(&scheduleAddName, "name", "n", "", "Job name (required)")
	scheduleAddCmd.Flags().StringVarP(&scheduleAddInterval, "interval", "i", "", `Interval (required): "hourly", "daily", "30m", "6h"`)
	scheduleAddCmd.Flags().StringVarP(&scheduleAddPrompt, "prompt", "p", "", "Prompt to execute (required)")
	scheduleAddCmd.Flags().StringVar(&scheduleAddDesc, "description", "", "Optional description")
	scheduleAddCmd.Flags().IntVar(&scheduleAddTimeout, "timeout", 0, "Timeout in seconds")
	scheduleAddCmd.Flags().BoolVar(&scheduleAddContinue, "continue", false, "Auto-continue: improve/test/code tanpa henti")
	scheduleAddCmd.MarkFlagRequired("name")
	scheduleAddCmd.MarkFlagRequired("interval")
	scheduleAddCmd.MarkFlagRequired("prompt")

	scheduleUpdateCmd.Flags().StringVarP(&scheduleAddName, "name", "n", "", "New job name")
	scheduleUpdateCmd.Flags().StringVarP(&scheduleAddInterval, "interval", "i", "", "New interval")
	scheduleUpdateCmd.Flags().StringVarP(&scheduleAddPrompt, "prompt", "p", "", "New prompt")
	scheduleUpdateCmd.Flags().StringVar(&scheduleAddDesc, "description", "", "New description")
	scheduleUpdateCmd.Flags().IntVar(&scheduleAddTimeout, "timeout", 0, "New timeout in seconds")
	scheduleUpdateCmd.Flags().BoolVar(&scheduleAddEnabled, "enabled", true, "Enable or disable")
	scheduleUpdateCmd.Flags().BoolVar(&scheduleAddContinue, "continue", false, "Auto-continue toggle")

	scheduleListCmd.Flags().BoolVarP(&scheduleListAll, "all", "a", false, "Include disabled jobs")

	scheduleGetCmd.Flags().BoolVarP(&scheduleJSON, "json", "j", false, "JSON output")
	scheduleListCmd.Flags().BoolVarP(&scheduleJSON, "json", "j", false, "JSON output")

	scheduleCmd.AddCommand(
		scheduleAddCmd,
		scheduleListCmd,
		scheduleGetCmd,
		scheduleUpdateCmd,
		scheduleDeleteCmd,
	)
}

func newSchedulerFromConfig(cmd *cobra.Command) (*scheduler.Scheduler, error) {
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	store, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg := store.Config()

	schedDir := filepath.Join(cfg.Options.DataDirectory, "scheduler")
	schedStore, err := scheduler.NewStore(schedDir)
	if err != nil {
		return nil, fmt.Errorf("scheduler store: %w", err)
	}
	return scheduler.NewScheduler(schedStore), nil
}
