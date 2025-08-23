package runtime

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type GolangRuntime struct{}

func (g *GolangRuntime) Name() string {
	return "golang"
}

func (g *GolangRuntime) Build(functionDir string, outputPath string) error {
	log.Printf("🔨 Building Go function in: %s", functionDir)

	// Crear directorio de output si no existe
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("error creating output directory: %w", err)
	}

	buildCmd := exec.Command("go", "build",
		"-o", fmt.Sprintf("%s/bootstrap", outputPath),
		"-ldflags", "-s -w",
		outputPath,
	)
	buildCmd.Dir = functionDir
	buildCmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)

	var stdout, stderr bytes.Buffer
	buildCmd.Stdout = &stdout
	buildCmd.Stderr = &stderr

	if err := buildCmd.Run(); err != nil {
		log.Printf("🚨 Go build failed:")
		log.Printf("STDERR: %s", stderr.String())
		return fmt.Errorf("go build failed: %w", err)
	}

	if stdout.Len() > 0 {
		log.Printf("Build output: %s", stdout.String())
	}

	return nil
}

func (g *GolangRuntime) WatchPatterns() []string {
	return []string{"*.go", "go.mod", "go.sum"}
}

func (g *GolangRuntime) NeedsBuild() bool {
	return true
}

func (g *GolangRuntime) StartCommand(binaryPath string) []string {
	return []string{binaryPath}
}
