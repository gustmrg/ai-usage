package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gustmrg/ai-usage/internal/app"
	"github.com/gustmrg/ai-usage/internal/cache"
	"github.com/gustmrg/ai-usage/internal/cli"
	"github.com/gustmrg/ai-usage/internal/provider/codex"
	"github.com/gustmrg/ai-usage/internal/provider/kimi"
	"github.com/gustmrg/ai-usage/internal/provider/opencodego"
)

func main() {
	cacheStore, err := cache.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-usage: %v\n", err)
		os.Exit(1)
	}
	authPath, err := codex.DefaultAuthPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-usage: %v\n", err)
		os.Exit(1)
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	service := app.NewService(cacheStore, codex.New(httpClient, authPath), kimi.New(httpClient), opencodego.New(httpClient))
	command := cli.New(service, os.Stdout, os.Stderr)
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "ai-usage: %v\n", err)
		os.Exit(1)
	}
}
