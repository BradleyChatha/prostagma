package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const CACHE_DIR = "/tmp/prostagma_cache/"

var g_fileCache map[string]string
var g_triggerCounts map[string]int

type GetCache struct {
	Secret string `json:"secret"`
	Url    string `json:"url"`
}
type SetCache = GetCache
type SetCacheS3 = GetCache

type GetTrigger struct {
	Secret  string `json:"secret"`
	Trigger string `json:"trigger"`
}
type SetTrigger = GetTrigger

type TriggerResult struct {
	Trigger string `json:"trigger"`
	Count   int    `json:"count"`
}

func serverMain() {
	httpHost := os.Getenv("PROSTAGMA_HOST")
	cleanCache()
	g_fileCache = make(map[string]string)
	g_triggerCounts = make(map[string]int)
	serverHttpMain(httpHost)
}

func cleanCache() {
	if _, err := os.Stat(CACHE_DIR); !os.IsNotExist(err) {
		g_logger.Info("Cleaning cache")
		os.RemoveAll(CACHE_DIR)
	}
	err := os.Mkdir(CACHE_DIR, os.ModePerm)
	if err != nil {
		g_logger.Fatal("Could not create cache directory", zap.Error(err))
	}
}

func serverHttpMain(addr string) {
	r := mux.NewRouter()
	r.Path("/cache").HandlerFunc(serveDownloadedFile).Methods("GET")
	r.Path("/cache").HandlerFunc(onCacheFile).Methods("POST")
	r.Path("/cache/s3").HandlerFunc(onCacheFileS3).Methods("POST")
	r.Path("/trigger").HandlerFunc(serveTriggerCount).Methods("GET")
	r.Path("/trigger").HandlerFunc(onIncrementTrigger).Methods("POST")
	r.Handle("/metrics", promhttp.Handler())
	g_logger.Info("Listening", zap.String("addr", addr))
	g_logger.Error("Server error", zap.Error(http.ListenAndServe(addr, r)))
}

func serveDownloadedFile(w http.ResponseWriter, r *http.Request) {
	var message GetCache

	g_logger.Info("Client is asking for a cached file", zap.String("ip", r.RemoteAddr))

	d := json.NewDecoder(r.Body)
	err := d.Decode(&message)
	defer r.Body.Close()

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		g_logger.Info("Client provided a bad request body", zap.String("ip", r.RemoteAddr), zap.Error(err))
		return
	}

	if message.Secret != os.Getenv("PROSTAGMA_SECRET") {
		w.WriteHeader(http.StatusForbidden)
		g_logger.Info("Client provided a bad secret", zap.String("ip", r.RemoteAddr))
		return
	}

	if path, ok := g_fileCache[message.Url]; ok {
		g_logger.Info("Serving cached file", zap.String("ip", r.RemoteAddr), zap.String("path", path), zap.String("url", message.Url))
		http.ServeFile(w, r, path)
	} else {
		g_logger.Info("Client asked for uncached file", zap.String("ip", r.RemoteAddr), zap.String("url", message.Url))
		w.WriteHeader(http.StatusNotFound)
	}
}

