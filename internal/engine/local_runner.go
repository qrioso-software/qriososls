// internal/engine/local.go
package engine

import (
	"bytes"
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
	"github.com/qrioso-software/qriososls/internal/util"
)

// LocalRunner maneja la ejecución local con hot reload
type LocalRunner struct {
	cfg        *config.ServerlessConfig
	watcher    *fsnotify.Watcher
	apiProcess *os.Process
	stopChan   chan bool
	lastBuild  time.Time
	buildMutex sync.Mutex
}

// NewLocalRunner crea una nueva instancia del ejecutor local
func NewLocalRunner(cfg *config.ServerlessConfig) (*LocalRunner, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &LocalRunner{
		cfg:      cfg,
		watcher:  watcher,
		stopChan: make(chan bool),
	}, nil
}

// Start inicia el entorno local con hot reload
func (lr *LocalRunner) Start() error {
	// 1. Compilación inicial de todas las funciones Go
	if err := lr.buildAllGoFunctions(); err != nil {
		return err
	}

	// 2. Iniciar API Gateway local
	if err := lr.startLocalAPI(); err != nil {
		return err
	}

	// 3. Configurar watchers para recompilación automática
	if err := lr.setupFileWatchers(); err != nil {
		return err
	}

	log.Println("✅ Hot reload enabled! Changes will auto-compile and SAM will reload")
	log.Println("🌐 API available at: http://localhost:3000")

	// 4. Mantener el proceso activo
	lr.keepAlive()

	return nil
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

// buildAllGoFunctions compila todas las funciones Go inicialmente
func (lr *LocalRunner) buildAllGoFunctions() error {
	log.Println("🔨 Building Go functions...")

	for funcName, function := range lr.cfg.Functions {
		if lr.isGoRuntime(function.Runtime) {
			if err := lr.buildGoFunction(funcName, function); err != nil {
				return fmt.Errorf("failed to build %s: %w", funcName, err)
			}
		}
	}
	return nil
}

// isGoRuntime verifica si el runtime es de Go
func (lr *LocalRunner) isGoRuntime(runtime string) bool {
	return runtime == "provided.al2" || runtime == "go1.x" ||
		strings.Contains(runtime, "go") || strings.Contains(runtime, "provided")
}

// buildGoFunction compila una función Go específica
func (lr *LocalRunner) buildGoFunction(funcName string, function config.LambdaFunc) error {
	lr.buildMutex.Lock()
	defer lr.buildMutex.Unlock()

	// Obtener path absoluto
	codePath := function.Code
	if !filepath.IsAbs(codePath) {
		codePath = filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))
	}

	codeDir := filepath.Dir(codePath)
	// binaryName := filepath.Base(codePath)
	log.Println("codePath..", codePath)
	log.Printf("🔨 Building %s in %s", funcName, codeDir)

	// Verificar archivos Go
	goFiles, err := util.FindGoFilesRecursively(codeDir)
	if err != nil {
		return fmt.Errorf("error finding Go files: %w", err)
	}
	if len(goFiles) == 0 {
		return fmt.Errorf("no Go files found in %s", codeDir)
	}

	// log.Println("binaryName..", binaryName)
	log.Println("codeDir..", codeDir)
	buildCmd := exec.Command("go", "build",
		"-o", fmt.Sprintf("%s/bootstrap", codePath), // Output path
		// "-ldflags", "-s -w", // Strip debug info
		codePath,
	)
	buildCmd.Dir = codeDir
	buildCmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)

	var stdout, stderr bytes.Buffer
	buildCmd.Stdout = &stdout
	buildCmd.Stderr = &stderr

	log.Printf("📦 Compiling: cd %s && %s", codeDir, strings.Join(buildCmd.Args, " "))

	if err := buildCmd.Run(); err != nil {
		log.Printf("🚨 Build failed for %s:", funcName)
		log.Printf("STDERR: %s", stderr.String())
		return fmt.Errorf("build failed: %w", err)
	}

	if stdout.Len() > 0 {
		log.Printf("Build output: %s", stdout.String())
	}

	log.Printf("✅ Built %s → %s", funcName, codePath)
	lr.lastBuild = time.Now()
	return nil
}

// setupFileWatchers configura los watchers con debounce
func (lr *LocalRunner) setupFileWatchers() error {
	// Monitorear todos los directorios de código Go
	for _, function := range lr.cfg.Functions {
		if lr.isGoRuntime(function.Runtime) {
			codeDir := filepath.Dir(function.Code)
			absPath := filepath.Join(lr.cfg.RootPath, codeDir)

			if _, err := os.Stat(absPath); err == nil {
				if err := lr.watcher.Add(absPath); err != nil {
					log.Printf("⚠️ Could not watch %s: %v", absPath, err)
				} else {
					log.Printf("👀 Watching: %s", absPath)
				}

				// También monitorear subdirectorios recursivamente
				lr.watchSubdirectories(absPath)
			}
		}
	}

	go lr.watchForChanges()
	return nil
}

// watchSubdirectories añade subdirectorios recursivamente al watcher
func (lr *LocalRunner) watchSubdirectories(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && !strings.HasPrefix(info.Name(), ".") {
			if err := lr.watcher.Add(path); err != nil {
				log.Printf("⚠️ Could not watch subdirectory %s: %v", path, err)
			}
		}
		return nil
	})
}

// watchForChanges con debounce inteligente
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

			// Solo procesar cambios en archivos .go
			if event.Op.Has(fsnotify.Write) && strings.HasSuffix(event.Name, ".go") {
				// Encontrar qué función fue afectada
				funcName := lr.findFunctionByPath(event.Name)
				if funcName != "" && !contains(changedFunctions, funcName) {
					changedFunctions = append(changedFunctions, funcName)
				}

				// Reiniciar debounce timer
				debounceTimer.Reset(800 * time.Millisecond)
			}

		case <-debounceTimer.C:
			if len(changedFunctions) > 0 {
				lr.handleFileChange(changedFunctions)
				changedFunctions = nil // Reset
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
		if lr.isGoRuntime(function.Runtime) {
			codeDir := filepath.Dir(function.Code)
			absCodeDir := filepath.Join(lr.cfg.RootPath, codeDir)

			if strings.HasPrefix(filePath, absCodeDir) {
				return funcName
			}
		}
	}
	return ""
}

// handleFileChange recompila las funciones cambiadas
func (lr *LocalRunner) handleFileChange(changedFunctions []string) {
	log.Printf("🔄 Changes detected in: %v", changedFunctions)

	for _, funcName := range changedFunctions {
		function := lr.cfg.Functions[funcName]
		if err := lr.buildGoFunction(funcName, function); err != nil {
			log.Printf("❌ Failed to rebuild %s: %v", funcName, err)
		} else {
			log.Printf("✅ Recompiled %s - SAM will auto-reload", funcName)
		}
	}
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
	sigChan := make(chan os.Signal, 1)
	// signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM) // Descomentar si usas signals

	<-sigChan
	log.Println("🛑 Shutting down...")
	lr.Stop()
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
