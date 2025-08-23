// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type ApiConfig struct {
	Id             string `yaml:"id"`
	RootResourceId string `yaml:"rootResourceId"`
	Name           string `yaml:"name"`
}

type ServerlessConfig struct {
	Service   string                `yaml:"service"`
	Stage     string                `yaml:"stage"`
	Api       *ApiConfig            `yaml:"api"`
	Functions map[string]LambdaFunc `yaml:"functions"`
	RootPath  string                `yaml:"-"`
}

type LambdaFunc struct {
	FunctionName string        `yaml:"functionName"`
	Runtime      string        `yaml:"runtime"`
	Handler      string        `yaml:"handler"`
	Code         string        `yaml:"code"`
	MemorySize   int           `yaml:"memorySize"`
	Timeout      int           `yaml:"timeout"`
	Events       []LambdaEvent `yaml:"events"`
}

type LambdaEvent struct {
	Type     string `yaml:"type"`
	Resource string `yaml:"resource"`
	Path     string `yaml:"path"`
	Method   string `yaml:"method"`
}

func Load(path string) (*ServerlessConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var c ServerlessConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	return &c, nil
}

func (c *ServerlessConfig) Validate() error {
	if c.Service == "" {
		return fmt.Errorf("field 'service' is required")
	}

	if !isValidServiceName(c.Service) {
		return fmt.Errorf("service name '%s' is invalid. Only alphanumeric and hyphens allowed", c.Service)
	}

	if c.Stage == "" {
		return fmt.Errorf("field 'stage' is required")
	}

	if len(c.Functions) == 0 {
		return fmt.Errorf("at least one function must be defined")
	}

	for funcName, function := range c.Functions {
		if err := function.Validate(funcName); err != nil {
			return err
		}
	}

	return nil
}

func (f *LambdaFunc) Validate(funcName string) error {
	if f.FunctionName == "" {
		return fmt.Errorf("functionName is required for function '%s'", funcName)
	}

	if f.Handler == "" {
		return fmt.Errorf("handler is required for function '%s'", funcName)
	}

	if f.Runtime == "" {
		return fmt.Errorf("runtime is required for function '%s'", funcName)
	}

	if f.MemorySize < 128 || f.MemorySize > 10240 {
		return fmt.Errorf("memorySize must be between 128 and 10240 for function '%s'", funcName)
	}

	if f.Timeout < 1 || f.Timeout > 900 {
		return fmt.Errorf("timeout must be between 1 and 900 seconds for function '%s'", funcName)
	}

	for i, event := range f.Events {
		if err := event.Validate(funcName, i); err != nil {
			return err
		}
	}

	return nil
}

func (e *LambdaEvent) Validate(funcName string, index int) error {
	if e.Type == "" {
		return fmt.Errorf("event type is required for event %d in function '%s'", index, funcName)
	}

	// Validaciones específicas por tipo de evento
	switch e.Type {
	case "http":
		if e.Path == "" {
			return fmt.Errorf("path is required for HTTP events in function '%s'", funcName)
		}
		if e.Method == "" {
			return fmt.Errorf("method is required for HTTP events in function '%s'", funcName)
		}
		// Puedes agregar más validaciones para otros tipos de eventos
	}

	return nil
}

func isValidServiceName(name string) bool {
	// Solo letras, números y guiones
	match, _ := regexp.MatchString("^[a-zA-Z0-9-]+$", name)
	return match
}
