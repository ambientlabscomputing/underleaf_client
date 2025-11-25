package main

import (
	"github.com/ambientlabscomputing/underleaf_client/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		panic(err)
	}
}
