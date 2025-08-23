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

	"github.com/fsnotify/fsnotify"
	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/util"
)

// LocalRunner maneja la ejecución local con hot reload
type LocalRunner struct {
	cfg         *config.ServerlessConfig
	watcher     *fsnotify.Watcher
	apiProcess  *os.Process
	lambdaFuncs map[string]*os.Process
}

// NewLocalRunner crea una nueva instancia del ejecutor local
func NewLocalRunner(cfg *config.ServerlessConfig) (*LocalRunner, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &LocalRunner{
		cfg:         cfg,
		watcher:     watcher,
		lambdaFuncs: make(map[string]*os.Process),
	}, nil
}

// Start inicia el entorno local con hot reload
func (lr *LocalRunner) Start() error {
	// 1. Iniciar API Gateway local
	if err := lr.startLocalAPI(); err != nil {
		return err
	}

	// 2. Iniciar funciones Lambda localmente
	if err := lr.startLambdas(); err != nil {
		return err
	}

	// 3. Configurar watchers para hot reload
	if err := lr.setupFileWatchers(); err != nil {
		return err
	}

	// 4. Mantener el proceso activo
	lr.keepAlive()

	return nil
}

func (lr *LocalRunner) startLocalAPI() error {
	// Usar SAM CLI o localstack para API Gateway local
	cmd := exec.Command("sam", "local", "start-api",
		"--template", "cdk.out/local-dev-qrioso-example-dev.template.json",
		"--port", "3000",
		// "--env-vars", "env.json",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting local API: %w", err)
	}

	lr.apiProcess = cmd.Process
	log.Println("✅ Local API Gateway started on http://localhost:3000")
	return nil
}

func (lr *LocalRunner) startLambdas() error {
	for funcName, function := range lr.cfg.Functions {
		if err := lr.startLambdaFunction(funcName, function); err != nil {
			return err
		}
	}
	return nil
}

// Modifica la función startLambdaFunction para capturar output de error
func (lr *LocalRunner) startLambdaFunction(funcName string, function config.LambdaFunc) error {
	// Para funciones Go
	if function.Runtime == "provided.al2" || function.Runtime == "go1.x" || strings.Contains(function.Runtime, "go") {
		// Obtener el path absoluto correcto
		code := function.Code
		if !filepath.IsAbs(code) {
			code = filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))
		}

		// El directorio donde están los archivos .go es el directorio del código
		codeDir := filepath.Dir(code)

		log.Printf("Building Go function in directory: %s", codeDir)
		log.Printf("Output binary: %s", code)

		// Verificar que el directorio existe y tiene archivos Go
		if _, err := os.Stat(codeDir); os.IsNotExist(err) {
			return fmt.Errorf("code directory does not exist: %s", codeDir)
		}

		goFiles, _ := util.FindGoFilesRecursively(codeDir)
		if len(goFiles) == 0 {
			return fmt.Errorf("no Go files found in directory: %s", codeDir)
		}

		log.Printf("Found %d Go files in directory", len(goFiles))

		// ¡CORRECCIÓN CLAVE! Cambiar el directorio de trabajo para go build
		println("codeDir", codeDir)
		buildCmd := exec.Command("go", "build", "-o", fmt.Sprintf("%s/bootstrap", code), code)
		buildCmd.Dir = codeDir // ← Esto hace que go build se ejecute en el directorio correcto
		log.Println(buildCmd.String())

		buildCmd.Env = append(os.Environ(),
			"GOOS=linux",
			"GOARCH=amd64", // <- sin Graviton
			"CGO_ENABLED=0",
		)

		// Capturar output
		var stdout, stderr bytes.Buffer
		buildCmd.Stdout = &stdout
		buildCmd.Stderr = &stderr

		log.Printf("Executing: cd %s && %s", codeDir, strings.Join(buildCmd.Args, " "))

		if err := buildCmd.Run(); err != nil {
			log.Printf("🚨 Build failed for %s:", funcName)
			log.Printf("Command: cd %s && %s", codeDir, strings.Join(buildCmd.Args, " "))
			if stdout.Len() > 0 {
				log.Printf("stdout: %s", stdout.String())
			}
			if stderr.Len() > 0 {
				log.Printf("stderr: %s", stderr.String())
			}
			return fmt.Errorf("error building function %s: %w", funcName, err)
		}

		if stdout.Len() > 0 {
			log.Printf("Build output: %s", stdout.String())
		}
		log.Printf("✅ Build successful for %s", funcName)
	}

	// Ejecutar función localmente con lambda-local o sam local invoke
	cmd := exec.Command("sam", "local", "invoke",
		function.FunctionName,
		"--template", "template.yaml",
		"--env-vars", "env.json",
		"--event", "event.json",
	)

	// Capturar output de SAM también
	var samOut, samErr bytes.Buffer
	cmd.Stdout = &samOut
	cmd.Stderr = &samErr

	if err := cmd.Start(); err != nil {
		log.Printf("SAM stderr: %s", samErr.String())
		return fmt.Errorf("error starting lambda %s: %w\nSAM output: %s", funcName, err, samErr.String())
	}

	lr.lambdaFuncs[funcName] = cmd.Process

	// Mostrar output de SAM
	if samOut.Len() > 0 {
		log.Printf("SAM stdout for %s: %s", funcName, samOut.String())
	}

	log.Printf("✅ Lambda function %s started", funcName)

	return nil
}

