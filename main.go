package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/rpc"
	"github.com/Hailer367/Ryowatch/watcher"
)

func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func main() {
	// Load .env manually (no dependency on godotenv).
	loadEnv(".env")
	if err := config.Load(); err != nil {
		fmt.Println("config error:", err)
		os.Exit(1)
	}

	fmt.Printf("Ryowatch starting. dry_run=%v\n", config.DryRun)
	fmt.Printf("Ryo contract: %s\n", config.RyoAddress)
	fmt.Printf("Operator: %s\n", config.OperatorAddress)

	// Primary: WebSocket (Alchemy). Secondary: HTTP (public or private).
	wsURL := config.RPCURL
	httpURL := config.RPCURL
	// If RPCURL is HTTP, derive a WS URL by replacing the scheme.
	if len(wsURL) > 5 && wsURL[:5] == "https" {
		wsURL = "wss" + wsURL[5:]
	} else if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}

	client := rpc.New(wsURL, httpURL)
	defer client.Close()

	w := watcher.New(client, config.DefaultRoutes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// graceful shutdown on Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nshutting down...")
		cancel()
	}()

	w.Run(ctx)
	fmt.Println("stopped")
}
