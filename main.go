package main

import (
	"sumi/config"
	"sumi/internal/apps"
)

func main() {
	apps.StartAPIServer(config.NewConfig())
}
