package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	C "github.com/compozed/deployadactyl/constants"
	"github.com/compozed/deployadactyl/creator"
	"github.com/compozed/deployadactyl/eventmanager/handlers/envvar"
	"github.com/compozed/deployadactyl/eventmanager/handlers/healthchecker"
	"github.com/compozed/deployadactyl/logger"
	"github.com/op/go-logging"
)

const (
	defaultConfigFilePath = "./config.yml"
	defaultLogLevel       = "DEBUG"
	logLevelEnvVarName    = "DEPLOYADACTYL_LOGLEVEL"
)

func main() {
	var (
		config               = flag.String("config", defaultConfigFilePath, "location of the config file")
		envVarHandlerEnabled = flag.Bool("env", false, "enable environment variable handling")
		healthCheckEnabled   = flag.Bool("health-check", false, "health checker to check endpoints during a deployment")
	)
	flag.Parse()

	level := os.Getenv(logLevelEnvVarName)
	if level == "" {
		level = defaultLogLevel
	}

	logLevel, err := logging.LogLevel(level)
	if err != nil {
		log.Fatal(err)
	}

	log := logger.DefaultLogger(os.Stdout, logLevel, "deployadactyl")
	log.Infof("log level : %s", level)

	c, err := creator.Custom(level, *config)
	if err != nil {
		log.Fatal(err)
	}

	em := c.CreateEventManager()

	if *envVarHandlerEnabled {
		envVarHandler := envvar.Envvarhandler{Logger: c.CreateLogger(), FileSystem: c.CreateFileSystem()}
		log.Infof("adding environment variable event handler")
		em.AddHandler(envVarHandler, C.DeployStartEvent)
	}

	if *healthCheckEnabled {
		healthHandler := healthchecker.HealthChecker{}
		log.Infof("health check handler registered")
		em.AddHandler(healthHandler, C.DeployStartEvent)
	}

	l := c.CreateListener()
	deploy := c.CreateControllerHandler()

	log.Infof("Listening on Port %d", c.CreateConfig().Port)

	err = http.Serve(l, deploy)
	if err != nil {
		log.Fatal(err)
	}
}
