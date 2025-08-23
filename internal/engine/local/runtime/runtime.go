package runtime

// Runtime define la interface que todos los runtimes deben implementar
type Runtime interface {
	// Name retorna el nombre del runtime
	Name() string

	// Build compila/construye la función
	Build(functionDir string, outputPath string) error

	// WatchPatterns retorna los patrones de archivos a monitorear
	WatchPatterns() []string

	// NeedsBuild indica si este runtime requiere compilación
	NeedsBuild() bool

	// StartCommand retorna el comando para ejecutar localmente
	StartCommand(binaryPath string) []string
}

// FunctionConfig configuración para una función
type FunctionConfig struct {
	Name     string
	Code     string
	Runtime  string
	Handler  string
	BuildDir string
}

// NewFunctionConfig crea configuración para una función
func NewFunctionConfig(name, code, runtime, handler, buildDir string) *FunctionConfig {
	return &FunctionConfig{
		Name:     name,
		Code:     code,
		Runtime:  runtime,
		Handler:  handler,
		BuildDir: buildDir,
	}
}
