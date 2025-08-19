package config

import "fmt"

func (c *ServerlessConfig) Validate() error {
	if c.Service == "" {
		return fmt.Errorf("service is required")
	}
	if c.Stage == "" {
		return fmt.Errorf("stage is required")
	}
	if len(c.Functions) == 0 {
		return fmt.Errorf("at least one function is required")
	}
	for name, fn := range c.Functions {
		if fn.FunctionName == "" {
			return fmt.Errorf("functions.%s.functionName is required", name)
		}
		if fn.Runtime == "" {
			return fmt.Errorf("functions.%s.runtime is required", name)
		}
		if fn.Handler == "" {
			return fmt.Errorf("functions.%s.handler is required", name)
		}
		if fn.Code == "" {
			return fmt.Errorf("functions.%s.code is required", name)
		}
		if fn.Timeout <= 0 {
			return fmt.Errorf("functions.%s.timeout must be > 0", name)
		}
	}
	return nil
}
