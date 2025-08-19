package engine

import (
	"strings"

	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
)

func toLambdaRuntime(s string) awslambda.Runtime {
	key := strings.ToLower(strings.TrimSpace(s))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, " ", "")

	switch key {
	case "nodejs20.x", "nodejs20x", "nodejs20":
		return awslambda.Runtime_NODEJS_20_X()
	case "nodejs18.x", "nodejs18x", "nodejs18":
		return awslambda.Runtime_NODEJS_18_X()
	case "python3.12", "python312":
		return awslambda.Runtime_PYTHON_3_12()
	case "python3.11", "python311":
		return awslambda.Runtime_PYTHON_3_11()
	// case "go1.x", "go1x", "go":
	// 	return awslambda.Runtime_GO_1_X()
	case "java17":
		return awslambda.Runtime_JAVA_17()
	case "dotnet8", "dotnet8.0", "dotnet80", "dotnetcore8":
		return awslambda.Runtime_DOTNET_8()
	case "ruby3.2", "ruby32":
		return awslambda.Runtime_RUBY_3_2()
	case "provided.al2", "providedal2", "provided", "go1.x", "go1x", "go":
		return awslambda.Runtime_PROVIDED_AL2()
	// Si tu versión de CDK lo trae, puedes habilitar:
	// case "provided.al2023", "providedal2023":
	// 	return awslambda.Runtime_PROVIDED_AL2023()
	default:
		return nil
	}
}
