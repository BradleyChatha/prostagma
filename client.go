package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var g_lastTriggerCount int

type DownloadStep struct {
	Cache bool
	Url   string
	Dest  string
}

type BuildScript struct {
	Steps []yaml.Node
}

func clientMain() {
	updateTriggerCount()

	for {
		time.Sleep(time.Second * 5)

		prevCount := g_lastTriggerCount
		updateTriggerCount()
		if prevCount == g_lastTriggerCount {
			continue
		} else if g_lastTriggerCount < prevCount {
			// server restarted
			continue
		}

		g_logger.Info("Triggered")
		runBuildScript()
	}
}

func askServerToDownloadFile(url string) error {
	var data SetCache
	data.Secret = os.Getenv("PROSTAGMA_SECRET")
	data.Url = url

	_, err := POST("/cache", data)
	if err != nil {
		return err
	}

	return nil
}

func askServerToDownloadFileS3(url string) error {
	var data SetCache
	data.Secret = os.Getenv("PROSTAGMA_SECRET")
	data.Url = url

	_, err := POST("/cache/s3", data)
	if err != nil {
		return err
	}

	return nil
}

func updateTriggerCount() {
	var data GetTrigger
	data.Trigger = os.Getenv("PROSTAGMA_TRIGGER")
	data.Secret = os.Getenv("PROSTAGMA_SECRET")

	r, err := GETWithBody("/trigger", data)
	if err != nil {
		return
	}
	defer r.Body.Close()

	var message TriggerResult
	jr := json.NewDecoder(r.Body)
	err = jr.Decode(&message)

	if err != nil {
		g_logger.Error("Could not decode body", zap.Error(err))
		return
	}

	g_lastTriggerCount = message.Count
	g_logger.Debug("Updated trigger conut", zap.Int("count", g_lastTriggerCount))
}

func downloadCachedFile(url string, file string) error {
	var data GetCache
	data.Url = url
	data.Secret = os.Getenv("PROSTAGMA_SECRET")

	g_logger.Info("Downloading cached file from server", zap.String("url", url), zap.String("file", file))

	f, err := os.Create(file)
	if err != nil {
		g_logger.Error("Could not create file", zap.Error(err))
		return err
	}
	defer f.Close()

	r, err := GETWithBody("/cache", data)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	_, err = io.Copy(f, r.Body)
	if err != nil {
		g_logger.Error("Could not download entire file", zap.Error(err))
		return err
	}

	return nil
}

func GETWithBody(path string, data interface{}) (*http.Response, error) {
	body, err := json.Marshal(data)
	if err != nil {
		g_logger.Fatal("Could not create JSON body?", zap.Error(err))
	}

	req, err := http.NewRequest("GET", os.Getenv("PROSTAGMA_HOST")+path, bytes.NewBuffer(body))
	if err != nil {
		g_logger.Error("Could not create HTTP request", zap.Error(err))
		return nil, err
	}

	client := http.Client{}
	r, err := client.Do(req)
	if err != nil {
		g_logger.Error("Error sending request", zap.Error(err))
		return nil, err
	} else if r.StatusCode != http.StatusOK {
		g_logger.Error("Server did not return status code 200", zap.String("status", r.Status))
		return nil, errors.New("server did not return status code 200")
	}

	return r, nil
}

func POST(path string, data interface{}) (*http.Response, error) {
	j, _ := json.Marshal(data)
	jr := bytes.NewReader(j)
	r, err := http.Post(os.Getenv("PROSTAGMA_HOST")+path, "application/json", jr)
	if err != nil {
		g_logger.Error("Could not send POST request", zap.Error(err))
		return nil, err
	} else if r.StatusCode != http.StatusOK {
		g_logger.Error("Server did not send status code 200", zap.String("status", r.Status))
		return nil, errors.New("server did not return status code 200")
	}

	return r, nil
}

func runBuildScript() {
	var script BuildScript
	file, err := os.ReadFile(os.Getenv("PROSTAGMA_SCRIPT"))
	if err != nil {
		g_logger.Error("Could not load build script", zap.Error(err))
		return
	}

	err = yaml.Unmarshal(file, &script)
	if err != nil {
		g_logger.Error("Could not load build script", zap.Error(err))
		return
	}

	for _, node := range script.Steps {
		if node.Kind != yaml.MappingNode {
			g_logger.Error("Expected a map as the element of the `steps` array")
			return
		}

		var nodeMap map[string]yaml.Node
		err := node.Decode(&nodeMap)
		if err != nil {
			g_logger.Error("Error converting map into a... map", zap.Error(err))
			return
		}

		for name, child := range nodeMap {
			if name == "shell" {
				var shell string
				err := child.Decode(&shell)
				if err != nil {
					g_logger.Error("Error running build script `shell` step", zap.Error(err))
					return
				}

				for _, str := range strings.Split(shell, "\n") {
					cmd := exec.Command(os.Getenv("PROSTAGMA_SHELL"), "-c", str)
					out, err := cmd.CombinedOutput()
					if err != nil {
						g_logger.Error("Error running command", zap.Error(err), zap.String("command", str))
						return
					} else if cmd.ProcessState.ExitCode() != 0 {
						g_logger.Error("Error running command, it returned a non-0 status code", zap.String("command", str), zap.String("output", string(out)))
						return
					}
					g_logger.Info("Ran command", zap.String("command", str), zap.String("output", string(out)))
				}
			} else if name == "download" {
				var download DownloadStep
				err := child.Decode(&download)
				if err != nil {
					g_logger.Error("Error running build script `download` step", zap.Error(err))
					return
				}
				err = doDownloadFile(download.Url, download.Dest, download.Cache, askServerToDownloadFile)
				if err != nil {
					return
				}
			} else if name == "download_s3" {
				var download DownloadStep
				err := child.Decode(&download)
				if err != nil {
					g_logger.Error("Error running build script `download` step", zap.Error(err))
					return
				}
				err = doDownloadFile(download.Url, download.Dest, download.Cache, askServerToDownloadFileS3)
				if err != nil {
					return
				}
			} else {
				g_logger.Error("Unknown step command", zap.String("command", name))
				return
			}
		}
	}
}

func doDownloadFile(url string, dest string, cache bool, downloader func(url string) error) error {
	var err error
	if !cache {
		err = downloader(url)
		if err != nil {
			g_logger.Error("Server could not download file", zap.Error(err))
			return err
		}
	}

	err = downloadCachedFile(url, dest)
	if err != nil {
		err = downloader(url)
		if err != nil {
			g_logger.Error("Server could not download file", zap.Error(err))
			return err
		}
		err = downloadCachedFile(url, dest)
		if err != nil {
			g_logger.Error("Could not download file", zap.Error(err))
			return err
		}
	}

	return nil
}
