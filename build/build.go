package build

import (
	"os"
	"path/filepath"
)

var WorkingDirectory = "."

func init() {
	var err error

	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	WorkingDirectory, err = filepath.Abs(pwd)
	if err != nil {
		panic(err)
	}
}


