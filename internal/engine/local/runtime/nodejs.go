package runtime

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type NodeJSRuntime struct{}

func (n *NodeJSRuntime) Name() string {
	return "nodejs"
}

func (n *NodeJSRuntime) Build(functionDir string, outputPath string) error {
	log.Printf("📦 Installing dependencies for Node.js function in: %s", functionDir)

	// npm install o yarn install
	if _, err := os.Stat(filepath.Join(functionDir, "package.json")); err == nil {
		cmd := exec.Command("npm", "install")
		cmd.Dir = functionDir

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("npm install failed: %w\nOutput: %s", err, string(output))
		}
	}

	return nil
}

func (n *NodeJSRuntime) WatchPatterns() []string {
	return []string{"*.js", "*.ts", "package.json", "tsconfig.json"}
}

func (n *NodeJSRuntime) NeedsBuild() bool {
	return false // Node.js normalmente no necesita build (a menos que sea TypeScript)
}

func (n *NodeJSRuntime) StartCommand(binaryPath string) []string {
	return []string{"node", binaryPath}
}
