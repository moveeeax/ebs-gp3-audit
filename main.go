package main

import (
	"os"

	"github.com/moveeeax/ebs-gp3-audit/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
