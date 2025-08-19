package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/qrioso-software/qriososls/internal/assets"
	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/engine"
	"github.com/spf13/cobra"
)

func main() {
	var cfgPath string
	var awsProfile string
	var requireApproval string
	root := &cobra.Command{
		Use:   "qriosls",
		Short: "Qrioso Sls: YAML -> AWS CDK (Go)",
	}
	service := "qrioso-serverless"
	stage := "dev"
	region := "us-east-1"

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
			fmt.Println("✅ Creado QriosoSls.yml y carpeta build/")
			return nil
		},
	}

	initCmd.Flags().StringVar(&service, "service", service, "Nombre del servicio")
	// initCmd.Flags().StringVar(&stage, "stage", stage, "Stage (dev|stg|prod)")
	// initCmd.Flags().StringVar(&region, "region", region, "Región AWS (ej. us-east-1)")

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
	validateCmd.Flags().StringVarP(&cfgPath, "config", "c", "QriosoSls.yml", "Ruta del YAML")

	synthCmd := &cobra.Command{
		Use:   "synth",
		Short: "Genera cdk.out (Cloud Assembly)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			engine.Synth(cfg)
			fmt.Println("✅ Synth listo en cdk.out/")
			return nil
		},
	}
	synthCmd.Flags().StringVarP(&cfgPath, "config", "c", "QriosoSls.yml", "Ruta del YAML")

	deployCmd := &cobra.Command{
		Use:   "deploy",
		Short: "Despliega usando CDK CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			// require cdk CLI
			if _, err := exec.LookPath("cdk"); err != nil {
				return fmt.Errorf("cdk CLI no encontrado. Instala con: npm i -g aws-cdk")
			}
			// synth first
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			engine.Synth(cfg)

			app := fmt.Sprintf("%s-%s", cfg.Service, cfg.Stage)
			log.Println(app)
			cmdArgs := []string{"deploy", "--app", filepath.Join(".", "cdk.out")}
			if awsProfile != "" {
				cmdArgs = append(cmdArgs, "--profile", awsProfile)
			}
			if requireApproval != "" {
				cmdArgs = append(cmdArgs, "--require-approval", requireApproval)
			}
			ex := exec.Command("cdk", cmdArgs...)
			ex.Stdout = os.Stdout
			ex.Stderr = os.Stderr
			ex.Env = os.Environ()

			fmt.Println("🚀 Ejecutando:", "cdk", cmdArgs)
			return ex.Run()
		},
	}
	deployCmd.Flags().StringVarP(&cfgPath, "config", "c", "QriosoSls.yml", "Ruta del YAML")
	deployCmd.Flags().StringVar(&awsProfile, "profile", "", "AWS profile")
	deployCmd.Flags().StringVar(&requireApproval, "require-approval", "", "never|any-change|broadening")

	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff con CDK CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath("cdk"); err != nil {
				return fmt.Errorf("cdk CLI no encontrado. Instala con: npm i -g aws-cdk")
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			engine.Synth(cfg)

			ex := exec.Command("cdk", "diff", "--app", filepath.Join(".", "cdk.out"))
			ex.Stdout = os.Stdout
			ex.Stderr = os.Stderr
			return ex.Run()
		},
	}
	diffCmd.Flags().StringVarP(&cfgPath, "config", "c", "QriosoSls.yml", "Ruta del YAML")

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

	root.AddCommand(initCmd, validateCmd, synthCmd, deployCmd, diffCmd, doctorCmd)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