func (lr *LocalRunner) setupFileWatchers() error {
	// Monitorear archivos de código fuente
	for _, function := range lr.cfg.Functions {
		codeDir := filepath.Dir(function.Code)
		if err := lr.watcher.Add(codeDir); err != nil {
			return err
		}
	}

	// Monitorear cambios y recargar
	go lr.watchForChanges()

	return nil
}

func (lr *LocalRunner) watchForChanges() {
	for {
		select {
		case event, ok := <-lr.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				lr.handleFileChange(event.Name)
			}
		case err, ok := <-lr.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (lr *LocalRunner) handleFileChange(filename string) {
	log.Printf("🔄 File changed: %s", filename)

	// Encontrar qué función fue afectada
	for funcName, function := range lr.cfg.Functions {
		codeDir := filepath.Dir(function.Code)
		if filepath.Dir(filename) == codeDir {
			lr.reloadLambdaFunction(funcName, function)
			break
		}
	}
}

func (lr *LocalRunner) reloadLambdaFunction(funcName string, function config.LambdaFunc) {
	// Detener proceso actual
	if proc, exists := lr.lambdaFuncs[funcName]; exists {
		if err := proc.Kill(); err != nil {
			log.Printf("Warning: error killing process for %s: %v", funcName, err)
		}
	}

	// Recompilar si es necesario con mejor logging
	if function.Runtime == "provided.al2" || strings.Contains(function.Runtime, "go") {
		log.Printf("🔨 Rebuilding %s...", funcName)

		buildCmd := exec.Command("go", "build", "-o", function.Code, "./")

		var stdout, stderr bytes.Buffer
		buildCmd.Stdout = &stdout
		buildCmd.Stderr = &stderr

		if err := buildCmd.Run(); err != nil {
			log.Printf("🚨 Rebuild failed for %s:", funcName)
			if stdout.Len() > 0 {
				log.Printf("stdout: %s", stdout.String())
			}
			if stderr.Len() > 0 {
				log.Printf("stderr: %s", stderr.String())
			}
			log.Printf("Error: %v", err)
			return
		}

		// Mostrar éxito de recompilación
		if stdout.Len() > 0 {
			log.Printf("✅ Rebuild output: %s", stdout.String())
		}
		log.Printf("✅ Successfully rebuilt %s", funcName)
	}

	// Reiniciar función
	if err := lr.startLambdaFunction(funcName, function); err != nil {
		log.Printf("Error restarting %s: %v", funcName, err)
	} else {
		log.Printf("✅ Successfully restarted %s", funcName)
	}
}

func (lr *LocalRunner) keepAlive() {
	// Mantener el proceso principal activo
	select {}
}

func (lr *LocalRunner) Stop() {
	if lr.apiProcess != nil {
		lr.apiProcess.Kill()
	}

	for _, proc := range lr.lambdaFuncs {
		proc.Kill()
	}

	lr.watcher.Close()
}
