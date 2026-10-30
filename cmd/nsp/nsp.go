package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/codingconcepts/next-slide-please/internal/keys"
	"github.com/codingconcepts/next-slide-please/internal/present"
	nspsignal "github.com/codingconcepts/next-slide-please/internal/signal"
	"github.com/codingconcepts/next-slide-please/internal/speak"
	"github.com/codingconcepts/next-slide-please/internal/tui"
)

var version string

func main() {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Display the application's version",
		Run: func(*cobra.Command, []string) {
			fmt.Println(version)
		},
	}

	presenterCmd := &cobra.Command{
		Use:     "present",
		Short:   "Start Next-Slide-Please as the presenter (the person with the slides)",
		RunE:    executePresent,
		Args:    cobra.ExactArgs(0),
		Example: "nsp present\nnsp present --code 0PpTrMetgz",
	}
	presenterCmd.Flags().String("code", "", "Code speakers will join with (generated if omitted)")
	presenterCmd.Flags().String("server", nspsignal.DefaultServer, "Signalling server to meet speakers through")
	presenterCmd.Flags().Bool("dry-run", false, "Show the keys received instead of pressing them")

	speakerCmd := &cobra.Command{
		Use:     "speak",
		Short:   "Start Next-Slide-Please as a speaker (the person asking for the next slide)",
		RunE:    executeSpeak,
		Args:    cobra.ExactArgs(0),
		Example: "nsp speak --code 0PpTrMetgz --name alice",
	}
	speakerCmd.Flags().String("code", "", "Code shown by the presenter")
	speakerCmd.Flags().String("server", nspsignal.DefaultServer, "Signalling server to meet the presenter through")
	speakerCmd.Flags().String("name", "", "Name to show in the presenter's list")
	speakerCmd.MarkFlagRequired("code")
	speakerCmd.MarkFlagRequired("name")

	rootCmd := &cobra.Command{
		Use:          "nsp",
		Short:        "Let someone else drive your slides",
		SilenceUsage: true,
	}
	rootCmd.AddCommand(versionCmd, presenterCmd, speakerCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func executePresent(cmd *cobra.Command, args []string) error {
	code, server, err := connectionFlags(cmd)
	if err != nil {
		return err
	}

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("reading dry-run flag: %w", err)
	}

	// Fail fast (rather than during a presentation).
	if !dryRun && !keys.Supported() {
		return fmt.Errorf(
			"presenting is not supported on this platform yet; use --dry-run to test everything but the keypress",
		)
	}

	if code == "" {
		if code, err = generateID(10); err != nil {
			return fmt.Errorf("generating code: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	session, err := present.New(ctx, server, code, presser(dryRun))
	if err != nil {
		return err
	}

	go session.Run(ctx)

	return run(ctx, tui.NewPresenter(session, dryRun))
}

func executeSpeak(cmd *cobra.Command, args []string) error {
	code, server, err := connectionFlags(cmd)
	if err != nil {
		return err
	}

	name, err := cmd.Flags().GetString("name")
	if err != nil {
		return fmt.Errorf("reading name flag: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Connecting before the TUI starts keeps failures readable.
	fmt.Printf("Connecting to the presenter on code %q...\n", code)

	session, err := speak.Connect(ctx, server, code, name)
	if err != nil {
		return err
	}
	defer session.Close()

	return run(ctx, tui.NewSpeaker(session))
}

// run drives a bubbletea program and makes sure a signal tears it down, so
// the terminal is always restored on the way out.
func run(ctx context.Context, p *tea.Program) error {
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running the interface: %w", err)
	}

	return nil
}

// presser decides what a key arriving from a speaker actually does. Under
// --dry-run there is no presser at all, which is what makes it safe to run
// both halves on one machine.
func presser(dryRun bool) present.Presser {
	if dryRun {
		return nil
	}

	return keys.Press
}

func connectionFlags(cmd *cobra.Command) (string, string, error) {
	code, err := cmd.Flags().GetString("code")
	if err != nil {
		return "", "", fmt.Errorf("reading code flag: %w", err)
	}

	server, err := cmd.Flags().GetString("server")
	if err != nil {
		return "", "", fmt.Errorf("reading server flag: %w", err)
	}

	return code, nspsignal.Server(server), nil
}

func generateID(size int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating random id: %w", err)
	}

	id := make([]byte, 0, size)
	for _, b := range buf {
		id = append(id, chars[int(b)%len(chars)])
	}

	return string(id), nil
}
