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
	"github.com/qrioso-software/qriososls/internal/util"
)

// LocalRunner handles local execution with hot reload capability
type LocalRunner struct {
	cfg              *config.ServerlessConfig
	watcher          *fsnotify.Watcher
	apiProcess       *os.Process
	stopChan         chan struct{}
	lastBuild        time.Time
	buildMutex       sync.Mutex
	mu               sync.Mutex
	runtimeFactory   *runtime.RuntimeFactory
	functionRuntimes map[string]runtime.Runtime
	watchedDirs      map[string]bool // Track watched directories to avoid duplicates
}

// NewLocalRunner creates a new local runner instance
func NewLocalRunner(cfg *config.ServerlessConfig) (*LocalRunner, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &LocalRunner{
		cfg:              cfg,
		watcher:          watcher,
		stopChan:         make(chan struct{}),
		runtimeFactory:   runtime.NewRuntimeFactory(),
		functionRuntimes: make(map[string]runtime.Runtime),
		watchedDirs:      make(map[string]bool),
	}, nil
}

// Start initializes the local environment with hot reload
func (lr *LocalRunner) Start() error {
	// Debug information first
	lr.debugFunctionInfo()

	// Initialize runtimes for each function
	if err := lr.initializeRuntimes(); err != nil {
		return err
	}

	// Initial build of all functions
	if err := lr.buildAllFunctions(); err != nil {
		return err
	}

	// Start local API Gateway
	if err := lr.startLocalAPI(); err != nil {
		return err
	}

	// Setup file watchers
	if err := lr.setupFileWatchers(); err != nil {
		return err
	}

	log.Println("✅ Hot reload enabled for multiple runtimes!")
	lr.keepAlive()
	return nil
}

// initializeRuntimes creates runtime instances for each function
func (lr *LocalRunner) initializeRuntimes() error {
	for funcName, function := range lr.cfg.Functions {
		codePath := filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))
		functionDir := filepath.Dir(codePath)

		var rt runtime.Runtime
		var err error

		// Try configured runtime first, fallback to auto-detection
		if function.Runtime != "" {
			rt, err = lr.runtimeFactory.GetRuntime(function.Runtime)
			if err != nil {
				log.Printf("⚠️ Configured runtime '%s' not supported, trying auto-detect: %v",
					function.Runtime, err)
				rt, err = lr.runtimeFactory.GetRuntimeFromFunction(functionDir)
			}
		} else {
			// Auto-detect runtime if not configured
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

// buildAllFunctions builds all functions that require compilation
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

// buildFunction builds a specific function
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

// getOutputPath determines the output path based on runtime type
func (lr *LocalRunner) getOutputPath(function config.LambdaFunc, rt runtime.Runtime) string {
	codePath := filepath.Join(lr.cfg.RootPath, filepath.Clean(function.Code))

	switch rt.(type) {
	case *runtime.GolangRuntime:
		return codePath // Binary goes in function directory
	case *runtime.NodeJSRuntime:
		return codePath // Main JS file
	case *runtime.PythonRuntime:
		return filepath.Dir(codePath) // Entire directory
	default:
		return codePath
	}
}

// debugFunctionInfo displays detailed debug information
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

		if files, err := os.ReadDir(functionDir); err == nil {
			log.Printf("     Files in directory (%d):", len(files))
			for i, file := range files {
				if i < 5 {
					log.Printf("       - %s (dir: %v)", file.Name(), file.IsDir())
				}
			}
			if len(files) > 5 {
				log.Printf("       ... and %d more", len(files)-5)
			}
		}
	}
}

// setupFileWatchers configures file watchers based on runtime patterns
func (lr *LocalRunner) setupFileWatchers() error {
	log.Println("👀 Setting up file watchers...")

	for funcName, function := range lr.cfg.Functions {
		rt := lr.functionRuntimes[funcName]
		completeCodePath := filepath.Join(lr.cfg.RootPath, function.Code)

		// Watch the main function directory
		if err := lr.addWatchedDir(completeCodePath); err != nil {
			log.Printf("⚠️ Could not watch %s: %v", completeCodePath, err)
			continue
		}
		log.Printf("👀 Watching %s for %s (%s)", completeCodePath, funcName, rt.Name())

		// Add runtime-specific watch patterns
		for _, pattern := range rt.WatchPatterns() {
			absPattern := filepath.Join(lr.cfg.RootPath, function.Code, pattern)
			matches, err := filepath.Glob(absPattern)
			if err != nil {
				continue
			}

			for _, match := range matches {
				matchDir := filepath.Dir(match)
				if err := lr.addWatchedDir(matchDir); err != nil {
					log.Printf("⚠️ Could not watch %s: %v", matchDir, err)
				}
			}
		}
	}

	go lr.watchForChanges()
	return nil
}

// addWatchedDir adds a directory to watch list avoiding duplicates
func (lr *LocalRunner) addWatchedDir(dirPath string) error {
	if lr.watchedDirs[dirPath] {
		return nil // Already watching
	}

	if err := lr.watcher.Add(dirPath); err != nil {
		return err
	}

	lr.watchedDirs[dirPath] = true
	return nil
}

