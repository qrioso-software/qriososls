package engine

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/util"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigateway"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

func norm(p string) string {
	s := "/" + strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
	s = strings.ReplaceAll(s, "//", "/")
	return s
}

// concatena resource + path manejando "/", "", etc.
func joinPath(resource, path string) string {
	r := strings.TrimSpace(resource)
	p := strings.TrimSpace(path)

	switch {
	case r == "" || r == "/":
		return norm(p)
	case p == "" || p == "/":
		return norm(r)
	default:
		return norm(r + "/" + strings.TrimPrefix(p, "/"))
	}
}

// Crea (o reutiliza) toda la cadena de recursos desde root: "/a/b/{id}/c"
func ensureResourceChain(api awsapigateway.IRestApi, cache map[string]awsapigateway.IResource, absPath string) awsapigateway.IResource {
	absPath = norm(absPath)

	// root
	if absPath == "/" {
		if r, ok := cache["/"]; ok {
			return r
		}
		cache["/"] = api.Root()
		return cache["/"]
	}

	// asegura root cacheado
	if _, ok := cache["/"]; !ok {
		cache["/"] = api.Root()
	}

	parent := cache["/"]
	acc := ""

	for _, seg := range strings.Split(strings.Trim(absPath, "/"), "/") {
		if seg == "" {
			continue
		}
		acc = norm(acc + "/" + seg)

		if r, ok := cache[acc]; ok {
			parent = r
			continue
		}
		parent = parent.AddResource(jsii.String(seg), nil)
		cache[acc] = parent
	}
	return parent
}

// Extrae nombres de {param} del path (p. ej. ["bookingId"])
var reParam = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func extractPathParams(p string) []string {
	matches := reParam.FindAllStringSubmatch(p, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			out = append(out, m[1])
		}
	}
	return out
}

// Construye el map correcto para REST v1: map[string]*bool
func requiredPathParamsMap(params []string) *map[string]*bool {
	if len(params) == 0 {
		return nil
	}
	m := make(map[string]*bool, len(params))
	for _, name := range params {
		// clave: "method.request.path.<param>"
		m["method.request.path."+name] = jsii.Bool(true)
	}
	return &m
}

func NewStack(scope constructs.Construct, id string, cfg *config.ServerlessConfig, env *awscdk.Environment) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &awscdk.StackProps{Env: env})

	// === 1) Resolver API: importar si existe, crear si no
	var api awsapigateway.IRestApi
	// if cfg.Api != nil && cfg.Api.Id != "" {
	// 	// Para poder agregar rutas a un API importado, necesitas también el rootResourceId
	// 	if cfg.Api.RootResourceId == "" {
	// 		panic("api.rootResourceId es requerido cuando api.id está definido (necesario para api.Root())")
	// 	}
	// 	api = awsapigateway.RestApi_FromRestApiAttributes(
	// 		stack,
	// 		jsii.String(fmt.Sprintf("%s-imported-api", cfg.Service)),
	// 		&awsapigateway.RestApiAttributes{
	// 			RestApiId:      jsii.String(cfg.Api.Id),
	// 			RootResourceId: jsii.String(cfg.Api.RootResourceId),
	// 		},
	// 	)
	// } else {

	// }

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

	// === 2) Lambdas y eventos
	for logicalName, fn := range cfg.Functions {
		functionName := util.ResolveVars(fn.FunctionName, cfg.Stage)
		codePath := util.ResolveVars(fn.Code, cfg.Stage)
		logicalName = strings.ReplaceAll(logicalName, "-", "")
		log.Println("codePath", codePath)
		lambdaFn := awslambda.NewFunction(stack, jsii.String(logicalName), &awslambda.FunctionProps{
			FunctionName: jsii.String(functionName),
			Runtime:      toLambdaRuntime(fn.Runtime),
			Handler:      jsii.String(fn.Handler),
			Code:         awslambda.AssetCode_FromAsset(jsii.String(codePath), nil),
			MemorySize:   jsii.Number(float64(fn.MemorySize)),
			Timeout:      awscdk.Duration_Seconds(jsii.Number(float64(fn.Timeout))),
		})

		for _, ev := range fn.Events {
			if strings.ToUpper(ev.Type) != "HTTP" {
				continue
			}

			// Construir ruta completa: resource + path
			fullPath := ev.Resource
			if ev.Path != "" && ev.Path != "/" {
				fullPath = strings.TrimRight(ev.Resource, "/") + ev.Path
			}

			if lambdaFn == nil {
				log.Fatalf("Lambda %s no tiene referencia a Function en stage %s", fn.FunctionName, cfg.Stage)
			}
			log.Println(fullPath)
			log.Println(ev.Method)
			// Usar addResourceByPath para crear o reutilizar
			res := addResourceByPath(api, fullPath)

			res.AddMethod(
				jsii.String(strings.ToUpper(ev.Method)),
				awsapigateway.NewLambdaIntegration(lambdaFn, nil),
				nil,
			)
		}

	}

	return stack
}

