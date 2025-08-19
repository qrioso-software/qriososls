package util

import "strings"

// Reemplaza ${stage} por el valor real
func ResolveVars(s, stage string) string {
	return strings.ReplaceAll(s, "${stage}", stage)
}
