package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/ff/v4/ffyaml"
	"github.com/sorayaormazabalmayo/general-service/internal/cli"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	//"github.com/kardianos/minwinsvc"
)

func main() {

	runService("nebula-on-premise-windows", false)

}

func runService(name string, isDebug bool) {
	if isDebug {
		err := debug.Run(name, &myService{})
		if err != nil {
			log.Fatalln("Error running service in debug mode.")
		}
	} else {
		err := svc.Run(name, &myService{})
		if err != nil {
			log.Fatalln("Error running service in Service Control mode.")
		}
	}
}

type myService struct{}

func (m *myService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {

	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue

	f, err := os.OpenFile("C:\\log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	logger := log.New(f, "nebula-on-premise-windows ", log.Ldate|log.Ltime|log.Lshortfile)

	log.Printf("Version 1 of the nebula-on-premise.exe")

	// Inform SCM the service is starting
	status <- svc.Status{State: svc.StartPending}

	// Inform SCM the service is now running and accepts commands BEFORE blocking operations
	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// Run your long-running operation in the background
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		generalServiceCmd := cli.NewGeneralServiceCommand(logger)
		opts := []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parse),
		}

		if err := generalServiceCmd.ParseAndRun(ctx, os.Args[1:], opts...); err != nil {
			if errors.Is(err, ff.ErrHelp) || errors.Is(err, ff.ErrDuplicateFlag) || errors.Is(err, ff.ErrAlreadyParsed) || errors.Is(err, ff.ErrUnknownFlag) || errors.Is(err, ff.ErrNotParsed) {
				fmt.Fprintf(os.Stderr, "\n%s\n", ffhelp.Command(&generalServiceCmd))
			}

			if !errors.Is(err, ff.ErrHelp) {
				logger.Fatal(err)
			}
			os.Exit(1)
		}
	}()

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logger.Println("Shutting down service...")
				break loop
			case svc.Pause:
				status <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				logger.Printf("Unexpected service control request #%d", c.Cmd)
			}
		}
	}

	// Signal cancellation to gracefully stop background tasks
	cancel()

	status <- svc.Status{State: svc.StopPending}

	// Return stopped status
	return false, 0
}
