package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	log "github.com/Sirupsen/logrus"
)

// port to eBPF?

type containerClient interface {
	Start()
}

// Plugin is the internal data structure
type Plugin struct {
	reporter *Reporter

	clients []containerClient
}


var pluginNameTimestamps map[string]string

func main() {
	// We put the socket in a sub-directory to have more control on the permissions
	const socketPath = "/var/run/scope/plugins/plugin-id/plugin-id.sock"

	// Handle the exit signal
	setupSignals(socketPath)

	listener, err := setupSocket(socketPath)
	if err != nil {
		log.Fatalf("Failed to setup socket: %v", err)
	}

	plugin, err := NewPlugin()
	if err != nil {
		log.Fatalf("Failed to create a plugin: %v", err)
	}

	trafficControlServeMux := http.NewServeMux()

	// Report request handler
	reportHandler := http.HandlerFunc(plugin.report)
	trafficControlServeMux.Handle("/report", reportHandler)

	// Control request handler
	controlHandler := http.HandlerFunc(plugin.control)
	trafficControlServeMux.Handle("/control", controlHandler)

	log.Println("Listening...")
	if err = http.Serve(listener, trafficControlServeMux); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func setupSignals(socketPath string) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupt
		os.RemoveAll(filepath.Dir(socketPath))
		os.Exit(0)
	}()
}

func setupSocket(socketPath string) (net.Listener, error) {
	os.RemoveAll(filepath.Dir(socketPath))
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create directory %q: %v", filepath.Dir(socketPath), err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %q: %v", socketPath, err)
	}

	log.Printf("Listening on: unix://%s", socketPath)
	return listener, nil
}

// NewPlugin instantiates a new plugin
func NewPlugin() (*Plugin, error) {
	store := NewStore()
	dockerClient, err := NewDockerClient(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create a docker client: %v", err)
	}
	reporter := NewReporter(store)
	plugin := &Plugin{
		reporter: reporter,
		clients: []containerClient{
			dockerClient,
		},
	}
	for _, client := range plugin.clients {
		go client.Start()
	}
	return plugin, nil
}

func (p *Plugin) report(w http.ResponseWriter, r *http.Request) {
	raw, err := p.reporter.RawReport()
	if err != nil {
		msg := fmt.Sprintf("error: failed to get raw report: %v", err)
		log.Print(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(raw)
}

type request struct {
	NodeID  string
	Control string
}

type response struct {
	Error string `json:"error,omitempty"`
}

func (p *Plugin) control(w http.ResponseWriter, r *http.Request) {
	xreq := request{}
	if err := json.NewDecoder(r.Body).Decode(&xreq); err != nil {
		log.Printf("Bad request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	handler, err := p.reporter.GetHandler(xreq.NodeID, xreq.Control)
	if err != nil {
		sendResponse(w, fmt.Errorf("failed to get handler: %v", err))
		return
	}
	if err := handler(); err != nil {
		sendResponse(w, fmt.Errorf("handler failed: %v", err))
		return
	}
	sendResponse(w, nil)
}

func sendResponse(w http.ResponseWriter, err error) {
	res := response{}
	if err != nil {
		res.Error = err.Error()
	}
	raw, err := json.Marshal(res)
	if err != nil {
		log.Printf("Internal server error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(raw)
}
