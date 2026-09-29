package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/boot"
	"github.com/magne4000/easy-pz-docker/internal/httpapi"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "openapi":
			// No environment, no listener: the same RegisterRoutes the server uses.
			_, _, api := httpapi.NewAPI(version, fiber.Config{})
			httpapi.RegisterRoutes(api, httpapi.Deps{})
			httpapi.RegisterAuthRoutes(api)
			b, err := api.OpenAPI().MarshalJSON()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if _, err := os.Stdout.Write(append(b, '\n')); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "version", "--version":
			fmt.Println(version)
			return
		}
	}

	cfg, err := app.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pzman: invalid configuration:\n%v\n", err)
		os.Exit(1)
	}
	log := app.NewLogger(cfg)
	log.Info("pzman starting", "version", version, "config", cfg.Redacted())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := boot.Run(ctx, cfg, log, version); err != nil {
		log.Error("pzman stopped with an error", "err", err)
		os.Exit(1)
	}
}
