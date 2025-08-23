package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeFactory crea instancias de runtimes basado en la configuración
type RuntimeFactory struct{}

func NewRuntimeFactory() *RuntimeFactory {
	return &RuntimeFactory{}
}

// GetRuntime retorna el runtime apropiado para el nombre técnico de AWS Lambda
func (f *RuntimeFactory) GetRuntime(awsRuntime string) (Runtime, error) {
	// Normalizar el runtime name
	runtime := strings.ToLower(awsRuntime)

	switch {
	case runtime == "provided.al2" || runtime == "provided":
		return &GolangRuntime{}, nil
	case strings.HasPrefix(runtime, "go"):
		return &GolangRuntime{}, nil
	case strings.HasPrefix(runtime, "node"):
		return &NodeJSRuntime{}, nil
	case strings.HasPrefix(runtime, "python"):
		return &PythonRuntime{}, nil
	// case runtime == "java11" || runtime == "java17" || runtime == "java21":
	// 	return &JavaRuntime{}, nil // ¡Podrías agregar esto después!
	// case runtime == "ruby3.2":
	// 	return &RubyRuntime{}, nil // ¡Podrías agregar esto después!
	// case runtime == "dotnet6" || runtime == "dotnet8":
	// 	return &DotNetRuntime{}, nil // ¡Podrías agregar esto después!
	default:
		return nil, fmt.Errorf("unsupported AWS Lambda runtime: %s", awsRuntime)
	}
}

// GetRuntimeFromFunction detecta el runtime basado en archivos en el directorio
func (f *RuntimeFactory) GetRuntimeFromFunction(functionDir string) (Runtime, error) {
	// Detección automática basada en archivos presentes
	if hasGoFiles(functionDir) {
		return &GolangRuntime{}, nil
	}
	if hasNodeJSFiles(functionDir) {
		return &NodeJSRuntime{}, nil
	}
	if hasPythonFiles(functionDir) {
		return &PythonRuntime{}, nil
	}

	return nil, fmt.Errorf("could not detect runtime for function in: %s", functionDir)
}

// Funciones helper para detección de runtime
func hasGoFiles(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	return len(files) > 0
}

func hasNodeJSFiles(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		return true
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.js"))
	return len(files) > 0
}

func hasPythonFiles(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		return true
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.py"))
	return len(files) > 0
}
