package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
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

func Sha256Hash(input string) string {
	// Crear un nuevo hash SHA-256
	hasher := sha256.New()

	// Escribir los bytes de entrada (sin añadir nueva línea como printf '%s')
	hasher.Write([]byte(input))

	// Obtener el hash resultante
	hashBytes := hasher.Sum(nil)

	// Convertir a string hexadecimal (equivalente a awk '{print $1}')
	return hex.EncodeToString(hashBytes)
}

// copyCompiledFile copia archivos compilados preservando permisos
func CopyCode(sourcePath, targetDir string) error {
	fileName := filepath.Base(sourcePath)
	targetPath := filepath.Join(targetDir, fileName)

	// Leer archivo fuente
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("no se pudo leer binario: %w", err)
	}

	// Obtener permisos del archivo original
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("no se pudo obtener permisos: %w", err)
	}

	// Escribir archivo destino con mismos permisos
	err = os.WriteFile(targetPath, data, sourceInfo.Mode())
	if err != nil {
		return fmt.Errorf("no se pudo escribir binario: %w", err)
	}

	// Preservar timestamp (opcional pero útil para caching)
	err = os.Chtimes(targetPath, sourceInfo.ModTime(), sourceInfo.ModTime())
	if err != nil {
		log.Printf("⚠️ No se pudo preservar timestamp: %v", err)
	}

	return nil
}
