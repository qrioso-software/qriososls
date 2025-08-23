//go:generate go doc -all
package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aws/jsii-runtime-go"
	"github.com/qrioso-software/qriososls/internal/assets"
	"github.com/qrioso-software/qriososls/internal/config"
	"github.com/qrioso-software/qriososls/internal/engine"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Application constants
const (
	defaultConfigPath  = "qrioso-sls.yml" // Default configuration file path
	defaultServiceName = "qrioso-example" // Default service name
	defaultStage       = "dev"            // Default deployment stage
	defaultRegion      = "us-east-1"      // Default AWS region
	buildDir           = "build"          // Build directory for artifacts
	cdkOutDir          = "cdk.out"        // CDK output directory for cloud assembly
)

var version = "dev"
var commit = "none"
var date = "unknown"

// App represents the main application structure holding configuration and state
type App struct {
	configPath      string // Path to the configuration file
	awsProfile      string // AWS profile to use for deployment
	requireApproval string // CDK require-approval setting
	service         string // Service name for init command
	stage           string // Stage name for init command
	region          string // AWS region for init command
	RootPath        string // Root directory of the project
}

// main is the application entry point
// Initializes jsii runtime and runs the application
func main() {
	defer jsii.Close()

	app := &App{}
	var err error

	app.RootPath, err = os.Getwd()
	if err != nil {
		log.Printf("Error getting project root: %v", err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

// Run initializes and executes the root command
// Returns: error if command execution fails
func (a *App) Run() error {
	rootCmd := a.setupRootCommand()
	return rootCmd.Execute()
}

// setupRootCommand configures the main CLI command with all subcommands
// Returns: *cobra.Command - the configured root command
func (a *App) setupRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "qriosls",
		Short: "Qrioso Sls: YAML -> AWS CDK (Go)",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.setupViper()
		},
	}

	// Global flags available for all commands
	root.PersistentFlags().StringVarP(&a.configPath, "config", "c", defaultConfigPath, "Configuration file path")
	root.PersistentFlags().StringVar(&a.awsProfile, "profile", "", "AWS profile name")
	root.PersistentFlags().StringVar(&a.requireApproval, "require-approval", "", "CDK approval level: never|any-change|broadening")

	// Register all subcommands
	root.AddCommand(
		a.initCommand(),
		a.validateCommand(),
		a.synthCommand(),
		a.deployCommand(),
		a.diffCommand(),
		a.doctorCommand(),
		a.cdkAppCommand(),
		a.versionCommand(),
		a.localCommand(),
	)

	return root
}

// setupViper configures the Viper configuration manager
// Returns: error if configuration file exists but cannot be read
func (a *App) setupViper() error {
	viper.SetConfigName(strings.TrimSuffix(filepath.Base(a.configPath), filepath.Ext(a.configPath)))
	viper.SetConfigType("yaml")
	viper.AddConfigPath(filepath.Dir(a.configPath))

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config: %w", err)
		}
	}

	return nil
}

// initCommand creates the 'init' subcommand for project initialization
// Returns: *cobra.Command - configured init command
func (a *App) initCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project with example serverless.yml",
		RunE:  a.runInit,
	}

	cmd.Flags().StringVar(&a.service, "service", defaultServiceName, "Service name")
	cmd.Flags().StringVar(&a.stage, "stage", defaultStage, "Deployment stage (dev|stg|prod)")
	cmd.Flags().StringVar(&a.region, "region", defaultRegion, "AWS region")

	return cmd
}

// runInit executes the init command logic
// Input: cmd - the command instance, args - command arguments
// Returns: error if template creation or file operations fail
// Output: Creates configuration file and build directory
func (a *App) runInit(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(a.configPath); err == nil {
		return fmt.Errorf("file %s already exists in directory", a.configPath)
	}

	file, err := assets.Templates.ReadFile("templates/qrioso-sls.tmpl.yml")
	if err != nil {
		return fmt.Errorf("error reading template: %w", err)
	}

	t := template.Must(template.New("srv").Parse(string(file)))
	f, err := os.Create(a.configPath)
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}
	defer f.Close()

	data := struct {
		Service string
		Stage   string
		Region  string
	}{a.service, a.stage, a.region}

	if err := t.Execute(f, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("error creating build directory: %w", err)
	}

	log.Printf("✅ Created %s and directory %s/", a.configPath, buildDir)
	return nil
}

// validateCommand creates the 'validate' subcommand for configuration validation
// Returns: *cobra.Command - configured validate command
func (a *App) validateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration file",
		RunE:  a.runValidate,
	}
}

// runValidate executes configuration validation
// Input: cmd - the command instance, args - command arguments
// Returns: error if configuration is invalid or cannot be loaded
// Output: Validation success/failure message
func (a *App) runValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	log.Println("✅ Configuration valid")
	return nil
}

// cdkAppCommand creates the hidden 'cdkapp' command used internally by CDK
// Returns: *cobra.Command - configured cdkapp command
func (a *App) cdkAppCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "cdkapp",
		Hidden: true,
		RunE:   a.runCdkApp,
	}
}

// runCdkApp executes the CDK application synthesis
// Input: cmd - the command instance, args - command arguments
// Returns: error if configuration validation or synthesis fails
// Output: Generates cloud assembly in specified output directory
func (a *App) runCdkApp(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	outdir := os.Getenv("CDK_OUTDIR")
	return engine.Synth(cfg, outdir)
}

// synthCommand creates the 'synth' subcommand for CDK synthesis
// Returns: *cobra.Command - configured synth command
func (a *App) synthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "synth",
		Short: "Generate cdk.out (Cloud Assembly)",
		RunE:  a.runSynth,
	}
}