func NewLocalDevStack(scope constructs.Construct, id string, cfg *config.ServerlessConfig, env *awscdk.Environment) constructs.Construct {

	api := awsapigateway.NewRestApi(scope, jsii.String(cfg.Service+"-local-api"), &awsapigateway.RestApiProps{
		RestApiName: jsii.String(cfg.Service + "-local-api"),
		DeployOptions: &awsapigateway.StageOptions{
			StageName: jsii.String("local"),
		},
		// 👇 Agregar esto para desarrollo local
	})

	// Cache de recursos creados para reutilizarlos entre rutas
	resources := make(map[string]awsapigateway.IResource)
	resources["/"] = api.Root()

	for logicalName, fn := range cfg.Functions {
		functionName := util.ResolveVars(fn.FunctionName, cfg.Stage)
		codePath := util.ResolveVars(fn.Code, cfg.Stage)
		logicalName = strings.ReplaceAll(logicalName, "-", "")

		lambdaFn := awslambda.NewFunction(scope, jsii.String(logicalName), &awslambda.FunctionProps{
			FunctionName: jsii.String(functionName),
			Runtime:      toLambdaRuntime(fn.Runtime),
			Handler:      jsii.String(fn.Handler),
			Code:         awslambda.Code_FromAsset(jsii.String(codePath), nil),
			MemorySize:   jsii.Number(float64(fn.MemorySize)),
			Timeout:      awscdk.Duration_Seconds(jsii.Number(float64(fn.Timeout))),
		})

		for _, ev := range fn.Events {
			if strings.ToUpper(ev.Type) != "HTTP" {
				log.Println("Skipping non-HTTP event", ev)
				continue
			}

			// Ruta final (abs) => ej: "/bookings/{bookingId}/end"
			fullPath := joinPath(ev.Resource, ev.Path)

			// Crea/Reutiliza la cadena de recursos
			finalRes := ensureResourceChain(api, resources, fullPath)

			// Path params requeridos (REST v1)
			params := extractPathParams(fullPath)
			reqParams := requiredPathParamsMap(params)
			// res.

			finalRes.AddMethod(
				jsii.String(ev.Method),
				awsapigateway.NewLambdaIntegration(lambdaFn, nil),
				&awsapigateway.MethodOptions{
					// AuthorizationType: awsapigateway.AuthorizationType_COGNITO,
					// Authorizer:        authorizer,
					RequestParameters: reqParams, // solo si hay {param}
				},
			)
			log.Printf("Agregando endpoint %s %s%s", ev.Method, ev.Resource, ev.Path)
		}
	}

	return scope
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
		Outdir: jsii.String(outdir),
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

	stack := awscdk.NewStack(app, jsii.String("local-dev-"+cfg.Service+"-"+cfg.Stage), &awscdk.StackProps{
		Env: stackEnv,
	})
	NewLocalDevStack(stack, cfg.Service+"-"+cfg.Stage, cfg, stackEnv)
	app.Synth(nil)

	// sanity check
	if _, err := os.Stat(outdir); err != nil {
		return fmt.Errorf("no se encontró %s después de synth: %w", outdir, err)
	}
	return nil
}
