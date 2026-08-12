// Command nats runs the NATS server embedded with chaos.plus. It provides a
// self-contained default for desktop installs while accepting a native NATS
// configuration file for clustered production deployments.
package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

const startupTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	opts, err := loadOptions()
	if err != nil {
		return err
	}

	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return fmt.Errorf("configure embedded NATS: %w", err)
	}
	ns.ConfigureLogger()
	go ns.Start()
	if !ns.ReadyForConnections(startupTimeout) {
		ns.Shutdown()
		return errors.New("embedded NATS did not become ready within 10s")
	}

	log.Printf("chaos.plus NATS ready at %s", ns.ClientURL())
	if addr := ns.MonitorAddr(); addr != nil {
		log.Printf("NATS monitoring ready at http://%s/healthz", addr)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	signal.Stop(stop)

	log.Print("shutting down chaos.plus NATS")
	ns.Shutdown()
	ns.WaitForShutdown()
	return nil
}

func loadOptions() (*natsserver.Options, error) {
	if configFile := strings.TrimSpace(os.Getenv("CONTROL_NATS_CONFIG")); configFile != "" {
		opts, err := natsserver.ProcessConfigFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("load CONTROL_NATS_CONFIG: %w", err)
		}
		opts.NoSigs = true
		return opts, nil
	}

	port, err := envInt("CONTROL_NATS_PORT", 4222)
	if err != nil {
		return nil, err
	}
	monitorPort, err := envInt("CONTROL_NATS_MONITOR_PORT", 8222)
	if err != nil {
		return nil, err
	}
	jetStream, err := envBool("CONTROL_NATS_JETSTREAM", true)
	if err != nil {
		return nil, err
	}

	host := envOr("CONTROL_NATS_HOST", "127.0.0.1")
	token := strings.TrimSpace(os.Getenv("CONTROL_NATS_TOKEN"))
	if !isLoopbackHost(host) && token == "" {
		return nil, errors.New("CONTROL_NATS_TOKEN is required when CONTROL_NATS_HOST is not loopback")
	}

	storeDir := strings.TrimSpace(os.Getenv("CONTROL_NATS_STORE_DIR"))
	if storeDir == "" {
		storeDir, err = defaultStoreDir()
		if err != nil {
			return nil, err
		}
	}

	certFile := strings.TrimSpace(os.Getenv("CONTROL_NATS_TLS_CERT"))
	keyFile := strings.TrimSpace(os.Getenv("CONTROL_NATS_TLS_KEY"))
	if (certFile == "") != (keyFile == "") {
		return nil, errors.New("CONTROL_NATS_TLS_CERT and CONTROL_NATS_TLS_KEY must be set together")
	}
	tlsVerify, err := envBool("CONTROL_NATS_TLS_VERIFY", false)
	if err != nil {
		return nil, err
	}

	return &natsserver.Options{
		ServerName:            envOr("CONTROL_NATS_SERVER_NAME", "chaosplus-nats"),
		Host:                  host,
		Port:                  port,
		HTTPHost:              envOr("CONTROL_NATS_MONITOR_HOST", "127.0.0.1"),
		HTTPPort:              monitorPort,
		Authorization:         token,
		JetStream:             jetStream,
		StoreDir:              storeDir,
		MaxConn:               4096,
		MaxPayload:            8 * 1024 * 1024,
		NoSigs:                true,
		Logtime:               true,
		TLS:                   certFile != "",
		TLSCert:               certFile,
		TLSKey:                keyFile,
		TLSCaCert:             strings.TrimSpace(os.Getenv("CONTROL_NATS_TLS_CA")),
		TLSVerify:             tlsVerify,
		DisableShortFirstPing: false,
	}, nil
}

func defaultStoreDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve NATS store directory: %w", err)
	}
	return filepath.Join(dir, "chaosplus", "nats"), nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < -1 || n > 65535 {
		return 0, fmt.Errorf("%s must be an integer between -1 and 65535", key)
	}
	return n, nil
}

func envBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return b, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