// runSynth executes CDK synthesis via external CDK CLI
// Input: cmd - the command instance, args - command arguments
// Returns: error if CDK CLI not found or synthesis fails
// Output: Cloud assembly in cdk.out directory
func (a *App) runSynth(cmd *cobra.Command, args []string) error {
	if _, err := a.checkCdkInstalled(); err != nil {
		return err
	}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	ex := exec.Command("cdk", "synth", "--output", cdkOutDir)
	ex.Env = a.prepareCdkEnvironment()
	ex.Stdout = os.Stdout
	ex.Stderr = os.Stderr

	if err := ex.Run(); err != nil {
		return fmt.Errorf("error in cdk synth: %w", err)
	}

	log.Printf("✅ Synthesis complete in %s/", cdkOutDir)
	return nil
}

// deployCommand creates the 'deploy' subcommand for infrastructure deployment
// Returns: *cobra.Command - configured deploy command
func (a *App) deployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy using CDK CLI",
		RunE:  a.runDeploy,
	}

	return cmd
}

// runDeploy executes CDK deployment via external CDK CLI
// Input: cmd - the command instance, args - command arguments
// Returns: error if deployment fails or prerequisites not met
// Output: Deploys AWS infrastructure resources
func (a *App) runDeploy(cmd *cobra.Command, args []string) error {
	if _, err := a.checkCdkInstalled(); err != nil {
		return err
	}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	cmdArgs := []string{"deploy"}
	if a.requireApproval != "" {
		cmdArgs = append(cmdArgs, "--require-approval", a.requireApproval)
	}
	if a.awsProfile != "" {
		cmdArgs = append(cmdArgs, "--profile", a.awsProfile)
	}

	ex := exec.Command("cdk", cmdArgs...)
	ex.Env = a.prepareCdkEnvironment()
	ex.Stdout = os.Stdout
	ex.Stderr = os.Stderr

	log.Printf("🚀 Executing: cdk %s", strings.Join(cmdArgs, " "))
	return ex.Run()
}

// diffCommand creates the 'diff' subcommand for infrastructure changes comparison
// Returns: *cobra.Command - configured diff command
func (a *App) diffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Compare changes with CDK CLI",
		RunE:  a.runDiff,
	}
}

// runDiff executes CDK diff to show infrastructure changes
// Input: cmd - the command instance, args - command arguments
// Returns: error if diff execution fails
// Output: Displays infrastructure changes between current and proposed state
func (a *App) runDiff(cmd *cobra.Command, args []string) error {
	if _, err := a.checkCdkInstalled(); err != nil {
		return err
	}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	ex := exec.Command("cdk", "diff")
	ex.Env = a.prepareCdkEnvironment()
	ex.Stdout = os.Stdout
	ex.Stderr = os.Stderr

	return ex.Run()
}

// doctorCommand creates the 'doctor' subcommand for environment verification
// Returns: *cobra.Command - configured doctor command
func (a *App) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify environment requirements",
		Run:   a.runDoctor,
	}
}

// runDoctor checks all required dependencies and environment setup
// Input: cmd - the command instance, args - command arguments
// Output: Diagnostic information about required tools and AWS configuration
func (a *App) runDoctor(cmd *cobra.Command, args []string) {
	checks := []struct {
		name  string
		check func() error
	}{
		{"Node.js", a.checkNode},
		{"CDK CLI", a.checkCdk},
		{"Go", a.checkGo},
		{"AWS Credentials", a.checkAwsCredentials},
	}

	for _, check := range checks {
		if err := check.check(); err != nil {
			log.Printf("❌ %s: %v", check.name, err)
		} else {
			log.Printf("✅ %s OK", check.name)
		}
	}
}

// versionCommand creates the 'version' subcommand for version information
// Returns: *cobra.Command - configured version command
func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("qriosls %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}

func (a *App) localCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "local",
		Short: "Run locally with hot reload",
		RunE:  a.runLocal,
	}
}

func (a *App) runLocal(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	cfg.RootPath = a.RootPath
	runner, err := engine.NewLocalRunner(cfg)
	if err != nil {
		return fmt.Errorf("error creating local runner: %w", err)
	}

	defer runner.Stop()
	return runner.Start()
}

// HELPER METHODS

// checkCdkInstalled verifies if CDK CLI is available in PATH
// Returns: (string, error) - path to CDK executable if found, error otherwise
func (a *App) checkCdkInstalled() (string, error) {
	return exec.LookPath("cdk")
}

// prepareCdkEnvironment prepares environment variables for CDK execution
// Returns: []string - environment variables array with CDK_APP configured
func (a *App) prepareCdkEnvironment() []string {
	env := os.Environ()
	appCommand := fmt.Sprintf("qriosls cdkapp --config %s", a.configPath)
	return append(env, "CDK_APP="+appCommand)
}

// checkNode verifies if Node.js is installed and available
// Returns: error if Node.js is not found in PATH
func (a *App) checkNode() error {
	_, err := exec.LookPath("node")
	return err
}

// checkCdk verifies if AWS CDK CLI is installed and available
// Returns: error if CDK is not found in PATH
func (a *App) checkCdk() error {
	_, err := exec.LookPath("cdk")
	return err
}

// checkGo verifies if Go programming language is installed
// Returns: error if Go is not found in PATH
func (a *App) checkGo() error {
	_, err := exec.LookPath("go")
	return err
}

// checkAwsCredentials verifies if AWS credentials are properly configured
// Returns: error if AWS credentials are invalid or AWS CLI not installed
func (a *App) checkAwsCredentials() error {
	var out bytes.Buffer
	cmd := exec.Command("aws", "sts", "get-caller-identity")
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("AWS credentials invalid or AWS CLI not installed")
	}

	return nil
}
