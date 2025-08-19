package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ServerlessConfig struct {
	Service   string                `yaml:"service"`
	Stage     string                `yaml:"stage"`
	Functions map[string]LambdaFunc `yaml:"functions"`
}

type LambdaFunc struct {
	FunctionName string        `yaml:"functionName"`
	Runtime      string        `yaml:"runtime"`
	Handler      string        `yaml:"handler"`
	Code         string        `yaml:"code"`
	MemorySize   int           `yaml:"memorySize"`
	Timeout      int           `yaml:"timeout"`
	Events       []LambdaEvent `yaml:"events"`
	// opcional futuro: Env, Layers, Policies, VPC, Arch, etc.
}

type LambdaEvent struct {
	Type     string `yaml:"type"`     // HTTP, SQS, S3, EventBridge...
	Resource string `yaml:"resource"` // /routes
	Path     string `yaml:"path"`     // /
	Method   string `yaml:"method"`   // GET, POST...
}

func Load(path string) (*ServerlessConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ServerlessConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