// watchForChanges handles file system changes with debouncing
func (lr *LocalRunner) watchForChanges() {
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	defer debounceTimer.Stop()

	var changedFunctions []string
	changeSet := make(map[string]bool) // Use set for O(1) lookups

	for {
		select {
		case event, ok := <-lr.watcher.Events:
			if !ok {
				log.Println("📭 Watcher events channel closed")
				return
			}

			// Ignore CHMOD events and temporary files
			if event.Op == fsnotify.Chmod || lr.shouldIgnoreEvent(event) {
				continue
			}

			log.Printf("📁 Event: %s - %s", event.Op, event.Name)

			// Handle file creation events
			if event.Op&fsnotify.Create == fsnotify.Create {
				lr.handleFileCreation(event.Name)
			}

			// Track changed functions for rebuilding
			if funcName := lr.findFunctionByPath(event.Name); funcName != "" {
				if !changeSet[funcName] {
					changeSet[funcName] = true
					changedFunctions = append(changedFunctions, funcName)
				}
				debounceTimer.Reset(800 * time.Millisecond)
			}

		case <-debounceTimer.C:
			if len(changedFunctions) > 0 {
				lr.handleFileChange(changedFunctions)
				changedFunctions = nil
				changeSet = make(map[string]bool)
			}

		case err, ok := <-lr.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("❌ Watcher error: %v", err)

		case <-lr.stopChan:
			log.Println("🛑 Received stop signal")
			return
		}
	}
}

// shouldIgnoreEvent determines if an event should be ignored
func (lr *LocalRunner) shouldIgnoreEvent(event fsnotify.Event) bool {
	ignorePatterns := []string{
		"~$", ".swp", ".tmp", ".log",
		"/.git/", "/node_modules/", ".idea/",
	}

	fileName := filepath.Base(event.Name)
	for _, pattern := range ignorePatterns {
		if strings.Contains(event.Name, pattern) || strings.HasSuffix(fileName, pattern) {
			return true
		}
	}
	return false
}

// handleFileCreation handles file creation events
func (lr *LocalRunner) handleFileCreation(filePath string) {
	if funcName := lr.findFunctionByPath(filePath); funcName != "" {
		hash := util.Sha256Hash(funcName)
		assetDir := fmt.Sprintf("%s/cdk.out/asset.%s", lr.cfg.RootPath, hash)

		if err := util.CopyCode(filePath, assetDir); err != nil {
			log.Printf("⚠️ Error copying file: %v", err)
		} else {
			log.Printf("✅ Copied %s to asset directory", filepath.Base(filePath))
		}
	}
}

// findFunctionByPath finds the function associated with a file path
func (lr *LocalRunner) findFunctionByPath(filePath string) string {
	for funcName, function := range lr.cfg.Functions {
		codeDir := filepath.Dir(function.Code)
		absCodeDir := filepath.Join(lr.cfg.RootPath, codeDir)

		if strings.HasPrefix(filePath, absCodeDir) && !lr.shouldIgnorePath(filePath) {
			return funcName
		}
	}
	return ""
}

// shouldIgnorePath checks if a path should be ignored
func (lr *LocalRunner) shouldIgnorePath(path string) bool {
	ignoreDirs := []string{".git", "node_modules", "cdk.out", "tmp"}
	for _, dir := range ignoreDirs {
		if strings.Contains(path, dir) {
			return true
		}
	}
	return false
}

// handleFileChange handles rebuilds for changed functions
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
			log.Printf("📦 Runtime %s doesn't need build", rt.Name())
		}
	}
}

// keepAlive keeps the process running
func (lr *LocalRunner) keepAlive() {
	<-make(chan struct{})
}

// Stop gracefully shuts down the local runner
func (lr *LocalRunner) Stop() {
	select {
	case <-lr.stopChan:
		// Already closed
	default:
		close(lr.stopChan)
	}

	if lr.apiProcess != nil {
		log.Println("🛑 Stopping SAM CLI...")
		lr.apiProcess.Kill()
	}

	if lr.watcher != nil {
		lr.watcher.Close()
	}
}

// startLocalAPI starts the local API Gateway using SAM CLI
func (lr *LocalRunner) startLocalAPI() error {
	templatePath := "cdk.out/local-dev-qrioso-example-dev.template.json"
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return fmt.Errorf("CDK template not found. Run 'qriosls synth' first: %w", err)
	}

	envPath := "env.json"
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := lr.createDefaultEnvFile(envPath); err != nil {
			log.Printf("⚠️ Could not create env.json: %v", err)
		}
	}

	cmdArgs := []string{
		"local", "start-api",
		"--template", templatePath,
		"--port", "3000",
		"--warm-containers", "EAGER",
	}

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

	time.Sleep(2 * time.Second)
	return nil
}

// createDefaultEnvFile creates a default environment file
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

// Helper functions
func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
