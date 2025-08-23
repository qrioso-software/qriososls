// internal/engine/local/local.go
package local

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/engine/local/runtime"
)

// LocalRunner maneja la ejecución local con hot reload
type LocalRunner struct {
	cfg              *config.ServerlessConfig
	watcher          *fsnotify.Watcher
	apiProcess       *os.Process
	stopChan         chan bool
	lastBuild        time.Time
	buildMutex       sync.Mutex
	mu               sync.Mutex                 // ← NUEVO: Para sincronización general
	runtimeFactory   *runtime.RuntimeFactory    // ← NUEVO: Factory de runtimes
	functionRuntimes map[string]runtime.Runtime // ← NUEVO: Mapa de funciones a runtimes
}

// NewLocalRunner crea una nueva instancia del ejecutor local
func NewLocalRunner(cfg *config.ServerlessConfig) (*LocalRunner, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &LocalRunner{
		cfg:              cfg,
		watcher:          watcher,
		stopChan:         make(chan bool),
		runtimeFactory:   runtime.NewRuntimeFactory(),      // ← NUEVO: Inicializar factory
		functionRuntimes: make(map[string]runtime.Runtime), // ← NUEVO: Inicializar mapa
	}, nil
}

// Start inicia el entorno local con hot reload
func (lr *LocalRunner) Start() error {
	// 0. Debug primero
	lr.debugFunctionInfo()

	// 1. Inicializar runtimes para cada función
	if err := lr.initializeRuntimes(); err != nil {
		return err
	}

	// 2. Build inicial de funciones
	if err := lr.buildAllFunctions(); err != nil {
		return err
	}

	// 3. Iniciar API Gateway local
	if err := lr.startLocalAPI(); err != nil {
		return err
	}

	// 4. Configurar watchers
	if err := lr.setupFileWatchers(); err != nil {
		return err
	}

	log.Println("✅ Hot reload enabled for multiple runtimes!")
	log.Println("🌐 API available at: http://localhost:3000")
	lr.keepAlive()
	return nil
}

// initializeRuntimes crea runtimes para cada función
func (lr *LocalRunner) initializeRuntimes() error {
	for funcName, function := range lr.cfg.Functions {
		// Obtener el directorio de la función
		codePath := filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))
		functionDir := filepath.Dir(codePath)

		var rt runtime.Runtime
		var err error

		// Primero intentar usar el runtime de la configuración
		if function.Runtime != "" {
			rt, err = lr.runtimeFactory.GetRuntime(function.Runtime)
			if err != nil {
				log.Printf("⚠️ Config runtime '%s' not supported, trying auto-detect: %v", function.Runtime, err)
				// Fallback a detección automática
				rt, err = lr.runtimeFactory.GetRuntimeFromFunction(functionDir)
			}
		} else {
			// Detección automática si no hay runtime configurado
			rt, err = lr.runtimeFactory.GetRuntimeFromFunction(functionDir)
		}

		if err != nil {
			return fmt.Errorf("error determining runtime for %s: %w", funcName, err)
		}

		lr.functionRuntimes[funcName] = rt
		log.Printf("✅ Function %s: %s runtime detected", funcName, rt.Name())
	}
	return nil
}

// buildAllFunctions construye todas las funciones
func (lr *LocalRunner) buildAllFunctions() error {
	for funcName, function := range lr.cfg.Functions {
		rt := lr.functionRuntimes[funcName]
		if rt.NeedsBuild() {
			if err := lr.buildFunction(funcName, function, rt); err != nil {
				return fmt.Errorf("failed to build %s: %w", funcName, err)
			}
		} else {
			log.Printf("📦 Skipping build for %s (runtime: %s)", funcName, rt.Name())
		}
	}
	return nil
}

// buildFunction construye una función específica
func (lr *LocalRunner) buildFunction(funcName string, function config.LambdaFunc, rt runtime.Runtime) error {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	outputPath := lr.getOutputPath(function, rt)

	if err := rt.Build(outputPath, outputPath); err != nil {
		return fmt.Errorf("build failed for %s: %w", funcName, err)
	}

	log.Printf("✅ Built %s → %s", funcName, outputPath)
	return nil
}

// getOutputPath determina donde debe ir el output basado en el runtime
func (lr *LocalRunner) getOutputPath(function config.LambdaFunc, rt runtime.Runtime) string {
	codePath := filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))

	switch rt.(type) {
	case *runtime.GolangRuntime:
		// Para Go, el binario va en el directorio con nombre específico
		return codePath
	case *runtime.NodeJSRuntime:
		// Para Node.js, usar el archivo principal
		return codePath
	case *runtime.PythonRuntime:
		// Para Python, usar el directorio completo
		return filepath.Dir(codePath)
	default:
		return codePath
	}
}

// debugFunctionInfo muestra información detallada de debug
func (lr *LocalRunner) debugFunctionInfo() {
	log.Println("🐛 Debug - Function Configuration:")
	for funcName, function := range lr.cfg.Functions {
		codePath := filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))
		functionDir := filepath.Dir(codePath)

		log.Printf("   Function: %s", funcName)
		log.Printf("     Runtime: '%s'", function.Runtime)
		log.Printf("     Handler: '%s'", function.Handler)
		log.Printf("     Code: '%s'", function.Code)
		log.Printf("     Absolute path: %s", codePath)
		log.Printf("     Directory exists: %v", dirExists(functionDir))

		// Mostrar archivos en el directorio
		if files, err := os.ReadDir(functionDir); err == nil {
			log.Printf("     Files in directory (%d):", len(files))
			for i, file := range files {
				if i < 5 { // Mostrar solo primeros 5 archivos
					log.Printf("       - %s (dir: %v)", file.Name(), file.IsDir())
				}
			}
			if len(files) > 5 {
				log.Printf("       ... and %d more", len(files)-5)
			}
		}
	}
}

