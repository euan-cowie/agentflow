package main

import (
	"context"
	"os"

	"github.com/euan-cowie/agentflow/internal/cli"
)

func main() {
	ctx := context.Background()
	root := cli.NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
