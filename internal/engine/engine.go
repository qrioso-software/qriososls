package engine

import (
	"fmt"
	"os"
	"strings"

	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/util"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigateway"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

func NewStack(scope constructs.Construct, id string, cfg *config.ServerlessConfig, env *awscdk.Environment) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &awscdk.StackProps{Env: env})

	// === 1) Resolver API: importar si existe, crear si no
	var api awsapigateway.IRestApi
	if cfg.Api != nil && cfg.Api.Id != "" {
		// Para poder agregar rutas a un API importado, necesitas también el rootResourceId
		if cfg.Api.RootResourceId == "" {
			panic("api.rootResourceId es requerido cuando api.id está definido (necesario para api.Root())")
		}
		api = awsapigateway.RestApi_FromRestApiAttributes(
			stack,
			jsii.String(fmt.Sprintf("%s-imported-api", cfg.Service)),
			&awsapigateway.RestApiAttributes{
				RestApiId:      jsii.String(cfg.Api.Id),
				RootResourceId: jsii.String(cfg.Api.RootResourceId),
			},
		)
	} else {
		apiName := cfg.Service + "-api"
		if cfg.Api != nil && cfg.Api.Name != "" {
			apiName = cfg.Api.Name
		}
		api = awsapigateway.NewRestApi(
			stack,
			jsii.String(apiName),
			&awsapigateway.RestApiProps{
				DeployOptions: &awsapigateway.StageOptions{
					StageName: jsii.String(cfg.Stage),
				},
			},
		)
	}

	// === 2) Lambdas y eventos
	for logicalName, fn := range cfg.Functions {
		functionName := util.ResolveVars(fn.FunctionName, cfg.Stage)
		codePath := util.ResolveVars(fn.Code, cfg.Stage)
		logicalName = strings.ReplaceAll(logicalName, "-", "")

		lambdaFn := awslambda.NewFunction(stack, jsii.String(logicalName), &awslambda.FunctionProps{
			FunctionName: jsii.String(functionName),
			Runtime:      toLambdaRuntime(fn.Runtime),
			Handler:      jsii.String(fn.Handler),
			Code:         awslambda.AssetCode_FromAsset(jsii.String(codePath), nil),
			MemorySize:   jsii.Number(float64(fn.MemorySize)),
			Timeout:      awscdk.Duration_Seconds(jsii.Number(float64(fn.Timeout))),
		})

		for _, ev := range fn.Events {
			switch strings.ToUpper(ev.Type) {
			case "HTTP":
				// Soporta paths con '/' tipo "/routes" o "/v1/routes"
				res := addResourceByPath(api, ev.Resource)
				res.AddMethod(jsii.String(strings.ToUpper(ev.Method)),
					awsapigateway.NewLambdaIntegration(lambdaFn, nil), nil)
			default:
				// TODO: SQS/S3/EventBridge
			}
		}
	}

	return stack
}

// Crea recursos anidados a partir de "/a/b/c"
func addResourceByPath(api awsapigateway.IRestApi, resourcePath string) awsapigateway.IResource {
	curr := api.Root()
	p := strings.Trim(resourcePath, "/")
	if p == "" {
		return curr
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		curr = curr.AddResource(jsii.String(seg), nil)
	}
	return curr
}

func Synth(cfg *config.ServerlessConfig, outdir string) error {
	if outdir == "" {
		outdir = "cdk.out"
	}

	app := awscdk.NewApp(&awscdk.AppProps{
		Outdir: jsii.String(outdir), // 👈 forzar carpeta de salida
	})

	var stackEnv *awscdk.Environment
	acct := os.Getenv("CDK_DEFAULT_ACCOUNT")
	reg := os.Getenv("CDK_DEFAULT_REGION")
	if acct != "" && reg != "" {
		stackEnv = &awscdk.Environment{
			Account: jsii.String(acct),
			Region:  jsii.String(reg),
		}
	}

	NewStack(app, cfg.Service+"-"+cfg.Stage, cfg, stackEnv)
	app.Synth(nil)

	// sanity check
	if _, err := os.Stat(outdir); err != nil {
		return fmt.Errorf("no se encontró %s después de synth: %w", outdir, err)
	}
	return nil
}
