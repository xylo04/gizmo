package cmdlets

import (
	"context"
	nhttp "net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/spf13/cobra"

	"github.com/bestrobotics/gizmo/internal/stats"
	"github.com/bestrobotics/gizmo/pkg/gamepad"
	"github.com/bestrobotics/gizmo/pkg/http"
	"github.com/bestrobotics/gizmo/pkg/mqttpusher"
	"github.com/bestrobotics/gizmo/pkg/mqttserver"
	"github.com/bestrobotics/gizmo/pkg/tlm/simple"
)

var (
	fieldPracticeCmd = &cobra.Command{
		Use:   "practice",
		Short: "practice <team>",
		Long:  fieldPracticeCmdLongDocs,
		Run:   fieldPracticeCmdRun,
		Args:  cobra.ExactArgs(1),
	}

	fieldPracticeCmdLongDocs = `Practice sets up a field server that only has one quadrant called "PRACTICE" and that only expects one gamepad to be available.  This enables a team to practice without running an entire field.`
)

func init() {
	fieldCmd.AddCommand(fieldPracticeCmd)
}

func fieldPracticeCmdRun(c *cobra.Command, args []string) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	ll := os.Getenv("LOG_LEVEL")
	if ll == "" {
		ll = "INFO"
	}
	appLogger := hclog.New(&hclog.LoggerOptions{
		Name:  "field",
		Level: hclog.LevelFromString(ll),
	})
	appLogger.Info("Log level", "level", appLogger.GetLevel())
	wg := new(sync.WaitGroup)

	prometheusRegistry, prometheusMetrics := stats.NewListener(appLogger)
	appLogger.Debug("Stats listeners created")

	jsc := gamepad.NewJSController(gamepad.WithLogger(appLogger))

	if err := jsc.BindController("field1:practice", 0); err != nil {
		appLogger.Error("Error initializing gamepad", "error", err)
		os.Exit(1)
	}

	tlm := simple.New(simple.WithLogger(appLogger), simple.WithStartupWG(wg))

	m, err := mqttserver.NewServer(
		mqttserver.WithLogger(appLogger),
		mqttserver.WithStartupWG(wg),
	)
	if err != nil {
		appLogger.Error("Error during mqtt initialization", "error", err)
		os.Exit(1)
	}

	p, err := mqttpusher.New(
		mqttpusher.WithLogger(appLogger),
		mqttpusher.WithJSController(&jsc),
		mqttpusher.WithMQTTServer("mqtt://127.0.0.1:1883"),
		mqttpusher.WithStartupWG(wg),
	)
	if err != nil {
		appLogger.Error("Error during mqtt pusher initialization", "error", err)
		quit <- syscall.SIGINT
	}

	w, err := http.NewServer(
		http.WithLogger(appLogger),
		http.WithJSController(&jsc),
		http.WithTeamLocationMapper(tlm),
		http.WithPrometheusRegistry(prometheusRegistry),
		http.WithQuads([]string{"field1:practice"}),
		http.WithStartupWG(wg),
	)

	go func() {
		if err := m.Serve(":1883"); err != nil {
			appLogger.Error("Error initializing", "error", err)
			quit <- syscall.SIGINT
		}
	}()

	go func() {
		if err := w.Serve(":8080"); err != nil && err != nhttp.ErrServerClosed {
			appLogger.Error("Error initializing", "error", err)
			quit <- syscall.SIGINT
		}
	}()

	go func() {
		if err := p.Connect(); err != nil {
			appLogger.Error("Error initializing", "error", err)
			quit <- syscall.SIGINT
			return
		}
		p.StartLocationPusher()
		p.StartControlPusher()
	}()

	go func() {
		if err := stats.MqttListen("mqtt://127.0.0.1:1883", prometheusMetrics, wg); err != nil {
			appLogger.Error("Error initializing", "error", err)
			quit <- syscall.SIGINT
		}
	}()

	tNum, err := strconv.Atoi(args[0])
	if err != nil {
		appLogger.Error("Team number must be a number", "error", err)
		quit <- syscall.SIGINT
	}

	tlm.InsertOnDemandMap(map[int]string{tNum: "field1:practice"})
	jsc.BeginAutoRefresh(50)
	tlm.Start()

	wg.Wait()
	appLogger.Info("Startup Complete!")

	<-quit
	appLogger.Info("Shutting down...")
	tlm.Stop()
	p.Stop()
	jsc.StopAutoRefresh()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		appLogger.Error("Error during shutdown", "error", err)
		os.Exit(2)
	}
	if err := m.Shutdown(); err != nil {
		appLogger.Error("Error during shutdown", "error", err)
		os.Exit(2)
	}
}
