package util

import (
	"os"
	"path/filepath"
	"strings"
)

// Función para encontrar archivos Go recursivamente
func FindGoFilesRecursively(rootDir string) ([]string, error) {
	var goFiles []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Ignorar directorios que empiecen con . (como .git, .vscode, etc.)
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
			return filepath.SkipDir
		}

		// Buscar archivos .go
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			goFiles = append(goFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return goFiles, nil
}
