package utils

import (
	"os"
	"path/filepath"
)

func GetExecutableName() string {
	executable, _ := os.Executable()
	executableFile := filepath.Base(executable)
	executableName := executableFile[:len(executableFile)-len(filepath.Ext(executableFile))]
	return executableName
}