func onCacheFile(w http.ResponseWriter, r *http.Request) {
	var message SetCache

	g_logger.Info("Client is asking us to download and cache a file", zap.String("ip", r.RemoteAddr))

	d := json.NewDecoder(r.Body)
	err := d.Decode(&message)
	defer r.Body.Close()

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		g_logger.Info("Client provided a bad request body", zap.String("ip", r.RemoteAddr), zap.Error(err))
		return
	}

	if message.Secret != os.Getenv("PROSTAGMA_SECRET") {
		w.WriteHeader(http.StatusForbidden)
		g_logger.Info("Client provided a bad secret", zap.String("ip", r.RemoteAddr))
		return
	}

	g_logger.Info("Attempting to download and cache file", zap.String("ip", r.RemoteAddr), zap.String("url", message.Url))

	resp, err := http.Get(message.Url)
	if err != nil {
		g_logger.Error("Failed to download from URL", zap.String("ip", r.RemoteAddr), zap.Error(err))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer resp.Body.Close()

	f, err := os.CreateTemp(CACHE_DIR, "*")
	if err != nil {
		g_logger.Error("Failed to create temporary file", zap.String("ip", r.RemoteAddr), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		g_logger.Error("Failed to copy data into file", zap.String("ip", r.RemoteAddr), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	g_fileCache[message.Url] = f.Name()
	g_logger.Info("Cached file", zap.String("ip", r.RemoteAddr), zap.String("url", message.Url), zap.String("path", f.Name()))
	w.WriteHeader(http.StatusOK)
}

func onCacheFileS3(w http.ResponseWriter, r *http.Request) {
	var message SetCacheS3

	g_logger.Info("Client is asking us to download and cache a file from S3", zap.String("ip", r.RemoteAddr))

	d := json.NewDecoder(r.Body)
	err := d.Decode(&message)
	defer r.Body.Close()

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		g_logger.Info("Client provided a bad request body", zap.String("ip", r.RemoteAddr), zap.Error(err))
		return
	}

	if message.Secret != os.Getenv("PROSTAGMA_SECRET") {
		w.WriteHeader(http.StatusForbidden)
		g_logger.Info("Client provided a bad secret", zap.String("ip", r.RemoteAddr))
		return
	}

	f, err := os.CreateTemp(CACHE_DIR, "*")
	if err != nil {
		g_logger.Error("Failed to create temporary file", zap.String("ip", r.RemoteAddr), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	cmd := exec.Command("/usr/local/bin/aws", "s3", "cp", message.Url, f.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		g_logger.Error("Failed to invoke AWS", zap.String("ip", r.RemoteAddr), zap.String("url", message.Url), zap.Error(err), zap.String("output", string(out)))
		w.WriteHeader(http.StatusInternalServerError)
		return
	} else if cmd.ProcessState.ExitCode() != 0 {
		g_logger.Error("Failed to download file from S3", zap.String("ip", r.RemoteAddr), zap.String("url", message.Url), zap.String("output", string(out)))
		w.WriteHeader(http.StatusNotFound)
		return
	}

	g_fileCache[message.Url] = f.Name()
	g_logger.Info("Cached file", zap.String("ip", r.RemoteAddr), zap.String("url", message.Url), zap.String("path", f.Name()))
	w.WriteHeader(http.StatusOK)
}

func serveTriggerCount(w http.ResponseWriter, r *http.Request) {
	var message GetTrigger

	g_logger.Info("Client is asking for a trigger count", zap.String("ip", r.RemoteAddr))

	d := json.NewDecoder(r.Body)
	err := d.Decode(&message)
	defer r.Body.Close()

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		g_logger.Info("Client provided a bad request body", zap.String("ip", r.RemoteAddr), zap.Error(err))
		return
	}

	if message.Secret != os.Getenv("PROSTAGMA_SECRET") {
		w.WriteHeader(http.StatusForbidden)
		g_logger.Info("Client provided a bad secret", zap.String("ip", r.RemoteAddr))
		return
	}

	count := 0
	if tcount, ok := g_triggerCounts[message.Trigger]; ok {
		count = tcount
	} else {
		g_triggerCounts[message.Trigger] = 0
	}

	g_logger.Info("Providing trigger count",
		zap.String("ip", r.RemoteAddr),
		zap.String("trigger", message.Trigger),
		zap.Int("count", count),
	)

	result := TriggerResult{
		Trigger: message.Trigger,
		Count:   count,
	}

	w.WriteHeader(http.StatusOK)
	jw := json.NewEncoder(w)
	jw.Encode(result)
}

func onIncrementTrigger(w http.ResponseWriter, r *http.Request) {
	var message SetTrigger

	g_logger.Info("Client is incrementing a trigger count", zap.String("ip", r.RemoteAddr))

	d := json.NewDecoder(r.Body)
	err := d.Decode(&message)
	defer r.Body.Close()

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		g_logger.Info("Client provided a bad request body", zap.String("ip", r.RemoteAddr), zap.Error(err))
		return
	}

	if message.Secret != os.Getenv("PROSTAGMA_SECRET") {
		w.WriteHeader(http.StatusForbidden)
		g_logger.Info("Client provided a bad secret", zap.String("ip", r.RemoteAddr))
		return
	}

	if tcount, ok := g_triggerCounts[message.Trigger]; ok {
		g_triggerCounts[message.Trigger] = tcount + 1
	} else {
		g_triggerCounts[message.Trigger] = 1
	}

	w.WriteHeader(http.StatusOK)
}
