package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"

	"github.com/aws/jsii-runtime-go"
	"github.com/qrioso-software/qriososls/internal/assets"
	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/engine"
	"github.com/spf13/cobra"
)

func main() {
	defer jsii.Close()

	var cfgPath string
	var awsProfile string
	var requireApproval string

	root := &cobra.Command{
		Use:   "qriosls",
		Short: "Qrioso Sls: YAML -> AWS CDK (Go)",
	}

	service := "qrioso-example"
	stage := "dev"
	region := "us-east-1"

	// ===== qriosls init =====
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Inicializa un proyecto con serverless.yml de ejemplo",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat("qrioso-sls.yml"); err == nil {
				return fmt.Errorf("ya existe qrioso-sls.yml en el directorio")
			}

			file, err := assets.Templates.ReadFile("templates/qrioso-sls.tmpl.yml")
			if err != nil {
				return fmt.Errorf("error reading template: %w", err)
			}

			t := template.Must(template.New("srv").Parse(string(file)))
			f, err := os.Create("qrioso-sls.yml")
			if err != nil {
				return err
			}
			defer f.Close()

			data := struct {
				Service string
				Stage   string
				Region  string
			}{service, stage, region}

			if err := t.Execute(f, data); err != nil {
				return err
			}
			_ = os.MkdirAll("build", 0755)
			log.Println("✅ Creado qrioso-sls.yml y carpeta build/")
			return nil
		},
	}
	initCmd.Flags().StringVar(&service, "service", service, "Nombre del servicio")
	// initCmd.Flags().StringVar(&stage, "stage", stage, "Stage (dev|stg|prod)")
	// initCmd.Flags().StringVar(&region, "region", region, "Región AWS (ej. us-east-1)")

	// ===== qriosls validate =====
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Valida el archivo de configuración",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			return cfg.Validate()
		},
	}
	validateCmd.Flags().StringVarP(&cfgPath, "config", "c", "qrioso-sls.yml", "Ruta del YAML")

	// ===== qriosls cdkapp (oculto) =====
	// Entry point que el CDK CLI invoca vía CDK_APP.
	// IMPORTANTE: engine.Synth debe respetar el outdir si viene seteado (CDK_OUTDIR).
	cdkAppCmd := &cobra.Command{
		Use:    "cdkapp",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			outdir := os.Getenv("CDK_OUTDIR") // CDK define esta var al invocar el app
			return engine.Synth(cfg, outdir)
		},
	}
	cdkAppCmd.Flags().StringVarP(&cfgPath, "config", "c", "qrioso-sls.yml", "Ruta del YAML")

	// ===== qriosls synth =====
	// Genera Cloud Assembly en ./cdk.out SIN escribir cdk.json
	synthCmd := &cobra.Command{
		Use:   "synth",
		Short: "Genera cdk.out (Cloud Assembly)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validar config temprano (opcional pero útil para fallar rápido)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			if _, err := exec.LookPath("cdk"); err != nil {
				return fmt.Errorf("cdk CLI no encontrado. Instala con: npm install -g aws-cdk")
			}

			ex := exec.Command("cdk", "synth", "--output", "cdk.out")
			ex.Env = append(os.Environ(),
				"CDK_APP=go run ./cmd/qriosls cdkapp --config "+cfgPath,
			)
			ex.Stdout = os.Stdout
			ex.Stderr = os.Stderr

			if err := ex.Run(); err != nil {
				return fmt.Errorf("error en cdk synth: %w", err)
			}
			log.Println("✅ Synth listo en cdk.out/")
			return nil
		},
	}
	synthCmd.Flags().StringVarP(&cfgPath, "config", "c", "qrioso-sls.yml", "Ruta del YAML")

	// ===== qriosls deploy =====
	// Deja que CDK haga synth+deploy invocando nuestro cdkapp (consistente con synth)
	deployCmd := &cobra.Command{
		Use:   "deploy",
		Short: "Despliega usando CDK CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath("cdk"); err != nil {
				return fmt.Errorf("cdk CLI no encontrado. Instala con: npm i -g aws-cdk")
			}

			// Validación previa del YAML (opcional)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			cmdArgs := []string{"deploy"}
			if requireApproval != "" {
				cmdArgs = append(cmdArgs, "--require-approval", requireApproval)
			}
			if awsProfile != "" {
				cmdArgs = append(cmdArgs, "--profile", awsProfile)
			}

			ex := exec.Command("cdk", cmdArgs...)
			ex.Env = append(os.Environ(),
				"CDK_APP=qriosls cdkapp --config "+cfgPath,
			)
			ex.Stdout = os.Stdout
			ex.Stderr = os.Stderr

			log.Println("🚀 Ejecutando:", "cdk", cmdArgs)
			return ex.Run()
		},
	}
	deployCmd.Flags().StringVarP(&cfgPath, "config", "c", "qrioso-sls.yml", "Ruta del YAML")
	deployCmd.Flags().StringVar(&awsProfile, "profile", "", "AWS profile")
	deployCmd.Flags().StringVar(&requireApproval, "require-approval", "", "never|any-change|broadening")

	// ===== qriosls diff =====
	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff con CDK CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath("cdk"); err != nil {
				return fmt.Errorf("cdk CLI no encontrado. Instala con: npm i -g aws-cdk")
			}

			// Validación previa del YAML (opcional)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			ex := exec.Command("cdk", "diff")
			ex.Env = append(os.Environ(),
				"CDK_APP=qriosls cdkapp --config "+cfgPath,
			)
			ex.Stdout = os.Stdout
			ex.Stderr = os.Stderr
			return ex.Run()
		},
	}
	diffCmd.Flags().StringVarP(&cfgPath, "config", "c", "qrioso-sls.yml", "Ruta del YAML")

	// ===== qriosls doctor =====
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verifica requisitos del entorno",
		Run: func(cmd *cobra.Command, args []string) {
			check := func(bin string) {
				if _, err := exec.LookPath(bin); err != nil {
					log.Printf("❌ %s no encontrado", bin)
				} else {
					log.Printf("✅ %s OK", bin)
				}
			}
			check("node")
			check("cdk")
			check("go")

			// prueba credenciales AWS (simple)
			var out bytes.Buffer
			ex := exec.Command("aws", "sts", "get-caller-identity")
			ex.Stdout = &out
			if err := ex.Run(); err != nil {
				log.Printf("❌ AWS creds no válidas o AWS CLI no instalado")
			} else {
				log.Printf("✅ AWS creds OK: %s", out.String())
			}
		},
	}

	// Registrar comandos
	root.AddCommand(initCmd, validateCmd, synthCmd, deployCmd, diffCmd, doctorCmd, cdkAppCmd)

	// Ejecutar CLI
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
