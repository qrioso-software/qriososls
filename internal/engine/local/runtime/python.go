package runtime

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type PythonRuntime struct{}

func (p *PythonRuntime) Name() string {
	return "python"
}

func (p *PythonRuntime) Build(functionDir string, outputPath string) error {
	log.Printf("🐍 Installing dependencies for Python function in: %s", functionDir)

	// pip install si hay requirements.txt
	if _, err := os.Stat(filepath.Join(functionDir, "requirements.txt")); err == nil {
		cmd := exec.Command("pip", "install", "-r", "requirements.txt", "-t", functionDir)
		cmd.Dir = functionDir

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pip install failed: %w\nOutput: %s", err, string(output))
		}
	}

	return nil
}

func (p *PythonRuntime) WatchPatterns() []string {
	return []string{"*.py", "requirements.txt"}
}

func (p *PythonRuntime) NeedsBuild() bool {
	return false
}

func (p *PythonRuntime) StartCommand(binaryPath string) []string {
	return []string{"python", binaryPath}
}
