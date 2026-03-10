package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	identify "github.com/realwhite/srv-identify"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	username := os.Getenv("IPMI_USER")
	password := os.Getenv("IPMI_PASS")

	if username == "" || password == "" {
		logger.Error("set IPMI_USER and IPMI_PASS environment variables")
		os.Exit(1)
	}

	id := identify.NewServerIdentifier(logger)

	res, err := id.IdentifyLocal(context.Background(), identify.IdentifyOptions{
		Credentials: identify.ServerCredentials{
			Username: username,
			Password: password,
		},
	})
	if err != nil {
		logger.Error("identify failed", "error", err)
		os.Exit(1)
	}

	fmt.Println(res)
}
