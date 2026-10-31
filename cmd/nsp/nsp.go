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

	"github.com/codingconcepts/next-slide-please/internal/desktop"
	"github.com/codingconcepts/next-slide-please/internal/keys"
	"github.com/codingconcepts/next-slide-please/internal/present"
	nspsignal "github.com/codingconcepts/next-slide-please/internal/signal"
	"github.com/codingconcepts/next-slide-please/internal/speak"
	"github.com/codingconcepts/next-slide-please/internal/target"
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
	presenterCmd.Flags().Bool("targeted", true, "Send keys to the slide application rather than whichever window has focus")
	presenterCmd.Flags().String("target", "", "Name of the slide application (defaults to the last one you used)")
	presenterCmd.Flags().Bool("hotkeys", true, "Steer the speaker list with ctrl+option from any window")

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

	targeted, err := cmd.Flags().GetBool("targeted")
	if err != nil {
		return fmt.Errorf("reading targeted flag: %w", err)
	}

	targetName, err := cmd.Flags().GetString("target")
	if err != nil {
		return fmt.Errorf("reading target flag: %w", err)
	}

	hotkeys, err := cmd.Flags().GetBool("hotkeys")
	if err != nil {
		return fmt.Errorf("reading hotkeys flag: %w", err)
	}

	if targetName != "" && !targeted {
		return fmt.Errorf("--target names an application to send keys to, which --targeted=false turns off")
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

	press, options, err := presenting(ctx, dryRun, targeted, targetName, hotkeys)
	if err != nil {
		return err
	}

	session, err := present.New(ctx, server, code, press)
	if err != nil {
		return err
	}

	go session.Run(ctx)

	return run(ctx, tui.NewPresenter(session, options))
}

// presenting settles the two questions that decide whether the presenter can
// use their own list during a talk: where a speaker's keys are typed, and
// whether the list can be steered without being brought to the front.
//
// Neither is load bearing. A target that cannot be worked out falls back to
// typing into the focused window, and hotkeys that the operating system will
// not grant are reported in the list rather than refused here: both leave a
// presenter exactly where they were before either existed.
func presenting(
	ctx context.Context,
	dryRun, targeted bool,
	targetName string,
	hotkeys bool,
) (present.Presser, tui.PresenterOptions, error) {
	options := tui.PresenterOptions{DryRun: dryRun}

	if hotkeys && desktop.Supported() {
		chords, err := desktop.Listen(ctx, tui.HotkeyKeys)
		if err != nil {
			options.HotkeyErr = err
		} else {
			options.Hotkeys = chords
		}
	}

	// Under --dry-run nothing is pressed, so there is nowhere for a key to go
	// and nothing to aim.
	if dryRun {
		return nil, options, nil
	}

	if !targeted {
		return keys.Press, options, nil
	}

	tracker, err := target.New(targetName)
	if err != nil {
		// A named application that's  not running is a typo or a forgotten
		// launch, and silently following focus instead would be a surprise
		// in the middle of a talk.
		if targetName != "" {
			return nil, options, err
		}

		return keys.Press, options, nil
	}

	go tracker.Run(ctx)

	options.Target = func() (string, bool) {
		app, ok := tracker.App()
		return app.Name, ok
	}

	return tracker.Press, options, nil
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
