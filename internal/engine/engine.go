package engine

import (
	"fmt"

	"github.com/qrioso/qriosls/internal/config"
	"github.com/qrioso/qriosls/internal/util"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigateway"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

func NewStack(scope constructs.Construct, id string, cfg *config.ServerlessConfig, env *awscdk.Environment) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &awscdk.StackProps{Env: env})

	// API única por servicio (simple). Se puede permitir múltiples APIs en el futuro.
	api := awsapigateway.NewRestApi(stack, jsii.String(fmt.Sprintf("%s-api", cfg.Service)), &awsapigateway.RestApiProps{
		DeployOptions: &awsapigateway.StageOptions{
			StageName: jsii.String(cfg.Stage),
		},
	})

	for logicalName, fn := range cfg.Functions {
		functionName := util.ResolveVars(fn.FunctionName, cfg.Stage)
		codePath := util.ResolveVars(fn.Code, cfg.Stage)

		lambdaFn := awslambda.NewFunction(stack, jsii.String(logicalName), &awslambda.FunctionProps{
			FunctionName: jsii.String(functionName),
			Runtime:      awslambda.Runtime_PROVIDED_AL2(),
			Handler:      jsii.String(fn.Handler),
			Code:         awslambda.AssetCode_FromAsset(jsii.String(codePath), nil),
			MemorySize:   jsii.Number(float64(fn.MemorySize)),
			Timeout:      awscdk.Duration_Seconds(jsii.Number(float64(fn.Timeout))),
		})

		for _, ev := range fn.Events {
			switch ev.Type {
			case "HTTP":
				res := api.Root().AddResource(jsii.String(ev.Resource), nil)
				res.AddMethod(jsii.String(ev.Method),
					awsapigateway.NewLambdaIntegration(lambdaFn, nil), nil)
			// TODO: SQS/S3/EventBridge aquí
			default:
				// ignorar o loguear para no romper
			}
		}
	}

	return stack
}
