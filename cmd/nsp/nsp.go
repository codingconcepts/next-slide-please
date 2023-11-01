package main

import (
	"crypto/rand"
	"fmt"
	"log"

	"github.com/go-vgo/robotgo"
	"github.com/nats-io/nats.go"
	hook "github.com/robotn/gohook"
	"github.com/spf13/cobra"
)

var (
	version string
)

const (
	natsServer = "nats://next-slide-please.fly.dev:4222"
)

func main() {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Display the application's version",
		Run:   executeVersion,
	}

	presenterCmd := &cobra.Command{
		Use:     "present",
		Short:   "Start Next-Slide-Please as the presenter (the person with the slides)",
		Run:     executePresent,
		Args:    cobra.ExactArgs(1),
		Example: "nsp present [topic]\nnsp present 0PpTrMetgz",
	}

	speakerCmd := &cobra.Command{
		Use:     "speak",
		Short:   "Start Next-Slide-Please as the speaker (the person asking for the next slide)",
		Run:     executeSpeak,
		Args:    cobra.ExactArgs(0),
		Example: "nsp speak",
	}

	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(versionCmd, presenterCmd, speakerCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("error running nsp: %v", err)
	}
}

func executeVersion(cmd *cobra.Command, args []string) {
	fmt.Println(version)
}

func executePresent(cmd *cobra.Command, args []string) {
	nc, err := connect()
	if err != nil {
		log.Fatalf("error connecting to nats server: %v", err)
	}
	defer nc.Close()

	topic := args[0]
	sub, err := nc.Subscribe(topic, func(m *nats.Msg) {
		direction := string(m.Data)
		robotgo.KeyPress(direction)
	})
	defer sub.Unsubscribe()

	fmt.Println("Subscribed for [left | right] direction keys, press Enter to close.")
	fmt.Scanln()
}

func executeSpeak(cmd *cobra.Command, args []string) {
	nc, err := connect()
	if err != nil {
		log.Fatalf("error connecting to nats server: %v", err)
	}
	defer nc.Close()

	topic, err := generateID(10)
	if err != nil {
		log.Fatalf("error generating topic: %v", err)
	}

	hook.Register(hook.KeyDown, []string{"left"}, pub(nc, topic, "left"))
	hook.Register(hook.KeyDown, []string{"right"}, pub(nc, topic, "right"))

	fmt.Printf(
		"Listening for [left | right] direction keys and publishing on %q, press ctrl+c to close.\n",
		topic)

	s := hook.Start()
	<-hook.Process(s)
}

func pub(nc *nats.Conn, topic, key string) func(e hook.Event) {
	return func(e hook.Event) {
		if err := nc.Publish(topic, []byte(key)); err != nil {
			log.Printf("error publishing message: %v", err)
		}
	}
}

func connect() (*nats.Conn, error) {
	nc, err := nats.Connect(natsServer)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}

	return nc, nil
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