// setupFileWatchers configura watchers basado en los runtimes
func (lr *LocalRunner) setupFileWatchers() error {
	for funcName, function := range lr.cfg.Functions {
		rt := lr.functionRuntimes[funcName]
		codeDir := filepath.Dir(filepath.Join(lr.cfg.RootPath, function.Code))

		// Agregar watchers para los patrones específicos del runtime
		for _, pattern := range rt.WatchPatterns() {
			absPattern := filepath.Join(codeDir, pattern)
			if matches, _ := filepath.Glob(absPattern); len(matches) > 0 {
				if err := lr.watcher.Add(codeDir); err != nil {
					log.Printf("⚠️ Could not watch %s: %v", codeDir, err)
				} else {
					log.Printf("👀 Watching %s for %s (%s)", codeDir, funcName, rt.Name())
				}
				break
			}
		}
	}

	go lr.watchForChanges()
	return nil
}

// watchForChanges maneja cambios con soporte multi-runtime
func (lr *LocalRunner) watchForChanges() {
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	defer debounceTimer.Stop()

	var changedFunctions []string

	for {
		select {
		case event, ok := <-lr.watcher.Events:
			if !ok {
				return
			}

			// Determinar qué función fue afectada basado en el path
			if funcName := lr.findFunctionByPath(event.Name); funcName != "" {
				if !contains(changedFunctions, funcName) {
					changedFunctions = append(changedFunctions, funcName)
				}
				debounceTimer.Reset(800 * time.Millisecond)
			}

		case <-debounceTimer.C:
			if len(changedFunctions) > 0 {
				lr.handleFileChange(changedFunctions)
				changedFunctions = nil
			}

		case err, ok := <-lr.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)

		case <-lr.stopChan:
			return
		}
	}
}

// findFunctionByPath encuentra la función basada en la ruta del archivo
func (lr *LocalRunner) findFunctionByPath(filePath string) string {
	for funcName, function := range lr.cfg.Functions {
		codeDir := filepath.Dir(function.Code)
		absCodeDir := filepath.Join(lr.cfg.RootPath, codeDir)

		if strings.HasPrefix(filePath, absCodeDir) {
			return funcName
		}
	}
	return ""
}

// handleFileChange maneja cambios para múltiples runtimes
func (lr *LocalRunner) handleFileChange(changedFunctions []string) {
	log.Printf("🔄 Changes detected in: %v", changedFunctions)

	for _, funcName := range changedFunctions {
		function := lr.cfg.Functions[funcName]
		rt := lr.functionRuntimes[funcName]

		if rt.NeedsBuild() {
			if err := lr.buildFunction(funcName, function, rt); err != nil {
				log.Printf("❌ Failed to rebuild %s: %v", funcName, err)
			} else {
				log.Printf("✅ Recompiled %s (%s)", funcName, rt.Name())
			}
		} else {
			log.Printf("📦 Runtime %s doesn't need build, SAM will auto-reload", rt.Name())
		}
	}
}

// Helper function
func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// keepAlive mantiene el proceso corriendo
func (lr *LocalRunner) keepAlive() {
	// Esperar señal de terminación (Ctrl+C)
	<-make(chan struct{})
}

func (lr *LocalRunner) Stop() {
	close(lr.stopChan)
	if lr.apiProcess != nil {
		log.Println("🛑 Stopping SAM CLI...")
		lr.apiProcess.Kill()
	}
	if lr.watcher != nil {
		lr.watcher.Close()
	}
}

// startLocalAPI inicia SAM CLI para API Gateway local
func (lr *LocalRunner) startLocalAPI() error {
	// Verificar que el template de CDK existe
	templatePath := "cdk.out/local-dev-qrioso-example-dev.template.json"
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return fmt.Errorf("CDK template not found. Run 'qriosls synth' first: %w", err)
	}

	// Verificar que el archivo env.json existe, si no crearlo
	envPath := "env.json"
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := lr.createDefaultEnvFile(envPath); err != nil {
			log.Printf("⚠️ Could not create env.json: %v", err)
		}
	}

	// Construir comando SAM
	cmdArgs := []string{
		"local", "start-api",
		"--template", templatePath,
		"--port", "3000",
		"--warm-containers", "EAGER",
	}

	// Agregar env-vars si el archivo existe
	if _, err := os.Stat(envPath); err == nil {
		cmdArgs = append(cmdArgs, "--env-vars", envPath)
	}

	cmd := exec.Command("sam", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("🚀 Starting SAM CLI: sam %s", strings.Join(cmdArgs, " "))

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting SAM CLI: %w", err)
	}

	lr.apiProcess = cmd.Process
	log.Println("✅ Local API Gateway started on http://localhost:3000")

	// Esperar un poco para que SAM se inicie completamente
	time.Sleep(2 * time.Second)

	return nil
}

// createDefaultEnvFile crea un archivo env.json por defecto
func (lr *LocalRunner) createDefaultEnvFile(path string) error {
	envContent := `{
  "Parameters": {
    "STAGE": "dev",
    "REGION": "us-east-1",
    "IS_PROD": "false"
  }
}`

	return os.WriteFile(path, []byte(envContent), 0644)
}
