package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"

	identify "github.com/realwhite/srv-identify"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	host := os.Getenv("TARGET_HOST")
	if host == "" && len(os.Args) >= 2 {
		host = os.Args[1]
	}

	if host == "" {
		logger.Error("specify target host via TARGET_HOST env or first argument")
		os.Exit(1)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		logger.Error("invalid IP address", "host", host)
		os.Exit(1)
	}

	username := os.Getenv("IPMI_USER")
	password := os.Getenv("IPMI_PASS")

	if username == "" || password == "" {
		logger.Error("set IPMI_USER and IPMI_PASS environment variables")
		os.Exit(1)
	}

	id := identify.NewServerIdentifier(logger)

	res, err := id.IdentifyRemote(context.Background(), ip, identify.IdentifyOptions{
		Credentials: identify.ServerCredentials{
			Username: username,
			Password: password,
		},
		IPMIConfig: identify.IPMIConfig{
			Port: 623,
		},
		RedfishConfig: identify.RedfishConfig{
			Port:     443,
			Insecure: true,
		},
	})
	if err != nil {
		logger.Error("identify failed", "error", err)
		os.Exit(1)
	}

	fmt.Println(res)
}
