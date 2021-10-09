package main

import (
	"os"

	"go.uber.org/zap"
)

var g_logger *zap.Logger

func main() {
	g_logger, _ = zap.NewProduction()

	if len(os.Args) < 2 {
		g_logger.Fatal("Expected at least 1 parameter")
	} else if os.Args[1] == "server" {
		serverMain()
	} else if os.Args[1] == "client" {
		clientMain()
	} else {
		g_logger.Fatal("Unknown argument", zap.String("arg", os.Args[1]))
	}
}
